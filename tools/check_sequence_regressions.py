#!/usr/bin/env python3
"""Check frozen historical failures and evaluate freshly computed Go reports.

This driver performs no acoustic fitting or location search. Expected positions
and aircraft identities are read only after inference has produced its report.
"""
import argparse
import hashlib
import json
import math
from pathlib import Path


def distance(a, b):
    lat1, lon1, lat2, lon2 = map(math.radians, (*a, *b))
    x = math.sin((lat2 - lat1) / 2) ** 2 + math.cos(lat1) * math.cos(lat2) * math.sin((lon2 - lon1) / 2) ** 2
    return 6371000 * 2 * math.asin(min(1, math.sqrt(x)))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest', default='references/sequence-failures.json')
    p.add_argument('--reference-report', default='work/sequence-search/reference-revalidated/automatic.json')
    p.add_argument('--desk-report', default='work/sequence-search/desk-revalidated/automatic.json')
    p.add_argument('--out', default='work/sequence-search/regression-check.json')
    args = p.parse_args()
    manifest = json.loads(Path(args.manifest).read_text())
    checks, errors = [], []
    for case in manifest['baselines']:
        raw = Path(case['report']).read_bytes()
        saved = json.loads(raw)
        ok = hashlib.sha256(raw).hexdigest() == case['sha256'] and saved['status'] == case['status']
        checks.append({'case': case['id'], 'historical_snapshot_unchanged': ok})
        if not ok:
            errors.append(case['id'] + ': historical failure was altered')
    for name, filename in [('reference', args.reference_report), ('desk', args.desk_report)]:
        r = json.loads(Path(filename).read_text())
        policy = manifest['new_sequence_policy'][name]
        ok = r['status'] in policy.get('allowed_unresolved', [policy.get('required_status')])
        changed = False
        if name == 'reference' and r['status'] == 'candidate_sequence':
            q = policy['conditional_promotion_requires']
            g = r['groups'][r['selected_group_id']]
            b = g['training_candidate']
            aircraft = {e['track_id'].split('-')[0] for e in b['events']}
            separation = r.get('separation_db')
            ok = (len(g['event_indices']) >= q['minimum_events'] and g['passes_validation']
                  and all(c['pass'] for c in g['event_checks'])
                  and set(q['required_aircraft']).issubset(aircraft)
                  and len(r['options']['control_shifts_s']) >= q['minimum_controls']
                  and (r.get('maximum_wrong_time_validation_margin_db') is None
                       or separation is not None and separation >= q['minimum_margin_db'])
                  and distance((b['lat'], b['lon']), q['evaluation_coordinate']) <= q['maximum_evaluation_distance_m'])
            changed = True
        checks.append({'case': name, 'new_status': r['status'], 'pass': ok,
                       'historical_outcome_changed_with_additional_evidence': changed,
                       'report_sha256': hashlib.sha256(Path(filename).read_bytes()).hexdigest()})
        if not ok:
            errors.append(name + ': new result violates the frozen evaluation policy')
    result = {'status': 'fail' if errors else 'pass', 'checks': checks, 'errors': errors,
              'note': 'Development regression checks. Conditional promotion is unverified; no microphone GPS was supplied to the reference search.'}
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))
    return bool(errors)


if __name__ == '__main__':
    raise SystemExit(main())
