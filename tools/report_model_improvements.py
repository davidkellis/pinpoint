"""Compare fixed trajectory reconstruction with preserved statewide failures."""
import argparse
from datetime import datetime, timezone
import gzip
import html
import json
import shutil
from pathlib import Path

from evaluate_texas_samples import metres, read, save
from report_texas_evaluation import metadata


def describe(path, truth):
    if not path.exists():
        return {"status": "pending", "scan": str(path)}
    m = metadata(path)
    p = next(iter(m["training_winner"]), None)
    result = {"status": "complete" if p else "no_location", "scan": str(path),
              "elapsed_s": m["elapsed_s"], "evaluated_points": m["evaluated_points"],
              "winner": p, "peaks": m["separated_training_peaks"],
              "reconstruct_track": m.get("reconstruct_track", False),
              "clock_window_s": m["clock_window_s"], "state_mask": m.get("state_mask"),
              "tracks": m["tracks"], "audio": m["audio"], "spectrum": m["spectrum"],
              "ridge_events": m["ridge_events"], "training_rank": p["training_rank"] if p else None}
    if p:
        result.update(offset_m=metres(p, truth), within_two_miles=metres(p, truth) <= 3218.688,
                      local_checks=p["all_reserved_checks_pass"])
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=Path("work/model-improvements-2026-11-08"))
    args = parser.parse_args()
    root = args.input
    root.mkdir(parents=True, exist_ok=True)
    old = Path("work/texas-evaluation-2026-11-08")
    new = root/"reconstructed-evaluation"/"cases"
    labels = read("work/personal-samples-2026-11-08/receiver-context.json")
    definitions = [
        ("desk-turboprop", "Reviewed November 5 turboprop", "desk", old/"nov05-turboprop-statewide"/"scan.json", root/"turboprop-texas-reconstructed"/"scan.json"),
        ("desk-automatic", "November 5 automatic longest-curve choice", "desk", old/"cases"/"2026_11_05_10_17_58_1"/"scan.json", new/"2026_11_05_10_17_58_1"/"scan.json"),
        ("park-prop", "November 7 park propeller case", "park", old/"park-prop-statewide"/"scan.json", new/"2026_11_07_17_38_51_1"/"scan.json"),
        ("park-jet", "November 7 park jet case", "park", old/"park-jet-statewide"/"scan.json", new/"2026_11_07_18_04_04_1"/"scan.json"),
    ]
    rows = []
    for identity, title, site, baseline, reconstructed in definitions:
        truth = {k: labels[site][k] for k in ("lat", "lon")}
        rows.append({"id": identity, "title": title, "truth": truth,
                     "site_basis": "supplied stationary desk" if site == "desk" else "approximate walking park centre",
                     "baseline": describe(baseline, truth), "reconstructed": describe(reconstructed, truth)})
    replay = describe(root/"turboprop-texas-fast"/"scan.json", rows[0]["truth"])
    if replay["status"] == "complete":
        a, b = rows[0]["reconstructed"], replay
        if a["status"] == "complete":
            replay["same_training_winner_and_leading_groups"] = a["winner"] == b["winner"] and a["peaks"] == b["peaks"]
            replay["same_evaluated_point_count"] = a["evaluated_points"] == b["evaluated_points"]
            replay["observed_wall_time_ratio"] = a["elapsed_s"] / b["elapsed_s"]
    result = {"version": 1, "generated_utc": datetime.now(timezone.utc).isoformat(), "private": True,
              "tolerance_m": 3218.688, "rows": rows, "optimized_replay": replay,
              "scope": "Four development cases: one pre-reviewed desk curve, its automatic longest-curve alternative, and two walk protocols. Receiver truth is evaluation-only. No full automatic corpus accuracy claim; source selection and walking paths remain unresolved.",
              "model_selection": "One fixed uncertainty preset, chosen before the statewide reconstruction run. A fixed-eight-peak ablation motivated this rerun; its outcome is development evidence, not untouched validation.",
              "timing": "All new searches retain the six-second clock window. Each candidate uses one recording start/clock. No exact-start substitution.",
              "runtime": "Benchmarks and concurrent wall-time observations measure different workloads. The speedup of a geometry primitive is not an end-to-end application speedup."}
    save(root/"comparison.json", result)
    for name in ("leaflet.css", "leaflet.js"):
        shutil.copyfile(Path("internal/mapview/assets")/name, root/name)
    with gzip.open("internal/geo/state-masks.json.gz", "rt") as f:
        borders = json.load(f)["TX"]
    table = []
    for row in rows:
        cells = []
        for method in ("baseline", "reconstructed"):
            case = row[method]
            if not case.get("winner"):
                cells.append("<td>"+html.escape(case["status"])+"</td>")
                continue
            ok = "within 2 mi" if case["within_two_miles"] else "outside 2 mi"
            cells.append(f'<td>{case["offset_m"] / 1609.344:.3f} mi / {case["offset_m"]:.0f} m · <b>{ok}</b>'
                         f'<br>Local checks: {"pass" if case["local_checks"] else "fail"}'
                         f'<br><button data-case="{row["id"]}" data-method="{method}">Map candidate groups</button></td>')
        table.append(f'<tr><td>{html.escape(row["title"])}<br><small>{row["site_basis"]}</small></td>'+"".join(cells)+"</tr>")
    curve = root/"turboprop-texas-reconstructed"/"peak-01-curve-01.svg"
    curve_html = '<h2>Reconstructed turboprop fit</h2><p>Measured ridge and selected flight prediction across the complete observed interval.</p><img src="turboprop-texas-reconstructed/peak-01-curve-01.svg" alt="Measured turboprop frequency ridge with reconstructed flight prediction">' if curve.exists() else ""
    benchmarks = []
    for path in sorted(root.glob("benchmark-*.txt")):
        benchmarks.append(f'<details><summary>{html.escape(path.name)}</summary><pre>{html.escape(path.read_text())}</pre></details>')
    replay_text = "Optimized statewide replay pending."
    if replay["status"] == "complete":
        replay_text = (f'Optimized replay: {replay["elapsed_s"]:.1f} s for {replay["evaluated_points"]:,} positions. '
                       f'Same training winner and leading groups: {replay.get("same_training_winner_and_leading_groups")}. '
                       f'Observed wall-time ratio: {replay.get("observed_wall_time_ratio", 0):.2f}×. Runs share CPU with other work; this is not a controlled application benchmark.')
    page = '''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Trajectory reconstruction and statewide localization</title><link rel="stylesheet" href="leaflet.css"><style>
body{margin:0;background:#18282e;color:#e0eded;font:16px/1.5 system-ui}main{max-width:1350px;margin:auto;padding:24px}a{color:#8edbcc}table{width:100%;border-collapse:collapse}td,th{padding:12px;text-align:left;vertical-align:top;border-bottom:1px solid #526971}button{padding:9px;background:#29454c;color:#e0eded;border:1px solid #8aa}small{color:#b3c4c9}.status{border-left:4px solid #82d6c5;padding:15px;background:#213a42}#map{height:65vh}img{width:100%;background:#0c1720}pre{overflow:auto}</style><main>
<h1>Trajectory reconstruction and statewide localization</h1><p class="status">The reviewed desk turboprop curve now selects a location about 427 m from the supplied desk across Texas with a ±6 s clock allowance. This is an assisted development success. The walk cases and automatic curve selection remain separate acceptance gates.</p>
<p>One fixed position/velocity Kalman + Rauch–Tung–Striebel reconstruction combines reported positions, ground speed, true track and geometric climb rate. No audio, source pitch or observer coordinates enters that reconstruction. Long gaps split independent chunks; excessive corrections preserve the original chunk. Original audio and flight measurements are unchanged.</p>
<table><tr><th>Case</th><th>Preserved baseline winner</th><th>Reconstructed trajectory winner</th></tr>__TABLE__</table>
<p>Every row evaluates the top training-selected location, never a nearer runner-up. Local checks are descriptive or conditional and do not establish a unique location. The two-mile circle is an evaluation target, not a search boundary or confidence interval. Park labels are approximate centres, not recorded receiver trajectories. Jet reconstruction also adds reported-motion interpolation to its prior position-linear protocol.</p>
<p><button id="nearby">Zoom to selected winner and supplied site</button> <button id="state">Show all Texas</button></p><p id="caption"></p><div id="map"></div>__CURVE__
<h2>Runtime</h2><p>__REPLAY__</p><p>The exact geometry optimizations cache segment motion, binary-search sound-arrival bounds when monotonicity is guaranteed, and stop range screening once a qualifying segment is found. Suspect geometry retains exhaustive checks. Unit and race tests compare physical outputs and complete selected fit statistics.</p>__BENCHMARKS__
<h2>What remains</h2><p>Single-curve ambiguity and automatic sound selection remain major obstacles. Strong tonal proposals are unconfirmed sources. A useful next stage must compare all plausible curves, combine independently supported flyovers with one clock and receiver path, preserve unresolved candidates, and calibrate full-search false positives. Dense grid spacing cannot substitute for independent evidence. Absence of a detected curve is not established acoustic silence indoors or during a noisy walk.</p>
<p>The full-spectrum/four-harmonic frozen-peak ablation did not separate the correct turboprop before reconstruction; it remains recorded. Synthetic noisy-telemetry cases reduce forward-model frequency error, but do not establish real-world detection accuracy.</p>
<p><a href="comparison.json">Comparison and frozen winners</a> · <a href="provenance.json">Inputs, sources and software hashes</a> · <a href="fixed-peak-spectral-ablation.json">Spectral ablation before reconstruction</a> · <a href="fixed-peak-spectral-ablation-reconstructed.json">Spectral ablation after reconstruction</a> · <a href="../texas-evaluation-2026-11-08/">Preserved full-corpus baseline</a></p>
<p>Algorithm reference: <a href="https://users.aalto.fi/~ssarkka/pub/bfs_book_2023_online.pdf">Särkkä and Svensson, Bayesian Filtering and Smoothing</a>. Telemetry schema: <a href="https://github.com/wiedehopf/readsb/blob/dev/README-json.md">readsb JSON documentation</a>.</p></main>
<script src="leaflet.js"></script><script>
const rows=__DATA__,borders=__BORDERS__;const map=L.map('map',{preferCanvas:true});L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© OpenStreetMap contributors; state outline: US Census'}).addTo(map);for(const ring of borders)L.polyline(ring.map(p=>[p[1],p[0]]),{color:'#75c9e0',weight:2}).addTo(map);const layer=L.layerGroup().addTo(map);let bounds;
function show(id,method){const row=rows.find(r=>r.id===id),v=row[method];if(!v.winner)return;layer.clearLayers();const truth=row.truth;L.circle([truth.lat,truth.lon],{radius:3218.688,color:'#2d9fff',fillOpacity:.1}).addTo(layer);L.circleMarker([truth.lat,truth.lon],{radius:7,color:'#28a6ff',fillOpacity:1}).addTo(layer).bindPopup('Supplied site, evaluated after search');for(let i=0;i<v.peaks.length;i++){const p=v.peaks[i];L.circleMarker([p.lat,p.lon],{radius:i===0?9:5,color:i===0?'#ffa651':'#aaa',fillOpacity:.8}).addTo(layer).bindPopup('Training group '+(i+1)+'<br>'+p.lat.toFixed(6)+', '+p.lon.toFixed(6)+'<br>Clock '+p.shared_clock_correction_s+' s');}bounds=L.latLngBounds([[truth.lat,truth.lon],[v.winner.lat,v.winner.lon]]).pad(.3);map.fitBounds(bounds,{maxZoom:15});document.getElementById('caption').textContent=row.title+' · '+method+' · '+v.offset_m.toFixed(1)+' m. Orange: winner. Gray: leading groups. Blue: reference, evaluation only.';}
document.querySelectorAll('button[data-case]').forEach(b=>b.onclick=()=>show(b.dataset.case,b.dataset.method));document.getElementById('nearby').onclick=()=>{if(bounds)map.fitBounds(bounds,{maxZoom:15})};document.getElementById('state').onclick=()=>map.fitBounds([[25.837,-106.646],[36.501,-93.508]]);show('desk-turboprop','reconstructed');
</script></html>'''
    page = page.replace("__TABLE__", "".join(table)).replace("__CURVE__", curve_html)
    page = page.replace("__REPLAY__", html.escape(replay_text)).replace("__BENCHMARKS__", "".join(benchmarks))
    page = page.replace("__DATA__", json.dumps(rows).replace("<", "\\u003c")).replace("__BORDERS__", json.dumps(borders))
    (root/"index.html").write_text(page)
    print("Wrote", root/"index.html")


if __name__ == "__main__":
    main()
