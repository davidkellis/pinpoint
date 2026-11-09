#!/usr/bin/env python3
"""Run Go location experiments, then evaluate against separate reference labels."""
import argparse
import concurrent.futures
import fcntl
import hashlib
import json
import math
import subprocess
import shutil
import time
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from audit_samples import sha, write


def distance(a, b):
    a, b, c, d = map(math.radians, (*a, *b))
    h = math.sin((a-c)/2)**2 + math.cos(a)*math.cos(c)*math.sin((b-d)/2)**2
    return 6371000*2*math.asin(min(1, math.sqrt(h)))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--audit', type=Path, default=Path('work/sample-audit-2026-11-07'))
    p.add_argument('--workers', type=int, choices=[1, 2], default=1)
    p.add_argument('--scoring-workers', type=int, default=12)
    p.add_argument('--single-binary', type=Path, help='Preserved executable for the unchanged single-passage protocol')
    p.add_argument('--summarize-only', action='store_true', help='Collect completed searches and disclosed reference diagnostics without launching inference')
    args = p.parse_args()
    audit = args.audit.resolve()
    root = Path('.').resolve()
    private_reference=root/'work/desk-flights/config.json'
    desk_coordinate=None
    if private_reference.exists():
        label=json.loads(private_reference.read_text())
        desk_coordinate=(label['lat'],label['lon'])
    auto_binary = root / 'bin/locate-sample'
    saved_binary = audit/'executables'/(sha(auto_binary)+'.bin')
    saved_binary.parent.mkdir(exist_ok=True)
    if not saved_binary.exists():
        shutil.copy2(auto_binary,saved_binary)
    auto_binary=saved_binary
    single_binary = args.single_binary.resolve() if args.single_binary else auto_binary
    attempted, active = set(), {}
    results = {}

    def summarize(r):
        reference=None
        diag=audit/r['id']/'reference-area-diagnostic'
        for filename in ['run/automatic.json','run/results.json']:
            path=diag/filename
            if path.exists():
                v=json.loads(path.read_text())
                reference=dict(status=v['status'],scope='Known-reference feasibility diagnostic, not blind localization',report_url=f'{r["id"]}/reference-area-diagnostic/run/'+('automatic.html' if filename.endswith('automatic.json') else 'map.html'))
                if 'groups' in v:
                    reference.update(groups=len(v['groups']),training_fits=sum(g.get('training_candidate') is not None for g in v['groups']),reserved_passes=sum(g.get('passes_validation',False) for g in v['groups']))
                fetch=audit/r['id']/'statewide-tracks/fetch.json'
                if fetch.exists():
                    f=json.loads(fetch.read_text())
                    reference['missing_traces']=len(f['stats'].get('missing_traces') or [])
                break
        variants = []
        for name in ['location-conditional', 'location-statewide']:
            out = audit / r['id'] / name
            outcome = out / 'outcome.json'
            if not outcome.exists():
                continue
            try:
                v = json.loads(outcome.read_text())
            except json.JSONDecodeError:
                continue  # A Go result is still being written.
            entry = dict(experiment=name, outcome=v['status'], mode=v.get('mode'),
                         report_url=f'{r["id"]}/{name}/' + ('automatic.html' if (out/'automatic.json').exists() else 'map.html'))
            candidates = v.get('candidates') or []
            if (out/'automatic.json').exists():
                a = json.loads((out/'automatic.json').read_text())
                entry['group_count'] = len(a['groups'])
                entry['controls'] = len(a['options'].get('control_shifts_s') or [])
                if a.get('selected_group_id') is not None:
                    g = a['groups'][a['selected_group_id']]
                    candidates = [g['training_candidate']]
                    entry['passes_reserved_validation'] = g['passes_validation']
            if candidates:
                entry['best'] = candidates[0]
                if r['family'] == 'pathak':
                    b = candidates[0]
                    entry['within_publisher_city_region'] = 37.5 <= b['lat'] <= 37.85 and -122 <= b['lon'] <= -121.6
                    entry['exact_gps_evaluable'] = False
                elif r['family'] == 'personal' and not r['id'].startswith('walking') and desk_coordinate is not None:
                    b = candidates[0]
                    entry['best_position_error_m'] = distance((b['lat'], b['lon']), desk_coordinate)
                    entry['within_250m_of_user_reference'] = entry['best_position_error_m'] <= 250
                    entry['reference_basis'] = 'User-reported desk coordinate, evaluation only; not surveyed'
                entry['ambiguity'] = v.get('ambiguity', '')
            fetch = audit / r['id'] / 'statewide-tracks/fetch.json'
            if name == 'location-statewide' and fetch.exists():
                f = json.loads(fetch.read_text())
                entry['missing_traces'] = len(f['stats'].get('missing_traces') or [])
                entry['data_scope'] = f['scope']
            variants.append(entry)
        if variants:
            primary = variants[-1]
            status = primary['outcome']
            if r['family'] == 'pathak':
                if primary['outcome'] == 'candidate_matches':
                    status = 'regional_agreement_unverified' if primary.get('within_publisher_city_region') else 'outside_publisher_region'
                note = 'Publisher supplies Livermore city only; exact microphone GPS is unpublished. Clock is inferred from publisher UTC frequency data. '
                note += 'Full-state competing-flight search.' if primary['experiment']=='location-statewide' else 'Supplied-aircraft conditional diagnostic; independent competing-flight evaluation is pending or unavailable.'
                fetch=audit/r['id']/'statewide-tracks/fetch.json'
                if primary['experiment']!='location-statewide' and fetch.exists() and json.loads(fetch.read_text()).get('error'):
                    note=note.replace('pending or unavailable','unavailable in this run ('+json.loads(fetch.read_text())['error']+')')
            elif r['id'].startswith('walking'):
                status = 'moving_receiver_screening_only'
                note = 'One-sound stationary-model screening is recorded. The tool does not reconstruct a walking route or establish the park location.'
            else:
                note = 'Texas search without the receiver coordinate as input. '
                if primary.get('best_position_error_m') is not None:
                    note += f'Top position is {primary["best_position_error_m"]:.0f} m from the user-reported desk. '
                    if not primary['within_250m_of_user_reference']:
                        status='outside_known_reference'
                if primary.get('missing_traces'):
                    note += f'{primary["missing_traces"]} discovered traces are unavailable; coverage is incomplete. '
                note += 'Sound matches and grid spacing do not establish calibrated GPS confidence.'
            value = dict(status=status, note=note, report_url=primary['report_url'], experiments=variants)
            if reference is not None:
                value['reference_diagnostic']=reference
            write(audit/r['id']/'localization.json', value)
            results[r['id']] = value
        elif reference is not None:
            kind='joint group' if 'groups' in reference else 'single-event model'
            value=dict(status='reference_area_no_match' if reference['status']=='no_match' else 'reference_diagnostic_only',note=f'No passing {kind} near the user-reported location in the disclosed reference-area diagnostic. A full state-wide ranking was not run; this is not a recovered location.',statewide_search_status='not_run',reference_diagnostic=reference,report_url=reference['report_url'])
            write(audit/r['id']/'localization.json',value)
            results[r['id']]=value
        elif r['id'].startswith('walking'):
            value=dict(status='moving_receiver_not_supported',note='Fresh complete-audio analysis found a curve proposal. The fixed-position model does not reconstruct the walking route; no location is established.',statewide_search_status='not_run')
            write(audit/r['id']/'localization.json',value)
            results[r['id']]=value

    def run(r, variant, tracks):
        directory=audit/r['id']/variant
        directory.mkdir(exist_ok=True)
        with (directory/'run.lock').open('a') as lock:
            fcntl.flock(lock,fcntl.LOCK_EX)
            return run_locked(r,variant,tracks)

    def run_locked(r, variant, tracks):
        directory = audit / r['id'] / variant
        directory.mkdir(exist_ok=True)
        automatic = r['family']=='personal' and r['detection']['strong_curves']>=2 and not r['id'].startswith('walking')
        binary = auto_binary if automatic else single_binary
        cmd = [str(binary), '--analysis', str(audit/r['id']/'analysis'), '--out', str(directory), '--workers', str(args.scoring_workers)]
        inputs = dict(helper_sha256=sha(binary), analysis_inputs=sha(audit/r['id']/'analysis-inputs.json'))
        if r['family'] == 'pathak':
            folder = root / r['audio']
            folder = folder.parent
            csv = folder/'frequency_vs_time_used.csv'
            kml = next(folder.glob('*.kml'))
            cmd += ['--csv', str(csv), '--kml', str(kml), '--track-id', folder.name.split('_')[0]]
            inputs.update(csv_sha256=sha(csv), kml_sha256=sha(kml))
        else:
            import re
            text = re.search(r'20\d\d_\d\d_\d\d_\d\d_\d\d_\d\d', r['audio']).group()
            start = datetime.strptime(text, '%Y_%m_%d_%H_%M_%S').replace(tzinfo=ZoneInfo('America/Chicago')).isoformat()
            method = 'automatic' if r['detection']['strong_curves'] >= 2 and not r['id'].startswith('walking') else 'single'
            cmd += ['--state', 'TX', '--recording-start', start, '--clock-uncertainty', '30', '--method', method, '--audio', str(root/r['audio'])]
            inputs.update(recording_start=start, method=method)
        if tracks:
            cmd += ['--tracks', str(tracks)]
            inputs['tracks_sha256'] = sha(tracks)
        key = directory/'inputs.json'
        if key.exists() and json.loads(key.read_text()) == inputs and (directory/'outcome.json').exists():
            return r, variant, True
        print('Locating', r['id'], variant, flush=True)
        write(key, inputs)
        with (directory/'run.log').open('w') as log:
            result = subprocess.run(cmd, cwd=root, stdout=log, stderr=subprocess.STDOUT)
        if result.returncode and not (directory/'outcome.json').exists():
            write(directory/'outcome.json', dict(status='error', error=(directory/'run.log').read_text()[-3000:]))
        print('Finished', r['id'], variant, result.returncode, flush=True)
        return r, variant, not result.returncode

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        while True:
            report = json.loads((audit/'report.json').read_text())
            pending = False
            external=set()
            for marker in audit.glob('*/location-*/external-process.json'):
                job=json.loads(marker.read_text())
                status=Path(f'/proc/{job["pid"]}/stat')
                if status.exists() and status.read_text().split(') ',1)[1][0]!='Z':
                    external.add((marker.parent.parent.name,marker.parent.name))
                else:
                    marker.unlink()
            for r in report['recordings']:
                directory = audit/r['id']
                directory.mkdir(exist_ok=True)
                if r['analysis_status'] not in ['complete', 'error']:
                    pending = True
                    continue
                if r['family'] == 'aerosonic':
                    value = dict(status='gps_not_evaluable', note='Publisher GPS is deliberately displaced; filename time zone is unverified and no trajectory accompanies these clips. Aircraft-presence/background labels assess detection only.', labeled_aircraft_present=r.get('presence_label',{}).get('class')=='1')
                    write(directory/'localization.json',value)
                    results[r['id']]=value
                    continue
                if r['family'] == 'dlr':
                    value=dict(status='gps_not_evaluable',note='Seven-second A320 array passage in a local Cartesian frame with unpublished GPS origin. These three channels are one flyover, not three independent localization tests.')
                    write(directory/'localization.json',value)
                    results[r['id']]=value
                    continue
                if r['family'] == 'colsto':
                    value=dict(status='unresolved', note='Fresh full hybrid scan; previous completed standard-profile automatic sequence experiment remains unresolved. Its known-area search is not blind statewide localization.', report_url='../sequence-search/reference-revalidated/automatic.html', previous_experiment='work/sequence-search/reference-revalidated/automatic.json')
                    write(directory/'localization.json',value)
                    results[r['id']]=value
                    continue
                if r.get('detection',{}).get('strong_curves',0)==0:
                    value=dict(status='no_usable_curve',note='Fresh complete-audio scan found no automatically strong predictive curve. The program cannot localize this recording; this is not evidence of no aircraft.')
                    write(directory/'localization.json',value)
                    results[r['id']]=value
                    continue
                options=[]
                if r['family']=='pathak':
                    options.append(('location-conditional',None))
                tracks=directory/'statewide-tracks/tracks.csv'
                if tracks.exists():
                    options.append(('location-statewide',tracks))
                else:
                    fetch=directory/'statewide-tracks/fetch.json'
                    if not fetch.exists():pending=True
                    elif json.loads(fetch.read_text()).get('error'):
                        if r['family']!='pathak':
                            value=dict(status='flight_data_unavailable',note=json.loads(fetch.read_text())['error'])
                            write(directory/'localization.json',value);results[r['id']]=value
                for variant, tracks in options:
                    if args.summarize_only:
                        continue
                    key=(r['id'],variant)
                    if key in external:
                        pending=True
                        continue
                    if key not in attempted and len(active)+len(external)<args.workers:
                        attempted.add(key)
                        future=pool.submit(run,r,variant,tracks)
                        active[future]=key
                    elif key not in attempted:pending=True
                summarize(r)
            for future in list(active):
                if future.done():
                    r,_,_=future.result()
                    del active[future]
                    summarize(r)
            write(audit/'locations-report.json',dict(version=1, note='Reference coordinates and city labels are evaluation-only. Conditional supplied-aircraft fits and independent competing-flight searches are reported separately; exact GPS is unavailable for public recordings.', recordings=results))
            if args.summarize_only or not pending and not active:break
            time.sleep(5)


if __name__=='__main__':
    main()
