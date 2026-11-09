# Additional jet samples and California replay — November 5, 2026

Two more real jet recordings are downloaded, tested, and pinned in the reference
dataset. **They do not yet establish reliable automatic jet geolocation.**
The Challenger's correct aircraft/location combination ranks second in a
California search. The regional jet has no automatically usable curve.

## Sources and observation locations

Both recordings come from [Praveen Pathak's v1 dataset](https://zenodo.org/records/23424278),
licensed CC BY 4.0. The author reports smartphone recordings in **Livermore,
California**, but does not publish verified microphone GPS. This is a city-area
reference, not a surveyed coordinate. Other projects' inferred microphone
positions are not used as truth or as search inputs.

| Passage | Aircraft | Audio | Independent references |
| --- | --- | --- | --- |
| N563FX / R21 | Bombardier Challenger 350, archive ICAO `a732a2` | `samples/public/pathak-2026/N563FX_R21/audio.wav`, about 130 s | 642 publisher UTC frequency samples, supplied KML with EGM96 heights, analysis PDF, Livermore city label |
| N958JX / R50 | Embraer ERJ-145, archive ICAO `ad533d` | `samples/public/pathak-2026/N958JX_R50/audio.wav`, about 47 s | 130 publisher UTC frequency samples, supplied KML with EGM96 heights, analysis PDF, Livermore city label |

These passages were selected by aircraft identity before inspecting detector or
location results. Both selections, including the unsuccessful one, remain in
the dataset. Original WAV, CSV, KML and PDF bytes are unchanged. The 78.5 MB
`Raw data_V_constant_H_varying.zip` archive's published MD5 and every extracted
member's SHA-256 were verified. The frozen member list is
[pathak-additional-jets-selection.json](../../references/provenance/pathak-additional-jets-selection.json).

The [FAA RVSM list](https://www.faa.gov/sites/faa.gov/files/2026-01/NAARMO_US_IGA_RVSM_Approvals_Part_135_91_Section_3.pdf)
lists N563FX as `CL30`; the archived aircraft record describes Challenger 350
and uses `CL35`. Both source values are preserved. No aircraft-type filter was
used to favor the target in the competing-flight search. The archive headers
confirm the two registrations; they are saved in `references/provenance/pathak-r21/`
and `pathak-r50/`.

Frequency labels align to the measured audio with their first samples at +5.000 s
in each WAV. The inferred starts are **2026-05-16 18:23:56.340 UTC** for R21 and
**18:31:19.748 UTC** for R50. The offset was chosen by measured spectral energy
alone, with no flight geometry or recorder position. These inferred clocks are
assisted inputs, not independently verified WAV timestamps. Searches allow ±5 s.

## Original November 5 results

The results below preserve the original baseline. The
[November 6 update](#november-6-tracking-and-fit-diagnostics) records the subsequent
tracking change and replay; the original reports and thresholds remain intact.

All publisher frequency samples are evaluated against **one continuous detected
ridge**. The evaluator cannot join unrelated fragments to pass a flyover.
Labels enter evaluation after extraction; no detector thresholds were changed.

| Case / profile | Publisher samples matched | Coverage | RMSE on covered samples | Strong matching ridge? |
| --- | ---: | ---: | ---: | --- |
| Challenger, standard 2/4 s | 37.5% | 39.3% | 3.45 Hz | No |
| Challenger, short-pass 0.5/1 s | 32.1% | 34.4% | 6.29 Hz | Yes, partial |
| ERJ-145, standard 2/4 s | 6.2% | 36.2% | 29.87 Hz | No |
| ERJ-145, short-pass 0.5/1 s | 16.2% | 18.5% | 1.62 Hz | No |

The Challenger's 3 strong proposals are **not three confirmed aircraft**. For
this explicitly known single-passage benchmark, the longest strong curve with
usable timing is selected by duration, before reading geographic or identity
labels. It spans 40.000–81.125 s. This benchmark policy does not change the web
app's multi-event behavior. Its other tones/fragments are not forced to represent
additional flights. The regional jet produces zero strong/usable proposals.

The comparison figure is `work/additional-jets/comparison.png`. It shows the same
raw spectrum in both columns, with publisher samples in yellow and strong
automatic ridges in cyan. It makes the Challenger's incomplete recovery visible.

### Supplied-track diagnostic

Using the Challenger's longest automatic usable ridge and **only its supplied
KML**, the California search returns **37.658514, −121.783856**, in the reported
Livermore area. It evaluates 39,003 points and refines from 200 m through 50,
25 and 12.5 m to 10 m. This is a passing **known-aircraft, partial-curve** test.
It does not establish exact GPS accuracy or aircraft discrimination.

Using the complete publisher frequency traces instead produces no match under
the current scorer, for either R21 or R50, even with the correct supplied target.
These diagnostic runs are saved under `work/additional-jets/r21/target-full-trace/`
and `work/additional-jets/r50/target-trace-result/`. The cause has not been
isolated; supplied traces are not a guaranteed escape from the current model's
limitations. No thresholds were loosened to make them pass.

### Competing California flights

The [ADS-B.lol daily archive](https://github.com/adsblol/globe_history_2026/releases/tag/v2026.05.16-planes-readsb-prod-0)
was downloaded because the live historical replay endpoint timed out. Both
parts, totaling 3,222,527,488 bytes, passed the publisher's SHA-256 checks.
The imported 18:21–18:35 UTC observations cover padded California bounds and
include 1,004 aircraft. The solver uses 1,834 usable trajectory segments.
These are the aircraft present in this archive, not a completeness claim about
every aircraft actually in the sky.

Only geometric-altitude points enter scoring. The search uses terrain plus
EGM96 geoid heights, actual retarded-time trajectories, the inferred clock,
and the automatically extracted Challenger ridge. It receives **no Livermore
restriction, recorder coordinate, target registration, or supplied KML**.
Twelve latitude bands cover California with a 200 m grid and 10 m final
refinement. In total, 6,445,482 locations are evaluated.

| Global rank | Aircraft ICAO | Predicted recording point | Score |
| --- | --- | --- | ---: |
| 1 | `4851b1` — incorrect aircraft | 32.808195, −116.611038 | 22.07 dB |
| 2 | `a732a2` — actual N563FX | 37.660210, −121.781879 | 21.77 dB |
| 3 | `a55055` — another aircraft | 38.442207, −121.505070 | 21.74 dB |

The correct combination is only **0.30 dB below the wrong winner**. The combined
map explicitly reports ambiguity. This is **not a successful unique location
identification**, even though the true aircraft has a plausible Livermore fit.
The raw scores are not probabilities. Ten-metre grid spacing is not a claim of
ten-metre accuracy. No exact microphone error is calculated.

## Regression gates versus unresolved challenges

- [jets-challenger.json](../../references/jets-challenger.json) freezes the
  presently working **partial** capability: at least 30% anchor hits, 33%
  coverage, strong evidence and at most 8 Hz RMSE; it also repeats the supplied
  target California fit from fresh audio. These deliberately partial baseline
  floors protect observed performance and do not claim whole-flyover recovery.
  The audio gate is added to CI.
- [jets-additional.json](../../references/jets-additional.json) keeps the stricter
  75% hit/coverage targets for both profiles and recordings, plus the Challenger
  competing-flight search and ERJ-145 automatic supplied-track attempt. These
  checks **currently return a nonzero exit code**. The wrong statewide winner
  fails both identity and region checks; ERJ-145 fails before searching because
  there is no usable automatic curve. Failures are retained, not changed into
  expected successes or aircraft-absence labels.

Both manifests pin inputs, configuration and terrain. Tests reanalyze WAVs and
rerun the solver; they do not replay saved results. Independent region/identity
labels are checked only after the search. Frozen baseline reports, including
the failures, are `references/provenance/additional-jets-*-baseline.json`.
The existing YouTube, propeller, jet-positive and background gates remain in place.

## Run and inspect

```sh
# Restore/verify the two new public recordings and accompanying files.
just jet-sample-fetch

# Passing partial-Challenger regression (about 15 seconds here).
just jet-sample-check

# Also repeat the supplied-track location test; needs pinned terrain/geoid caches.
just jet-sample-check --suite all

# Run unresolved detection challenges. A failure exit is the current result.
just jet-challenge-check

# Also re-run the full California competing-flight and ERJ-145 attempts.
just jet-challenge-check --suite all

# Open the combined statewide map generated during this experiment.
just jet-sample-map

# View the measured spectra and publisher/detector overlays.
xdg-open work/additional-jets/comparison.png
```

The checked-in normalized competing tracks are
`references/search/jet-challenger/california-tracks.csv`. They are a derivative
of [ADS-B.lol's ODbL 1.0 database](https://www.adsb.lol/docs/open-data/historical),
separate from the CC BY 4.0 audio. Source release metadata and hashes are pinned
in `references/provenance/adsblol-2026-05-16-release.json`. The local raw archives
are in `data/2026-05-16/`; they are not committed. Terrain is separately cached
in `data/terrain-ca-jets-3arc/` so the older suites' pinned inputs remain intact.

For an independent re-import:

```sh
just fetch --date 2026-05-16 --download --max-gb 4 --out data/2026-05-16
just import --archive 'data/2026-05-16/*.tar.*' \
  --from 2026-05-16T18:21:00Z --until 2026-05-16T18:35:00Z \
  --bounds 32.3,-124.7,42.2,-113.8 --out work/california-jets-reimport.csv
```

The fetch command refuses existing archive parts. Import ordering can vary
because decoding is parallel; the frozen CSV is sorted by track ID and timestamp.
Current input preparation receipts are in `work/additional-jets/`.
`tools/align_frequency_labels.go` reproduces the acoustic-only clock alignment.
`tools/plot_additional_jets.py` recreates the figure with NumPy, SciPy and Matplotlib.
`tools/merge_search_bands.go` combines completed compatible band results into a
single map with global rankings rather than presenting a locally winning band
as the statewide winner.

## What this evidence calls for next

The useful next improvement is retaining a longer coherent jet frequency path
through crossings and noise, while keeping the background controls intact.
This is measurable against the new publisher labels. The full-trace rejection
also needs a separate residual analysis of clock, trajectory, source-frequency
stability and frequency-label error before changing the physical scorer.
Accurate metre-level validation still requires an independently documented
microphone coordinate and recording clock. Neither an inferred Livermore
coordinate nor a finer search grid supplies that missing ground truth.

## November 6: tracking and fit diagnostics

### Implemented and measured

The experimental `short-pass` profile now extends predictive seed curves in
both time directions. A bounded beam keeps alternative paths through frequency
crossings, rewards local spectral contrast, penalizes abrupt slope changes, and
permits brief masked intervals. New observations must be actual spectral peaks;
missing frames are not filled with synthetic measurements. Extension stops at
unsupported gaps and is capped at 60 seconds per direction. The existing whole
curve shape, coverage, held-out prediction and cross-resolution/harmonic checks
still apply. Original candidates remain available if the extension fails.

No aircraft identity, recorder coordinate, flight track or publisher frequency
labels enter this extraction. The new algorithm is a development change tested
on these known cases, not an independently held-out scientific evaluation.

| Challenger short-pass measurement | Before | After |
| --- | ---: | ---: |
| Publisher anchors matched on one ridge | 32.1% | 70.6% |
| Publisher anchor time coverage | 34.4% | 72.7% |
| RMS error on covered labels | 6.29 Hz | 4.48 Hz |
| Longest usable matching curve | 40.000–81.125 s | 37.8125–124.9375 s |

The 75% full-trace challenge still fails, and R50 still has no automatically
usable matching curve. A new `references/jets-tracking.json` regression protects
at least 68% hits, 70% coverage and at most 6 Hz RMS error. It supplements the
unchanged partial baseline and stricter challenge; it does not replace them.
Existing jet positives and background-control assertions are retained. Unlabeled
airliner observations are not counted as detection successes.

There is a measured tradeoff: the A320 keeps 100% anchor hits and coverage, but
its covered-anchor RMSE increases from 1.79 to 2.82 Hz, still within its unchanged
regression limit. Phenom anchor hits rise from 78.7% to 80.0%, with RMSE changing
from 2.22 to 2.19 Hz. A passing suite means the stated tolerances remain satisfied;
it does not mean every numerical metric improves.

Testing this extension in the standard 2/4-second detector moved one YouTube
reference anchor outside its existing tolerance. That variant was not retained.
The web app's standard detector and reference thresholds remain unchanged. The
improved tracker is available through `just jet-analyze` (short-pass v2).

### Localization remains unresolved

With the longer automatic curve and only the supplied target track, the search
still returns Livermore: approximately **37.659053, −121.776669**, from 43,658
evaluated points. The recorder GPS remains unknown, so movement of the estimate
cannot be interpreted as an improvement in metre-level accuracy.

The competing California search evaluates 6,769,423 locations. Its leading
groups are:

| Rank | Aircraft ICAO | Predicted point | Score |
| --- | --- | --- | ---: |
| 1 | `781a1f`, incorrect | 34.012533, −118.450953 | 16.70 dB |
| 2 | `a732a2`, actual N563FX | 37.656033, −121.782390 | 16.33 dB |
| 3 | `a55055`, other aircraft | 38.444587, −121.509687 | 16.10 dB |

The full-resolution diagnostic finds 6.38 Hz RMS frequency error for the wrong
winner and 5.47 Hz for the actual flight. Their median local tone contrasts are
both about 8.9 dB. The brightness scorer therefore favors a slightly different
criterion from frequency residual. These small differences do not establish a
unique location. Rankings are not replaced with a new formula tuned to this
one known example. The failure remains in the challenge suite.

### Inspectable model failures

`just diagnose` now exports `diagnostic.html`, `diagnostic.json` and
`residuals.csv` for a supplied location, clock and exact trajectory ID. It uses
the search scorer to report geometry coverage, CPA/speed/distance exclusions,
frequency residuals and the applicable rejection limit. It plots the observed
and predicted frequencies, residuals over time, and implied source frequency.
It also checks every spectrogram frame at the fixed hypothesis, rather than
only the roughly one-Hz samples used by the search scorer. New map evidence
includes these dense residual and local-contrast metrics in **Compare Doppler
curves**. Existing saved maps need a new search to contain the added metrics.

The diagnostic is per event; it is not a substitute for joint assignment,
silence checks, or a statewide search. Position and clock may already have been
selected using this audio, so the dense check is not independent validation.
Contrast is measured against neighboring frequency bands and is not a calibrated
probability of an aircraft. A synthetic regression confirms that dense residual
checks expose errors occurring between the scorer's one-Hz samples, and that
flat broadband power has zero local tone contrast.

At the earlier R21 supplied-track hypothesis (37.658514, −121.783856; clock
+1.62 s), the complete publisher trace has a scorer RMSE of **26.39 Hz**, above
its **9.96 Hz** limit. At that same *hypothesized* point, R50's full trace has
**5.93 Hz** RMSE against a **2.02 Hz** limit with zero clock correction. Neither
fails the geometry coverage check there. R50 is not known to share R21's
microphone position. These results explain rejection at these hypotheses;
they do not prove engine pitch changes or an incorrect published track.
Clock error, geometry, source pitch variation and label errors remain possible.

A numerical boundary issue was also fixed: a least-squares source frequency
infinitesimally outside a fixed frequency bound no longer leaves the scorer
with no candidates. The bounded frequency is evaluated against the same
residual and signal checks. Diagnostics do not relax acceptance thresholds.

Verification: `go test -race ./...`, `go vet ./...` and the frontend JavaScript
syntax check pass. The full YouTube audio, propeller reference, jet positives,
background controls and private desk regression pass their existing assertions.
An initial web race run timed out; the final complete race run passes, including
the web integration. Challenge failures remain reported separately.

Report generation now uses faster **lossless** PNG compression for spectrum
previews and embedded plots. This changes container size, not pixels or audio
analysis. A slow web integration run exposed substantial work in repeated PNG
encoding under the race detector; no search precision is reduced to accelerate
the reports.

### Reproduce the update

```sh
just jet-tracking-check
just jet-sample-check --suite all
just jet-challenge-check --suite all  # unresolved challenges should fail

just jet-analyze --audio samples/public/pathak-2026/N563FX_R21/audio.wav \
  --out work/jet-improvements/r21-extended
xdg-open work/jet-improvements/r21-extended/curves.svg

# These hypothesis/config paths are from the local November 5 experiment.
just diagnose --config work/additional-jets/r21/publisher-trace-band-2.json \
  --lat 37.658514 --lon -121.783856 --clock-offset 1.62 --track N563FX \
  --out work/jet-improvements/r21-diagnostic
xdg-open work/jet-improvements/r21-diagnostic/diagnostic.html

# Optional figure; requires NumPy, SciPy and Matplotlib.
python3 tools/plot_additional_jets.py \
  --r21-detection work/jet-improvements/r21-extended/detection.json \
  --out work/jet-improvements/comparison.png
```

Local evaluation outputs are under `work/jet-improvements/`: `tracking-regression/`,
`supplied-track/`, `california-challenge/`, `california-challenge-final/` (final
combined replay), `challenges/`, `reference-quick/`,
`reference-audio/`, `jet-regression/`, `jet-test/` and `desk-regression/`.
Each reference report records input and Go-source hashes. Published-data report
snapshots are stored separately under `references/provenance/jet-tracking-2026-11-06/`;
private desk reports remain local. All data sources, licenses and original
locations remain as documented above; no new source data were acquired.

### What still needs work and what new recordings will test

The standard detector still uses greedy seed linking and an ideal-flight shape
screen. The extended jet tracker still needs a predictive seed and misses R50.
The physical scorer assumes a stationary observer, still air and a constant
source frequency. It has no validated variable-RPM jet model or walking-path
estimator. Dense final spatial grids already exist, but cannot remove model
bias or flight ambiguity. Multi-event assignment and reviewed silence constraints
remain in place; this change does not solve acoustic classification or calibrate
audibility. These are remaining implementation/research problems, not all
problems that more recordings alone will fix.

New samples should preserve the original recording, independently logged start
time with time zone and estimated clock uncertainty, measured microphone GPS
and its uncertainty, and whether the phone was indoors/outdoors and moving.
Record which elapsed intervals contain a seen/heard aircraft and which were
reviewed without one. Include lead-in and trailing audio around each pass;
several minutes containing multiple passes and intervening quiet periods are
especially useful. Stationary outdoor sessions offer the clearest initial
localization test; desk and walking sessions test different failure modes.
Keep some sessions untouched for evaluation after algorithm development.

### November 6: telemetry-led search and locked relative timing

The next private desk experiment reverses the discovery order: predict flight
curves at the user-supplied stationary location, then inspect the measured audio
at those times, including intervals rejected by the acoustic detector. This is
an association experiment with a known receiver, not a blind localization test.
The original 3 h 28 min recording is unchanged. Local artifacts are under
`work/nov06-desk/adsb-led/`; private audio and coordinates are not redistributed.

For flight i, the predicted elapsed arrival time is
`emission_time_i + slant_distance_i / sound_speed - recording_start - clock_offset`.
A single `clock_offset` applies to every flight. Subtracting two arrival times
cancels that offset. We must not independently slide each flight onto whichever
sound looks most promising, or count two fits of the same sound as two aircraft.
The observation of a flight in ADS-B also does not establish its audibility
through a building; missing audio is not a reviewed silence constraint.

The Go experiment uses the cached geometric trajectories, receiver terrain and
geoid correction, the existing retarded-time solver, and low/high-band measured
spectra covering 30–6500 Hz. It fits a constant source tone per flight in broad
and short windows. Six workers process passages independently; a subsequent
report aggregates their scores at the **same clock correction**. This changes
neither production detector thresholds nor existing reference labels.

Frequency and band selection use alternating ten-second training blocks, with
two-second boundary guards. Heldout blocks are evaluated without refitting.
The experiment scans a shared ±30 s clock uncertainty and shifted-timeline
controls from two to thirty minutes away. Each control preserves the same
relative flight times. A four-flight comparison selects separate aircraft and
nonoverlapping windows on training scores only; both the selected aircraft and
the common clock are then fixed for heldout scoring. These are exploratory
controls, not calibrated false-alarm probabilities. Whitening uses the entire
recording and the heldout blocks are not an independent recording.

The cache contains 388 retrieved regional aircraft traces; one additional
discovered trace was unavailable. The experiment enumerates 412 modeled
encounters from 180 aircraft within 20 km slant range, including repeated
approaches and partial trace endpoints. These are **not** 412 detected flyovers.
There are 29 complete modeled approaches within 5 km during the recording.

The new pass reveals a descending-tone review lead, compatible
in time with one modeled encounter. A short fit is stronger than the longer-window
fit; the aircraft identity remains unconfirmed. Short fits to other aircraft
can follow persistent hum or brief bursts. The strongest training-selected
four-aircraft group gives 2.15 dB mean heldout contrast; the shifted-timeline
control gives 2.29 dB. This does not validate a multiple-aircraft association.
The previously supported November 5 tracked-flyover development reference is recovered
as a positive control: 5.51 dB heldout contrast at zero clock correction versus
0.33 dB for the training-selected shifted control. This is a previously examined
reference, not a new blind test.

The report includes expected sound-arrival times, inter-flight gaps, a shared
clock slider, playback links, all eight earlier strong tonal proposals with
overlapping flight hypotheses, raw/whitened spectrograms, diagnostic overlays,
and JSON/CSV evidence. Independent best-fit overlays are explicitly separated
from the shared-clock comparison. No aircraft identity or absence-of-aircraft
label was added to the regression suite.

```sh
just experiment-open work/nov06-desk/adsb-led/index.html

# Reproduce the local Go scoring experiment; uses the existing private assets.
go run work/nov06-desk/adsb-led/audit.go
```

`audit.go` performs spectral/trajectory fitting, `export.go` exports model
curves, and `report.py` aggregates shared-clock scores and writes the report.
`plots.py` generates measured spectrogram comparisons. The Python reporting
steps require NumPy, SciPy and Matplotlib. `provenance.json` records hashes of
inputs, scripts and results; `source.json` in the parent directory records the
original MP3 hash. Use a fresh output directory when changing input audio or
spectral parameters. The limitations remain constant engine pitch, fixed sound
speed, indoor masking, telemetry coverage and uncalibrated acoustic evidence.

### Promoting shared timing into partial-curve localization

The production stationary search already fitted complete flyovers with one
recording clock. Its automatic selector and staged geometry required a bounded
CPA, however, so strong partial curves were excluded before their relative
sample timing could help identify flights or reject locations.

Joint selection now also retains partial curves that already pass the strong,
predictive acoustic checks and have a measured ridge. It does not change the
detector's acoustic thresholds or promote review/weak fragments. Partial events
have zero CPA tolerance: their relative sample timestamps remain fixed, but no
CPA measurement is invented. The coarse stage uses conservative received-sound
overlap intervals for partials, then the normal retarded-time scorer enforces
shape, spectral/geometry coverage, range, and the individual event threshold.
All selected events must fit one location and clock, and each gets a distinct
trajectory. Distinct assignment now also applies to non-staged multi-event CLI
searches. Narrowing/refinement retains these constraints down to the requested
final grid spacing. Partial curves can prune less effectively than complete
curves; there is no promise of a faster statewide run.

The curve comparison viewer now displays each interval's fixed offset from
event one. For measured complete events it also compares observed and predicted
arrival-time gaps, including the measurement tolerance and residual. Those gaps
do not change when the common clock changes. Partial events never display an
invented measured CPA gap. The UI's existing `joint_curve_count` statistic is
described as the count of bounded closest approaches; strong partials can also
participate in joint search.

A synthetic regression uses two intersecting flight paths. The wrong mirror
location can explain both sounds separately by changing their clocks, but fails
when their timing is locked. The joint search retains the correct region and
refines to a 10 m grid; this is a synthetic algorithm check, not real-world
10 m accuracy. Additional regressions change one flight's time while retaining
its shape, reject a trajectory reused as two flights, preserve strong partial
samples without inventing a CPA, and verify that displayed gaps are invariant
under the common clock. The acoustic reference tests still check the two
original YouTube flyovers against their measured spectral anchors.

The remaining major step is **telemetry-guided discovery during a blind
location search**: use plausible location/flight sequences to look for weaker
measured ridges at their predicted relative times, then validate those sequences
on heldout sound and competing flight/time/location hypotheses. The private
known-location experiment demonstrates that diagnostic workflow, but it is not
yet an automatic statewide detector, and this change does not claim to recover
the November 6 aircraft or determine a new recording's location. A predicted
curve alone must not be turned into an acoustic detection or a silence label.

Validation passed: `go test -race ./...`, `go vet ./...`, JavaScript syntax
checks, `just reference-check --suite quick` (both YouTube anchor fixtures and
the public propeller reference), and `just jet-tracking-check`. Reports are in
`work/joint-timing/reference-quick/` and `work/joint-timing/jet-tracking/`.
The search package's race tests were rerun after adding reuse of partial arrival
intervals across events at each location. Production detection thresholds and
published regression expectations were not changed.

### Guided discovery outside the seed events

`just corroborate` adds a separate Go experiment for stationary recordings. It
starts from the candidate locations and shared clocks in an existing search
report, rechecks their seed events against the supplied data, and examines the
rest of the recording for other trajectories. It accepts no reference GPS
coordinate or expected additional flight ID. The quality and completeness of
the initial candidates still limit the experiment: freezing a wrong seed cannot
recover a location that was never supplied.

Each additional trajectory is evaluated using its retarded-time Doppler curve
at the fixed receiver and clock. Thus, the gap from the seed flyover is set by
the telemetry, not independently slid into place. The report gives that predicted
arrival-CPA gap, including negative gaps for earlier sounds. A predicted gap is
not labeled as an observed CPA measurement. The default range is 15 km, with
40 s and 120 s windows centered on each modeled closest approach. Windows that
overlap any seed event, including an FFT-window guard, are excluded.

The source tone (40–800 Hz in 0.5 Hz steps by default) and window are selected on
alternating training blocks. Block length is at least 10 s and four FFT windows;
half-window guards prevent adjacent train/test frames sharing input samples.
The frozen fit is then measured on the other blocks. Spectral contrast compares
the predicted frequency with four nearby sidebands. Defaults require at least
95% trajectory coverage, 3 dB training and test contrast, 65% positive test
samples, and at least a 3% modeled frequency range. A steady-tone control uses
the frequency span of the fitted prediction, padded by two FFT bins. Comparing
against the loudest steady tone anywhere in the spectrum incorrectly rejected
real curved sound when unrelated hum was louder; the synthetic flyover-plus-hum
regression reproduces that failure and verifies the correction. Up to six
shifted-time controls (±120, ±240, ±480 s) independently refit source tones over
the full configured source range on training blocks. At least three shifted controls must be usable;
the actual heldout score must beat every usable control by at least 1 dB.

These are research thresholds, not calibrated false-alarm rates. Shared spectral
normalization and the recording environment can correlate the heldout blocks.
The many frequency, flight, window and location trials also create selection
bias. Different aircraft/locations may explain the same sound. In particular,
counts of passing extra flights do not establish a location probability or a
unique aircraft identity. The experiment never promotes its predictions into
observed ridges, production detections, regression labels, or silence intervals.

Each nonoverlapping sound interval counts once. Imported segments sharing an
ICAO address count as one aircraft, and segments of seed aircraft are excluded.
Opaque IDs from other providers need explicit identity metadata for equivalent
deduplication. One closest approach is proposed per continuous in-range
encounter; repeated circuits that never leave the range can be missed. Source
RPM drift, moving receivers, broadband-only jet noise, missing flight data and
short recordings lacking control windows remain limitations. Jets are included
unless the original search excluded known jet types; higher-frequency testing
requires a suitable spectrum and an increased `--max-hz`.

The default four workers operate on independent candidate hypotheses with
read-only spectra/tracks. `--max-candidates` defaults to 24 (maximum 256), and
`--max-passes` to 512 per candidate (maximum 2000). Exceeding the pass budget is
an error, not a silently partial report. Cancellation is supported. Outputs are
`guided.json`, `guided.html`, `guided-candidates.geojson`, and `provenance.json`
with hashes of the inputs and executable. The source search's note is carried
into the report to preserve disclosures about reference-assisted seed selection.
Original maps/results remain intact.

Synthetic regressions start with two mirrored locations that both explain the
seed sound. Only one explains an additional, unused sound at the required time.
Other tests remove the second sound, replace it with steady hum, change its
flight time by 20 s, reuse an anchor aircraft under another segment ID, duplicate
segments, overlap hypotheses, invalidate the anchor, and cancel the operation.
None may create extra supporting aircraft or shift the receiver/clock to rescue
a mismatch.

#### Reference replay and outcome

The cached original YouTube recording and New York flight CSV were reused
locally. This is a replay with a manually reviewed first-event window and
approximate start time, not an audio-only discovery benchmark. The legacy search
uses New York's bounding rectangle, not its state polygon. No additional audio
or telemetry was downloaded for these experiments.

| Experiment | Seed hypotheses | Additional encounters fitted | Seeds with passing leads | Outcome |
| --- | ---: | ---: | ---: | --- |
| Existing 2 km, first-event-only search | 32 | 1,764 | 18 | Wrong locations also gain support; no unique localization |
| First-event-only search refined to 25 m | 20 | 792 | 8 | Refinement does not resolve the ambiguity; wrong positions still gain support |
| Previously established two-event location/clock, second event removed from follow-up inputs | 1 | 84 | 1 | Known second flyover recovered, alongside six other unverified leads; positive control only |

The refined first-event search evaluated 96,398 points over the original bounds
without supplying the expected location or second-event window. Its top result
was 40.838994, -73.603254 with a +31.825 s clock correction, which disagrees with
the video reference. The fixed-seed follow-up did not recover the reference
location. This failure matters: concentrating refinement around an ambiguous
single-event peak can lose the location that fits the entire sequence.

For the explicitly reference-assisted positive control, the previously fitted
position/clock from `work/methodology-v2/colsto-refined-clock/results.json` were
retained and only the first event was supplied as an anchor. The follow-up found
the known `ace391-1788825600.000-496` curve around 3060.1–3100.1 s, with a 125 Hz
source tone, 15.86 dB test contrast, and 9.00 dB margin over the strongest control.
The predicted arrival CPA was 3080.15 s, 924.10 s after the first anchor's modeled
CPA. These are modeled times; they do not replace the independently measured
event times. Since both sounds originally helped select this seed, recovery of
the second is **not independent validation**. The six other passing leads are
not labeled as confirmed aircraft.

Artifacts and configurations:

- `work/guided/colsto-first-event/`: coarse first-event follow-up, plots and hashes.
- `work/guided/first-event-refined.json` and `first-event-refined/`: complete
  first-event search configuration, scores and map.
- `work/guided/colsto-refined-followup/`: follow-up at fixed refined seeds.
- `work/guided/reference-assisted-seed.json` and `colsto-assisted-control/`:
  disclosed positive-control input and report.

Reproduce the refined experiment from the prepared local assets:

```sh
just locate --config work/guided/first-event-refined.json --out work/guided/first-event-refined
just corroborate --config work/guided/first-event-refined.json --results work/guided/first-event-refined/results.json --max-candidates 40 --out work/guided/colsto-refined-followup
just experiment-open work/guided/colsto-refined-followup/guided.html
```

Validation passed: full Go race tests, `go vet ./...`, the three quick acoustic
reference cases, and the Challenger tracking regression. Search race tests and
vet were rerun after correcting the unrelated-hum control. Regression reports
are in `work/guided/reference-quick/` and `work/guided/jet-tracking/`. Production
detection thresholds and reference labels were unchanged.

The next integration requirement is to compare whole competing flight sequences
and preserve multiple spatial/clock hypotheses until additional sounds constrain
them. That requires empirical false-match calibration across the full search,
not simply a higher count of passing local fits. The diagnostic remains outside
the website's automatic location decision until it can reject the demonstrated
wrong-location leads. It has not established new November 6 aircraft identities.

### Whole-sequence timing controls and retained hypotheses

The next experiment adds `--sequence-controls N` to `just corroborate` (zero
disables it; 5–39 trials are accepted). It addresses selection bias in counting
extra flights that individually passed their heldout tests. The additional
sequence is now selected strictly on training data before its heldout score is
examined. Source tone/window selection still uses training contrast; flight
selection uses training contrast minus the nonnegative steady-tone contrast.
The sequence statistic is the **weakest** additional flight's heldout margin
over its local steady-tone control. A strong flyover cannot compensate for a
badly matching required flyover. `--sequence-extra` chooses 1–4 additional
distinct aircraft; one plus the anchor reproduces the two-event question.

Each control shifts all additional flight arrivals together while leaving the
anchor, its receiver position and its recording clock fixed. This preserves
spacing among the additional flights but deliberately breaks their timing
relative to the anchor. The deterministic shifts are +140, −140, +160, −160,
and so on, extending through +320 s for 19 trials. Every control repeats source
frequency, window and aircraft selection using its own training samples. All
trials use the same encounter opportunities: both proposed windows must fit
inside the recording and avoid anchor audio at **every** shift. Encounter-set
hashes are compared, and candidates without complete paired trials are excluded
from the global comparison. This can leave short recordings untestable.

For each shift, the audit takes the maximum heldout score across all eligible
candidate locations. The actual search must beat every such wrong-time search
by 1 dB, as well as meet the existing acoustic contrast/continuity/variation
checks. Different flight sequences or positions more than 1 km apart within
1 dB are flagged as competing. Results that pass are explicitly unverified.
These are **conditional diagnostic controls**, not permutation p-values or a
calibrated false-alarm probability: the shifts and acoustic blocks are
correlated, the anchor was already selected, and seed coverage is finite.
Predicted flights remain hypotheses, and failures are not silence evidence.

Location search now retains a separate `followup_hypotheses` pool of up to 256
passing fits before the display shortlist is truncated. Selection balances
flight groups, reserving at least half the budget for alternatives within them.
Within a group, points closer than 500 m and clocks closer than 5 s are merged
in favor of the higher score. The audit applies the same policy at its requested
`--max-candidates` budget. This preserves more alternatives but is still bounded
pruning: it neither saves every clock tried at a point nor restores locations
discarded earlier. Original search scores, refinement and map rankings are
unchanged. Old reports lack the pool and need a fresh `locate` run.

The optional `--full-curves` experiment removes the 40-second proposal and
requires a 120-second window. It asks whether longer temporal structure reduces
the false matches seen in short snippets; it is not a new detector default.
It can miss short jet passages and incomplete curves, so both variants must be
reported. The model still assumes a constant source tone within each passage.

New regressions check a shared-clock three-aircraft sequence, failure after
changing just the third aircraft's timing, missing sound, steady hum, training-
only flight selection, the maximum wrong-time score at a competing location,
equal encounter cohorts, anchor/boundary exclusion, spatial/clock diversity,
rendered report output, and deterministic results across worker counts.

The replay also exposed a spectral-boundary bug in the follow-up fitter: a
source frequency could win on training frames but put its predicted test
frequencies outside the saved spectrum, changing which encounters were available
in a control. Frequency eligibility now checks geometry and sideband bounds over
the entire window before selecting a source tone. This uses no heldout spectral
power. All 64 candidate hypotheses in the final replays had identical encounter
cohorts across their actual and 19 shifted trials.

#### Sequence-control replay results

The first-event location search was rerun with the same configuration, bounds
and cached inputs. It again evaluated 96,398 points and returned the same top
coordinate/clock; the new output additionally retained 256 hypotheses. The
follow-up selected 64 candidates and evaluated 1,216 candidate/control trials.
No reference coordinate, second-event time or second aircraft ID was supplied
to this run. The manually chosen first event and approximate start remain prior
information, so it is not an end-to-end blind audio benchmark.

| Seed source and window variant | Candidates with at least one local passing lead | Best actual sequence margin | Best wrong-time search margin | Sequence outcome |
| --- | ---: | ---: | ---: | --- |
| First-event search, 40/120 s proposals | 24 / 64 | 9.68 dB | 18.83 dB | Not separated from wrong timing |
| First-event search, 120 s only | 7 / 64 | 5.54 dB | 9.43 dB | Not separated from wrong timing |
| Reference-assisted position/clock, 40/120 s proposals | 1 / 1 | 9.00 dB | 18.00 dB | Not separated from wrong timing |
| Reference-assisted position/clock, 120 s only | 1 / 1 | 11.12 dB | 1.58 dB | Passes these timing controls, unverified |

The margin columns are the weakest additional flight's test contrast minus its
nonnegative steady-tone test contrast, not physical signal-to-noise measurements
or location probabilities. Counts are review leads, not confirmed aircraft or a
calibrated false-positive rate. The 120-second variant reduced short-fragment
leads but did **not** identify the recording location from the first-event search.

The assisted 40/120-second run selected the known second flyover `ace391` around
3080.15 s. The assisted 120-second run instead selected `a63e21` around 782.09 s,
whose identity has **not** been independently validated. Its 9.54 dB separation
from shifted controls is an experimental lead, not confirmation of that flight
or evidence of a new localization. The position and clock already came from the
prior two-event reproduction, and this one-position test does not include all
the location alternatives of the 64-candidate search. The different trial counts
are another reason not to compare their passing status as equal benchmarks.

Artifacts are in `work/sequence-audit/`: `first-event-refined/` contains the new
search/pool, `first-event-followup/` and `full-curves-followup/` contain the
64-candidate audits, and `assisted-control/` and `full-curves-assisted/` contain
the explicitly assisted controls. Each follow-up includes the selected real and
shifted sequences, encounter-set hashes, global control maxima and input/binary
hashes. Reproduce the longer-window audit from the prepared inputs:

```sh
just locate --config work/guided/first-event-refined.json --out work/sequence-audit/first-event-refined
just corroborate --config work/guided/first-event-refined.json --results work/sequence-audit/first-event-refined/results.json --max-candidates 64 --sequence-controls 19 --full-curves --out work/sequence-audit/full-curves-followup
just experiment-open work/sequence-audit/full-curves-followup/guided.html
```

Full Go race tests, static checks, the three quick reference cases and the
Challenger tracking regression passed. Search race tests/static checks were
rerun after the spectral-boundary correction. Reference reports are in
`work/sequence-audit/reference-quick/` and `jet-tracking/`.

The remaining localization issue is earlier pruning: alternate flight geometry
and clocks need to survive until multiple long observed curves constrain them,
before concentrating dense refinement around a single-event peak. More reliable
observed-ridge shape/continuity checks and independently labeled recordings are
still needed to validate aircraft identities, especially for short jet passes.
Neither new control procedure changes the website's detection thresholds or
claims additional aircraft in the private November 6 recording.

### Measured curves before joint refinement

The next experiment connects fresh acoustic ridge measurements to the existing
staged joint search. It avoids starting from the earlier single-event shortlist.
`just joint-locate` takes the recording and a configuration containing reviewed
time windows and source-tone frequency bands. The reviewed choices remain prior
information; this is not blind event discovery or aircraft classification.

The Go pipeline now:

1. Recomputes the whole recording's 2 s and 4 s spectrograms and unchanged
   acoustic detection checks. Each reviewed window must have exactly one strong,
   predictive Doppler-shaped measured ridge, overlapping at least 60% of both
   the window and the ridge. Missing or ambiguous selections stop the search.
2. Attempts sub-bin peak refinement within two frequency bins of that measured
   path. The Doppler model does not supply missing observations: points must be
   actual local spectral maxima with local contrast, and unsupported gaps are
   retained. Refinement must pass the acoustic checks again, preserve at least
   95% of the prior extent and not lose an already bounded CPA. Otherwise the
   original strong measured trace is retained. The review plot uses the measured
   trace's FFT resolution and can hide its overlay.
3. Starts again from the original region, aircraft data and clock uncertainty.
   No candidate coordinate, aircraft ID or former clock shortlist is an input.
   The reviewed physical speed/slant/frequency limits remain explicit. The ideal
   acoustic fit supplies a measured CPA with tolerance when it is bounded; a
   partial pass retains its full time samples without an invented CPA constraint.
4. Applies shared arrival-time geometry on at most a 200 m coarse grid, then
   scores measured curves across **all surviving regions** at at most 100 m.
   Actual flight trajectories, 3D sound travel time, one shared recording clock,
   distinct tracks, measured-ridge residual limits and per-event spectral gates
   must all agree. An independent clock shift for each event cannot rescue a fit.
5. Refines the best joint flight-group/location alternatives through the requested
   finer spacings, including the 10 m final grid in this example. Final spacing
   describes evaluated points, not an accuracy guarantee.

The command writes `detection.json`, `curve-traces.json`, `curves.html`, a reusable
`joint-search.json`, the spectrum, input/binary hashes in `provenance.json`, and
`results/` with the interactive map and measured/predicted curve comparisons.
The curve page links to results only after this run completes. Its prepared
configuration uses absolute data paths so rerunning it from the output directory
does not resolve tracks or terrain against the wrong directory.

#### Reference replay

Inputs are the existing Colsto MP3 in `samples/`, typed ADS-B lol tracks in
`work/methodology/ny-tracks-typed.csv`, terrain in `data/terrain-ny-3arc`, and
`work/web/cache/egm96_15.gtx`. Their upstream sources and limitations are recorded
in [the source register](sources-and-validation.md). The example uses the same
reviewed event windows, time uncertainty and physical bounds as the earlier
reproduction, but includes known jets in the competing telemetry. The expected
coordinate and target aircraft identities are not provided to the search.

```sh
just joint-locate --audio 'samples/Unknown location - raw audio [4CxVxuMVkMc].mp3' --config examples/colsto-measured-joint.json --out work/traced-joint/reference
just experiment-open work/traced-joint/reference/curves.html
```

| Quantity | Result |
| --- | --- |
| Region | New York state mask within its bounding box |
| Start-time prior | 2026-09-08 20:16:00 UTC, ±120 s |
| Reviewed sound windows | 2,100–2,280 s and 3,040–3,140 s |
| Accepted measured extents | 2,124–2,243 s and 3,036–3,133 s |
| Measured ideal-fit CPA gap | 920.99 s; each CPA has 5 s tolerance |
| Coarse geometry | 1,454,877 locations tested; 24,568 survived |
| Joint scoring | 130,548 dense points, 131,151 total including refinement |
| Fine grids | 50, 25, 12.5, then 10 m |
| Best position | 40.876206, −73.525405 |
| Shared clock correction | +89.073 s |
| First flight / source tone | `a1dfeb-1788825600.000-631` / 112.155 Hz |
| Second flight / source tone | `ace391-1788825600.000-496` / 125.128 Hz |
| Mean per-event spectral score | 13.77 dB; one passing flight-pair group |
| Distance to video's model coordinate | 47.88 m from 40.876466, −73.524951 |

The first trace passed sub-bin refinement; the second retained its original
strong measured trace. Its departures from an ideal straight flight still enter
the actual-trajectory comparison. Source-tone frequencies are independently
fitted for each flight, but the recording clock is shared. The geometry-predicted
delayed CPA gap is about 924.22 s; this differs from the ideal-fit measurement
within its declared uncertainty. Curve sample times, rather than an artificially
exact 920-second interval, determine the final match.

The quoted coordinate is the video's reported model result, not surveyed recorder
GPS. This replay does not establish arbitrary-phone accuracy or improve on every
previous coordinate estimate. It shows that the measured two-curve search can
recover the reference region without pruning around a single-event winner.
Reviewed windows and bounds, reused development audio, finite grid resolution,
terrain/telemetry error and fixed source-pitch assumptions remain limitations.
Known jets are included as competitors; the winning pair here is propeller
aircraft, so this run does not validate jet localization.

#### Timing sensitivity and regressions

After the unmodified result, a separate copy of the tracks moved every timestamp
of its second winning flight **+20 s**. The other aircraft, original region,
measured curves, spectrum, physical limits and common-clock range were unchanged.
This perturbed statewide search evaluated 130,020 dense points and returned
`no_match`. Artifacts and the explicit changed-track/hash record are in
`work/traced-joint/timing-control/`. This is a post-result sensitivity check,
not an independent validation set or a calibrated false-match rate. It verifies
that this result depends on the relative flyover timing.

New Go tests trace both independently annotated real reference excerpts on their
shared timeline, preserve their roughly 921-second measured gap, require the
known frequency anchors, reject ambiguous/weak choices, and test sub-bin peak
estimation. A synthetic two-flight fixture tests full-region joint refinement
and rejection after changing just one flight's timing. The full race suite and
static checks passed, as did all three quick reference cases and the Challenger
tracking regression. Reports are under `work/traced-joint/reference-quick/` and
`work/traced-joint/jet-tracking/`.

The website's automatic event selection is unchanged. This command uses the
standard 40–1,800 Hz analysis, not the experimental brief/high-frequency jet
profile. Automatic selection of the right pair among many plausible acoustic
curves, variable engine pitch, and a moving receiver remain unresolved. Existing
explicit silence intervals are preserved; absence of a detected ridge does not
automatically become a silence rejection. No new aircraft are claimed for the
private November 6 desk recording by this reference-only experiment.


### Automatic sound pairs and combined analysis (November 6, 2026)

This entry supersedes the preceding notes about unchanged web selection.
Stationary automatic uploads now search every eligible pair of strong measured
curves, rather than forcing every strong tone in a noisy recording to be an
independent aircraft at the same position. Reviewed mode still requires all
selected flyovers together, including three or more. Automatic inference of a
maximal three-or-more-flight sequence remains unimplemented.

#### Implemented analysis and validation

- The experimental `hybrid-v2` profile combines the unchanged long-curve and
  short-sweep evidence checks. Detection retains each source profile's original
  frequency floor and normalization (40 Hz long, 100 Hz short); the separate
  scoring spectrum covers 40–6,500 Hz. This preserves the stronger original jet
  tracker coverage while making low tones available to matching. It retains measured sample paths; it does not draw
  synthetic curves into the measurements. Duplicate harmonic/resolution traces
  are merged. Source classification as jet versus propeller is not inferred from
  a strong-curve label. Jets remain eligible in telemetry by default.
- Long files use overlapping eight-minute cores with two-minute padding. Every
  block retains original recording-relative times. A curve belongs to the core
  containing its midpoint; the overlap exceeds the detector's maximum curve
  extent. Up to four independent block workers run, with deterministic report
  merging and coordinator progress updates. A less-overlapping full-band
  spectrum is retained for full-recording scoring, so the shared clock and event
  offsets never depend on treating blocks as separate recordings.
- The whitening median uses an order-statistic selection instead of fully sorting
  every column. Tests compare it with independently sorted medians for random,
  ascending, descending, constant and repeated-value inputs. This changes cost,
  not the median statistic. Block-wise background normalization can still change
  detections compared with one global normalization pass.
- Pair searches use 3D terrain/geoid geometry, retarded emission time, distinct
  trajectories and one shared recording clock. Measured source tones fit
  independently; per-event timing shifts cannot repair an inconsistent pair.
  Strong partial curves retain broad motion bounds instead of treating an
  underdetermined ideal-fit speed/distance as ground truth.
- Reviewed silence tests the entire modeled arrival interval within the supplied
  audibility radius. An unrelated tonal proposal inside reviewed silence is
  ineligible for automatic selection and is listed in the report. Reviewed
  mandatory flyovers still cannot contradict a declared absence. A detector miss
  alone never supplies an absence constraint.
- Coarse geometry is at most 200 m, dense scoring at most 100 m, followed by
  competing-hypothesis refinement to the requested final spacing. Large regions
  split into complete tiles at explicit point budgets. Interval bounds account
  for receiver-cell extent, terrain extrema and common-clock bins before full
  residual/spectral scoring. Missing bounding information keeps a cell rather
  than rejecting it. Cached arrival indexes and a checked closed-form retarded
  solver avoid repeated work without changing the trajectory model.
- Default controls rerun every eligible sound pair, the original supplied region
  and refinement at two incorrect absolute recording starts. Failed trials and
  inadequate telemetry spans yield `incomplete`; near-ties or spatial/clock
  alternatives yield `unresolved`. A 1 dB margin and two trials are development
  heuristics, not calibrated location confidence. Receiver-network completeness
  is not established by having a CSV spanning the necessary times.
- Successful pair results are cached against executable bytes, every spectrum
  value, complete trajectory data, effective settings and potentially used
  elevation/geoid bytes (including control footprints). Changed/corrected input
  tiles invalidate the cache. `automatic.json` retains settings, input digests,
  exclusions and all actual/control outcomes. Pair maps remain explicitly
  provisional; the overview gives the overall decision.

The UI offers **Sound analysis**, retains the selected profile across reloads,
redecodes when profiles change, and shows **All groups and timing controls**
above individual pair maps. A curve whose frequency is outside an earlier cache
is never silently scored against that cache. Long-file progress reports block
work. Large status replies contain stronger/reviewed curves with original source
indices; weak measurements load in pages of 200. Full traces stay in the detailed
files, instead of being duplicated in status or rewritten at every progress
update. Tests verify exhaustive paging, preserved source indices and restoration
of full measurements after restart. A real-file HTTP smoke test returned all
111 stronger/reviewed curves with original indices in 617,626 bytes (about 32 ms
on this machine); one 200-fragment page was 497,037 bytes (about 16 ms). The
full detector report is about 200 MB. These are local response measurements, not
a recording-analysis runtime guarantee; artifacts are in
`work/automatic-sequences/ui-smoke/requests.json`. The standard profile remains available; combined detection is experimental
and takes longer.

```sh
just serve
just hybrid-analyze --audio recording.mp3 --out work/combined-audio
just hybrid-reference-check
just auto-locate --audio recording.mp3 --profile hybrid --config region.json --out work/automatic
```

`examples/colsto-automatic.json` supplies New York bounds and existing cache paths
with no reviewed event windows. `--detection FILE` can reuse matching saved
measurements and spectrum. More than 512 eligible pairs stops explicitly rather
than silently dropping sounds. A statewide all-pair search can be much more
expensive than the reviewed two-event replay.

#### Combined-profile regressions

`references/hybrid.json` copies the existing acoustic labels and numerical
acceptance limits without relaxing them. An earlier trial that lowered the short detector's
frequency floor slightly reduced Challenger coverage; it was replaced by keeping
source-profile detection independent of the wider matching band. The five positive cases and five
background controls all passed. Results are in
`work/automatic-sequences/hybrid-validation-v2/audio.json`, with manifest,
source, input and decoder provenance. The full Colsto recording passed both
flyover labels with 100% anchor hits/coverage; its 28 strong tonal proposals are
not 35 confirmed aircraft.

| Labeled curve | Anchor hits | Label coverage | RMS residual |
| --- | --- | --- | --- |
| Propeller R04 | 95.8% | 100% | 2.74 Hz |
| A320 upper tonal branch | 100% | 100% | 2.83 Hz |
| Phenom R20 | 80.0% | 83.0% | 2.19 Hz |
| Challenger R21 | 70.6% | 72.7% | 4.48 Hz |

These are reused development labels. The Challenger remains short of its
separate full-trace challenge. Zero strong curves in these five background clips
is not a measured general false-positive rate.

`references/hybrid-location.json` keeps the supplied-track Phenom regional
expectations, explicitly selecting the longest usable ridge from this **known
single-passage clip**. It passed the existing California/Livermore regional check
in `work/automatic-sequences/hybrid-location-v2/location.json`. An initial combined variant returned three usable tones and the one-event
replay stopped without an explicit selection policy; different tones from one engine cannot be counted as
independent airplanes. The supplied target KML and unverified recorder GPS make
this an assisted regional test, not an independent precise jet-location result.
The original suite and its expectations remain unchanged.

#### Reference selection and incorrect-time controls

A broad-motion two-event statewide performance benchmark retained the two known
measured extents, but removed their reviewed source-frequency, speed and distance
bounds. During development, scoring work decreased from 5,145,509 point
evaluations to 1,217,285 with curve/clock screening. Both runs found the same
reference area. Different tile lattices and finite refinement mean these workload
counts are not proof of identical coordinates or runtime. Those diagnostic runs
used no timing controls and did not test automatic selection among all curves.
They are in `work/automatic-sequences/{tiled-benchmark,clockmask-benchmark}/`.

The subsequent **all-19-curve** selection replay did not hand-select a pair. It
searched all 151 nonoverlapping pairs and repeated all 151 at each ±600 s control
start: 453 complete searches. It used a deliberately small rectangle
`40.85–40.90 N, -73.56–-73.50 E` around the known reference area, with the same
±120 s clock uncertainty. This is a conditional selection/control experiment,
not a statewide blind localization benchmark.

| Quantity | Outcome |
| --- | --- |
| Actual-time passing sound pairs | 16 of 151 |
| Negative-time passing pairs | 7 of 151 |
| Positive-time passing pairs | 5 of 151 |
| Highest actual pair | Curve indices 8 and 12 (zero-based), the two known flyovers |
| Aircraft | `a1dfeb-1788825600.000-631`, `ace391-1788825600.000-496` |
| Highest actual position | 40.876276, -73.525399 |
| Shared clock correction | +88.563 s |
| Highest actual mean spectral score | 13.610 dB |
| Highest incorrect-time mean spectral score | 13.448 dB |
| Separation | 0.162 dB, below the declared 1 dB gate |
| Overall outcome | **unresolved**; competing actual positions/clocks also occur |

The highest actual position is about 43 m from the video's reported model
coordinate, which is not surveyed microphone GPS. Selecting that attractive map
and ignoring the controls would conceal the unresolved false-match problem.
No gate was relaxed after observing this outcome. Complete artifacts, pair maps,
and the input rectangle are in
`work/automatic-sequences/reference-conditional/run/automatic.{json,html}`.
The statewide all-19-curve control experiment has not been completed.

#### November 6 full desk recording

The entire November 6 desk recording was freshly analyzed with the combined
profile: **12,480.678 s**, no initial-clip truncation. It yielded 41,782 tonal
fragments, 103 needing review and **8 strong curves**, concentrated around
3.6–5.6 kHz. These remain sound hypotheses. Spectrum and detections are in
`work/automatic-sequences/desk-full-v2/`.

All 28 strong-curve pairs were then tested at the **supplied desk GPS**
the private desk coordinate, with one shared ±30 s clock, local terrain/geoid correction,
all engine types, and the previously downloaded competing flight history.
All 28 selections were repeated at ±600 s recording-start controls. There was
**no passing actual-time pair**, and no supporting location/aircraft claim.
Each of the eight curves was also checked individually at the supplied
position and clock range; none produced a passing match. The tiny input
rectangle fixes the known position: this is explicitly an
association diagnostic, not a blind location search. Artifacts and scope notes
are in `work/automatic-sequences/desk-fixed-position-v2/`.

A no-match does not establish that no aircraft were audible. Possible causes
include unrelated desk tones, missing/poor telemetry, source pitch variation,
insufficient acoustic signal and limitations of the current stable-tone model.
The earlier private November 5 tracked-flyover-associated curve and regional association
regression still passed with unchanged expectations; the new report is in
`work/automatic-sequences/<private-regression>/all.json`.

#### What remains unproven

We can now preserve the tested propeller and jet curves, evaluate shared flyover
timing, inspect wrong-time selection failures, and refuse an unsupported unique
position. Reliable general aircraft identity and location have **not** been
established. The remaining work requires independent, representative recordings
with actual microphone GPS/time, aircraft intervals, quiet/confuser annotations
and competing flight history. Split those by session/site before calibration.
Automatic three-or-more-flight inference, broad jet roar without trackable tones,
variable source RPM, wind/refraction and receiver motion remain limitations.
More elaborate fitting must be evaluated against controls and held-out sessions;
forcing this reference's highest map into a success would not resolve them.

Full Go race tests, static checks, JS syntax validation and the original quick
reference suite passed. Additional tests cover original-time preservation,
profile retention/band compatibility through upload, complete group/control UI
artifacts, cached/uncached retarded-geometry equivalence, cell/clock bound
retention, terrain cache invalidation, tiling coverage and reviewed silence in
the presence of unrelated strong tones.

## Automatic multi-flyover groups and reserved-audio validation — November 7, 2026

The stationary automatic workflow now searches groups with three or more
flyovers, replacing the pair-only default. It starts with every eligible
nonoverlapping strong-curve pair. A larger group is attempted only if every
immediate smaller subset has a training fit. Each attempt searches the original
region, rather than a selected parent's local peak. Aircraft identities are
normalized across readsb track segments, and assignment maximizes the weakest
event's score. Every selected curve shares the same position and recording
clock; individual events retain separately fitted source tones. Simultaneous
harmonics are not independent flyovers. Unassigned proposals are neither
identified aircraft nor evidence of silence.

This expansion is a finite-grid computational strategy. Coarse geometry at
200 m and scoring at 100 m are followed by progressive refinement around up to
eight competing peaks to the configured final spacing. A parent can be missed
on a finite grid, preventing expansion. This is not a proof that every possible
continuous location has been searched. The 2,048-group per-trial limit fails
explicitly; it never returns a completed result after quietly truncating groups.

The observed proposal interval determines alternating blocks. Their length is
`max(2 * FFT_duration, min(12 s, proposal_duration / 4))`. A half-FFT guard on
each block edge leaves opposite folds separated by a full FFT duration. Training
and reserved windows therefore do not share measured samples. Ridge interpolation
also requires both endpoints to belong to the same fold and block. Each proposal
gets a freshly decoded spectrum at its analysis resolution, with per-frequency
backgrounds estimated only from training frames. Per-frame spectral background
removal uses only that frame.

Training measurements choose location, clock, aircraft, source frequency and
steady/linear-pitch baseline parameters. Full-proposal ideal-flight estimates
are not reused as fitted CPA, speed or range bounds; broad proposal limits
remain. Training determines group expansion and the actual-time winner: more
compatible events first, then the strongest weakest-event score. Reserved
measurements never choose a replacement location, frequency or group. A selected
event that fails validation is not removed to rescue a smaller sequence.

Reserved checks require at least five frames, 95% modeled spectral coverage,
95% baseline coverage, supported ridge samples, ridge RMSE no greater than
`max(1.5 Hz, 1.2% of mean ridge frequency)`, at least the configured 5 dB signal,
a 1 dB improvement over both steady and linear-pitch alternatives, and a flight
model advantage on at least 65% of comparable frames. Aggregate scores are
clipped to −10…30 dB; frame-by-frame preference compares raw measurements so
bright model and baseline bins do not become artificial 30 dB ties. These are
explicit development gates, not calibrated likelihoods.

Proposals and their acoustic evidence were discovered using the complete
recording. The procedure is consequently **proposal-conditioned validation**,
not independent detector validation. Correlated audio frames, annotation
selection, narrow development datasets and unknown real-world false-positive
rates remain limitations. It must not be described as an untouched test set.

Eight default controls repeat the entire group search at ±600, ±900, ±1,200 and
±1,500 seconds; wider clock priors expand these offsets. The conservative
competitor is the maximum reserved weakest-event margin across all groups in
all wrong-time searches, including training winners failing a different quality
gate. A candidate requires all selected events to pass, no distant/common-clock
training ambiguity and at least 1 dB separation from that competitor when one
exists. Missing history or a failed/budget-limited trial makes the run incomplete.
The defaults can still be computationally expensive; bounded scoring workers,
coarse curve envelopes, time indexes and input-bound cached fits are retained.

Reviewed silence continues to exclude shared clocks predicting sound within an
explicit audibility radius. This remains a user-reviewed assumption about audible
coverage, not a deduction from an automatic detector's failure. Jets are included
by default. Stable source pitch, adequate telemetry, a stationary receiver and
still-air propagation are still required. Broad jet roar, RPM changes, wind,
indoor reverberation and receiver motion can defeat this acoustic model.

The website now defaults to this sequence workflow for stationary automatic
searches. Its overview lists every actual-time group, per-aircraft reserved
checks, and structured rejection reasons (geometry, shared arrival timing,
identity reuse, reviewed silence, curve coverage and signal). Doppler overlays
shade training and reserved intervals and preserve all relative event times.
A changed map candidate is not shown as having the selected candidate's validation.
Detailed results and controls remain downloadable as `automatic.json`.

`auto-locate --method pairs --controls 2` preserves the historical method.
`revalidate-sequences` can check saved sequence training winners after a
validation-only implementation change. It verifies original audio, full spectrum,
trajectories, model files and original executable against the saved fingerprint,
regenerates guarded spectra, and changes no fitted parameter or training winner.
It writes to a new directory with the original report digest and both executable
digests. It does not execute the old binary. Maps retain their original training
artifacts; the revalidation overview explicitly identifies its newer checks.

The historical reference-area failure and known-desk-position failure are frozen
in `references/sequence-failures.json`. Evaluation-only aircraft and coordinate
checks are applied after inference by `tools/check_sequence_regressions.py`.
They are never supplied to the Go search. The manifest allows the reference
experiment to remain unresolved; conditional promotion requires additional
sequence evidence rather than relaxed acoustic gates.

An independent closed-form emitted-phase fixture now covers WAV decoding,
automatic curve detection, three-flight fitting, one common clock, reserved-audio
checks, wrong-time controls and progressive 10 m grid refinement. It recovers the
synthetic receiver within the explicit 180 m test tolerance. Type codes exercise
jet/propeller eligibility; synthetic narrow tones are not realistic jet acoustics
and this test does not establish 10 m accuracy. Other tests cover unrelated tones,
aircraft identity reuse, training isolation, cross-fold FFT support, validation-only
input changes, budget failures, cancellation and HTTP report delivery.

The November 7 group search supersedes the earlier limitation on automatic
three-or-more-flight inference. It does not remove the limitations on walking
routes, untrackable jet roar, variable source pitch, telemetry completeness or
independently measured GPS accuracy.

The unchanged hybrid acoustic suite passes all ten development cases. Both
Colsto labels retain 100% anchor hits and coverage (0.64/0.91 Hz RMSE). R04 keeps
95.8% hits and full coverage; A320 keeps 100% hits/coverage; Phenom keeps 80.0%
hits and 83.0% coverage; Challenger keeps 70.6% hits and 72.7% coverage. The five
background controls pass their existing no-strong-curve gates. Partial recovery
is not full jet-curve recovery. The separate quick suite, assisted supplied-track
Phenom regional replay and private tracked-flyover detection/association regression also
pass. These reused development cases are not independent accuracy estimates.

### Completed sequence outcomes

The Colsto known-area run completed **1,411 group searches** across the actual
time and eight wrong-time controls in about 53 minutes 35 seconds with 12 scoring
workers. Every trial searched all 151 eligible pairs; compatible training parents
also produced 15 actual-time triples and 37 control-time triples. No four-event
group survived all training parents. The outcome is **unresolved**, including
when frozen training fits are rechecked with the final validation implementation.

The group selected using training alone contains curves 8, 10 and 13 (one-based
indices into the report's strong-curve list). Its weakest reserved margin is
−13.53 dB. The worst wrong-time group reaches +2.14 dB, producing −15.66 dB
separation; a competing training location/clock also remains. These failures
are shown in the overview. No failed event was removed to replace this group
with a more attractive smaller hypothesis.

The previously identified pair (curves 9 and 13) still fits PC12 `a1dfeb` and
B350 `ace391`. Its training-only position is `40.875939, -73.525102`, about 60 m
from the video's reported model coordinate, and its shared clock correction is
+89.74 s. This coordinate is an evaluation comparison, not verified microphone
GPS. On reserved audio its model exceeds the steady/linear controls by 12.25 and
12.68 dB, but ridge errors are **1.62 and 1.71 Hz**, each exceeding the unchanged
1.50 Hz gate. It therefore also fails validation. The gates were not relaxed to
promote this known reference after viewing the result.

The private November 6 desk diagnostic completed **252 group searches**: 28
eligible pairs across nine time trials, with no compatible training triples.
Its eight strong sound proposals produce **no passing joint aircraft match at
the supplied desk position**. This is a known-position association check, not
blind localization and not evidence that the recording contains no aircraft.
The broader cached trace import covers the time span needed by the final
+1,500-second control; the original truncated attempt remains an incomplete
historical artifact.

The historical snapshot hashes remain unchanged, and
`just sequence-regression-check` passes with reference `unresolved` and desk
`no_match`. Current overviews are
`work/sequence-search/reference-revalidated/automatic.html` and
`work/sequence-search/desk-revalidated/automatic.html`; original training reports
and maps remain in `reference-final/run/` and `desk-final/`. The reference replay
is conditional on a known-area rectangle, not statewide. Neither these
regressions nor the synthetic success establish reliable localization of arbitrary
jet or propeller recordings.

Verification: full Go tests with the race detector pass; final changed error,
identity, partition, overview and web cases also pass targeted race/integration
checks. `go vet`, JavaScript syntax checks and `just build` pass. No acoustic
acceptance labels, historical snapshots or published-jet coverage gates were
weakened. Independent stationary jet recordings with confirmed receiver GPS,
accurate start time and aircraft intervals remain necessary to measure real
localization accuracy and detection error rates.

## Complete local sample audit — November 7, 2026

The collection contains 40 audio files, representing 39 unique byte sequences.
The two November 4 13:01 desk filenames are identical and are evaluated once.
The three DLR channels are separate audio inputs from one array flyover, not
three independent aircraft passages. Every unique file receives fresh full-length
Go hybrid analysis, with the original bytes, executable and decoder pinned.
Python orchestrates the commands and renders evaluation metadata; it performs
no acoustic fitting or localization.

The collection comprises eight unique personal/reference recordings, ten
Livermore passages, thirteen AeroSonic aircraft-present clips, five AeroSonic
background clips and three DLR array channels. Precise surveyed receiver GPS
is absent for the public recordings. Livermore is a city-level label; AeroSonic
coordinates are deliberately displaced, and its filename time zone is unverified;
DLR provides a local Cartesian frame without a GPS origin. No precise GPS success
rate can be calculated by treating these labels as exact microphone coordinates.

For the private stationary recordings, source discovery covers the complete
Texas bounding box with 0.3-degree padding. Fresh closed-date replay indexes
identify aircraft and full trajectories supply geometric altitudes. Existing
GPS-centred discovery caches are not substituted for this state-wide input.
A recording's user-confirmed Central filename start time supplies a ±30 s prior.
The receiver coordinate is read from the private evaluation record only after
inference. Single usable curves receive a single-event screening search;
multiple curves receive automatic shared-position/shared-clock group searches.
Walking audio receives screening only because route reconstruction is unsupported.
Missing geometric traces are reported as a coverage limitation.

The Livermore diagnostic selects the longest automatically usable strong curve
before reading aircraft or location labels. An audio-only sweep aligns publisher
UTC frequency measurements with the recording to infer its sample-zero time.
This clock is assisted rather than independently verified. Supplied-aircraft
KML fits are reported separately from California searches against competing
archived flights; only the latter can test aircraft selection. All spatial
searches retain the complete state mask, 200 m initial grid and progressive
10 m final spacing. The broad pre-existing Livermore evaluation box and private
250 m reference radius are applied after inference and never passed to scoring.

For computational staging, automatic multi-event experiments first run all
eligible actual-time groups. A missing training hypothesis or failed mandatory
reserved-audio check cannot become a positive by comparing wrong times, so these
necessary failures stop with an explicit rejection and controls-not-run note.
Actual-time hypotheses that pass reserved checks require all eight wrong-time
trials. Incomplete searches remain incomplete. This evaluation staging does not
relax a threshold, omit events from a successful group, or change the production
website's default control policy. Source and executable versions are retained
per experiment, including the unchanged single-passage executable.

Artifacts are in `work/sample-audit-2026-11-07/`: `index.html`, `report.json`,
`protocol.json`, individual fresh `analysis/` outputs, `statewide-tracks/fetch.json`,
case input fingerprints, conditional/state-wide result maps and detailed outcomes.
The complete sample list and aliases come from scanning the actual sample tree,
not merely the older catalog. New command recipes are `just sample-audit`,
`just sample-audit-tracks` and `just sample-audit-locations`.

### Collection audit findings and limits

Every unique input completed fresh hybrid analysis. Nine of the ten Pathak
recordings contain automatically strong curve proposals; the ERJ-145 R50 clip
still has none. Seven of the nine supplied-aircraft conditional diagnostics
produce a Livermore-area fit; R06 and R41's selected curves do not fit their own
supplied tracks. These diagnostics identify no aircraft independently and have
no surveyed microphone coordinate.

Five independent competing-aircraft California searches were completed using
full local daily archives. Only Phenom R20 ranks the broad Livermore region first,
and it retains distant ambiguity. R04, R22, R49 and R21 rank locations outside
the publisher's region. Four other competing-flight experiments lack replay
history: May 12/13/17 endpoints returned HTTP 502 on three acquisition passes.
Their conditional supplied-track diagnostics are retained separately. This is
one regional agreement among five completed competing searches, not 20% measured
GPS accuracy: exact public receiver coordinates remain unpublished.

The fresh reviewed YouTube replay retraces the two selected sound windows and
searches New York without supplying receiver GPS or aircraft identity. It returns
`40.876206, -73.525405`, 47.9 m from the video-reported model coordinate, at a 10 m
final grid. The automatic-selection experiment remains unresolved. This reviewed
reproduction and the video reference are not independent surveyed GPS validation.

The November 4 13:01 phone recording has one strong proposal. Its completed
state-wide single-event screen ranks a position about **567 km** from the
user-reported desk, with ambiguity; it fails the location comparison. The 21:49
recording has two strong proposals and its completed Texas joint search finds
no actual-time training match. No wrong-time controls are needed to turn an
absent actual hypothesis into a negative result; the report discloses that they
were not run. The 15:05 recording has no automatically strong curve.

The 15:43 recording has two strong proposals, November 5 has thirteen and
November 6 has eight. Full Texas rankings for these three were not run. A separate
necessary feasibility test uses the known reference only for evaluation: a square
extending 250 m around the supplied desk, 50 m coarse geometry and 10 m dense
scoring. All eligible actual-time pairs are tested: 1, 76 and 28, respectively.
None yields a passing joint training match in that area. The 13:01 single-event
reference-area check also has no match. These are disclosed known-position
failures, not blind localizations or proof that no aircraft is present. In
particular, the earlier isolated tracked-flyover association diagnostic is not invalidated
by a failure to find two distinct aircraft jointly in the full recording.

The walking recording has one strong proposal. The current model does not
reconstruct its moving receiver, and no route or location is established.
Across the thirteen AeroSonic aircraft-present clips, nine contain at least one
strong proposal; four do not. This is usable-curve availability, not verified
aircraft recognition recall. All five labeled backgrounds contain zero strong
proposals. The three seven-second DLR channels contain no strong proposal; their
unpublished GPS origin precludes global position evaluation regardless.

No reliable correct phone location is demonstrated. Passing curve-recovery and
assisted-fit regressions did not establish end-to-end localization reliability:
the broader competing-flight tests expose wrong winners. The current selector
and spectral-ranking policy need stronger aircraft attribution and confuser
rejection. Scores, visual alignment and dense grid spacing must not be presented
as location confidence.
