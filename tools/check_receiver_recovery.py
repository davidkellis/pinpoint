"""Replay private development scoring fixtures; not aircraft/GPS accuracy tests."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path("references/private/receiver-recovery.json"))
    parser.add_argument("--out", type=Path, default=Path("work/receiver-recovery-check"))
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text())
    if manifest["version"] != 1:
        raise ValueError("unsupported receiver fixture version")
    config_path = Path(manifest["source_config"])
    if digest(config_path) != manifest["source_config_sha256"]:
        raise ValueError("source configuration changed; inspect rather than silently updating expectations")
    source = json.loads(config_path.read_text())
    recordings = {r["id"]: r for r in source["recordings"]}
    args.out.mkdir(parents=True, exist_ok=True)
    executable = (args.out / "review-receiver-audio.bin").resolve()
    subprocess.run(["go", "build", "-o", str(executable), "tools/review_receiver_audio.go"], check=True)
    executable_hash = digest(executable)
    results = []
    for case in manifest["cases"]:
        if Path(case["id"]).name != case["id"]:
            raise ValueError("invalid fixture ID")
        for key in ["audio", "tracks"]:
            if digest(case[key]) != case[key + "_sha256"]:
                raise ValueError(f"altered or unavailable fixture: {case[key]}")
        recording = recordings[case["recording"]]
        if recording["audio"] != case["audio"] or recording["tracks"] != case["tracks"]:
            raise ValueError("fixture/configuration association changed")
        directory = args.out / case["id"]
        directory.mkdir(exist_ok=True)
        config = {**source, **case["receiver"], "recordings": [recording]}
        path = directory / "config.json"
        save(path, config)
        command = [str(executable), "--config", str(path), "--out", str(directory),
                   "--mode", "short", "--short-window", str(case["fft_window_s"])]
        began = time.monotonic()
        with (directory / "replay.log").open("w") as stream:
            subprocess.run(command, check=True, stdout=stream, stderr=subprocess.STDOUT)
        report = json.loads((directory / recording["id"] / "receiver-review-short.json").read_text())
        matches = [m for m in report["matches"] if m["track_id"].split("-")[0] == case["track_prefix"]]
        supported = any(m["additional_support"] for m in matches)
        passed = bool(matches) and supported == case["expected_experimental_support"]
        results.append({"id": case["id"], "passed": passed, "experimental_support": supported,
                        "expected_experimental_support": case["expected_experimental_support"],
                        "modeled_target_windows": len(matches), "elapsed_s": time.monotonic() - began,
                        "command": command, "executable_sha256": executable_hash})
        print(case["id"], "pass" if passed else "FAIL", flush=True)
    result = {"status": "pass" if all(x["passed"] for x in results) else "fail", "cases": results,
              "manifest": str(args.manifest), "manifest_sha256": digest(args.manifest),
              "note": manifest["note"]}
    save(args.out / "regression.json", result)
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
