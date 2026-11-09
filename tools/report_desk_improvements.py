"""Build the local desk-model experiment report from saved Go outputs."""
import argparse
from datetime import datetime, timezone
import hashlib
import html
import json
from pathlib import Path
import shutil

from check_desk_localization import distance


def read(path):
    return json.loads(Path(path).read_text())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=Path("work/desk-model-improvements-2026-11-08"))
    args = parser.parse_args()
    root = args.input
    replay = "desk-regression-final" if (root / "desk-regression-final/regression.json").exists() else "desk-regression-with-controls"
    result = read(root / replay / "regression.json")
    primary = read(root / replay / "scan/scan.json")
    truth = result["evaluation_only"]
    winner = result["training_winner"]
    rows, experiments = [], []
    variants = [
        ("Spectral fragments, 0.25 s FFT", "nov05-clear-curve-region"),
        ("Spectral fragments, 2 s FFT", "nov05-clear-curve-high-resolution"),
        ("Complete curve, linear positions", "ridge-linear"),
        ("Complete curve, position Hermite", "ridge-position-smooth"),
        ("Complete curve, reported motion", "ridge-reported"),
        ("Reported motion, source drift allowed", "ridge-reported-source-drift"),
        ("Measured local peaks, one tone", "ridge-measured-h1"),
        ("Measured local peaks, up to four harmonics", "ridge-measured-h4"),
        ("Complete curve, 40 km region, reported motion", "desk-regression-with-controls/scan"),
    ]
    for label, directory in variants:
        path = root / directory / "scan.json"
        if not path.exists():
            continue
        scan = read(path)
        best = next(iter(scan["training_winner"] or []), None)
        offset = distance(best, truth) if best else None
        experiments.append({"label": label, "report": str(path), "offset_from_user_desk_m": offset,
                            "training_winner": best, "elapsed_s": scan["elapsed_s"]})
        rows.append(f'<tr><td><a href="{html.escape(directory)}/index.html">{html.escape(label)}</a></td>'
                    f'<td>{offset:.0f} m</td><td>{len(scan["points"]):,}</td><td>{scan["elapsed_s"]:.1f} s</td>'
                    f'<td>{best["all_reserved_checks_pass"]}</td></tr>' if best else f'<tr><td>{html.escape(label)}</td><td colspan="4">No complete assignment</td></tr>')
    control_rows = []
    for c in result["wrong_time_full_search_controls"]:
        b = c.get("training_winner")
        rmse = b["ridge_matches"][0]["fit"]["training_rmse_hz"] if b else None
        name = f'control-{c["shift_s"]:+g}'
        control_rows.append(f'<tr><td><a href="desk-regression-with-controls/{name}/index.html">{c["shift_s"]:+g} s</a></td>'
                            f'<td>{rmse:.3f} Hz</td><td>{b["all_reserved_checks_pass"]}</td><td>{c["training_outranks_actual_time"]}</td></tr>' if b else f'<tr><td>{c["shift_s"]:+g}</td><td>No complete curve assignment</td></tr>')
    desk_rows, desk_checks = [], []
    cfg = read(root / "desk-enriched-config.json")
    for r in cfg["recordings"]:
        desk_dir = "desk-low-band-linear" if (root / "desk-low-band-linear" / r["id"] / "receiver-review-short.json").exists() else "desk-low-band"
        path = root / desk_dir / r["id"] / "receiver-review-short.json"
        if not path.exists():
            desk_rows.append(f'<tr><td>{r["id"]}</td><td>Not complete</td><td>Unresolved</td></tr>')
            continue
        review = read(path)
        support = [m for m in review["matches"] if m["additional_support"]]
        desk_checks.append({"recording": r["id"], "modeled_windows": len(review["matches"]),
                           "experimental_supported": support, "report": str(path)})
        detail = '<br>'.join(html.escape(m["track_id"]) + f' at {m["start_s"]:.1f}–{m["end_s"]:.1f} s' for m in support) or "No supported window in this profile"
        desk_rows.append(f'<tr><td><a href="{desk_dir}/{r["id"]}/receiver-review-short.json">{r["id"]}</a></td><td>{detail}</td><td>Supplied-position diagnostic; no GPS inference</td></tr>')
    joint = read(root / "ridge-three-curves/scan.json")
    candidates = primary["points"]
    payload = json.dumps({"points": candidates, "winner": winner, "truth": truth, "peaks": primary["separated_training_peaks"]})
    fit = winner["ridge_matches"][0]["fit"]
    page = '''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Desk recording: full-curve model study</title><link rel="stylesheet" href="leaflet.css"><style>
body{margin:0;background:#17282d;color:#e0eded;font:16px system-ui;line-height:1.5}main{max-width:1280px;margin:auto;padding:24px}a{color:#8cddcc}table{border-collapse:collapse;width:100%}td,th{padding:9px;text-align:left;border-bottom:1px solid #476066;vertical-align:top}.status{padding:16px;border-left:4px solid #e8ba70;background:#20343b}#map{height:65vh}.desk-marker span{display:block;width:18px;height:18px;border:3px solid white;border-radius:50%;background:#118aff;box-shadow:0 0 4px black}img{width:100%;background:#111827}code{overflow-wrap:anywhere}button{padding:10px;margin:6px;background:#29454e;color:#e0eded;border:1px solid #69928e}.scroll{overflow:auto}</style><main>
<h1>Desk recording: complete-curve location study</h1>
<p class="status"><strong>__OFFSET__ m from the user-supplied desk</strong> for the clear November 5 curve, across a supplied region about 40 km wide. This is a conditional development result with nearby alternatives, not a verified unique GPS fix or a successful localization of every desk recording.</p>
<p>The search fits the complete reviewed audio ridge to actual flight geometry, sound delay and terrain height. It searches all eligible supplied aircraft, one receiver and one shared clock. Pitch is fitted on training blocks. Reported ground motion is retained in optional Hermite interpolation. The winning candidate is the tracked turboprop, at <strong>__POSITION__</strong>, clock __CLOCK__ s. Training ridge RMSE is __TRAIN__ Hz; reserved ridge RMSE is __RESERVED__ Hz.</p>
<p>The source interval had already been investigated at the known desk location. Its final location fit excludes desk coordinates and a fixed flight ID; the regional bounds and selected sound remain supplied priors. Reserved ridge errors are descriptive because discovery can use the full audio. A 20 m final grid is sampling resolution. Six full searches at incorrect recording times were all weaker on training; these correlated trials do not establish a false-positive probability.</p>
<h2>Map</h2><p>Redder points fit better. Colors show the range within 1 normalized training RMSE of the winner, clipped below it; they are not probabilities. Gray points lack a complete model. The blue marker is the user-supplied desk, added only after the search. The orange marker is the training winner. Click a point for residuals and clock.</p><button id="near">Zoom to leading candidates</button><button id="full">Show full search region</button><div id="map"></div>
<p><a href="desk-regression-with-controls/scan/index.html">Standalone candidate map</a> · <a href="desk-regression-with-controls/regression.json">Regression, all controls and commands</a> · <a href="provenance.json">Input/source provenance</a></p>
<h2>Complete measured curve and prediction</h2><p>Cyan: flight forward model. Blue: audio-selected observed ridge. Both are shown over the measured two-second FFT spectrogram, cropped in time/frequency for visibility.</p>
<img src="desk-regression-with-controls/scan/peak-01-curve-01.svg" alt="Complete observed ridge and predicted aircraft Doppler curve on measured spectrogram">
<h2>Ablations</h2><p>Each row retains its actual training winner, even when its GPS offset is worse. Grid origins and bounds differ between the small and wide studies. Settings were explored after seeing earlier desk results; this is development data. The search itself ranks training ridge residuals, never distance to the desk.</p><div class="scroll"><table><tr><th>Model</th><th>Offset from supplied desk</th><th>Points</th><th>Run time</th><th>Local reserved checks</th></tr>__ROWS__</table></div>
<p>The 40-second brightness-fragment score could prefer a point roughly 1.8 km away. Fitting the entire observed curve removes that freedom. More flexible source pitch and harmonic refinement did not consistently improve geographic agreement. They remain explicit optional research settings.</p>
<h2>Incorrect-time controls</h2><p>Each trial repeats the complete 40 km location/clock/source/aircraft search against the same measured curve. A local shape pass at an incorrect time does not make it the strongest explanation.</p><table><tr><th>Wrong start time</th><th>Training RMSE</th><th>Reserved shape passes</th><th>Outranks actual time</th></tr>__CONTROLS__</table>
<h2>Multiple sounds remain mandatory</h2><p>The independent acoustic selection produced three long strong intervals (source events 9338, 13973 and 42752). Trying to explain all three with distinct aircraft and a common clock produced <strong>__JOINT__ complete location assignments</strong>. The program does not discard a failing event to rescue that group. This does not prove the other sounds are not aircraft, nor establish acoustic silence.</p>
<h2>All six stationary desk recordings</h2><p>New whole-recording 2 s FFT, 40–800 Hz checks use the supplied desk location and reported-motion interpolation. Linear pitch changes are now compared on the same audio for long recordings as well as short clips. These low-band recognition diagnostics supplement the prior quarter-second jet experiments. No-support does not establish aircraft absence, and these diagnostic matches do not locate the recording.</p><table><tr><th>Recording</th><th>Experimental acoustic support</th><th>Scope</th></tr>__DESKS__</table>
<p>The broader November 5 whole-schedule audit still fails its controls. Its matched-cohort rule cannot validate every provisional encounter (including the K900) across all shifted schedules. A failed selected pair is not rescued using another pair's reserved scores. The extra matches remain provisional, not a confirmed two-aircraft location. <a href="nov05-low-band-schedules-final/summary.json">Final schedule audit</a>.</p>
<h2>Reproduce</h2><pre><code>just desk-localization-check --out work/desk-location-new
just sample-audit-serve</code></pre><p>Requires pinned private audio, enriched tracks and measured spectrum. The checker verifies input hashes, removes receiver truth from the search config, freezes the training winner, then measures distance to the desk. The 750 m regression threshold catches the earlier neighborhood-scale failure; it is not a demonstrated accuracy or confidence radius. Earlier unresolved multi-event policies remain unchanged.</p>
<p><a href="../overnight-improvements-2026-11-08/">Earlier park, jet and schedule experiments</a>. Motion field units and flags follow the <a href="https://github.com/wiedehopf/readsb/blob/dev/README-json.md">readsb JSON specification</a>. Original private audio is unchanged. This work does not yet replace the website's automatic analysis flow.</p>
</main><script src="leaflet.js"></script><script>
const d=__DATA__,map=L.map('map',{preferCanvas:true});L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© OpenStreetMap contributors'}).addTo(map);
const valid=d.points.filter(p=>p.model_available),hi=Math.max(...valid.map(p=>p.training_rank)),lo=hi-1;
for(const p of d.points){let f=Math.max(0,Math.min(1,(p.training_rank-lo)/(hi-lo||1)));let color=p.model_available?'hsl('+(250-250*f)+',85%,55%)':'#888';L.circleMarker([p.lat,p.lon],{radius:p.grid_m<100?3:4,color,weight:.5,fillOpacity:.7}).addTo(map).bindPopup(p.lat.toFixed(6)+', '+p.lon.toFixed(6)+'<br>Clock '+p.shared_clock_correction_s+' s<br>'+p.ridge_matches?.map(m=>m.track_id+'<br>Train '+m.fit.training_rmse_hz.toFixed(3)+' Hz; reserved '+m.fit.reserved_rmse_hz.toFixed(3)+' Hz').join('<br>'));}
L.marker([d.truth.lat,d.truth.lon],{icon:L.divIcon({className:"desk-marker",html:'<span></span>',iconSize:[24,24],iconAnchor:[12,12]})}).addTo(map).bindPopup('User-supplied stationary desk; evaluation only');L.circleMarker([d.winner.lat,d.winner.lon],{color:'#ffad5c',radius:10,weight:3,fill:false}).addTo(map).bindPopup('Training winner; no unique GPS established');
const near=()=>map.fitBounds(L.latLngBounds([...d.peaks.map(p=>[p.lat,p.lon]),[d.truth.lat,d.truth.lon]]).pad(.4));document.getElementById('near').onclick=near;document.getElementById('full').onclick=()=>map.fitBounds(L.latLngBounds(d.points.map(p=>[p.lat,p.lon])));near();</script></html>'''
    substitutions = {"__OFFSET__": f'{result["offset_from_user_supplied_desk_m"]:.0f}',
                     "__POSITION__": f'{winner["lat"]:.6f}, {winner["lon"]:.6f}',
                     "__CLOCK__": f'{winner["shared_clock_correction_s"]:+g}', "__TRAIN__": f'{fit["training_rmse_hz"]:.3f}',
                     "__RESERVED__": f'{fit["reserved_rmse_hz"]:.3f}', "__ROWS__": "".join(rows),
                     "__CONTROLS__": "".join(control_rows), "__JOINT__": str(sum(p["model_available"] for p in joint["points"])),
                     "__DESKS__": "".join(desk_rows), "__DATA__": payload}
    for key, value in substitutions.items():
        page = page.replace(key, value)
    page = page.replace("desk-regression-with-controls/", replay + "/")
    for name in ["leaflet.js", "leaflet.css"]:
        shutil.copyfile(Path("internal/mapview/assets") / name, root / name)
    (root / "index.html").write_text(page)
    summary = {"generated_utc": datetime.now(timezone.utc).isoformat(), "result": result,
               "experiments": experiments, "desk_diagnostics": desk_checks,
               "mandatory_three_curve_valid_locations": sum(p["model_available"] for p in joint["points"]),
               "scope": "Conditional private development research; not all desk samples located or unique GPS verified"}
    (root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(root / "index.html")


if __name__ == "__main__":
    main()
