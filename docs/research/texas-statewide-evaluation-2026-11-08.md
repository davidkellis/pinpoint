# Full-Texas evaluation, November 8 session

Private-case details (aircraft, timings, coordinates) are kept in the gitignored references/private/ directory.

The current automatic attempts **do not meet the two-mile target**. Across 11
unique Texas phone recordings, 10 automatic primary attempts produced locations;
none of those winners was within two miles of its supplied site. One recording
had no strong curve and produced no location. These are development attempt
outcomes, not a measured aircraft-detection accuracy rate: most proposed sounds
have no independent aircraft labels.

There is one conditional success. The previously reviewed November 5
tracked-flyover curve selects a point **320.5 m (0.20 mi)** from the desk across
Texas when the filename start is treated as exact. With a ±6-second clock
allowance, its winner is **360.36 mi away**. A candidate for the same flyover
312.5 m from the desk remains fourth among the leading aircraft groups in that
run. It is not substituted for the winner.

Results, source details and candidate maps:

- `work/texas-evaluation-2026-11-08/index.html`
- `work/texas-evaluation-2026-11-08/accuracy.json`
- `work/texas-evaluation-2026-11-08/accuracy.csv`
- `work/texas-evaluation-2026-11-08/provenance.json`

```sh
just sample-audit-serve
# http://127.0.0.1:8081/texas-evaluation-2026-11-08/

# Use new result directories; reuse the pinned private corpus and cached data.
just texas-evaluate --out work/texas-recheck
just texas-evaluate --out work/texas-exact-recheck --exact-clock
just texas-evaluation-report

# Return nonzero when the two-mile target is not met; expected to fail now.
python3 tools/evaluate_texas_samples.py --summarize-only --require-accuracy
```

## Frozen winners

Every value below is the winning training-selected location's offset, in miles.
Receiver truth is read only after inference. Exact-start results are a sensitivity
study explored after the initial failures, not an untouched validation set or an
independently calibrated sample-zero timestamp.

| Recording (Central date), case | ±6 s | Exact filename start |
| --- | ---: | ---: |
| Nov 3, park walk | 258.81 | 186.70 |
| Nov 4, desk (first of four) | 158.63 | 158.20 |
| Nov 4, desk (second of four) | No strong curve / no estimate | No strong curve / no estimate |
| Nov 4, desk (third of four) | 9.97 | 10.61 |
| Nov 4, desk (fourth of four) | 213.13 | 295.62 |
| Nov 5, desk, automatic curve choice | 259.62 | 219.51 |
| Nov 6, desk | 210.74 | 232.04 |
| Nov 7 afternoon, one of two sites | 195.18 | 228.14 |
| Nov 7, park propeller case | 241.81 | 219.17 |
| Nov 7, park jet case | 229.56 | 242.51 |
| Nov 7, park walk | 261.60 | 211.94 |
| Nov 5, separately reviewed tracked-flyover interval | 360.36 | **0.20** |
| Nov 5, two reviewed sound intervals | 265.79 | 266.06 |

The two-window winners fail confirmation. Some other distant winners pass the
current local checks, including the ±6 s park jet winner. Those checks therefore
cannot certify GPS confidence after a statewide search.

The November 7 afternoon file's per-file site remains unresolved. Its distance
to both possible sites is checked; the table shows the smaller offset. Both
are far outside the target. Walk references are approximate park centres, not
surveyed microphone trajectories. The threshold is 3,218.688 m.

## Protocol and scope

`protocol.json` freezes case choices before their new statewide GPS evaluation.
Automatic curve cases select the longest independently strong proposal from the
existing complete-recording acoustic analysis, with no GPS selection. This is a
simple declared policy, not a claim that the chosen curve is an aircraft. The
known tracked-flyover interval and two reviewed sounds are separate assisted
cases. The jet repeats the previous trajectory-guided spectral method without a
supplied aircraft identity or acoustic ridge.

All location searches use the Census Texas polygon and all eligible supplied
flight corridors, with 15 km audibility-range screening. Known city/desk/park
bounds are absent. No jet type is filtered out. Statewide acquisition uses the
Texas bounds plus 0.3 degrees of discovery padding. The primary ±6 s scans use
two-second clock steps, with one-second steps for the reviewed tracked-flyover
case. The exact-start sensitivity holds the filename epoch fixed.

Complete-ridge searches use 1,000 → 100 → 20 m grids. Guided jet and reviewed
two-window searches use 3,000 → 500 → 50 m. Twenty separated coarse flight
combinations and ten refined seeds receive denser evaluation. The coarse grid
can miss narrow high-frequency peaks; these are implemented search results,
not proof of the global mathematical optimum. No grid is silently coarsened.

The guided two-window experiment requires distinct aircraft and a common clock.
It retains training-selected spectral windows, which can be shorter than the
full reviewed interval. It is not equivalent to two independently measured,
confirmed complete aircraft curves. The recording remains available for controls.
No-support is not treated as proof of silence or aircraft absence.

## Computation and verification

The statewide extension adds a conservative immutable ECEF index around flight
geometry, state-polygon corridor grids and broader refinement across aircraft
groups. It omits redundant prediction samples during repeated ridge fits.
Training-only guided scans defer expensive confirmation until candidate winners
are frozen; their aircraft, source pitch and window must remain unchanged during
confirmation. Unit tests verify that both optimizations preserve fit/selection
statistics. Four scoring workers per job use immutable spectra and tracks.

All evaluated points are retained in `scan.json` or `points.jsonl.gz`. When
compressed storage is selected, `scan.json` explicitly contains only a map
sample and a separate evaluated-point count. Map thinning does not choose the
winner. Missing terrain is downloaded explicitly, never replaced by a flat
height. One initial missing-tile failure was corrected by acquiring that tile.

Go search/track race tests, spatial-index/display tests, vet and the production
build pass. These checks do not assert that the real statewide accuracy target
passes. Original audio hashes match the cached analyses. The first result
collator stopped on the reviewed recording's `_1` filename alias; identifier
normalization was corrected and the completed search was collected again without
changing its scores. Initial and corrected logs are retained.

## Sources and reproducibility

Private original recordings remain in `samples/`; none was edited or uploaded.
Duplicate audio and the public California/New York examples are excluded from
the Texas cohort. Existing hybrid analyses are reused with their audio-bound
input manifests rather than changing detector thresholds during this test.
Flight tracks come from public ADSB.lol replay indexes and geometric traces;
terrain and geoid data are cached locally. New whole-state trace availability
ranged from about 99.3% to 100% of discovered aircraft/day entries for the
November 7/8 recordings, and missing geometric traces remain a coverage
limitation. This batch uses ADSB.lol, not new FR24 playback capture.
Per-window acquisition metadata, executable/input hashes, source archives and
the accuracy manifest are indexed in the experiment provenance and
`references/private/provenance/texas-statewide-evaluation-2026-11-08.json`.

## Findings

The forward model can explain a real curve near a supplied location while
distant aircraft/location hypotheses explain it almost as well. The near-desk
fit's training residual is about 0.442 Hz, versus 0.428 Hz for the distant
±6 s winner. A 20 m final grid does not resolve that ambiguity.

Clock precision helps the reviewed tracked-flyover case, but the automatic
primary attempts still fail with exact starts. Event classification/selection
and global ambiguity remain substantial problems. Many strong proposals are
unconfirmed, and the automatic longest-curve choice differs from the previously
investigated interval. Joint evidence needs multiple reliable aircraft curves;
the reviewed two-sound experiment does not yet provide a validated pair. These
findings do not establish reliable general aircraft localization from the
current phone corpus.
