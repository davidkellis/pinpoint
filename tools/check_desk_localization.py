"""Replay a private, regional desk-location development regression."""
import argparse
from datetime import datetime, timedelta
import hashlib
import json
import math
from pathlib import Path
import subprocess
import sys
import time


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def distance(a, b):
    lat1, lon1, lat2, lon2 = map(math.radians, [a["lat"], a["lon"], b["lat"], b["lon"]])
    x = math.sin((lat2-lat1)/2)**2 + math.cos(lat1)*math.cos(lat2)*math.sin((lon2-lon1)/2)**2
    return 12742000 * math.asin(math.sqrt(min(1, x)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path("references/private/desk-localization.json"))
    parser.add_argument("--out", type=Path, default=Path("work/desk-localization-check"))
    parser.add_argument("--control-shifts", default="-480,-240,-120,120,240,480",
                        help="repeat the complete regional search at incorrect UTC times, comma-separated seconds")
    args = parser.parse_args()
    shifts = [] if not args.control_shifts else [float(x) for x in args.control_shifts.split(",")]
    if len(shifts) > 12 or any(not math.isfinite(x) or abs(x) < 60 or abs(x) > 900 for x in shifts):
        raise ValueError("use at most 12 finite control shifts, 60..900 seconds in magnitude")
    m = json.loads(args.manifest.read_text())
    if m["version"] != 1:
        raise ValueError("unsupported fixture version")
    for v in [*m["inputs"].values(), m["observed_curves"]]:
        if digest(v["path"]) != v["sha256"]:
            raise ValueError("fixture changed: " + v["path"])
    # A spectrum is reusable only with a sidecar bound to the pinned original
    # audio. Its measured data are still an input; no synthetic ridge is drawn.
    proof = json.loads(Path(m["spectrum"] + ".inputs.json").read_text())
    if proof["audio_sha256"] != m["inputs"]["audio"]["sha256"]:
        raise ValueError("spectrum does not belong to pinned recording")
    if digest(m["spectrum"]) != m["spectrum_sha256"]:
        raise ValueError("pinned measured spectrum changed")
    args.out.mkdir(parents=True, exist_ok=True)
    cfg = {"terrain_dir": m["terrain_dir"], "geoid_file": m["geoid_file"],
           "recordings": [{"id": m["recording"], "start": m["recording_start"],
                           "audio": m["inputs"]["audio"]["path"], "tracks": m["inputs"]["tracks"]["path"]}]}
    # Receiver truth is deliberately absent from the search configuration.
    config = args.out / "search-config.json"
    save(config, cfg)
    exe = (args.out / "scan-receiver-region.bin").resolve()
    subprocess.run(["go", "build", "-o", str(exe), "tools/scan_receiver_region.go"], check=True)
    o = m["search"]
    cmd = [str(exe), "--config", str(config), "--recording", m["recording"], "--spectrum", m["spectrum"],
           "--ridge-events", m["observed_curves"]["path"], "--bounds", o["bounds"], "--grid", str(o["grid"]),
           "--refine", str(o["refine"]), "--final-grid", str(o["final_grid"]),
           "--clock-window", str(o["clock_window"]), "--clock-step", str(o["clock_step"]),
           "--smooth-track-step", str(o["smooth_track_step"]), "--workers", "4", "--max-points", "6000",
           "--out", str(args.out / "scan")]
    if o["reported_motion"]:
        cmd.append("--reported-motion")
    began = time.monotonic()
    with (args.out / "replay.log").open("w") as stream:
        subprocess.run(cmd, stdout=stream, stderr=subprocess.STDOUT, check=True)
    scan = json.loads((args.out / "scan/scan.json").read_text())
    winner = next(iter(scan["training_winner"] or []), None)
    controls = []
    for shift in shifts:
        control_cfg = {**cfg, "recordings": [dict(cfg["recordings"][0])]}
        control_cfg["recordings"][0]["start"] = (datetime.fromisoformat(m["recording_start"]) + timedelta(seconds=shift)).isoformat()
        stem = f"control-{shift:+g}"
        config_path = args.out / f"{stem}.json"
        save(config_path, control_cfg)
        command = cmd[:]
        command[command.index("--config")+1] = str(config_path)
        command[command.index("--out")+1] = str(args.out / stem)
        with (args.out / f"{stem}.log").open("w") as stream:
            p = subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT)
        control = {"shift_s": shift, "complete": p.returncode == 0, "command": command}
        if p.returncode == 0:
            result = json.loads((args.out / stem / "scan.json").read_text())
            best = next(iter(result["training_winner"] or []), None)
            control["training_winner"] = best
            control["training_outranks_actual_time"] = bool(best and winner and best["training_rank"] >= winner["training_rank"])
        controls.append(control)
        print(stem, "complete" if p.returncode == 0 else "INCOMPLETE", flush=True)
    evaluation = m["evaluation_only"]
    offset = distance(winner, evaluation) if winner else None
    correct_track = bool(winner) and all(q["aircraft_identity"] == evaluation["expected_candidate_icao"] for q in winner["ridge_matches"])
    passed = bool(winner) and offset <= evaluation["max_offset_m"] and correct_track and (winner["all_reserved_checks_pass"] or not evaluation["require_reserved_shape"])
    report = {"status": "pass" if passed else "fail", "offset_from_user_supplied_desk_m": offset,
              "evaluation_only": evaluation, "training_winner": winner,
              "separated_training_peaks": scan["separated_training_peaks"], "elapsed_s": time.monotonic()-began,
              "command": cmd, "manifest": str(args.manifest), "manifest_sha256": digest(args.manifest),
              "spectrum_sha256": digest(m["spectrum"]), "executable_sha256": digest(exe), "scope": m["scope"],
              "note": m["note"], "truth_in_search_configuration": False}
    report["wrong_time_full_search_controls"] = controls
    report["all_controls_complete"] = bool(controls) and all(c["complete"] for c in controls)
    report["controls_note"] = "Each incorrect time repeats all position/clock/source/aircraft selection. These correlated development trials do not calibrate a false-positive probability. GPS regression status and control separation are separate outcomes; passing the location tolerance is not a verified fix."
    save(args.out / "regression.json", report)
    print(report["status"], f"{offset:.1f} m from supplied desk" if offset is not None else "no location")
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
