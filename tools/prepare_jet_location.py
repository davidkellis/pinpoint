#!/usr/bin/env python3
"""Prepare an assisted jet location experiment, not a blind ADS-B search.

The supplied target KML identifies the aircraft. Only automatically accepted
audio ridges enter scoring. The publisher's frequency labels supply an inferred
sample-zero clock alignment, not a verified recorder clock or location.
"""
import argparse
import csv
import datetime as dt
import json
import math
from pathlib import Path
import struct
import xml.etree.ElementTree as ET


def geoid_reader(path):
    raw = path.read_bytes()
    lat, lon, dy, dx, rows, cols = struct.unpack('>ddddii', raw[:40])
    if len(raw) != 40 + 4 * rows * cols or min(dx, dy) <= 0:
        raise ValueError('Invalid GTX geoid')

    def height(latitude, longitude):
        x, y = (longitude - lon) / dx, (latitude - lat) / dy
        if not (0 <= x <= cols - 1 and 0 <= y <= rows - 1):
            raise ValueError('Outside geoid')
        i, j = min(int(x), cols - 2), min(int(y), rows - 2)
        value = 0.0
        for b in range(2):
            for a in range(2):
                sample = struct.unpack_from('>f', raw, 40 + 4 * ((j + b) * cols + i + a))[0]
                if not math.isfinite(sample) or abs(sample) > 150:
                    raise ValueError('Invalid geoid sample')
                value += sample * (x - i if a else 1 - x + i) * (y - j if b else 1 - y + j)
        return value
    return height


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--analysis', type=Path, default=Path('work/jet-reference-results/phenom-r20'))
    parser.add_argument('--out', type=Path, default=Path('work/jet-location'))
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    geoid = Path('work/web/cache/egm96_15.gtx')
    height = geoid_reader(geoid)
    alignment = json.loads(Path('references/provenance/phenom-alignment.json').read_text())
    epoch = dt.datetime.fromisoformat(alignment['inferred_wav_start_utc'].replace('Z', '+00:00')).timestamp()
    kml = Path('samples/public/pathak-2026/N394FX_R20/N394FX-track-EGM96.kml')
    ns = {'k': 'http://www.opengis.net/kml/2.2', 'g': 'http://www.google.com/kml/ext/2.2'}
    tree = ET.parse(kml)
    times, points = tree.findall('.//k:when', ns), tree.findall('.//g:coord', ns)
    if len(times) != len(points):
        raise ValueError('KML times/coordinates differ')
    track = args.out / 'supplied-target.csv'
    with track.open('w') as f:
        writer = csv.writer(f)
        writer.writerow(['track_id', 'time_unix_s', 'lat', 'lon', 'alt_ellipsoid_m', 'type_code'])
        seen = set()
        for when, point in zip(times, points):
            timestamp = dt.datetime.fromisoformat(when.text.replace('Z', '+00:00')).timestamp()
            if timestamp in seen or not epoch - 120 <= timestamp <= epoch + 180:
                continue
            seen.add(timestamp)
            lon, lat, altitude = map(float, point.text.split())
            writer.writerow(['N394FX', timestamp, lat, lon, altitude + height(lat, lon), 'E55P'])
    detection = json.loads((args.analysis / 'detection.json').read_text())
    events = []
    for event in detection['events']:
        m = event['measurement']
        if event['acoustic_evidence']['level'] != 'strong' or not m['usable_for_joint']:
            continue
        event['partial_flyover'] = False
        for key in ['cpa_s', 'cpa_tolerance_s', 'f0_min_hz', 'f0_max_hz',
                    'min_speed_mps', 'max_speed_mps', 'min_slant_m', 'max_slant_m']:
            event[key] = m[key]
        events.append(event)
    # One source recording contains one aircraft. Do not treat different tones
    # from its engines as independent flyovers.
    if len(events) != 1:
        raise ValueError(f'Expected one usable acoustic event, found {len(events)}; review instead of guessing')
    for i in range(1, 4):
        config = json.loads(Path(f'references/search/california-band-{i}.json').read_text())
        config.update(spectrogram=str(args.analysis / 'spectrogram.bin'), tracks=str(track),
                      recording_start=alignment['inferred_wav_start_utc'], events=events)
        for key in ['spectrogram', 'tracks', 'terrain_dir', 'geoid_grid']:
            config[key] = str(Path(config[key]).resolve())
        (args.out / f'search-band-{i}.json').write_text(json.dumps(config, indent=2) + '\n')
    (args.out / 'protocol.json').write_text(json.dumps({
        'mode': 'assisted known-aircraft location fit', 'search_region': 'California, three latitude bands',
        'known_target': str(kml), 'audio_evidence': 'automatic short-pass detector; one usable ridge',
        'altitude_conversion': 'published EGM96 orthometric altitude + EGM96 GTX undulation',
        'clock': alignment, 'recorder_gps': None,
        'limitations': ['No competing aircraft', 'Inferred recording clock',
                        'City-level recording reference only; no metre-level accuracy assertion']
    }, indent=2) + '\n')
    print(f'Prepared {args.out}/search-band-{{1,2,3}}.json (known target, not blind localization)')


if __name__ == '__main__':
    main()
