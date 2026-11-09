"""Render statewide phone-recording accuracy results without changing winners."""
import argparse
import csv
from datetime import datetime, timezone
import gzip
import html
import json
from pathlib import Path
import re
import shutil

from evaluate_texas_samples import metres, read, save


def metadata(path):
    """Read root metadata while streaming past the potentially huge points array."""
    path = Path(path)
    cache = path.with_name("metadata-summary.json")
    stat = path.stat()
    if cache.exists():
        v = read(cache)
        if v.get("original_size") == stat.st_size and v.get("original_mtime_ns") == stat.st_mtime_ns:
            return v["metadata"]
    lines = []
    skip = False
    with path.open() as f:
        for line in f:
            if line.startswith('  "points": ['):
                lines.append('  "points": [],\n')
                skip = not line.startswith('  "points": []')
                continue
            if skip:
                if line.startswith('  ],'):
                    skip = False
                continue
            lines.append(line)
    value = json.loads("".join(lines))
    if "evaluated_points" not in value:
        log = path.parent.with_suffix(".log")
        counts = re.findall(r"(\d+) total points", log.read_text()) if log.exists() else []
        value["evaluated_points"] = int(counts[-1]) if counts else None
    save(cache, {"original_size": stat.st_size, "original_mtime_ns": stat.st_mtime_ns, "metadata": value,
                 "note": "Derived metadata view; complete points remain in the original scan or its points.jsonl.gz."})
    return value


def describe(path, truth):
    m = metadata(path)
    best = next(iter(m["training_winner"] or []), None)
    result = {"scan": str(path), "evaluated_points": m.get("evaluated_points"), "elapsed_s": m["elapsed_s"],
              "training_winner": best, "separated_training_peaks": m["separated_training_peaks"],
              "state_mask": m.get("state_mask"), "clock_window_s": m["clock_window_s"]}
    if best:
        ds = [metres(best, t) for t in truth]
        result.update(distance_m=min(ds), distance_max_m=max(ds), distance_miles=min(ds)/1609.344,
                      within_two_miles=max(ds)<=3218.688, local_checks=best["all_reserved_checks_pass"])
        if min(ds)<=3218.688<max(ds):
            result["within_two_miles"]=None
            result["accuracy_status"]="unknown_site_prevents_decision"
    return result


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--input", type=Path, default=Path("work/texas-evaluation-2026-11-08"))
    args = p.parse_args()
    root = args.input
    context = read("work/personal-samples-2026-11-08/receiver-context.json")
    protocols = []
    for directory, label in [(root, "six_second_clock_uncertainty"), (root/"exact-clock-sensitivity", "exact_filename_start")]:
        if (directory/"protocol.json").exists():
            protocols.append((directory, label, read(directory/"protocol.json")))
    by_id = {}
    for directory, label, protocol in protocols:
        for c in protocol["cases"]:
            rid = c["recording"]["id"]
            park = rid.startswith("walking_") or any(t in rid for t in ["17_38_51", "18_04_04", "18_05_46"])
            sites = [context["park" if park else "desk"]]
            if "11_07_15_01_53" in rid:
                sites = [context["desk"], context["park"]]
            row = by_id.setdefault(c["id"], {"id": c["id"], "recording": rid, "truth": sites,
                                             "truth_basis": "approximate park centre" if park else "supplied desk",
                                             "assisted_curve": c["id"].startswith("nov05-prior"), "protocol": c["protocol"]})
            if len(sites)>1:
                row["truth_basis"] = "one of two user sites; both distances checked"
            scan = Path(c["search_dir"])/"scan.json"
            if scan.exists():
                row[label] = {"status": "complete", **describe(scan, sites)}
            elif c["method"] == "no_strong_curve":
                row[label] = {"status": "no_location_no_strong_curve"}
            else:
                outcome = directory/"outcomes"/(c["id"]+".json")
                row[label] = read(outcome) if outcome.exists() else {"status": "pending"}
    # Separate the assisted two-window experiments from automatic curve choice.
    for directory, label in [("nov05-two-sounds-statewide", "six_second_clock_uncertainty"),
                             ("nov05-two-sounds-exact-clock", "exact_filename_start")]:
        scan = root/directory/"scan.json"
        row = by_id.setdefault("nov05-two-reviewed-sounds", {"id": "nov05-two-reviewed-sounds", "recording": "2026_11_05_10_17_58",
                                                             "truth": [context["desk"]], "truth_basis": "supplied desk",
                                                             "assisted_curve": True,
                                                             "protocol": "Two reviewed sound intervals, distinct aircraft, one shared clock; trajectory-guided spectral matching, not two confirmed aircraft"})
        row[label] = {"status": "complete", **describe(scan, row["truth"])} if scan.exists() else {"status": "pending"}
    rows = list(by_id.values())
    rows.sort(key=lambda r: (r["assisted_curve"], r["recording"], r["id"]))
    counts = {}
    for label in ["six_second_clock_uncertainty", "exact_filename_start"]:
        primary = [r.get(label, {}) for r in rows if not r["assisted_curve"]]
        counts[label] = {"recordings": len(primary), "with_estimate": sum(bool(r.get("training_winner")) for r in primary),
                         "within_two_miles": sum(r.get("within_two_miles") is True for r in primary),
                         "without_strong_curve": sum(r.get("status")=="no_location_no_strong_curve" for r in primary),
                         "pending": sum(r.get("status")=="pending" for r in primary),
                         "incomplete": sum(r.get("status") in ["missing_input", "incomplete_search"] for r in primary)}
    result = {"generated_utc": datetime.now(timezone.utc).isoformat(), "tolerance_m": 3218.688,
              "primary_counts": counts, "rows": rows,
              "note": "GPS is used only for post-search evaluation. Evaluate the frozen top training winner, never substitute a nearer runner-up. Exact-start results are conditional development sensitivity, not calibrated sample-zero timestamps. Single-curve matches and local checks do not establish unique GPS."}
    save(root/"accuracy.json", result)
    with (root/"accuracy.csv").open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["recording", "assisted", "timing", "status", "top_offset_miles", "within_two_miles", "local_checks", "lat", "lon"])
        for r in rows:
            for label in counts:
                v = r.get(label, {}); b = v.get("training_winner") or {}
                w.writerow([r["id"], r["assisted_curve"], label, v.get("status"), v.get("distance_miles"), v.get("within_two_miles"), v.get("local_checks"), b.get("lat"), b.get("lon")])
    with gzip.open("internal/geo/state-masks.json.gz", "rt") as f:
        rings = json.load(f)["TX"]
    save(root/"texas-border.json", rings)
    for name in ["leaflet.css", "leaflet.js"]:
        shutil.copyfile(Path("internal/mapview/assets")/name, root/name)
    table = []
    for r in rows:
        cells = []
        for label in counts:
            v = r.get(label, {})
            if v.get("training_winner"):
                suffix = "unknown" if v["within_two_miles"] is None else "yes" if v["within_two_miles"] else "no"
                check = "pass" if v["local_checks"] else "rejected"
                figure=Path(v["scan"]).parent/"peak-01-curve-01.svg"
                figure_link=f'<br><a href="{figure.relative_to(root)}">Doppler comparison</a>' if figure.exists() else ''
                cells.append(f'<td>{v["distance_miles"]:.2f} mi · <strong>{suffix}</strong><br><small>Local checks: {check}</small><br>'
                             f'<button data-case="{html.escape(r["id"])}" data-timing="{label}">Show candidate groups</button>{figure_link}</td>')
            else:
                cells.append('<td>'+html.escape(v.get("status", "pending").replace("_", " "))+'</td>')
        assist = '<br><small>Assisted/development interval</small>' if r["assisted_curve"] else ''
        table.append('<tr><td>'+html.escape(r["id"])+assist+'<br><small>'+html.escape(r["truth_basis"])+'</small></td>'+''.join(cells)+'</tr>')
    headline = []
    for label, text in [("six_second_clock_uncertainty", "±6 s"), ("exact_filename_start", "Exact filename start")]:
        n = counts[label]
        headline.append(f'{text}: {n["within_two_miles"]}/{n["with_estimate"]} available primary estimates within two miles; {n["without_strong_curve"]} with no strong curve; {n["pending"]} pending.')
    page = '''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Texas statewide accuracy evaluation</title><link rel="stylesheet" href="leaflet.css"><style>
body{margin:0;background:#17282d;color:#e0eded;font:16px/1.5 system-ui}main{max-width:1400px;margin:auto;padding:24px}a{color:#8cddcc}table{width:100%;border-collapse:collapse}th,td{padding:10px;text-align:left;border-bottom:1px solid #49636c;vertical-align:top}small{color:#aebfc4}.status{padding:16px;border-left:4px solid #e8ba70;background:#22383e}.scroll{overflow:auto}button{background:#29454e;color:#e0eded;border:1px solid #69928e;padding:8px;cursor:pointer}#map{height:65vh}.truth span{display:block;width:14px;height:14px;background:#118aff;border:3px solid white;border-radius:50%}</style><main>
<h1>Texas statewide accuracy evaluation</h1><p class="status">__OUTCOME__ Two miles is <strong>3,218.688 m</strong>. Every location search uses the Texas polygon and available eligible flight corridors across the state. Receiver coordinates are omitted from Go configurations and evaluated after the training winner is frozen. A nearby runner-up cannot make an incorrect winner pass.</p><p>__HEADLINE__</p>
<p>Automatic primary cases use the longest independently strong proposal from the complete recording. “Strong” is an acoustic heuristic, not confirmed aircraft identity. The earlier reviewed turboprop curve and two reviewed sounds are listed separately as assisted development cases. The jet uses the previous trajectory-guided spectral protocol. Public California and New York recordings are excluded; these are the user's Texas phone recordings, with duplicate audio counted once.</p>
<div class="scroll"><table><tr><th>Recording / protocol</th><th>±6 seconds: top offset / within 2 mi</th><th>Assume exact filename start: top offset / within 2 mi</th></tr>__TABLE__</table></div>
<h2>Candidate groups, state borders and streets</h2><p id="map-caption">Select a result above. Orange is the top training winner; gray shows other leading aircraft groups. Blue marks the supplied site after evaluation; the blue circle is the two-mile target, not a search constraint or confidence radius.</p><div id="map"></div>
<h2>What the test establishes</h2><p>The earlier narrow-area result does not establish statewide accuracy. For the investigated November 5 curve, a point near the desk remains among the leading groups, while a slightly better training fit can win hundreds of miles away when a small clock error is permitted. The exact-clock sensitivity uses a stronger assumption than an approximate recording time and was investigated after the initial failures; it is development evidence, not an untouched test set.</p>
<p>Local checks are conditional scores or descriptive ridge-shape checks, not calibrated GPS confidence. Some wrong locations pass them. Multi-aircraft timing keeps a common clock and distinct aircraft; a failed selected group is not rescued with a different reserved-data winner. The two reviewed intervals use training-selected spectral windows and are not a verified pair of independently extracted aircraft curves. Unsupported audio is not treated as proven silence.</p>
<p>Coarse grids are followed by refinement of leading flight combinations. They do not guarantee the global optimum: narrow high-frequency jet peaks can be missed. Ridge scans use 1,000 → 100 → 20 m spacing; the guided jet/two-window scans use 3,000 → 500 → 50 m. No grid is silently coarsened. Walking labels are approximate park centres, not measured receiver paths. One November 7 file has two possible site labels; both distances are checked conservatively. Missing traces remain a coverage limitation.</p>
<p><a href="accuracy.json">All outcomes and winners</a> · <a href="accuracy.csv">Accuracy CSV</a> · <a href="protocol.json">Frozen case choices</a> · <a href="provenance.json">Sources and hashes</a> · <a href="../desk-model-improvements-2026-11-08/">Earlier regional desk study</a></p>
<pre><code>just texas-evaluate --out work/texas-recheck
just texas-evaluate --out work/texas-exact-recheck --exact-clock
just texas-evaluation-report</code></pre><p>Private media and cached statewide tracks/spectra are required. Full evaluated points remain in each scan or its <code>points.jsonl.gz</code>; this overview maps the best separated groups. Original audio is unchanged.</p>
</main><script src="leaflet.js"></script><script>
const rows=__DATA__,border=__BORDER__;const map=L.map('map',{preferCanvas:true});L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© OpenStreetMap contributors; state boundary: US Census'}).addTo(map);for(const ring of border)L.polyline(ring.map(p=>[p[1],p[0]]),{color:'#66bed8',weight:2,opacity:.8}).addTo(map);const layer=L.layerGroup().addTo(map);map.fitBounds([[25.837,-106.646],[36.501,-93.508]]);
function show(id,timing){const r=rows.find(r=>r.id===id),v=r[timing];if(!v?.training_winner)return;layer.clearLayers();const b=v.training_winner;for(const t of r.truth){L.circle([t.lat,t.lon],{radius:3218.688,color:'#118aff',fillOpacity:.12}).addTo(layer);L.marker([t.lat,t.lon],{icon:L.divIcon({className:'truth',html:'<span></span>',iconSize:[20,20],iconAnchor:[10,10]})}).addTo(layer).bindPopup('Supplied reference, evaluation only');}const points=v.separated_training_peaks||[b];for(let i=0;i<points.length;i++){const p=points[i];const ids=(p.ridge_matches||p.training_selected_matches||[]).map(m=>m.track_id).join(', ');L.circleMarker([p.lat,p.lon],{radius:i===0?9:6,color:i===0?'#ffa546':'#aaa',weight:2,fillOpacity:.8}).addTo(layer).bindPopup('Training group '+(i+1)+'<br>'+p.lat.toFixed(6)+', '+p.lon.toFixed(6)+'<br>'+ids+'<br>Clock '+p.shared_clock_correction_s+' s<br>Local checks '+p.all_reserved_checks_pass);}map.fitBounds(L.latLngBounds([...points.map(p=>[p.lat,p.lon]),...r.truth.map(t=>[t.lat,t.lon])]).pad(.2));document.getElementById('map-caption').textContent=id+' · '+timing+' · top offset '+v.distance_miles.toFixed(2)+' miles. Only the frozen top winner determines the accuracy result.';}
for(const button of document.querySelectorAll('button[data-case]'))button.onclick=()=>show(button.dataset.case,button.dataset.timing);
</script></html>'''
    status="The automatic attempts do not meet the two-mile target." if any(n["within_two_miles"]<n["with_estimate"] for n in counts.values()) else "All available primary estimates meet the two-mile target; missing estimates remain separate."
    if any(n["pending"] for n in counts.values()):status="Statewide evaluation is in progress."
    reviewed=next((r for r in rows if r["id"].startswith("nov05-prior-")),{})
    exact=reviewed.get("exact_filename_start",{})
    if exact.get("within_two_miles"):status+=f' The separately reviewed turboprop curve reaches {exact["distance_m"]:.0f} m under the exact-start assumption.'
    page = page.replace("__OUTCOME__",status)
    page = page.replace('__HEADLINE__', '<br>'.join(headline)).replace('__TABLE__', ''.join(table))
    page = page.replace('__DATA__', json.dumps(rows).replace('<', '\\u003c')).replace('__BORDER__', json.dumps(rings))
    (root/'index.html').write_text(page)
    print(json.dumps(counts))


if __name__ == "__main__":
    main()
