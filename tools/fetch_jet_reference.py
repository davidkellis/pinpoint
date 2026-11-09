#!/usr/bin/env python3
"""Restore pinned jet WAVs without replacing changed files.

AeroSonicDB is CC BY-NC 4.0; DLR RECORD 139 is CC BY 4.0.
DLR extraction needs h5py/scipy only when a derived WAV is absent.
Go/FFmpeg perform analysis; these Python dependencies only prepare test data.
"""
import hashlib
import io
import json
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def missing(name, digest):
    dest = ROOT / name
    if dest.exists() and hashlib.sha256(dest.read_bytes()).hexdigest() != digest:
        raise ValueError(f'Changed file; refusing to overwrite: {name}')
    return not dest.exists()


def save(name, data, digest):
    if hashlib.sha256(data).hexdigest() != digest:
        raise ValueError(f'Download/extraction checksum mismatch: {name}')
    dest = ROOT / name
    dest.parent.mkdir(parents=True, exist_ok=True)
    with dest.open('xb') as f:
        f.write(data)


class RemoteZip(io.RawIOBase):
    def __init__(self, source):
        self.url, self.size, self.pos = source['links']['self'], source['size'], 0

    def seekable(self):
        return True

    def readable(self):
        return True

    def tell(self):
        return self.pos

    def seek(self, offset, whence=0):
        self.pos = offset if whence == 0 else self.pos + offset if whence == 1 else self.size + offset
        if self.pos < 0:
            raise ValueError('Negative archive offset')
        return self.pos

    def read(self, n=-1):
        n = self.size - self.pos if n < 0 else min(n, self.size - self.pos)
        if n <= 0:
            return b''
        request = urllib.request.Request(self.url, headers={'Range': f'bytes={self.pos}-{self.pos+n-1}'})
        with urllib.request.urlopen(request, timeout=40) as response:
            expected = f'bytes {self.pos}-{self.pos+n-1}/{self.size}'
            if response.status != 206 or response.headers.get('Content-Range') != expected:
                raise ValueError('Archive server did not honor requested byte range')
            data = response.read(n + 1)
        if len(data) != n:
            raise ValueError('Truncated archive range')
        self.pos += n
        return data


def restore():
    selection = json.loads((ROOT / 'references/provenance/aerosonic-selection.json').read_text())
    test_selection = json.loads((ROOT / 'references/provenance/aerosonic-test-selection.json').read_text())
    samples = selection['samples'] + test_selection['samples']
    needed = [r for r in samples if missing(r['audio'], r['sha256'])]
    if needed:
        record = json.loads((ROOT / 'references/provenance/zenodo-10215080.json').read_text())
        source = next(f for f in record['files'] if f['key'] == 'audio.zip')
        with zipfile.ZipFile(RemoteZip(source)) as archive:
            for row in needed:
                member = row['zip_member']
                if f'{archive.getinfo(member).CRC:08x}' != row['zip_crc32']:
                    raise ValueError(f'Archive member CRC changed: {member}')
                # Read only pinned members; never extract arbitrary ZIP paths.
                save(row['audio'], archive.read(member), row['sha256'])
    print(f'Verified {len(samples)} AeroSonicDB clips (member CRC + SHA-256; no whole-archive MD5 assertion).')

    geometry = json.loads((ROOT / 'samples/public/dlr-a320-rec139/geometry.json').read_text())
    needed = [r for r in geometry['microphones'] if missing(r['audio'], r['sha256'])]
    if needed:
        try:
            import h5py
            from scipy.io import wavfile
        except ImportError as e:
            raise SystemExit('Missing DLR WAVs: run this script in a Python environment with h5py and scipy.') from e
        record = json.loads((ROOT / 'references/provenance/zenodo-20494053.json').read_text())
        source = next(f for f in record['files'] if f['key'] == 'REC139.h5')
        cache = ROOT / 'work/jet-reference-source/20494053/REC139.h5'
        if not cache.exists():
            with urllib.request.urlopen(source['links']['self'], timeout=60) as response:
                data = response.read(source['size'] + 1)
            if len(data) != source['size'] or 'md5:' + hashlib.md5(data).hexdigest() != source['checksum']:
                raise ValueError('DLR HDF5 checksum mismatch')
            cache.parent.mkdir(parents=True, exist_ok=True)
            with cache.open('xb') as f:
                f.write(data)
        if 'md5:' + hashlib.md5(cache.read_bytes()).hexdigest() != source['checksum']:
            raise ValueError('Cached DLR HDF5 differs from pinned source')
        with h5py.File(cache) as h:
            for row in needed:
                buffer = io.BytesIO()
                x = h['MicrophoneData/microphoneDataPa'][row['index_zero_based'], :]
                wavfile.write(buffer, geometry['sample_rate_hz'], x)
                save(row['audio'], buffer.getvalue(), row['sha256'])
    print('Verified 3 DLR channels (one flight, not three independent flyovers).')


if __name__ == '__main__':
    restore()
