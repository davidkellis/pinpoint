#!/usr/bin/env python3
"""Restore the eight public reference passages from pinned Zenodo v1 bytes.

No third-party code is executed. Existing differing files are never replaced.
"""
import hashlib
import io
import json
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def restore():
    manifest = json.loads((ROOT / "samples/public/pathak-2026/manifest.json").read_text())
    suite = json.loads((ROOT / "references/suite.json").read_text())
    assets = {a["path"]: a["sha256"] for a in suite["assets"] if a["source"] == "pathak-2026"}
    missing = []
    for path, expected in assets.items():
        p = ROOT / path
        if p.exists():
            if hashlib.sha256(p.read_bytes()).hexdigest() != expected:
                raise ValueError(f"existing file differs; refusing to replace: {path}")
        else:
            missing.append(path)
    if not missing:
        print(f"All {len(assets)} public reference files already verified.")
        return
    record = json.loads((ROOT / "references/provenance/zenodo-23424278.json").read_text())
    source = next(f for f in record["files"] if f["key"] == manifest["archive"])
    with urllib.request.urlopen(source["links"]["self"], timeout=120) as response:
        raw = response.read(source["size"] + 1)
    if len(raw) != source["size"] or hashlib.md5(raw).hexdigest() != manifest["archive_md5"]:
        raise ValueError("download differs from pinned Zenodo archive")
    # Read only explicitly enumerated members; never extract arbitrary ZIP paths.
    with zipfile.ZipFile(io.BytesIO(raw)) as z:
        pending = []
        for path in missing:
            p = Path(path)
            member = "V_constant_H_constant/" + p.parent.name + "/" + p.name
            data = z.read(member)
            if hashlib.sha256(data).hexdigest() != assets[path]:
                raise ValueError(f"member checksum mismatch: {member}")
            pending.append((ROOT / path, data))
        for dest, data in pending:
            dest.parent.mkdir(parents=True, exist_ok=True)
            with dest.open("xb") as f:
                f.write(data)
    print(f"Restored {len(missing)} verified public reference files (Pathak, CC BY 4.0).")


if __name__ == "__main__":
    restore()
