#!/usr/bin/env python3
"""Restore two additional CC BY 4.0 Pathak jet passages from pinned bytes.

Existing differing files are never overwritten. Reads only listed ZIP members.
"""
import hashlib
import json
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def restore():
    selection = json.loads((ROOT / 'references/provenance/pathak-additional-jets-selection.json').read_text())
    missing = []
    for asset in selection['assets']:
        path = ROOT / asset['path']
        if not path.is_relative_to(ROOT / 'samples/public/pathak-2026') or '..' in path.parts:
            raise ValueError('Invalid asset path')
        if path.exists():
            if hashlib.sha256(path.read_bytes()).hexdigest() != asset['sha256']:
                raise ValueError(f'Existing asset differs: {path}')
        else:
            missing.append(asset)
    if not missing:
        print(f"Verified all {len(selection['assets'])} additional jet assets.")
        return
    record = json.loads((ROOT / 'references/provenance/zenodo-23424278.json').read_text())
    source = next(f for f in record['files'] if f['key'] == selection['archive'])
    dest = ROOT / 'work/public-flyover-source' / source['key']
    if not dest.exists():
        with urllib.request.urlopen(source['links']['self'], timeout=120) as response:
            raw = response.read(source['size'] + 1)
        if len(raw) != source['size'] or hashlib.md5(raw).hexdigest() != selection['archive_md5']:
            raise ValueError('Download differs from pinned archive')
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(raw)
    raw = dest.read_bytes()
    if len(raw) != source['size'] or hashlib.md5(raw).hexdigest() != selection['archive_md5']:
        raise ValueError('Cached archive differs from pinned archive')
    pending = []
    with zipfile.ZipFile(dest) as archive:
        for asset in missing:
            data = archive.read(asset['member'])
            if hashlib.sha256(data).hexdigest() != asset['sha256']:
                raise ValueError('ZIP member checksum mismatch')
            pending.append((ROOT / asset['path'], data))
    for path, data in pending:
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open('xb') as output:
            output.write(data)
    print(f'Restored {len(pending)} verified assets (Praveen Pathak, CC BY 4.0).')


if __name__ == '__main__':
    restore()
