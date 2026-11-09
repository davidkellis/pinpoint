# Jet curve experiments — November 5, 2026

For the later Challenger/ERJ-145 samples and the actual competing-flight
California replay, see [additional jet samples](additional-jet-samples.md).
That replay ranks the correct Challenger second, below a wrong location, and
is preserved as a failing localization challenge.

Jets are already included in flight matching by default. The new work concerns
finding their acoustic tones: the standard 2/4-second FFTs and 30-second shape
criterion can miss faster or shorter sweeps. A loud jet recording does not
necessarily contain a sustained tone suitable for this geolocation method.

## Implementation and current result

An **opt-in experimental short-pass profile** runs the Go detector at 16,384 Hz,
with 0.5/1-second FFTs, 100–6,500 Hz coverage, frequency-bin-aware ridge linking,
4-second proposal and 6-second shape minima. It retains the contrast, coverage,
held-out prediction, harmonic/resolution corroboration and CPA uncertainty
checks. Exported ridges retain 0.125-second spacing. Faster sampling and a wider
band increase cost; this is not automatically added to every long web upload.
The standard website workflow and propeller detector remain unchanged.

`strong` remains an acoustic evidence tier, not aircraft identification. Two
engine tones or harmonics from one jet must not become two independent flights.
Neither method may infer recorder position from a broadband loudness peak alone.

Two labeled **development** cases now exercise actual jet audio:

| Case | Evaluation reference | Standard result | Short-pass result |
|---|---|---|---|
| AeroSonicDB A320 | 8 manually annotated raw-spectrogram points on the upper descending branch, 22–50 s | No strong curve | All 8 points recovered; 1.79 Hz RMSE |
| Pathak N394FX/R20, E55P | All 235 publisher frequency samples, audio-aligned +5.05 s | No continuous ridge recovered more than 30% of the published trace; a different tone was rated strong | 78.7% of points matched; 81.3% coverage; 2.22 Hz RMSE on covered points |

The A320 annotations were made during development, not by an independent
annotator. The Phenom alignment was estimated using the published trace and the
audio, without geography or a flight trajectory. Labels only enter evaluation,
not automatic curve extraction. These are regression gates, not a measured
generalization/recall rate. The Phenom result is explicitly **partial** recovery.

Five selected AeroSonicDB airliner types were exercised: A320, B738, E190, A333
and B77W. Short-pass still found no strong curve for the selected B738, and the
standard mode did better for the selected B77W. Three publisher-verified
background clips returned zero strong curves. They last only ten seconds, so
they meaningfully exercise short-pass but cannot validate the standard detector's
30-second criterion. More background and aircraft sessions are needed before
enabling this mode automatically on noisy walks.

## Location experiment

The jet location regression re-runs automatic extraction on N394FX/R20, selects
its single strong curve with usable CPA timing, and searches all California
using the publisher's **supplied target KML**. The three latitude bands collectively
cover the state. Telemetry corridors eliminate places that could not hear this
known aircraft. Terrain and EGM96 undulation put observer and aircraft heights
on a consistent ellipsoidal datum. Grid refinement proceeds 200 → 50 → 25 →
12.5 → 10 m.

The frozen replay evaluated 5,703 locations and returned **37.667340,
−121.790936**, in the reported Livermore area. It fitted a −4.4 s correction to
the inferred audio clock. The regression checks the broad Livermore region;
that region is reserved for evaluation and not supplied to the search.

This is an **assisted known-aircraft test**, not a blind California search:

- Only the supplied aircraft is present; competing flights have not been tested.
- The publisher does not supply verified microphone GPS or a verified WAV
  sample-zero UTC epoch. The recording clock is inferred from frequency labels.
- The external Livermore coordinates documented in
  [sources-and-validation.md](sources-and-validation.md) are model estimates,
  not ground truth. No metre-level location error is asserted here.
- Ten-metre grid spacing is search sampling, not ten-metre accuracy. A single
  pass may leave mirrored or otherwise similar locations even with a known track.

## Data and attribution

| Source | Local files | What can be verified |
|---|---|---|
| [AeroSonicDB YPAD-0523 v1.1.2](https://zenodo.org/records/10215080), Blake Downward; [paper with Jon Nordby](https://arxiv.org/abs/2311.06368), **CC BY-NC 4.0** | `samples/public/aerosonic-ypad0523/`; pinned selection and metadata in `references/provenance/` | Aircraft presence/type, audible interval, background labels. Coordinates are **deliberately displaced** nearby public locations; not exact recorder GPS. Full trajectories are not included in the selected metadata. |
| [DLR RECORD 139](https://zenodo.org/records/20494053), Timo Schumacher and Henri Siller, **CC BY 4.0** | `samples/public/dlr-a320-rec139/`; source HDF5 in `work/jet-reference-source/20494053/` | Exact local positions of 231 microphones, three aircraft position/velocity references and sound speed 339.89 m/s. Unknown GPS origin/UTC epoch in inspected assets. |
| [Pathak 2026](https://zenodo.org/records/23424278), **CC BY 4.0** | `samples/public/pathak-2026/N394FX_R20/` | Real jet audio, publisher UTC frequency trace, target trajectory with EGM96 heights, reported city. [FAA RVSM approvals](https://www.faa.gov/sites/faa.gov/files/2026-04/NAARMO_US_IGA_RVSM_Approvals_Part_135_91_Section_3.pdf) identify N394FX as E55P. |

AeroSonicDB selection was frozen before inspecting audio: first chronological
training clip at least 45 seconds long per chosen type, and first training
background clip at each of the three sites. Original WAV bytes are preserved.
ZIP members were fetched using HTTP ranges and verified with CRC32 and pinned
SHA-256. The full 1.17 GB archive was not downloaded; its MD5 is **not** claimed
verified. The third background clip's filename site suffix differs from its CSV
site field; both original fields are preserved.
The upstream `locations.json` also lacks its final closing brace and spells
latitude `lattitude`; original verified bytes are preserved, not silently repaired.

The complete DLR HDF5 was downloaded and its published MD5 verified. Channels
80 (nearest array origin), 221 (minimum x), and 222 (maximum y) were selected
before listening, exported as unchanged float32 pressure samples at 48,192 Hz.
Each is seven seconds, and all share one flight. The tested center channel
currently yields **no usable Doppler curve**, so this is a pending positioning
benchmark, not a successful localization. Trajectory sample-time conventions,
airframe source offsets and wind require confirmation before interpreting a
precise local-coordinate fit.

The [source index](../../references/sources.json),
[recording catalog](../../references/catalog.json),
[jet suite](../../references/jets.json), pinned source metadata and baseline
reports preserve provenance. Audio retains its upstream license; in particular,
do not treat AeroSonicDB's noncommercial license as the project's code license.

## Reproduce

Run from the project root, with Go and FFmpeg available:

```sh
# Two labeled positive cases, three background gates, five observational cases.
just jet-reference-check

# Re-extract curves and repeat the assisted California location regression.
# Requires the existing California terrain and EGM96 caches, pinned by the suite.
just jet-reference-check --suite location

# Analyze another recording with the additional experimental mode.
just jet-analyze --audio recording.wav --out work/my-jet
xdg-open work/my-jet/curves.svg
```

`spectrogram.svg` is the measured spectrum without overlays; `curves.svg` adds
accepted/review measured ridges. `detection.json` contains every proposal,
rejection/evidence tier and fit. No aircraft identity or GPS is asserted by
`jet-analyze`. `just reference-check` continues to exercise the original
YouTube and propeller regressions.

The current before/after figure is `work/jet-reference-results/comparison.png`.
Recreate the standard baseline with `just experiment --config
examples/jet-baseline.json --out work/jet-reference-baseline --workers 2`, run
`jet-analyze` for A320 into `work/jet-reference-results/a320` and R20 into
`work/jet-reference-results/phenom-r20`, then run
`python3 tools/plot_jet_comparison.py` in an environment with NumPy, SciPy and
Matplotlib. The figure uses the same raw spectrogram in each column; only the
detector overlays change. Baseline metrics are preserved in
`references/provenance/jet-standard-baseline.json`.

To regenerate the regional map from the supplied jet track:

```sh
just jet-analyze --audio samples/public/pathak-2026/N394FX_R20/audio.wav \
  --out work/jet-reference-results/phenom-r20
python3 tools/prepare_jet_location.py
just locate --config work/jet-location/search-band-1.json --out work/jet-location/results-band-1
just locate --config work/jet-location/search-band-2.json --out work/jet-location/results-band-2
just locate --config work/jet-location/search-band-3.json --out work/jet-location/results-band-3
xdg-open work/jet-location/results-band-2/map.html
```

`just jet-reference-fetch` verifies existing WAVs and restores missing ones from
pinned sources. If the DLR derived WAVs are missing, use a Python environment
with `h5py` and `scipy` for extraction (approximately 312 MB download). These
are data-preparation dependencies only. `just reference-fetch` restores the
Pathak files. Changed inputs fail rather than being overwritten.

## Next evidence needed

Precise GPS validation still needs synchronized jet recordings with a published
recorder latitude/longitude/elevation, a clock uncertainty, complete nearby
flight tracks, and permission to redistribute those references. No inspected
dataset yet supplies all of these. DLR can supply local-coordinate validation
if a usable tone and the trajectory conventions are established. AeroSonicDB
supplies much better aircraft/background coverage but cannot substantiate
metre-level GPS claims. Use held-out recording sessions for the next evaluation;
do not treat additional channels or tones from the same flight as new trials.

## Additional test sessions — November 5, 2026

Eight more original jet WAVs and two background controls have been downloaded
from the publisher's **test split**, across sessions 22, 24, 25 and 26. The
selection was frozen before inspecting audio: first filename for each of eight
specified type/site combinations, plus first test background at sites 0 and 2.
No detector thresholds were changed for this evaluation.

| Aircraft | Registration | Public site | Strong ridges | Review ridges |
|---|---|---|---:|---:|
| A350-900 (A359) | 9V-SHK | 0 | 1 | 0 |
| A321neo (A21N) | ZK-NNF | 0 | 0 | 0 |
| Fokker 100 (F100) | VH-FKF | 0 | 0 | 1 |
| Pilatus PC-24 | VH-FMP | 0 | 1 | 0 |
| Boeing 737-800 (B738) | VH-IJQ | 0 | 0 | 1 |
| Airbus A320 | VH-YXU | 2 | 14 | 4 |
| Boeing 737-700 (B737) | VH-NBV | 2 | 9 | 6 |
| Embraer 190 | VH-UYW | 2 | 3 | 0 |
| Two no-aircraft controls | — | 0, 2 | 0 each | 0 each |

These are **ridge counts, not aircraft counts or recognition accuracy**. In
particular, the A320 and B737 have multiple tonal branches in a single pass.
Five clips have at least one strong ridge; this is not a 5/8 recall estimate:
presence labels do not tell us which recordings contain an extractable curve,
and these new curves do not yet have independent frequency annotations. The
regression runner records eight observations and two asserted background passes.

The published nearby public coordinates are site 0: **−34.85364, 138.67517**,
and site 2: **−34.96428, 138.51011**. Their unknown privacy displacement prevents
metre-level error scoring. No precise recorder GPS or complete flight tracks
were added by this batch. Filename times remain timezone-unverified. The site-2
background filename has a site-1 suffix; the original CSV site field and filename
are both preserved. See the [dataset description](https://zenodo.org/records/10215080)
and [collection paper](https://arxiv.org/abs/2311.06368).

Files and reproducibility:

- Original WAVs: `samples/public/aerosonic-ypad0523/`.
- Complete selection, source rows and hashes:
  `references/provenance/aerosonic-test-selection.json`.
- Frozen evaluation: `references/jets-test-baseline-2026-11-05.json`.
- Review plots and individual detections: `work/jet-additional/`;
  overview: `work/jet-additional/jet-spectrograms.png`.
- Re-run with `just jet-test-check`. This uses `references/jets-test.json`;
  `just jet-reference-fetch` now restores/verifies all 18 AeroSonicDB clips.

All files retain **CC BY-NC 4.0**, credited to Blake Downward, with the paper by
Downward and Nordby. As before, selected member CRC32 and SHA-256 are verified;
the full archive MD5 has not been verified.

### Other sources investigated

The following leads did not supply an additional ready-to-run, precise GPS jet
benchmark in the inspected public material:

- [Rotterdam ground-effect study](https://doi.org/10.1007/s13272-026-00947-1):
  real flyovers, microphone arrays and ADS-B, but no raw-audio dataset link found.
- [NASA Learjet flyover study](https://ntrs.nasa.gov/api/citations/20230006356/downloads/AIAA_2023_Learjet_Paper_AlmostFinal_5.pdf):
  promising synchronized acoustic and positioning measurements; no downloadable
  raw waveform archive identified. A paper alone is not a runnable fixture.
- [AeroLoop baseline](https://zenodo.org/records/17765718): audio and presence
  labels, but no recorder GPS/trajectory reference in the inspected record.
- [Lisbon Finals](https://www.lisbonfinals.com/research.html) and
  [TrueNoise](https://truenoise.org/data.html): position/flight and noise-level
  observations; no raw waveform export identified on their data pages.
- [Freesound, Mount Rundle recording](https://freesound.org/people/IESP/sounds/337709/):
  coarse location and aircraft audio, but no verified jet type, UTC or trajectory.
  Its stated weekday/date are inconsistent. Not admitted as a jet GPS fixture.

No data requests were sent to authors. These are documented leads, not claimed
downloaded datasets or proof that additional data cannot exist.

### New indoor recording

The November 5 desk recording was reported by the user as **indoors and
stationary**. The full approximately 2 h 47 min file was scanned in 21 sections
of up to 600 seconds with 120-second overlaps. Standard 2/4-second analysis and
experimental 0.5/1-second analysis were both run without detector changes.
Chunked whitening can differ from whole-recording whitening, and overlapping
sections or harmonics must not be counted as separate aircraft.

Two low-frequency intervals are priority review candidates. Both pass
the standard acoustic checks and have bounded fitted closest-approach times;
the second is also partly recovered in short-pass. A further standard candidate
has unbounded closest-approach timing. The complete standard
scan returned three strong and 69 review ridge proposals; short-pass returned
eleven strong and 175 review proposals, many at higher frequencies. These
counts are not aircraft counts, and no flight identity or location is established.

Review page: `work/nov05-indoor/index.html`. It contains raw and background-removed
plots, original-audio playback links, and original-file timestamps. Full scan
configuration, source SHA-256, runner and results are in that directory; the
recording is also listed as unlabeled in `references/catalog.json`.
Open with `just experiment-open work/nov05-indoor/index.html`.

The MP3 produces an FFmpeg invalid-packet warning near the end, and its container
duration is bitrate-estimated. The final decoded section reaches 10039.375 s
against a reported 10039.455 s. Do not infer subsecond UTC accuracy or an exact
recording start from its filename. No ADS-B search was performed in this review.
Original personal media remains local and is not authorized for redistribution.

### Follow-up: compare desk recordings with flight telemetry

After the user supplied a desk coordinate and confirmed filenames as Central
recording start times, all five distinct November 4/5 desk recordings were
compared against nearby ADS-B.lol trajectories. The identical `desk_` alias was
counted once. Existing standard strong/review ridges were tested for all files;
November 5 additionally used the short-pass candidates. This does not constitute
an exhaustive aircraft inventory or a fresh short-pass scan of November 4.

The first of the two priority November 5 sweeps has a strong association with
**one tracked turboprop aircraft (aircraft A)**. The known-position model
fits at zero clock adjustment, with 0.49 Hz RMS error, versus 2.51 Hz for the
next-best available flight. Fitting one half and predicting the other gives
0.53 Hz RMS in either direction. Twelve wrong-time controls (±5 to ±30 minutes)
are worse, at 1.42–6.71 Hz. These small diagnostics do not establish a calibrated
identification probability. Aircraft A is a turboprop, so this is useful personal
recording evidence but not a successful personal jet identification.

The second November 5 sweep and tested November 4 candidates did not produce a
similarly convincing association. Incomplete/missing traces, source RPM changes
and missed acoustic ridges remain possible; this is not proof of no aircraft.
No silence-based exclusions were applied to these unlabelled indoor recordings.

Private input coordinates, original audio hashes, normalized competing tracks,
source trace snapshot, all ranked fits, holdout checks and plots are saved under
`work/desk-flights/`. Open `just experiment-open work/desk-flights/index.html`.
This is a **known-position association**, not a blind geolocation regression.
No personal audio or location was published.

Two implementation findings:

- The actual tracked trajectory exceeds the ideal-curve measurement's upper bounds
  for both closest range and speed, so those bounds are too
  narrow for this association. At the time of this diagnostic, `JointEvents`
  copied those measurement bounds, although the original partial-event proposal
  had broader limits. The subsequent production correction and replay are
  documented below.
- A synthetic forward emission-to-arrival check exposed dropped predictions
  at telemetry vertices because float64 Unix timestamps can be less precise
  than the solver's fixed 0.1-microsecond tolerance. `Track.Received` now allows
  timestamp precision at bracket endpoints, retains the tighter interior
  tolerance, and permits enough bounded iterations. The new epoch regression,
  track/search race tests and analytic clock-recovery check verify the fix.

### Regional replay and frozen desk regression (November 5, 2026)

`whole-recording-v9` changes automatic search constraints, without relaxing the
detector's acoustic evidence thresholds. The ideal straight-flight fit remains
visible as a measurement. Its fitted speed, distance, frequency and tight CPA
range are no longer hard exclusions for actual turning/climbing trajectories.
Automatic stationary selection keeps the proposal's broad physical limits and
allows delayed CPA anywhere in the observed complete curve. Real telemetry,
ridge agreement, spectral scoring, and the shared recording clock still decide
the matches. Reviewed configurations can explicitly set tighter limits. This
trades some pruning efficiency for avoiding demonstrated false exclusions; it
does not establish calibrated parameter confidence intervals.

The private regression uses a float32, 4096 Hz excerpt of the original recording
that includes the automatically detected curve.
The excerpt was selected after the known-position diagnostic, so this is a
development replay of a known event, not an untouched whole-recording test.
Fresh 2 + 4 second analysis recovers the strong curve. Six previously measured
ridge samples are frozen as regression anchors; they are not independent
aircraft-identity labels.

The normal Go location engine searched a regional rectangle in Texas, with the
filename-derived start time, ±30 seconds clock uncertainty, terrain/geoid data,
and all 49 available competing track segments in the event's time window.
Those tracks came from the earlier buffered regional discovery, whose extent
covers this search plus the configured 15 km acoustic range. Coverage is still
limited by the provider and one unavailable trace in the larger recording's
download. The search received neither the desk coordinate nor the expected
aircraft ID. It used a 200 m initial grid, followed by 50, 25, 12.5 and 10 m
refinement of competing peaks.

| Replay | Outcome |
| --- | --- |
| Old ideal-fit bounds, same audio/flight inputs | No match; 49,434 positions evaluated |
| Broader automatic bounds | Aircraft A first; 96,435 positions evaluated |
| Highest-scoring position | Approximately 19.5 km from the user-reported desk |
| Near-desk support | A sampled point about 20 m from the desk scores about 0.59 dB below the winner |

**This identifies a strong aircraft association, not a unique recorder
position.** The near-desk sample is supporting evidence, not a 20 m accuracy
claim. The final 10 m grid likewise does not imply 10 m accuracy. Geographic
ambiguity checks now inspect the full score surface, including alternative
locations for the same aircraft; grouping by flight ID previously hid this
failure. The generated map now explicitly reports the ambiguity.

The private suite fails if it loses the observed curve, ranks another aircraft
first, loses near-tied support within 250 m of the desk, or stops reporting the
distant ambiguity. These acceptance limits were frozen after this development
replay. The actual winning-position error is reported separately. Coordinates
appear only in evaluation labels, never in search configuration or detection.
Aircraft association remains inferred rather than independently confirmed.

```sh
just desk-reference-check                       # fresh audio + regional replay
just desk-reference-check --suite audio         # faster detection-only check
just map --results work/desk-regression/regional-result
```

The recipe requires private local assets and is separate from the public CI
suite. `work/desk-regression/suite.json` pins audio, competing tracks, original
trace, terrain, geoid and search configuration hashes; `check/all.json` records
the tested Go source hash and decoder version. The original frozen expectations
are also retained as `frozen-suite-2026-11-05.json`. `regional-auto.json` has no
event annotations: the reference runner regenerates them from audio. Map and
curve comparisons are in `regional-result/`; the counterfactual using the old
bounds is in `legacy-result/`. Personal coordinates/audio remain local.

A Texas-wide search has not been run for this regression. Expanding the region
cannot eliminate the competing positions already present in this regional
search. The next localization experiment should seek a second independently
supported flyover at the same stationary receiver or independently calibrated
recording timing, then test whether shared constraints resolve these peaks.
The unmatched later sweep should not be forced into a joint aircraft model,
and detector misses indoors should not be labeled as silence. Jet recognition
remains a separate validation task; this tracked-flyover case is a turboprop.
