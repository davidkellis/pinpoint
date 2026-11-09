#!/usr/bin/env python3
"""Inventory/deduplicate local audio and orchestrate Go analysis, without DSP.

Location labels are evaluation metadata only. Each location experiment writes a
separate localization.json in its case directory; this report never supplies
expected coordinates to the inference commands.
"""
import argparse
import concurrent.futures
import csv
import hashlib
import html
import json
import subprocess
import threading
import time
from datetime import datetime, timezone
from pathlib import Path


def sha(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def write(path, value):
    path = Path(path)
    temp = path.with_suffix(path.suffix + '.tmp')
    temp.write_text(json.dumps(value, indent=2) + '\n')
    temp.replace(path)


def inventory(root):
    catalog = json.loads((root / 'references/catalog.json').read_text())
    known = {r.get('sha256'): r for r in catalog['recordings'] if r.get('sha256')}
    with (root / 'samples/public/aerosonic-ypad0523/sample_meta.csv').open() as f:
        aero = {r['filename']: r for r in csv.DictReader(f)}
    items, by_hash = [], {}
    for p in sorted((root / 'samples').rglob('*')):
        if not p.is_file() or p.suffix.lower() not in {'.mp3', '.wav', '.flac', '.m4a', '.ogg', '.webm'}:
            continue
        digest = sha(p)
        if digest in by_hash:
            by_hash[digest]['aliases'].append(str(p.relative_to(root)))
            continue
        probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'quiet', '-show_entries', 'format=duration', '-of', 'json', str(p)]))
        label = known.get(digest, {})
        family = 'personal'
        if 'pathak-2026' in p.parts:
            family = 'pathak'
            name = 'pathak-' + p.parent.name
        elif 'aerosonic-ypad0523' in p.parts:
            family = 'aerosonic'
            name = 'aerosonic-' + p.stem
        elif 'dlr-a320-rec139' in p.parts:
            family = 'dlr'
            name = 'dlr-' + p.stem
        elif '4CxVxuMVkMc' in p.name:
            family, name = 'colsto', 'colsto-reference'
        else:
            name = p.stem
        r = dict(id=name, audio=str(p.relative_to(root)), sha256=digest,
                 bytes=p.stat().st_size, duration_s=float(probe['format']['duration']),
                 aliases=[], family=family, reference=label,
                 analysis_status='not_run', localization={'status': 'not_tested', 'note': 'No location experiment completed yet.'})
        if family == 'aerosonic':
            r['presence_label'] = aero.get(p.name, {})
        items.append(r)
        by_hash[digest] = r
    return sorted(items, key=lambda r: (r['duration_s'], r['id']))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--root', type=Path, default=Path('.'))
    p.add_argument('--out', type=Path, default=Path('work/sample-audit-2026-11-07'))
    p.add_argument('--workers', type=int, choices=range(1, 5), default=2)
    p.add_argument('--report-only', action='store_true')
    args = p.parse_args()
    root = args.root.resolve()
    out = (root / args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    binary = root / 'bin/pinpoint'
    binary_sha = sha(binary)
    decoder = subprocess.check_output(['ffmpeg', '-version'], text=True).splitlines()[0]
    items = inventory(root)
    lock = threading.Lock()

    def refresh():
        rows = []
        for r in items:
            loc = out / r['id'] / 'localization.json'
            if loc.exists():
                r['localization'] = json.loads(loc.read_text())
            reviewed=out/r['id']/'reviewed-evaluation.json'
            if reviewed.exists():
                r['reviewed_evaluation']=json.loads(reviewed.read_text())
            rows.append(r)
        report = dict(version=1, generated_utc=datetime.now(timezone.utc).isoformat(),
                      binary_sha256=binary_sha, decoder=decoder,
                      file_count=sum(1 + len(r['aliases']) for r in items), unique_audio_count=len(items),
                      note='All audio analysis/search is performed by Go. Python inventories files, runs commands and renders evaluation metadata. Proposals are not identified aircraft. Location truth is never supplied by this report to inference. Aliases and three DLR microphones must not inflate independent-success counts.',
                      recordings=rows)
        write(out / 'report.json', report)
        body = []
        esc = lambda x: html.escape(str(x))
        for r in rows:
            d, loc = r.get('detection', {}), r['localization']
            link = f'{r["id"]}/analysis/events.json'
            extra=r.get('reviewed_evaluation')
            extra_link=f'<br>Reviewed reference replay: {extra["comparison_distance_m"]:.1f} m from the video-reported coordinate · <a href="{esc(extra["report_url"])}">map</a>. This uses reviewed sound windows.' if extra else ''
            body.append(f'<tr><td>{esc(r["audio"])}<br><small>{r["duration_s"]:.1f} s · {esc(r["family"])}{(" · aliases: " + esc(", ".join(r["aliases"]))) if r["aliases"] else ""}</small></td><td>{esc(r["analysis_status"])}<br>{d.get("strong_curves", "—")} strong / {d.get("fragments", "—")} proposals<br><a href="{esc(link)}">Measured curves</a> · <a href="{esc(r["id"])}/analysis/spectrogram.svg">Spectrogram</a></td><td>{esc(loc["status"])}<br>{esc(loc.get("note", ""))}' + (f'<br><a href="{esc(loc["report_url"])}">Location experiment</a>' if loc.get('report_url') else '') + extra_link + '</td></tr>')
        (out / 'index.html').write_text('<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>All sample recordings: detection and localization</title><style>body{background:#17282d;color:#e0eded;font:15px system-ui;line-height:1.5;max-width:1500px;margin:24px auto;padding:20px}a{color:#81d8c6}table{width:100%;border-collapse:collapse}td,th{text-align:left;vertical-align:top;padding:12px;border-bottom:1px solid #476066}small{color:#b2c4ca}</style><h1>Sample detection and localization audit</h1><p>' + esc(report['note']) + f'</p><p>{report["file_count"]} audio files · {len(items)} unique recordings. <a href="report.json">Complete report, input hashes and scope</a></p><table><tr><th>Recording</th><th>Audio analysis</th><th>Localization evaluation</th></tr>' + ''.join(body) + '</table></html>')

    def analyze(r):
        directory = out / r['id']
        directory.mkdir(exist_ok=True)
        saved = directory / 'analysis-inputs.json'
        key = dict(audio_sha256=r['sha256'], binary_sha256=binary_sha, decoder=decoder, profile='hybrid')
        detection = directory / 'analysis/detection.json'
        cached = saved.exists() and json.loads(saved.read_text()) == key and detection.exists()
        start = time.monotonic()
        with lock:
            r['analysis_status'] = 'running'
            refresh()
            print('Analyzing', r['id'], flush=True)
        if not cached and not args.report_only:
            command = [str(binary), 'analyze', '--audio', str(root / r['audio']), '--detector-profile', 'hybrid', '--out', str(directory / 'analysis')]
            with (directory / 'analysis.log').open('w') as log:
                result = subprocess.run(command, cwd=root, stdout=log, stderr=subprocess.STDOUT)
            r['analysis_command'] = command
            if result.returncode:
                with lock:
                    r['analysis_status'] = 'error'
                    r['analysis_error'] = (directory / 'analysis.log').read_text()[-3000:]
                    refresh()
                return
            write(saved, key)
        with lock:
            if detection.exists():
                d = json.loads(detection.read_text())
                r['analysis_status'] = 'complete'
                r['reused_analysis'] = cached
                r['detection'] = dict(version=d['version'], fragments=len(d['events']),
                                      strong_curves=d.get('strong_curve_count', 0), review_curves=d.get('review_curve_count', 0),
                                      bounded_cpa_curves=d.get('joint_curve_count', 0))
            else:
                r['analysis_status'] = 'not_run'
            r['analysis_seconds_this_run'] = time.monotonic() - start
            refresh()
            print(r['id'], r['analysis_status'], r.get('detection', {}), flush=True)

    with lock:
        refresh()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        for result in pool.map(analyze, items):
            pass
    print('Audit:', out / 'index.html', flush=True)
    return any(r['analysis_status'] != 'complete' for r in items)


if __name__ == '__main__':
    raise SystemExit(main())
