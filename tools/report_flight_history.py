"""Build the local flight/audio review from Go diagnostics and saved FR24 maps."""
import argparse
import csv
import datetime as dt
import hashlib
import html
import json
import math
from pathlib import Path
import shutil
from urllib.parse import quote


def load(path, default=None):
    return json.loads(path.read_text()) if path.exists() else default


def dump(path, value):
    path.write_text(json.dumps(value, indent=2))


def sha(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def elapsed(seconds):
    return f"{int(seconds // 3600):02}:{int(seconds % 3600 // 60):02}:{seconds % 60:04.1f}"


def distance(a, b):
    la, lb = map(math.radians, (a[0], b[0]))
    dlat = lb - la
    dlon = math.radians(b[1] - a[1])
    x = math.sin(dlat / 2) ** 2 + math.cos(la) * math.cos(lb) * math.sin(dlon / 2) ** 2
    return 6371008.8 * 2 * math.asin(min(1, math.sqrt(x)))


STYLE = """body{font:16px/1.5 system-ui;background:#15252b;color:#dfebed;margin:24px auto;max-width:1450px;padding:0 24px}a{color:#81d6c6}h1,h2{line-height:1.2}.note{border-left:4px solid #e5bd72;padding:12px 18px;background:#203239}table{border-collapse:collapse;width:100%;font-size:14px}th,td{text-align:left;padding:10px;border-bottom:1px solid #3c5158;vertical-align:top}th{background:#243a42}small{color:#b5c5ca}button,select,input{background:#29414a;color:#e7f4f5;border:1px solid #637f89;border-radius:4px;padding:8px}button{cursor:pointer}details{background:#1c3038;padding:12px;margin:12px 0}summary{cursor:pointer}img{width:100%;background:#101820}.ok{color:#87d8af}.bad{color:#efb890}.scroll{overflow:auto}audio{width:100%;margin:16px 0}nav{display:flex;gap:24px;flex-wrap:wrap}svg{width:100%;height:160px}.chart{background:#0f1c22;padding:10px}code{word-break:break-all}"""


def page(title, content):
    return f'<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{html.escape(title)}</title><style>{STYLE}</style><body><h1>{html.escape(title)}</h1>{content}</body></html>'


def diagnosis_panel(root):
    diagnosis = load(root / "diagnosis-summary.json")
    if not diagnosis:
        return ""
    limits = "".join(f"<li>{html.escape(item)}</li>" for item in diagnosis["demonstrated_implementation_limits"])
    example = diagnosis["real_forward_model_example"]
    fits = []
    for label, key in [("Park centre", "park_fit"), ("Desk alternative", "desk_alternative_fit")]:
        fit = example[key]
        fits.append(f"<tr><td>{label}</td><td>{fit['common_clock_correction_s']:+g} s</td><td>{fit['training_source_hz']:.2f} Hz</td><td>{fit['reserved_rmse_hz']:.3f} Hz</td></tr>")
    return f'''<section><h2>What this batch establishes</h2>
<p class="note">Status: {html.escape(diagnosis['status'])}. These findings identify limitations; production recognition and localization have not been fixed by this report.</p>
<ul>{limits}</ul>
<p>The {html.escape(example['aircraft'])} {html.escape(example['type_code'])} trajectory explains a measured curve at both supplied static receiver alternatives, {example['desk_to_park_centre_m']:.0f} m apart. The phone was walking. Neither curve fit establishes its route or a unique GPS location.</p>
<table><thead><tr><th>Static receiver hypothesis</th><th>Clock correction</th><th>Fitted source tone</th><th>Reserved curve RMS error</th></tr></thead><tbody>{''.join(fits)}</tbody></table>
<p>The synthetic illustrations test scoring logic with an explicitly present tone. They are not real-aircraft recognition rates.</p>
<nav><a href="diagnosis-summary.json">Diagnosis and priorities</a><a href="model-diagnosis/diagnosis.json">Controlled counterexamples</a></nav></section>'''


def timeline(passes, duration, strong):
    marks = []
    for event in strong:
        left = 50 + 1300 * event["start_s"] / duration
        width = max(2, 1300 * (event["end_s"] - event["start_s"]) / duration)
        marks.append(f'<rect x="{left:.2f}" y="22" width="{width:.2f}" height="110" fill="#d99565" opacity=".3"/>')
    for p in passes:
        x = 50 + 1300 * p["predicted_audio_cpa_s"] / duration
        y = 132 - min(15000, p["slant_m"]) / 15000 * 110
        color = "#e8cf7b" if p["engine_class"] == "J" else "#80cfcb"
        label = html.escape(f"{p.get('callsign') or p['icao']} · {p['type_code']} · {elapsed(p['predicted_audio_cpa_s'])} · {p['slant_m']/1000:.2f} km")
        marks.append(f'<circle cx="{x:.2f}" cy="{y:.2f}" r="3" fill="{color}"><title>{label}</title></circle>')
    return '<div class="chart"><svg viewBox="0 0 1400 165" role="img" aria-label="Predicted flight time and slant distance"><text x="2" y="20" fill="white">15 km</text><text x="2" y="140" fill="white">0 km</text>' + ''.join(marks) + f'<text x="50" y="160" fill="white">00:00</text><text x="1250" y="160" fill="white">{elapsed(duration)}</text></svg><small>Yellow: predicted jets. Teal: other/unknown aircraft. Shaded intervals: automatic strong tonal proposals. Hover over a point for its identity. These are geometry predictions, not detections.</small></div>'


def build_case(root, folder):
    data = load(folder / "encounters.json")
    recording = data["recording"]
    context_status = recording.get("context_status", "Receiver context and filename time provided for this supervised review")
    passes = data["encounters"]
    comparison = load(folder / "fr24-comparison.json", {"observations": []})
    observations = comparison["observations"]
    ridges = load(folder / "ridge-comparison.json", {"fits": [], "review_curves": [], "common_clock_trials": []})
    manifest = load(folder / "fr24/manifest.json", {"snapshots": []})
    detection = load(Path(recording.get("analysis", "")) / "detection.json", {"events": []})
    strong = [x for x in detection["events"] if x.get("acoustic_evidence", {}).get("level") == "strong"]
    observed_ids = {o["fr24"]["id"] for o in observations}
    matched = {o["geometric_icao"] for o in observations if o["status"] == "consistent_callsign_and_position"}
    rows = []
    epoch = dt.datetime.fromisoformat(recording["start"]).timestamp()
    for p in passes:
        related = [o for o in observations if o.get("geometric_icao") == p["icao"]
                   and abs((o["fr24"].get("timestampMs", 0) / 1000 or o["fr24"]["timestamp"]) - (epoch + p["predicted_audio_cpa_s"] - p["propagation_delay_s"])) < 300]
        agreed = [o for o in related if o["status"] == "consistent_callsign_and_position"]
        row = dict(p)
        row["fr24_ids"] = sorted({o["fr24"]["id"] for o in agreed})
        row["fr24_status"] = ("callsign + position agree" if agreed else "position association only" if related else "not cross-checked in sparse snapshots") if manifest["snapshots"] else "FR24 not acquired for this case"
        row["status"] = "Predicted flyover; acoustic identity unconfirmed"
        rows.append(row)
    fields = ["icao", "callsign", "registration", "type_code", "engine_class", "arrival_cpa_utc", "predicted_audio_cpa_s", "propagation_delay_s", "slant_m", "speed_mps", "gap_from_previous_encounter_s", "gap_from_previous_near_encounter_s", "partial_trajectory", "fr24_ids", "fr24_status", "status"]
    with (folder / "encounters.csv").open("w") as stream:
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)
    audio = folder / "audio.mp3"
    if not audio.exists() and not audio.is_symlink():
        audio.symlink_to(Path(recording["audio"]).resolve())
    accepted = [x for x in ridges.get("training_selected_review_fits", []) if x["ridge_agreement"]]
    maximum = max((x["distinct_nonoverlapping_ridge_agreements"] for x in ridges["common_clock_trials"]), default=0)
    selected_curves = []
    direct_sections = []
    direct_tested=0
    direct_supported=set()
    direct_profiles=0
    for path in sorted(folder.glob("receiver-review-*.json")):
        if path.name.endswith("-inputs.json"):
            continue
        review=load(path);profile=path.stem.removeprefix("receiver-review-")
        direct_profiles+=1;direct_tested+=len(review["matches"])
        entries=[]
        for index,match in enumerate(review["matches"]):
            if match["additional_support"]:
                direct_supported.add(match["track_id"].split("-")[0])
            status="Experimental support; identity remains unconfirmed" if match["additional_support"] else ("Unavailable: " if match.get("validation_status")=="unavailable" else "Rejected: ")+", ".join(match["reasons"])
            fit=match["fit"]
            image=folder/f"receiver-{profile}-{index:03d}.svg"
            preview=f'<button data-start="{match["start_s"]}" data-end="{match["end_s"]}">Listen to predicted window</button><img loading="lazy" src="{image.name}" alt="Measured spectrum and conditional flight prediction">' if image.exists() else '<p>No eligible curve preview is available for this window.</p>'
            entries.append(f'''<details><summary>{elapsed(match['predicted_audio_cpa_s'])} · {html.escape(match.get('type_code') or 'unknown')} · {html.escape(match['track_id'].split('-')[0])} · {html.escape(status)}</summary>
<p>Source tone {fit['f0_hz']:.1f} Hz; training / reserved contrast {fit['training_contrast_db']:.2f} / {fit['heldout_contrast_db']:.2f} dB; margin over competing controls {match['heldout_control_margin_db']:.2f} dB. Position and the recording clock are fixed. This modeled line is not an identified Doppler curve.</p>
<p>Control mode: {html.escape(match.get('control_mode','shifted_audio'))}. Partial window: {match.get('partial_window',False)}. Validation status: {html.escape(match.get('validation_status','legacy'))}.</p>
{preview}</details>''')
        direct_sections.append(f'''<details><summary>{html.escape(profile)} spectrum · {len(review['matches'])} modeled checks (including unavailable windows) · {review['experimental_supported_windows']} experimental support</summary><p>Scored common clock correction: {review['supplied_receiver']['clock_correction_s']:+.2f} s.</p><p>{html.escape(review['note'])}</p>{''.join(entries)}</details>''')
    for curve in sorted(ridges["review_curves"], key=lambda c: not c["fit"]["ridge_agreement"]):
        fit = curve["fit"]
        status = "Shape agrees; aircraft association remains conditional" if fit["ridge_agreement"] else "Shape does not agree"
        selected_curves.append(f'''<details><summary>{elapsed(fit['start_s'])}–{elapsed(fit['end_s'])} · {html.escape(fit['track_id'][:6])} · {html.escape(fit.get('source_evidence_level','strong'))} tier · {status}</summary>
<p>Training / reserved ridge error: {fit['training_rmse_hz']:.2f} / {fit['reserved_rmse_hz']:.2f} Hz; fitted source tone {fit['training_source_hz']:.2f} Hz; clock {fit['common_clock_correction_s']:+.0f} s. This per-event review winner was selected on training error; its clock cannot be combined with a different event's clock.</p>
<button data-start="{max(0,fit['start_s']-10)}" data-end="{fit['end_s']+10}">Listen to this interval</button>
<img loading="lazy" src="ridge-{fit['event']}.svg" alt="Measured spectrogram with observed ridge and flight model"></details>''')
    # Include uncovered proposals explicitly: a lack of a comparable trajectory
    # or enough separate test samples is not a perfect model fit or no-plane label.
    tested = {x["event"] for x in ridges["fits"]}
    for index, event in enumerate(detection["events"], 1):
        if event in strong and index not in tested:
            selected_curves.append(f'''<details><summary>{elapsed(event['start_s'])}–{elapsed(event['end_s'])}: automatic tonal proposal, unverified</summary><p>Insufficient comparable geometry or separate test samples. No aircraft identity assigned.</p><button data-start="{max(0,event['start_s']-10)}" data-end="{event['end_s']+10}">Listen to candidate</button><img loading="lazy" src="proposal-{index}.svg" alt="Measured spectrum and unverified automatic ridge"></details>''')
    jet_plots = []
    for image in sorted(folder.glob("jet-*-low.svg")):
        stem = image.name.removesuffix("-low.svg")
        _, icao, stamp = stem.split("-")
        p = min((p for p in passes if p["icao"] == icao), key=lambda p: abs(p["predicted_audio_cpa_s"]-int(stamp)))
        jet_plots.append(f'''<details><summary>{html.escape(p.get('callsign') or icao)} · {p['type_code']} · predicted {elapsed(p['predicted_audio_cpa_s'])}</summary>
<p>Measured energy around the predicted arrival. No source frequency or synthetic matching curve is asserted.</p><button data-start="{p['review_start_s']}" data-end="{p['review_end_s']}">Listen around predicted passage</button>
<img loading="lazy" src="{stem}-low.svg" alt="Measured 40 to 800 Hz spectrum"><img loading="lazy" src="{stem}-high.svg" alt="Measured 800 to 4000 Hz spectrum"></details>''')
    result = load(folder / "flight-conditioned-location/results/results.json")
    location = ""
    error = None
    followup_tested = 0
    followup_support = 0
    if result and result["candidates"]:
        best = result["candidates"][0]
        error = distance([best["lat"], best["lon"]], data["receiver"][:2])
        evaluation = dict(mode="Known-flight-conditioned single-curve location comparison; not blind",
                          best_candidate=best, distance_from_user_reported_desk_m=error,
                          locations_evaluated=result["locations_evaluated"], ambiguity=result["ambiguity"],
                          coordinate_used_in_search=False, association_previously_used_known_coordinate=True,
                          note="Comparison coordinate loaded after inference. Single-event candidate fits are unresolved; no verified receiver position or jet localization.")
        dump(folder / "flight-conditioned-location/evaluation.json", evaluation)
        location = f'''<h2>Flight-conditioned Texas location experiment</h2><p class="note">Highest-scoring fit: {best['lat']:.6f}, {best['lon']:.6f}, {error:.0f} m from the user-reported desk. {html.escape(result['ambiguity'])}</p>
<p>{result['locations_evaluated']:,} grid locations evaluated; final grid {result['final_grid_m']:.0f} m; score {best['score_db']:.2f} dB; clock {best['clock_offset_s']:+.2f} s. The aircraft/window association had already used the known desk location. The search itself receives the aircraft track, audio curve, approximate time and Texas bounds. This is a conditional fit, not a blind test or an established location.</p><a href="flight-conditioned-location/results/map.html">Inspect conditional candidate map</a> · <a href="flight-conditioned-location/evaluation.json">Evaluation JSON</a>'''
        for name, label in [("followup", "Highest-scoring location, clock frozen"), ("known-receiver-followup", "User-reported desk, filename clock frozen")]:
            guided = load(folder / "flight-conditioned-location" / name / "guided.json")
            if not guided:
                continue
            candidates = guided["candidates"]
            tested = sum(len(c["matches"]) for c in candidates)
            support = sum(c["additional_flights_with_support"] for c in candidates)
            followup_tested += tested
            followup_support += support
            anchor = all(c["anchor_rechecked"] for c in candidates)
            location += f'''<p>{label}: anchor recheck {'passes' if anchor else 'fails'}; {tested} additional trajectory windows tested directly against measured spectral energy, including windows without an automatic proposal; {support} pass the experimental support checks. All flights retain the same shared clock and propagation model. Source tones are selected on training blocks; separate blocks, stationary-tone and wrong-time controls assess the result. A positive energy score alone is insufficient.</p><a href="flight-conditioned-location/{name}/guided.html">Inspect additional-flight comparison</a>'''
    fr24_link='<a href="fr24-comparison.json">FR24 comparisons</a>' if (folder/'fr24-comparison.json').exists() else ''
    source_note=(f'{len(observed_ids)} distinct airborne FR24 flight IDs observed; {len(matched)} airframes have agreeing callsign and horizontal position in both sources. These are sparse observations, not a complete flight count.' if manifest['snapshots'] else 'Trajectory and identity source: cached ADSB.lol geometric tracks. This case has no FR24 playback acquisition.')
    text = f'''<nav><a href="../index.html">All recordings</a><a href="encounters.csv">Flight timeline CSV</a>{fr24_link}<a href="ridge-comparison.json">Curve diagnostics</a></nav>
<p class="note">{html.escape(context_status)}. This page is supervised recognition review. No new verified recording location is claimed. A nearby aircraft can be inaudible indoors. An absent acoustic detection does not establish silence.</p>
<p>{html.escape(recording['audio'])}<br>UTC interval: {data['start_utc']} – {data['end_utc']}<br>{source_note}</p>
{timeline(passes, recording['duration_s'], strong)}
<p>{len(strong)} automatic strong tonal proposals; maximum {maximum} distinct nonoverlapping curve agreements at one shared clock in this diagnostic. No validated multiple-aircraft acoustic sequence.</p>
<audio id="audio" controls preload="none" src="audio.mp3"></audio><p id="playing">Select a predicted passage or curve interval to listen.</p>
{location}<h2>Automatic ridge comparisons</h2>{''.join(selected_curves) or '<p>No automatic strong ridge available for trajectory comparison.</p>'}
{('<h2>Direct spectral flight checks</h2><p>These checks inspect predicted flight windows without requiring an automatic proposal. Different FFT profiles examine the same aircraft; their counts must not be added as independent flyovers.</p>'+''.join(direct_sections)) if direct_sections else ''}
<h2>Nearby jet review windows</h2>{''.join(jet_plots) or '<p>No complete jet passage within the 5 km plot subset; this does not exclude more distant aircraft.</p>'}
<h2>All predicted encounters within 15 km</h2><p>Arrival times include sound propagation at 343 m/s. All offsets stay fixed when changing the common clock. The near-pass gap column uses the previous encounter within 5 km. Review windows are display choices, not audibility measurements.</p>
<p>The clock control changes displayed and listening times. Stored acoustic checks keep the clock recorded in their review.</p><label><input id="near" type="checkbox"> Only within 5 km</label> <label>Common playback clock correction (s) <input id="clock" type="number" value="0" min="-30" max="30"></label>
<div class="scroll"><table><thead><tr><th>Aircraft</th><th>Predicted audio time</th><th>Gap / near gap</th><th>Slant / delay</th><th>FR24 comparison</th><th>Listen</th></tr></thead><tbody id="flights"></tbody></table></div>
<script>const rows={json.dumps(rows).replace('<', chr(92)+'u003c')};
const audio=document.getElementById('audio');let stop=Infinity;audio.ontimeupdate=()=>{{if(audio.currentTime>=stop)audio.pause()}};
const fmt=s=>`${{String(Math.floor(s/3600)).padStart(2,'0')}}:${{String(Math.floor(s%3600/60)).padStart(2,'0')}}:${{(s%60).toFixed(1).padStart(4,'0')}}`;
function play(start,end){{stop=end;audio.currentTime=Math.max(0,start);audio.play().catch(()=>{{}});document.getElementById('playing').textContent='Playing '+fmt(Math.max(0,start))+'–'+fmt(end)}}
document.querySelectorAll('button[data-start]').forEach(b=>b.onclick=()=>play(+b.dataset.start,+b.dataset.end));
function draw(){{const clock=+document.getElementById('clock').value;const table=document.getElementById('flights');table.replaceChildren();for(const r of rows){{if(document.getElementById('near').checked&&r.slant_m>5000)continue;const tr=document.createElement('tr');const cells=[`${{r.callsign||r.icao}} · ${{r.type_code||'unknown'}} · ${{r.registration||''}}${{r.partial_trajectory?' (partial)':''}}`,fmt(r.predicted_audio_cpa_s-clock),`${{r.gap_from_previous_encounter_s?.toFixed(1)||'—'}} / ${{r.gap_from_previous_near_encounter_s?.toFixed(1)||'—'}} s`,`${{(r.slant_m/1000).toFixed(2)}} km / ${{r.propagation_delay_s.toFixed(1)}} s`,r.fr24_status];for(const text of cells){{const td=document.createElement('td');td.textContent=text;tr.appendChild(td)}}const td=document.createElement('td'),button=document.createElement('button');button.textContent='Play';button.onclick=()=>play(r.review_start_s-clock,r.review_end_s-clock);td.appendChild(button);for(const id of r.fr24_ids){{const link=document.createElement('a');link.textContent=' FR24 '+id;link.href='https://www.flightradar24.com/'+encodeURIComponent(r.callsign||r.registration||r.icao)+'/'+id;link.target='_blank';link.rel='noreferrer';td.appendChild(link)}}tr.appendChild(td);table.appendChild(tr)}}}}
document.getElementById('near').onchange=draw;document.getElementById('clock').oninput=draw;draw();</script>'''
    (folder / "index.html").write_text(page(recording["id"] + " · flight and sound review", text))
    return dict(id=recording["id"], context_status=context_status, start_utc=data["start_utc"], end_utc=data["end_utc"],
                snapshots=len(manifest["snapshots"]), unverified_snapshots=sum(s["status"] != "captured" for s in manifest["snapshots"]),
                airborne_fr24_flight_ids=len(observed_ids), callsign_and_position_matched_airframes=len(matched),
                predicted_encounters=len(passes), complete_within_5km=sum(p["slant_m"]<=5000 and not p["partial_trajectory"] for p in passes),
                automatic_strong_ridges=len(strong), training_selected_shape_agreements=len(accepted),
                max_common_clock_distinct_shape_agreements=maximum,
                direct_spectral_profiles=direct_profiles,direct_window_evaluations=direct_tested,
                direct_supported_airframes=len(direct_supported),
                additional_windows_tested_across_followups=followup_tested,
                additional_support_across_followups=followup_support,
                conditional_location_error_m=error, outcome="No validated multiple-aircraft acoustic sequence or verified location")


def build_moving(root):
    config=load(root/"walking-capture-config.json")
    if not config:
        return []
    summaries=[]
    for recording in config["recordings"]:
        folder=root/recording["id"]
        manifest=load(folder/"fr24/manifest.json")
        if not manifest:
            continue
        epoch=dt.datetime.fromisoformat(recording["start"]).timestamp()
        rows=[]
        for path in sorted((folder/"fr24").glob("[0-9]*.json")):
            snapshot=load(path)
            if snapshot["status"]!="captured" or snapshot["requested_timestamp_s"]!=snapshot["playback_timestamp_s"]:
                continue
            for a in snapshot["aircraft"]:
                stamp=a.get("timestampMs",0)/1000 or a["timestamp"]
                relative=stamp-epoch
                if a.get("onGround") or relative<0 or relative>recording["duration_s"] or abs(stamp-snapshot["requested_timestamp_s"])>90:
                    continue
                rows.append(dict(fr24_id=a["id"],callsign=a.get("callsign") or "Unknown",
                            observation_utc=dt.datetime.fromtimestamp(stamp,dt.timezone.utc).isoformat(),
                            telemetry_time_since_audio_start_s=relative,latitude=a["latitude"],longitude=a["longitude"],
                            note="Telemetry observation time; sound arrival delay and moving receiver are not modeled"))
        fields=["fr24_id","callsign","observation_utc","telemetry_time_since_audio_start_s","latitude","longitude","note"]
        with (folder/"observations.csv").open("w") as stream:
            writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader();writer.writerows(rows)
        dump(folder/"observations.json",dict(recording=recording,observations=rows,note="Visible-aircraft inventory only. No fixed receiver location, flight acoustics, audibility, or Doppler match is inferred."))
        body=[]
        for row in rows:
            url="https://www.flightradar24.com/"+quote(row["callsign"])+"/"+row["fr24_id"]
            body.append(f"<tr><td><a href='{url}'>{html.escape(row['callsign'])}</a></td><td>{row['observation_utc']}</td><td>{elapsed(row['telemetry_time_since_audio_start_s'])}</td><td>{row['fr24_id']}</td></tr>")
        (folder/"index.html").write_text(page("Walking recording · FR24 flight inventory",f'''<nav><a href="../index.html">All recording reviews</a><a href="observations.csv">Observation CSV</a></nav><p class="note">The phone was moving around a neighborhood park. This is a sparse inventory of aircraft shown in the surrounding map, not a stationary-receiver Doppler test or a recording location result.</p><p>Times below are aircraft telemetry observation times relative to the filename's recording start. They are emission/position times; sound arrival delay and the moving receiver have not been modeled. Repeated rows are observations of a flight, not additional flyovers. No aircraft audibility or acoustic identity is established.</p><table><thead><tr><th>Callsign / FR24 link</th><th>Observation UTC</th><th>Relative telemetry time</th><th>Flight ID</th></tr></thead><tbody>{''.join(body)}</tbody></table>'''))
        summaries.append(dict(id=recording["id"],snapshots=len(manifest["snapshots"]),
                             airborne_fr24_flight_ids=len({x["fr24_id"] for x in rows}),
                             outcome="Moving receiver: flight inventory only; no acoustic identity or location inferred"))
    return summaries


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    args = parser.parse_args()
    root = args.input
    summaries = [build_case(root, p.parent) for p in sorted(root.glob("2026_*/encounters.json"))]
    moving=build_moving(root)
    generated = dt.datetime.now(dt.timezone.utc).isoformat()
    summary = dict(generated_utc=generated, recordings=summaries,moving_recordings=moving,
                   note="FR24 trajectory/callsign cross-checks do not prove an acoustic identity. A supplied or explicitly assumed receiver is used for recognition diagnostics. Conditional location experiments reuse prior associations and do not independently establish a recording location.")
    dump(root / "summary.json", summary)
    with (root / "results.csv").open("w") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(summaries[0]))
        writer.writeheader(); writer.writerows(summaries)
    total = sum(x["snapshots"] for x in summaries)
    conditional = [r for r in summaries if r["conditional_location_error_m"] is not None]
    conditional_html = ''.join(f"<p>{html.escape(r['id'])}: a flight-conditioned search ranks a point {r['conditional_location_error_m']:.0f} m from the comparison receiver first. Read the case's ambiguity and scope before interpreting this distance.</p>" for r in conditional)
    max_shapes = max((r["max_common_clock_distinct_shape_agreements"] for r in summaries), default=0)
    screenshot_note = '<p>The supplied screenshot shows November 6 at 21:21:56 UTC. The November 6 recording spans 16:23:41–19:51:41 UTC, so that screenshot frame is outside its audio interval.</p>' if any(r['id']=='2026_11_06_10_23_41' for r in summaries) else ''
    moving_html=''.join(f"<p><a href='{quote(r['id'])}/index.html'>Walking recording: FR24 flight inventory</a> — {r['snapshots']} snapshots, {r['airborne_fr24_flight_ids']} airborne flight IDs observed. The moving receiver is not modeled; no acoustic identity or position is established.</p>" for r in moving)
    cells = []
    for r in summaries:
        cells.append(f"<tr><td><a href='{quote(r['id'])}/index.html'>{html.escape(r['id'])}</a></td><td>{r['snapshots']}</td><td>{r['airborne_fr24_flight_ids']}</td><td>{r['callsign_and_position_matched_airframes']}</td><td>{r['complete_within_5km']}</td><td>{r['automatic_strong_ridges']}</td><td>{r['max_common_clock_distinct_shape_agreements']}</td></tr>")
    capture_note=f'Additional playback evidence includes {total} saved anonymous FR24 snapshots.' if total else 'The trajectory source for this batch is ADSB.lol.'
    provider_note = 'Two-minute sampling and the visible map omit some aircraft. The two tracking services can share underlying ADS-B reports; agreement is a cross-check of trajectory/identity, not independent acoustic truth. FR24 displayed altitude is not substituted for geometric height.' if total or moving else 'No FR24 playback was acquired for this batch. ADS-B coverage and proximity alone do not establish that an aircraft is audible.'
    (root / "index.html").write_text(page("Flight history and recording review", f'''<p class="note">This review covers {len(summaries)} recording(s). {capture_note} Aircraft geometry is compared with whole-recording automatic acoustic proposals. Receiver and timing context is provided or explicitly assumed for each case; see its status before interpreting a match.</p>
<p>The maximum number of distinct curve agreements sharing one clock in this diagnostic is {max_shapes}. A shape agreement is a hypothesis to validate against measured energy and competing controls. No new verified receiver location is asserted by this review.</p>{conditional_html}
<nav><a href="results.csv">Results CSV</a><a href="summary.json">Summary JSON</a><a href="provenance.json">Source hashes and limitations</a></nav>
{diagnosis_panel(root)}
<div class="scroll"><table><thead><tr><th>Recording</th><th>FR24 snapshots</th><th>Airborne flight IDs observed</th><th>Airframes agreeing across sources</th><th>Complete predicted passes ≤5 km</th><th>Automatic strong tones</th><th>Distinct curves sharing one clock</th></tr></thead><tbody>{''.join(cells)}</tbody></table></div>
{moving_html}
<p>Flight IDs, airframes, repeated encounters and acoustic curves are different counts. {provider_note}</p>
{screenshot_note}<p>All calculations use the corresponding recording intervals and their UTC dates.</p>
<p>Each recording page includes expected flight times and gaps, audio playback, cropped spectrograms and curve comparisons. A shared clock correction moves the entire schedule together. A missing sound is not automatically treated as evidence of silence.</p>'''))
    assets = set(root.glob("*/fr24/*.json")) | set(root.glob("2026_*/*.json")) | set(root.glob("2026_*/*.svg"))
    assets |= set(root.glob("walking*/*.json")) | set(root.glob("walking*/*.csv")) | set(root.glob("*/*.html")) | {root/"index.html"}
    if (root/"walking-capture-config.json").exists():assets.add(root/"walking-capture-config.json")
    assets |= {root/"config.json",root/"analysis-config.json",root/"summary.json",root/"results.csv"}
    for r in load(root/"analysis-config.json")["recordings"]:
        assets |= {Path(r["tracks"]),Path(r["audio"]),Path(r["analysis"])/"analysis-inputs.json"}
        assets |= {Path(r["analysis"])/"spectrogram.bin",Path(r["analysis"])/"detection.json"}
        # The audit's input manifest lives one directory above analysis.
        assets.add(Path(r["analysis"]).parent/"analysis-inputs.json")
        for p in load(root/r["id"]/"encounters.json")["encounters"]:
            if p.get("metadata_path"): assets.add(Path(p["metadata_path"]))
    for location in root.glob("*/flight-conditioned-location"):
        assets |= set(location.glob("*.json")) | set(location.glob("*.csv")) | {location/"results/results.json"}
        assets |= set(location.glob("followup/*.json")) | set(location.glob("known-receiver-followup/*.json"))
    assets |= {Path("tools/flight_timeline.go"),Path("tools/flight_timeline_test.go"),Path("tools/capture_fr24_playback.py"),Path("tools/report_flight_history.py"),Path("tools/fetch_flight_review.go"),Path("tools/review_receiver_audio.go"),Path("internal/search/guided.go"),Path("internal/search/receiver_review.go"),Path("internal/search/receiver_review_test.go"),Path("bin/flight-timeline"),Path("bin/pinpoint"),root/"source.json"}
    assets |= set(root.glob("*/receiver-short-spectrum.bin*"))
    assets |= {Path("references/sources.json"),Path("references/catalog.json"),Path("docs/research/sources-and-validation.md"),Path("README.md"),Path("justfile"),Path(".github/workflows/test.yml"),root/"browser-verification.json",root/"validation.json"}
    assets |= {root/name for name in ["receiver-context.json", "inventory.json", "diagnosis-summary.json", "desk-alternative-config.json"]}
    assets |= set((root/"model-diagnosis").glob("*")) | set((root/"desk-alternative").glob("*/*.json"))
    if (root/"diagnosis-summary.json").exists():
        assets.add(Path("tools/diagnose_receiver_limits.go"))
    for executable in [Path("bin/flight-timeline"),Path("bin/pinpoint"),Path("bin/review-receiver-audio"),Path("bin/fetch-flight-review")]:
        if executable.exists():
            frozen=root/"executables"/(sha(executable)+".bin")
            frozen.parent.mkdir(exist_ok=True)
            if not frozen.exists(): shutil.copy2(executable,frozen)
            assets.add(frozen)
    provenance = dict(generated_utc=generated, private=True,
         source_providers=([dict(name="Flightradar24",role="Actual anonymous historical map observations: callsign, flight leg ID, timestamp, horizontal position; sparse two-minute viewport sampling",url="https://www.flightradar24.com/")] if total or moving else [])+[dict(name="ADSB.lol",role="Cached geometric tracks, identity/type metadata",url="https://globe.adsb.lol/")],
         source_docs=["https://support.fr24.com/support/solutions/articles/3000115499-is-it-possible-to-see-playback-of-past-flights-","https://www.flightradar24.com/blog/inside-flightradar24/understanding-altitude-on-flightradar24/"],
         limitations=["A supplied or explicitly assumed receiver is revealed in recognition diagnostics; this is not a blind test.","FR24 map altitude has no verified ellipsoidal basis and is never used for acoustic geometry.","Providers can share the same underlying messages. Callsign and position agreement does not prove an audio identity.","Sparse snapshots and source-specific gaps do not enumerate all aircraft or establish audibility.","Strong tonal proposals are not automatically aircraft. Separate spectral/null-control tests and multiple matching aircraft are still required.","Direct review uses a fixed receiver/clock, stable-tone templates and finite review windows; failures do not establish aircraft absence."] + (["Conditional location experiments reuse an association previously informed by the receiver; inspect their geographic ambiguity."] if conditional else []) + ["Current helper versions are pinned here. Per-capture executor hashes are recorded when available; initial older captures may lack them. Saved observations and request/aircraft times are pinned."],
         assets=[dict(path=str(p),bytes=p.stat().st_size,sha256=sha(p)) for p in sorted(assets) if p.is_file()])
    dump(root/"provenance.json",provenance)
    print(json.dumps(summary,indent=2))


if __name__ == "__main__":
    main()
