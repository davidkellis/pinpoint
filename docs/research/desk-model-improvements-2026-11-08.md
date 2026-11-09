# Desk model experiments, November 8, 2026

Private-case details (aircraft, timings, coordinates) are kept in the gitignored references/private/ directory.

The clear November 5 desk curve produces a training winner **315.8 m from the
user-supplied desk** in a supplied region approximately 40 km wide, with zero
clock correction. The search selects one aircraft from the supplied Texas tracks.
All six complete searches at incorrect recording times are weaker on training.
Nearby alternatives still fit almost as well. This is conditional development
evidence, not a unique verified fix, blind state-wide localization or successful
localization of all desk files.

The report with maps, full-curve overlays, ablations and controls is at
`work/desk-model-improvements-2026-11-08/index.html`:

```sh
just sample-audit-serve
# Open http://127.0.0.1:8081/desk-model-improvements-2026-11-08/
just desk-localization-check --out work/desk-location-new
just test
```

The research server may already be running. Go performs geometry, interpolation,
fitting, assignment and scoring. FFmpeg decodes audio. Python verifies inputs,
drives experiments and renders the report. These experiments do not yet replace
the website's automatic analysis flow.

## Changes and the failure they address

The former regional spectral score could optimize a bright 40-second fragment
of a longer sound. On this case it preferred points about 1.8 km from the desk.
A quarter-second FFT also has 4 Hz bin spacing, coarse for a low propeller tone.
The whole-file two-second FFT has 0.5 Hz bins and retains the rest of the
recording for wrong-time comparisons.

`FitJointRidges` fits every supplied measured curve across its complete observed
extent. All curves share one receiver/path and clock. Thresholded bipartite
matching assigns distinct aircraft and minimizes the worst normalized training
residual. Source frequency, optional bounded source drift and aircraft assignment
use training blocks. Reserved ridge residuals cannot select a replacement. They
remain descriptive because acoustic discovery can already use the full recording.

New imports retain ground speed, true track and geometric climb rate in optional
CSV columns. Old five/six-column CSV files remain readable. Units and flags follow
the primary [readsb JSON specification](https://github.com/wiedehopf/readsb/blob/dev/README-json.md).
Ground track is used rather than heading; missing geometric climb never falls
back to barometric climb. Optional reported-motion Hermite interpolation preserves
recorded nodes, long gaps and acceleration limits. It remains experimental and
is not automatically enabled in every website search.

`enrich_track_motion.go` attaches cached motion only when aircraft identity/type,
absolute timestamps (5 ms) and positions (1 m) agree. Original track IDs, timestamps,
positions and heights remain unchanged. Receiver/audio/desired GPS do not enter
the join. Per-source hashes and unmatched counts are retained. Trace base epochs
can differ, so ICAO and relative timestamps alone are insufficient join keys.

```sh
just enrich-track-motion \
  --config work/overnight-improvements-2026-11-08/desk-config.json \
  --cache-root work/sample-audit-data/cache/traces \
  --out work/track-motion-enrichment-new
```

Regional screening uses conservative complete-segment time/region bounds before
interpolation. It retains intervening points/gaps and segments crossing the region
whose endpoints lie outside it. A bounded worker pool evaluates independent
receivers against immutable spectra/trajectories. Guided recognition now tests
a competing linear pitch change on the same audio for long recordings as well
as short clips; distant background and steady tones alone cannot exclude a chirp.
Existing detection thresholds were not relaxed to make desk hypotheses pass.

## Measured outcomes

Offsets are from the user-supplied desk coordinate (private). Small and wide
searches have different grid origins/bounds. All variants are preserved, including
ones with worse geographic agreement. These are development comparisons.

| Model | Winning offset |
| --- | ---: |
| Spectral fragments, two-second FFT, small region | 1,798 m |
| Complete curve, linear positions | 550 m |
| Complete curve, position-derived Hermite | 244 m |
| Complete curve, reported-motion Hermite | 318 m |
| Reported motion with bounded source drift allowed | about 850 m |
| Measured sub-bin local peaks, one tone | 349 m |
| Measured local peaks, up to four harmonics | 318 m |
| Complete curve, reported-motion Hermite, 40 km region | **316 m** |

Reported motion does not improve every residual; interpolation still changes the
optimum. Flexible source pitch and harmonic measurements did not consistently
improve geographic agreement. The latter found multiple harmonics at 48 of 111
sample times. Harmonics are correlated measurements of one sound, not extra
aircraft. Unsupported measurements remain absent and changed observations do not
inherit earlier quality labels.

The wide search evaluates 3,314 points at progressive 1,000 → 100 → 20 m spacing,
with one shared ±6 s clock grid in 1 s steps. Training/reserved ridge RMSE is
approximately **0.442/0.749 Hz**. Incorrect start times are ±120, ±240 and ±480 s;
each repeats the complete regional search. Some wrong times pass local shape
checks, but none outranks the actual time on training. These correlated trials
do not calibrate a false-positive probability. Grid spacing is not accuracy.

Initially, the acoustic rules selected three long strong intervals: source events
9338, 13973 and 42752. Requiring all three with distinct aircraft and one clock
produced **zero complete assignments** in the small region. The final single
curve (#13973) had already been investigated at the known receiver, so interval
selection is assisted development work. Its location search excludes the exact
desk GPS and a fixed aircraft ID; the region and selected sound remain supplied
priors.

## All six stationary desk recordings

Fresh whole-recording checks use a two-second FFT, 40–800 Hz, half-second hops,
0.5 Hz source sweep, reported-motion interpolation, linear-tone comparisons and
the supplied receiver. These are recognition diagnostics, not GPS inference.
Provisional support appears in three of the six recordings (four supported
windows in total); the other three have none in this profile. Two of the
supported aircraft belong to the same recording and are predicted to arrive far
apart in time at the supplied receiver. This is a useful candidate constraint,
not a confirmed two-aircraft location. The whole-schedule audit still fails: its
training-selected pair does not validate and a wrong schedule has a larger
reserved margin. The second aircraft lacks complete comparable coverage in the
cohort across every schedule shift, so it cannot be declared validated by that
audit or substituted using reserved scores. Missing support establishes neither
aircraft absence nor acoustic silence. Low-band checks supplement the earlier
high-frequency quarter-second jet checks.

## Regression and sources

`references/private/desk-localization.json` and
`references/fixtures/desk-nov05-ridge.json` pin this private development case.
The checker verifies original audio, enriched tracks, measured spectrum and
audio-bound cache sidecar. Receiver truth is absent from the Go configuration;
Python measures its distance only after the training winner is frozen. The
750 m regression tolerance catches the earlier neighborhood-scale failure. It
is not an accuracy guarantee or confidence radius. Existing unresolved multi-event
reference policies remain unchanged.

The experiment's `provenance.json` links source archive, executable/source/input
hashes, outputs and per-recording enrichment provenance. The latter links every
raw trace used. It is indexed under
`references/private/provenance/desk-model-improvements-2026-11-08.json`.

Original private audio stays in `samples/`. Previously acquired ADSB.lol traces
stay in `work/sample-audit-data/cache/traces/YYYY-MM-DD/`. Original CSV paths are
recorded in `desk-enriched-config.json` and enrichment provenance. Terrain/geoid
are `work/web/cache/terrain` and `work/web/cache/egm96_15.gtx`. No new FR24 data
were downloaded and no original audio was edited or uploaded.

Final Go race tests, explicit helper tests and vet pass, as do the four existing
private recovery fixtures and this regional replay. The public first-pass
detector is unchanged; the real-curve acoustic tests still pass.
