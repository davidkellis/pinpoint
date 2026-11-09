# Consensus search: curvature and loudness evidence, November 9

## Why single-curve searches cannot localize statewide

One Doppler curve fixes roughly four quantities: source pitch, aircraft speed,
closest-approach time and closest-approach distance. Across a state, many
airborne aircraft can reproduce any one curve from some receiver position
beside their track. The earlier statewide scans show this directly: in the
reviewed November 5 scan, the top eight separated candidates had
worst-normalized residuals between 0.285 and 0.296, and the correct one was
fourth before track reconstruction moved it to first. Choosing a different
"best" single curve cannot fix this, because the problem is missing
information, not curve choice.

A diagnostic at the known desk, used only for diagnosis and never for
scoring, confirmed it for that recording. Of the 13 strong November 5
proposals, only the tracked-flyover curve (#13973) is explained by any flight at the
desk. Eleven are short 4–7 kHz sweeps that no flight explains. The curve the
old automatic policy chose (#42752, the longest) has no aircraft explanation
at the desk, yet it "fits" a distant aircraft statewide at 0.33 Hz.

## Method

`pinpoint consensus` (`just consensus`, and the web app's **Several flyovers
agree** mode) changes three things.

1. **Offer every plausible curve, not one.**
   `acoustic.LocatableCurves` keeps every strong, measured proposal whose
   median pitch is at most 2.5 kHz, up to 16 (the longest if more). Every
   labeled aircraft curve in the public data is at or below 1.3 kHz. These
   include Pathak propellers and jets, the AeroSonicDB A320 and the YouTube
   reference. Atmospheric absorption removes most tonal energy above a few kHz
   over kilometre-scale ranges, so the cap only saves compute.

2. **Require agreement, and measure how much of each curve's bend is
   explained.** At every candidate receiver and shared clock,
   `search.FitRidgeSubset` fits every offered curve against every aircraft in
   range, then picks the best group of at least two non-overlapping curves
   assigned to distinct aircraft. Curves no aircraft explains are left out. A
   fit counts only if all three conditions hold:
   - the curve has real Doppler swing (constant-pitch residual ≥ 3 Hz);
   - its normalized residual is ≤ 1;
   - the aircraft explains most of the bend: Doppler residual ≤ 0.35 × the
     residual of a straight line through the same samples (*unexplained
     curvature*).

   The ranking metric is the group's worst unexplained curvature. This was the
   decisive change. With raw residuals, nearly flat curves (#527: 1.5 Hz total
   swing) and nearly straight ones (#1439) "fit" almost any aircraft, and they
   appeared in nearly every false region. The two true YouTube flyovers have
   ratios of 0.13 and 0.17. `search.Consensus` searches coarse to fine:
   1 km, then 100 m, then 20 m by default, with coarse clock steps followed by
   1 s and 0.5 s clocks around each seed.

3. **Treat silence as evidence.** For each reported region,
   `search.CheckLoudness` lists the aircraft predicted to pass within 4 km
   (segment-accurate closest approach) and measures the recording's 50–2000 Hz
   level rise at each predicted arrival (±10 s against the surrounding ±90 s).
   The whitened spectrogram removes broadband level, so the level comes from a
   separate FFmpeg decode. At the true receiver, closer passes are louder. A
   region is marked *contradicted* when the Spearman correlation between
   proximity and rise is negative, or when at least max(3, 25%) of the passes
   are within 1.5 km and less than 1 dB loud. Contradicted regions never make
   a result ambiguous and are never "resolved".

Results list every separated region (two-mile separation) with its aircraft,
clock and loudness check. They also report a status: *resolved* when the best
non-contradicted runner-up has at least 1.5× the winner's unexplained
curvature, otherwise *ambiguous*, plus an agreement radius. These thresholds
are engineering choices, not calibrated probabilities.

## Results

No receiver coordinates were supplied to any search. Distances were computed
afterwards. Curves were selected automatically from the existing detection
reports, with no reviewed windows.

### YouTube reference (New York, two propeller flyovers)

The detector reports 19 strong proposals across the hour; 16 are offered. The
video gives only the date, so the honest protocol uses the assumed start
20:16:00Z with a ±120 s clock window. The reviewed fit needs +89 s.

| Run | Area | Clock | Status | Top region | Aircraft | Runner-up ratio | Points / time |
| --- | --- | --- | --- | --- | --- | ---: | ---: |
| A0 (sensitivity) | 40 km box | ±6 s around the reviewed offset | resolved | 43 m from the video coordinate | a1dfeb + ace391 (the reviewed pair) | 2.76 | 14,865 / 59 s |
| A | 40 km box | ±120 s | **resolved** | 43 m, clock +90 s | a1dfeb + ace391 | 1.54 | 52,922 / 601 s |
| B | **all of New York** | ±120 s | ambiguous | **35 m**, clock +90 s | a1dfeb + ace391 | 1.35 (27 km away) | 209,123 / 1,475 s |

All three top regions are within 6 m of the earlier reviewed-window fit. In
the statewide run the true area ranks first, but a 27 km alternative (another
aircraft pair) is within the 1.5× resolve threshold. The result is therefore
reported as ambiguous, with all 30 regions listed. In run A, without the
loudness check, a region that loudness contradicts (0.145) would have been
the runner-up and made the result ambiguous.

Earlier iterations on the same data are informative:

- **Raw normalized residuals as the metric:** 10 wrong regions, none within
  2 miles, even with the clock known.
- **±1 coarse step of clock refinement:** missed the truth statewide, because
  a coarse seed's best clock was +84 s and refinement stopped at +88 s.
- **Non-overlapping refinement boxes:** missed the truth statewide, because
  the nearest seed was 1.95 km away and boxes reached only 1 km.

All three fixes are in the code.

### November 5 desk recording (Texas)

Three strong curves have median pitch ≤ 2.5 kHz: #9338, #13973 and #42752.

| Run | Status | Notes |
| --- | --- | --- |
| C: consensus, ≥2 curves, all of Texas, ±6 s | **no match** | 622,363 points, 241 s. No location in Texas has two of the three curves explained by different aircraft under one clock. This is correct: only #13973 is explained by a tracked aircraft at the desk. The previous automatic protocol instead returned a confident location 260 miles away. |
| D: single-curve mode, all of Texas | ambiguous | 709,243 points, 332 s. Thirty near-tied regions span 451 km, and the desk is not among them. One curve cannot localize statewide, whatever the metric. |

### Other stationary desk recordings

| Recording (Central date) | Strong curves ≤ 2.5 kHz | Consensus |
| --- | ---: | --- |
| Nov 4 (first of four) | 1 | too few curves; no search |
| Nov 4 (second of four) | 0 | too few curves; no search |
| Nov 4 (third of four) | 1 | too few curves; no search |
| Nov 4 (fourth of four) | 0 | too few curves; no search |
| Nov 6 | 0 | too few curves; no search |
| Nov 7 (afternoon) | 0 | too few curves; no search |

The previous automatic protocol searched the "longest strong" proposal in each
of these recordings regardless of pitch, and returned locations 10–296 miles
away. Consensus search does not return a location when the recording lacks the
evidence. The park walks are excluded because consensus assumes a stationary
microphone.

## What this means for phone recordings

Statewide localization needs **at least two flyovers in one recording whose
aircraft broadcast ADS-B and whose curves bend clearly** (a pass within a few
km). The method then works without reviewed windows, as the YouTube
reproduction shows. A stationary outdoor recording near a flight path, an hour
or more long, is the most useful next sample. The indoor desk recordings so
far contain at most one such flyover.

The thresholds are uncalibrated engineering choices:

| Setting | Value |
| --- | --- |
| Curvature limit | 0.35 |
| Minimum swing | 3 Hz |
| Resolve ratio | 1.5× |
| Contradiction rule | negative correlation, or ≥ max(3, 25%) close-but-quiet passes |

They were set on the YouTube reference and the November 5 diagnostics, so
those are development cases, not held-out tests. Loudness is weaker indoors
and near other noise sources. A contradicted region is evidence against a
location, not proof.

## Reproduction

The YouTube audio, personal recordings and cached tracks are local only
(gitignored).

```sh
just consensus --detection work/traced-joint/reference/detection.json \
  --tracks work/methodology/ny-tracks-typed.csv --start 2026-09-08T20:16:00Z \
  --clock-window 120 --clock-step 4 --fine-clock-step 1 --state NY --download-terrain \
  --terrain-dir data/terrain-ny-3arc --smooth-track-step 0 --reported-motion=false \
  --reconstruct-track=false --loudness-audio "samples/Unknown location - raw audio [4CxVxuMVkMc].mp3" \
  --out work/consensus-ny

just consensus --detection work/sample-audit-2026-11-07/<nov05-recording>/analysis/detection.json \
  --tracks work/desk-model-improvements-2026-11-08/enriched-tracks/<nov05-recording>/tracks.csv \
  --start 2026-11-05T<HH:MM:SS>-06:00 --state TX --download-terrain --clock-window 6 \
  --loudness-audio samples/<nov05-recording>.mp3 --out work/consensus-nov05
```

The local evaluation drivers are `work/claude-consensus/run_eval.sh` and
`run_desk.sh`. Each output directory holds `consensus.json` (all regions with
aircraft, clocks and loudness checks), `consensus.html`, `regions.geojson` and
`points.jsonl.gz` (every evaluated point).

