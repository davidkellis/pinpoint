"""Run frozen full-Texas location attempts, then evaluate supplied site labels."""
import argparse
import concurrent.futures
from datetime import datetime
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import time
from zoneinfo import ZoneInfo


def read(path):
    return json.loads(Path(path).read_text())


def save(path, value):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_text(json.dumps(value, indent=2) + "\n")


def sha(path):
    with Path(path).open("rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def metres(a, b):
    x, y, u, v = map(math.radians, [a["lat"], a["lon"], b["lat"], b["lon"]])
    h = math.sin((x-u)/2)**2 + math.cos(x)*math.cos(u)*math.sin((y-v)/2)**2
    return 12742000*math.asin(math.sqrt(min(1, h)))


def prepare(out, data):
    audit = Path("work/sample-audit-2026-11-07")
    desk = Path("work/desk-model-improvements-2026-11-08")
    park = Path("work/personal-samples-2026-11-08")
    recordings = []
    for r in read(audit / "report.json")["recordings"]:
        if r["family"] != "personal":
            continue
        text = re.search(r"20\d\d_\d\d_\d\d_\d\d_\d\d_\d\d", r["audio"]).group()
        recordings.append({"id": r["id"], "audio": r["audio"], "duration_s": r["duration_s"],
                           "start": datetime.strptime(text, "%Y_%m_%d_%H_%M_%S").replace(tzinfo=ZoneInfo("America/Chicago")).isoformat(),
                           "analysis": str(audit/r["id"]/"analysis")})
    recordings += read(park / "config.json")["recordings"]
    recordings += read("work/nov07-desk/config.json")["recordings"]
    cases = []
    for r in recordings:
        rid = r["id"]
        detection = Path(r["analysis"]) / "detection.json"
        d = read(detection)
        strong = [(i, e) for i, e in enumerate(d["events"], 1) if e.get("acoustic_evidence", {}).get("level") == "strong" and len(e.get("ridge", [])) >= 8]
        # A uniform, declared audio-only choice, independent of supplied GPS.
        selected = max(strong, key=lambda item: (item[1]["end_s"]-item[1]["start_s"], -item[0])) if strong else None
        tracks = data/"statewide-tracks"/rid/"tracks.csv"
        recorded = r["id"].removesuffix("_1")
        enriched = desk/"enriched-tracks"/recorded/"tracks.csv"
        if enriched.exists():
            tracks = enriched
        if rid.startswith("walking_"):
            tracks = data/"enriched-tracks"/rid/"tracks.csv"
        case = {"id": rid, "recording": r, "tracks": str(tracks), "analysis": str(detection),
                "audio_sha256": sha(r["audio"]), "search_dir": str(out/"cases"/rid),
                "protocol": "longest independently strong acoustic proposal; all supplied aircraft; full Texas polygon",
                "grid": 1000, "refine": 100, "final_grid": 20, "clock_window": 6, "clock_step": 2,
                "smooth_track_step": .25, "reported_motion": True,
                "spectrum": str(Path(r["analysis"])/"spectrogram.bin"),
                "method": "complete_measured_ridge" if selected else "no_strong_curve"}
        proof=read(Path(r["analysis"]).parent/"analysis-inputs.json")
        if proof["audio_sha256"]!=case["audio_sha256"]:
            raise ValueError("cached acoustic analysis belongs to different audio: "+r["audio"])
        if selected:
            index, event = selected
            path = out/"curves"/(rid+".json")
            save(path, [event])
            case.update(source_event=index, curve=str(path))
        if "18_04_04" in rid:
            case.update(method="trajectory_guided_spectral", protocol="previous best jet protocol; complete audio; no aircraft ID; full Texas polygon",
                        spectrum="work/overnight-improvements-2026-11-08/fft-study-final/w025/2026_11_07_18_04_04_1/receiver-short-spectrum.bin",
                        windows=str(data/"park-jet-windows.json"), grid=3000, refine=500, final_grid=50,
                        smooth_track_step=0, reported_motion=False, search_dir=str(data/"park-jet-statewide") if out==data else str(out/"cases"/rid))
        if "17_38_51" in rid:
            case["search_dir"] = str(data/"park-prop-statewide") if out==data else str(out/"cases"/rid)
            case["spectrum"] = "work/overnight-improvements-2026-11-08/receiver-regression-final/park-turboprop/2026_11_07_17_38_51_1/receiver-short-spectrum.bin"
        cases.append(case)
    # Preserve the best previously investigated desk curve as a separate,
    # explicitly assisted case; do not let GPS choose the automatic primary.
    r = next(c for c in cases if "11_05_10_17_58" in c["id"])
    extra = {**r, "id": "nov05-prior-turboprop-curve", "curve": "references/private/fixtures/desk-nov05-ridge.json", "source_event": 13973,
             "spectrum": str(desk/"nov05-low-band/2026_11_05_10_17_58/receiver-short-spectrum.bin"),
             "search_dir": str(data/"nov05-turboprop-statewide") if out==data else str(out/"cases"/"nov05-prior-turboprop-curve"), "clock_step": 1,
             "protocol": "previously investigated desk curve, assisted interval choice; full Texas; no supplied aircraft ID"}
    cases.append(extra)
    save(out/"protocol.json", {"version": 1, "state": "TX", "tolerance_m": 3218.688,
                              "truth_supplied_to_go": False, "cases": cases,
                              "note": "One automatic primary per recording uses the longest strong proposal; these proposals are not confirmed aircraft. The prior turboprop curve and trajectory-guided jet are declared assisted/development protocols. No GPS error chooses a winner or curve."})
    return cases


def run_case(case, out, binary, workers):
    name = case["id"]
    directory = Path(case["search_dir"])
    if case["method"] == "no_strong_curve":
        result = {"id": name, "status": "no_location_no_strong_curve", "statewide_search_run": False}
        save(out/"outcomes"/(name+".json"), result)
        return result
    needed = [case["tracks"], case["spectrum"]]
    for path in needed:
        if not Path(path).exists():
            result = {"id": name, "status": "missing_input", "missing": path, "statewide_search_run": False}
            save(out/"outcomes"/(name+".json"), result)
            return result
    config = {"terrain_dir": "work/web/cache/terrain", "geoid_file": "work/web/cache/egm96_15.gtx",
              "recordings": [{"id": case["recording"]["id"], "audio": case["recording"]["audio"],
                              "start": case["recording"]["start"], "tracks": case["tracks"]}]}
    config_path = out/"configs"/(name+".json")
    save(config_path, config)
    cmd = [str(binary), "--config", str(config_path), "--recording", case["recording"]["id"],
           "--spectrum", case["spectrum"], "--state", "TX", "--corridors", "--spatial-index", "--download-terrain",
           "--grid", str(case["grid"]), "--refine", str(case["refine"]), "--final-grid", str(case["final_grid"]),
           "--clock-window", str(case["clock_window"]), "--clock-step", str(case["clock_step"]),
           "--refine-peaks", "20", "--final-peaks", "10", "--diverse-flights", "--compress-points",
           "--max-points", "700000", "--workers", str(workers), "--out", str(directory)]
    if case["method"] == "complete_measured_ridge":
        cmd += ["--ridge-events", case["curve"]]
    else:
        cmd += ["--review-windows", case["windows"], "--source-step", "2", "--defer-validation"]
    if case["smooth_track_step"]:
        cmd += ["--smooth-track-step", str(case["smooth_track_step"])]
    if case["reported_motion"]:
        cmd += ["--reported-motion"]
    if case.get("reconstruct_track"):
        cmd += ["--reconstruct-track"]
    key = {"audio_sha256": case["audio_sha256"], "tracks_sha256": sha(case["tracks"]),
           "spectrum_sha256": sha(case["spectrum"]), "curve_or_windows_sha256": sha(case.get("curve") or case["windows"]),
           "executable_sha256": sha(binary), "command": cmd}
    directory.mkdir(parents=True, exist_ok=True)
    previous = directory/"evaluation-inputs.json"
    scan = directory/"scan.json"
    if scan.exists():
        m = read(scan)
        if m.get("state_mask") != "TX" or m.get("recording", "").removesuffix("_1") != case["recording"]["id"].removesuffix("_1") or m["clock_window_s"] != case["clock_window"] or m["grid_m"] != case["grid"]:
            raise ValueError("existing search does not match declared full-state protocol: " + str(directory))
        if previous.exists() and read(previous) != key:
            raise ValueError("changed inputs; use a new output directory: " + str(directory))
        key["reuse_completed_run_from_this_evaluation"] = True
        key["command_is_proposed_replay_not_original_producer"] = True
        key["executable_hash_is_for_proposed_replay"] = True
        save(directory/"reused-inputs.json", key)
        code = 0
    else:
        save(previous, key)
        print("Searching", name, flush=True)
        with (directory/"run.log").open("w") as log:
            code = subprocess.run(cmd, stdout=log, stderr=subprocess.STDOUT).returncode
    result = {"id": name, "status": "complete" if code == 0 and scan.exists() else "incomplete_search",
              "statewide_search_run": True, "exit_code": code, "scan": str(scan), "protocol": case["protocol"]}
    save(out/"outcomes"/(name+".json"), result)
    print(name, result["status"], flush=True)
    return result


def summarize(out):
    protocol = read(out/"protocol.json")
    context = read("work/personal-samples-2026-11-08/receiver-context.json")
    rows = []
    for case in protocol["cases"]:
        path = out/"outcomes"/(case["id"]+".json")
        result = read(path) if path.exists() else {"status": "pending"}
        rid = case["recording"]["id"]
        key = "park" if rid.startswith("walking_") or any(t in rid for t in ["17_38_51", "18_04_04", "18_05_46"]) else "desk"
        truth = [context[key]]
        if "11_07_15_01_53" in rid:
            truth = [context["desk"], context["park"]]
        row = {"id": case["id"], "recording": rid, "protocol": case["protocol"], "status": result["status"],
               "method": case["method"], "site_label": "unknown_of_two_sites" if len(truth)>1 else key,
               "location_basis": "user-supplied desk" if key=="desk" else "approximate park centre, not a recorded walking path"}
        if result["status"] == "complete":
            scan = read(result["scan"])
            best = next(iter(scan["training_winner"] or []), None)
            row.update(evaluated_points=scan.get("evaluated_points", len(scan["points"])), elapsed_s=scan["elapsed_s"], scan=result["scan"],
                       training_winner=best, separated_training_peaks=scan["separated_training_peaks"])
            if best:
                distances = [metres(best, t) for t in truth]
                row.update(distance_m=min(distances), distance_max_m=max(distances), distance_miles=min(distances)/1609.344,
                           within_two_miles=max(distances)<=protocol["tolerance_m"], local_checks=best["all_reserved_checks_pass"],
                           within_two_miles_is_not_validated_identity=True)
                row["top_group_offsets_m"] = [{"distance_m": min(metres(p,t) for t in truth), "lat": p["lat"], "lon": p["lon"],
                                               "rank": p["training_rank"]} for p in scan["separated_training_peaks"]]
            else:
                row["status"] = "no_complete_curve_assignment"
        rows.append(row)
    value = {"version": 1, "tolerance_m": protocol["tolerance_m"], "rows": rows,
             "known_location_coordinates_used_by_search": False,
             "note": "Top training winner is evaluated; no substitution by a nearer runner-up. Confidence/identity remain uncalibrated. Approximate walking labels are not surveyed microphone paths. No usable curve or incomplete work is not an accuracy success."}
    save(out/"evaluation.json", value)
    for r in rows:
        print(r["id"], r["status"], f'{r["distance_miles"]:.2f} mi' if "distance_miles" in r else "", flush=True)
    return value


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--out", type=Path, default=Path("work/texas-evaluation-2026-11-08"))
    p.add_argument("--data-root", type=Path, default=Path("work/texas-evaluation-2026-11-08"))
    p.add_argument("--binary", type=Path, default=Path("work/texas-evaluation-2026-11-08/scan-receiver-region-state-v3.bin"))
    p.add_argument("--jobs", type=int, choices=[1,2], default=1)
    p.add_argument("--scoring-workers", type=int, choices=range(1,9), default=4)
    p.add_argument("--prepare-only", action="store_true")
    p.add_argument("--exact-clock", action="store_true", help="sensitivity protocol: assume the filename start is sample-exact instead of allowing six seconds")
    p.add_argument("--reconstruct-track", action="store_true", help="development comparison: reconstruct position/velocity trajectories with fixed measurement/process errors; use a new --out directory")
    p.add_argument("--only", help="comma-separated experiment IDs; preserve full protocol and other results")
    p.add_argument("--summarize-only", action="store_true")
    p.add_argument("--require-accuracy",action="store_true",help="return nonzero unless every recording has a top estimate within two miles")
    args = p.parse_args()
    if args.reconstruct_track and args.out.resolve() == args.data_root.resolve():
        p.error("--reconstruct-track needs a new --out directory to preserve the baseline")
    args.out.mkdir(parents=True, exist_ok=True)
    if args.summarize_only:
        value=summarize(args.out)
        if args.require_accuracy and any(not r.get("within_two_miles",False) for r in value["rows"]):raise SystemExit(1)
        return
    cases = prepare(args.out, args.data_root)
    if args.reconstruct_track:
        for c in cases:
            c.update(reconstruct_track=True, reported_motion=True, smooth_track_step=.25)
            c["protocol"] += "; fixed-uncertainty position/velocity Kalman + RTS reconstruction, no audio/receiver input"
        frozen=read(args.out/"protocol.json");frozen["cases"]=cases;save(args.out/"protocol.json",frozen)
    if args.exact_clock:
        for c in cases:
            c["clock_window"]=0
            c["clock_step"]=1
            c["search_dir"]=str(args.out/"cases"/c["id"])
            c["protocol"]+="; exact filename-start assumption, evaluated as development sensitivity"
            if c["id"]=="nov05-prior-turboprop-curve" and args.out==args.data_root/"exact-clock-sensitivity":
                c["search_dir"]=str(args.data_root/"nov05-exact-clock-statewide")
        frozen=read(args.out/"protocol.json");frozen["cases"]=cases;frozen["timing_assumption"]="filename start treated as exact; sensitivity, not independently calibrated sample-zero time";save(args.out/"protocol.json",frozen)
    if args.prepare_only:
        return
    if args.only:
        selected=set(args.only.split(","));cases=[c for c in cases if c["id"] in selected]
        if len(cases)!=len(selected):raise ValueError("unknown requested experiment")
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
        futures = [pool.submit(run_case,c,args.out,args.binary,args.scoring_workers) for c in cases]
        for f in concurrent.futures.as_completed(futures):
            f.result()
            summarize(args.out)
    value=summarize(args.out)
    if args.require_accuracy and any(not r.get("within_two_miles",False) for r in value["rows"]):raise SystemExit(1)


if __name__ == "__main__":
    main()
