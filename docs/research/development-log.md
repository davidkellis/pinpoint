# Pinpoint development log

This is the chronological development log that used to be the project
[README](../../README.md). It was written incrementally while the prototype was
built, and it is kept here so that nothing is lost. Entries describe the state of
the project on the day they were written, so earlier entries can be superseded by
later ones (for example, "accuracy has not yet been measured" predates the
November 8 Texas evaluation). For the current status, read the README and the
dated research records it indexes.

Everything below the rule is the original README text, unchanged except for:

- Relative links, rewritten for this directory. Links to sibling research notes
  are now bare file names.
- Texas-area coordinates, which are withheld under the repository privacy rule:
  two search-candidate coordinates in "Texas walking recording" and the
  placeholder latitude/longitude values in the example track CSV, now written
  as `LAT_A`, `LON_A`, `LAT_B` and `LON_B`.

Paths under `work/`, `data/`, `samples/*.mp3` and `references/private/` refer to
local artifacts that are not published; commands that depend on them only run on
the owner's machine.

---

# Pinpoint

Estimate recording locations by comparing aircraft flyby sounds with
Doppler curves predicted from historical ADS-B flight tracks. Written in Go,
with no third-party Go dependencies. FFmpeg handles phone audio/video formats.

**Status: working research prototype for a bounded region and approximate
recording time.** The included synthetic experiment recovers a known location,
aircraft identities, and clock offset. Accuracy on real phone recordings has
not yet been measured. State-wide searches and searches without a state hint
are supported within explicit resource limits. Date-only nationwide searches
and moving-receiver trajectory reconstruction are not implemented.

The input audio contains **aircraft sound**, not ADS-B messages. ADS-B radio
telemetry is downloaded independently. The inspiration is the
[video supplied with this project](https://youtu.be/UytYrhYLOKI).

An additional [experimental jet profile and real-audio regression suite](jet-validation.md)
now recover labeled A320 and Phenom jet curves. Run `just jet-reference-check`
or `just jet-analyze --audio recording.wav --out work/my-jet`. The assisted
Phenom search returns the Livermore region; precise recorder GPS remains unverified.

[Two additional jet recordings](additional-jet-samples.md) now test
Challenger and ERJ-145 curves. `just jet-sample-check` protects partial Challenger
recovery; `just jet-challenge-check --suite all` exercises the unresolved failures,
including a California search where the actual aircraft ranks second behind a
wrong location. Use `just jet-sample-map` to inspect that combined map.

The experimental jet tracker now follows supported peaks beyond a detected
fragment. On the Challenger reference, publisher-label hits increase from 32.1%
to 70.6%; statewide localization remains ambiguous. `just jet-tracking-check`
protects this improvement. The web app now offers the standard detector, the short-sweep profile,
and experimental combined analysis. New map results show full-resolution frequency error and tone contrast
under **Compare Doppler curves**. For rejection reasons and residual plots at a
specific hypothesis, use `just diagnose --config search.json --lat LAT --lon LON
--track TRACK_ID --out work/fit-diagnostic`. See the
[November 6 evaluation](additional-jet-samples.md#november-6-tracking-and-fit-diagnostics).

To look for additional flyovers outside the events used by a completed stationary
search, run the experimental Go follow-up:

```sh
just corroborate --config search.json --results work/results/results.json --out work/followup
just experiment-open work/followup/guided.html
```

It fixes each candidate's position and shared clock, predicts other flight
arrival times, and tests the unused audio against those curves. Source tones are
fitted on training blocks, then checked on separate blocks and against shifted
times and steady tones. Reports show measured spectrograms, predicted overlays,
and competing fits. Passing checks means a review lead, not a confirmed aircraft
or location. This command does not change the website's detection counts or
location results; absence of support is not a silence exclusion. It needs the
original spectrum, tracks, configuration and candidate results. For higher jet
tones, supply a spectrum covering those frequencies and use `--max-hz 6500`.
See [the follow-up method and limitations](additional-jet-samples.md#guided-discovery-outside-the-seed-events).

For a stricter **sequence timing audit**, add `--sequence-controls 19`. It selects
the additional aircraft on training sound, tests the weakest match on separate
blocks, and repeats the whole selection at incorrect times across all candidate
locations. `--sequence-extra 2` requires two additional aircraft plus the anchor.
`--full-curves` is an optional experiment using 120-second windows throughout;
it can miss short or partial passes. These controls diagnose false matches and
do not produce calibrated location confidence.

```sh
just corroborate --config search.json --results work/results/results.json --sequence-controls 19 --max-candidates 64 --out work/sequence-review
```

New `locate` results retain up to 256 spatial/clock hypotheses separately from
the display's top 20. The sequence audit uses this broader pool; older results
need a fresh location search to populate it. Each saved position still carries
its original best clock: discarded clocks and unsearched positions cannot be
reconstructed by the follow-up. The audit and its plots remain separate from
the website's automatic location decision.

To search the original region using **multiple measured curves before choosing
refinement regions**, use the reviewed-window workflow:

```sh
just joint-locate --audio 'samples/Unknown location - raw audio [4CxVxuMVkMc].mp3' --config examples/colsto-measured-joint.json --out work/traced-joint/reference
just experiment-open work/traced-joint/reference/curves.html
```

This example needs the existing New York flight/terrain caches. For another
stationary recording, supply its own time, region, tracks, terrain and reviewed
flyover windows/source-frequency bands. The command measures curves afresh from
audio, requires one strong curve per reviewed window, retains the common-clock
geometry survivors, scores all selected curves jointly, then refines competing
joint fits. The review page links to the map and its Doppler comparisons.
No previous candidate results or expected GPS are supplied to the search.

The reference replay found `40.876206, -73.525405`, about 48 m from the video's
reported model coordinate, using a final 10 m grid. Shifting only the second
winning flight's telemetry by 20 seconds produced no passing joint location.
This is a reviewed reference reproduction, not a measured 48 m accuracy claim
for arbitrary recordings. The standard 40–1,800 Hz detector is used; brief jet
passes still need the experimental profile. This reviewed workflow remains
separate from the automatic sequence search described below. See the
[method, replay and limits](additional-jet-samples.md#measured-curves-before-joint-refinement).

## Automatic sound groups and combined analysis

```sh
just hybrid-analyze --audio recording.mp3 --out work/combined-audio
just hybrid-reference-check
just auto-locate --audio 'samples/Unknown location - raw audio [4CxVxuMVkMc].mp3' --config examples/colsto-automatic.json --out work/automatic-reference
```

The last command needs the existing New York flight/terrain caches. Its event
list is empty: the program selects sound hypotheses from the complete recording.
Use `--profile hybrid` to add the short-sweep detector, or `--detection FILE`
with the corresponding configuration's saved spectrum to reuse an analysis.
When reusing detections, pass `--validation-audio ORIGINAL_FILE` if the saved
spectrum's source path is unavailable. Reserved-audio checks require the original
recording, not just a spectrogram.
The program never extracts ADS-B radio packets from the recording.

Automatic stationary search tests **all eligible nonoverlapping strong-curve
pairs**, then expands compatible pairs into **three-or-more-flyover groups**.
Every immediate smaller subset must have a training fit before a larger group
is attempted. Each larger group searches the original supplied region; it is
not restricted to a winning pair's position. All selected events share one
location and one recording clock, with distinct aircraft identities and a
separately fitted source tone per event. The objective maximizes the weakest
event, so one bright tone cannot conceal another weak match. Overlapping tones
are not treated as separate flyovers. A default 2,048-group budget per trial
fails explicitly instead of silently discarding work.

The full proposed curves determine alternating training/reserved intervals.
FFT-width guards separate their measured samples. New spectra derive frequency
backgrounds from training frames only. Training measurements choose location,
clock, aircraft and source tones; reserved intervals check those frozen choices
against steady-tone and linear-pitch alternatives. The selected training group
prefers more compatible events, then the strongest weakest-event score. A
validation failure leaves it unresolved; the program does not drop its failed
event to rescue a smaller group. Proposals were discovered using the full audio,
so this is **proposal-conditioned validation**, not independent detection
validation or calibrated GPS confidence.

Default wrong-time controls repeat the entire group search and refinement at
eight offsets: ±600, ±900, ±1,200 and ±1,500 seconds, expanded for wide clock
priors. The maximum reserved-event margin across **all** wrong-time groups is
the competitor. Missing telemetry, failed trials and exhausted budgets produce
an incomplete result. Distant near-ties, reserved-audio failures, or less than
1 dB separation remain unresolved. These are development gates, not a measured
false-match probability. CLI `--controls 0`, `2` or `4` allow smaller diagnostic
runs; zero is explicitly unchecked.

The overview shows every actual-time group's rejection reasons and each
aircraft's reserved-audio checks. Maps link measured/predicted Doppler curves;
their overlays shade training and reserved intervals. Complete results and
wrong-time trials are in `automatic.json`. Reviewed silence excludes hypotheses
that predict a flyover within its specified audibility radius at the shared
clock. Unmatched sounds or detector misses never establish silence.

For a reviewed selection, turn off automatic selection and choose its measured
curves; every selected event becomes mandatory. Moving recordings still use
independent-event screening: a walking route is not reconstructed.

Long recordings use overlapping eight-minute analysis blocks with two-minute
padding, bounded parallel workers, and original recording-relative timestamps.
Both FFT resolutions still inspect the complete duration. A separate spectrum
with fewer overlapping frames supports full-recording trajectory scoring.
Median selection accelerates whitening without changing its numerical statistic.
Long-file block whitening can produce different candidate counts from one global
whitening pass. Combined detection can also expose extra engine tones; counts
are sound proposals, not aircraft counts. Changing sound profile replaces the
matching spectrum so higher-frequency curves are never scored against an old
narrow-band cache.

Large search areas split into tiles at explicit grid limits, retaining the
requested spacing. Conservative curve envelopes and cached arrival-time indexes
skip impossible position/clock fits; all survivors receive full scoring.
Progress metadata stays compact; full weak-curve details load in pages of 200
when requested. Original curve indices and saved detections survive restart.
Completed group results can be resumed when executable, original audio, measured
spectrum, telemetry, elevation inputs and settings match. Per-curve spectra and
group fits are cached. Reports retain effective search settings and input
fingerprints. Geometry at 200 m, scoring at 100 m, then progressively denser
searches around up to eight competing peaks reach the requested final spacing.
Finite grids and pruning remain approximations. A 10 m final grid is evaluated
spacing, not demonstrated 10 m accuracy. Statewide combinations can take a long
time; reviewed groups are much cheaper than hundreds of group/control searches.

The previous pair-only method is retained for historical comparisons with
`--method pairs --controls 2`. Validation-only development changes can recheck
frozen sequence winners using `just revalidate-sequences --report OLD/automatic.json
--training-binary ORIGINAL_EXECUTABLE --audio ORIGINAL_AUDIO --out NEW_DIRECTORY`.
This hashes the original binary, audio, spectrum, tracks and terrain, preserves
all training parameters and selection, and records both implementations and the
original report digest. Original files are not overwritten.

The combined development suite preserves the existing two YouTube flyover labels,
R04 propeller label, A320, Phenom and Challenger labels, and five background
controls without relaxing their acceptance limits. The supplied-track Phenom
regional replay also passes when the known single-passage clip explicitly uses
the longest usable measured curve; this is an assisted regional test with
unverified recorder GPS. The full 19-curve reference selection test found the
correct pair first but remained **unresolved** because of competing fits and a
near-equal wrong-time result. These outcomes and the desk-recording checks are
recorded in [the historical pair experiment](additional-jet-samples.md#automatic-sound-pairs-and-combined-analysis-november-6-2026). The new sequence algorithm, reserved-audio gates and evaluation scope are documented in [the November 7 research entry](additional-jet-samples.md#automatic-multi-flyover-groups-and-reserved-audio-validation--november-7-2026).

## Use the web app

Requires Go 1.24+, `just`, `ffmpeg`, and `ffprobe` on PATH. Start the app:

```sh
just serve
```

Open **http://127.0.0.1:8080** and leave the terminal running.

1. Choose **Sound analysis**: long tonal curves, short sweeps, or experimental
   **Long curves + short sweeps** to check both. Drag an audio or video file onto
   the upload box. Analysis starts automatically.
2. Enter the **approximate recording start** and its **time zone**. Recognized
   filenames such as `2026_11_03_18_50_11_1.mp3` prefill a suggested time; check it.
   Choose the uncertainty, from exact to within an hour. A recording date and
   approximate time are still necessary; arbitrary audio does not encode a date.
3. Optionally choose a **state**. Leaving it blank searches eligible flight
   corridors across the contiguous US. To limit the search, click **Draw rectangle**
   on the map and drag between opposite corners, or tap the two corners.
4. Check **I was walking or moving** for a walk. Leave the elevation override
   blank to download terrain and geoid elevations automatically.
5. Click **Find likely locations**. New stationary uploads default to
   **Combine flyovers · stationary** with automatic curve selection. It tests
   compatible groups of two or more strong curves at one location and one clock,
   reserves audio to validate the fitted parameters, and repeats the group search
   at eight wrong recording times. The overview links to group maps, per-aircraft
   rejection reasons and recorded/predicted Doppler comparisons. An unresolved result remains
   unresolved even when its highest-scoring map looks convincing.

The outlined rectangle limits candidate locations, including every refinement
pass. **Redraw rectangle** replaces it; **Clear rectangle** returns to the state
or nationwide search. **Cancel drawing** or Escape leaves the previous area
unchanged. **Search rectangle dimensions** lets you edit its center, width and
height. If a state is selected, only the portion of the rectangle inside that
state is searched. Saved searches and **Adjust search** preserve the exact
rectangle instead of converting it to a square. Dragging normally pans the map;
rectangle drawing starts only when you click its button.

Detection scans the entire recording at two spectrogram resolutions: two-second
windows retain faster changes, while four-second windows resolve narrow tones.
Duplicate ridges/harmonics are merged. Every proposal remains available for
review; the **Recording coverage** timeline and downloadable detection report
show the full interval, not only the first few seconds.

After updating the program, restart `just serve`, reopen your recording, and
select the desired **Sound analysis** and click **Reanalyze recording**.
Compatible standard analyses can reuse cached spectra; changing profiles
redecodes the audio. This keeps
the recording/time/area settings and flight data, and clears previous search
results. It does not download flights or start a location search. You can also
upload a new file. Expand the curve count below Flight history, then click
**View detected Doppler curve** to inspect a measured path on the spectrogram.

Jets are **included by default**. **Exclude known jet aircraft** is an optional
filter for reproducing a propeller-focused search. Turbofan engines can produce
fan tones as well as broadband noise ([NASA acoustic models](https://www.nasa.gov/centers-and-facilities/glenn/acoustics-software/)).
The matcher can test a stable tonal component from either engine type against
flight telemetry. It does not identify an aircraft from broadband roar alone,
and does not model arbitrary engine RPM changes. The standard profile analyzes
40–1,800 Hz; combined analysis adds short sweeps up to 6,500 Hz. Combined
analysis is experimental and retains both source profiles' acoustic checks.

The standard long-curve profile, **including walking/independent searches**,
requires stronger acoustic evidence:

- At least 30 seconds of measured ridge, with at least 85% frame coverage and
  8 dB median contrast against nearby frequencies.
- An ideal Doppler fit substantially better than a linear pitch trend (RMS
  error at most half the linear fit, with a separate absolute error bound).
- Predictions of held-out time blocks better than a fitted linear trend:
  RMS below 70% of the trend's error. The full-data fit retains its absolute
  error bound; the additional fixed held-out Hz cutoff was removed after it
  rejected real reference curves. FFT-window guards separate the samples used
  to fit and check each fold. Ridge selection still uses the full spectrogram;
  this is a model-consistency diagnostic, not an independent validation set.
- Corroboration at another FFT resolution or matching harmonic support in at
  least 65% of eligible samples. A long-window spectrogram can corroborate a
  fast curve directly even when spectral smearing prevents ridge linking.

These are **uncalibrated screening thresholds**, not a probability that a sound
is an aircraft. The two resolutions use the same recording; they are not
independent observations. Cars, sirens and other changing tones can still pass;
faint, maneuvering or variable-RPM aircraft can fail. A convincing flight-track
match, competing-flight comparisons and (for a stationary microphone) jointly
consistent flyovers are still needed. Do not infer silence from rejected curves.

The UI distinguishes **strong acoustic candidates**, **needs review**, and
**weak fragments**, and displays coverage, contrast, harmonic/resolution support
and held-out residuals in the curve viewer. Several fragments/harmonics can come
from one flyover; fragment counts are not aircraft counts. Harmonic duplication
uses relative frequency tolerance, including across different FFT windows.

Joint selection accepts strong complete and partial curves. Complete curves
can constrain closest approach; partial curves constrain the trajectory using
their measured frequency samples at their original recording timestamps, without
inventing a closest-approach time. Automatic selection preserves broad speed,
slant-distance and source-frequency bounds because ideal-flight parameters can
be biased by maneuvers. Reported ideal-fit ranges are not confidence intervals.
Every eligible event is used together; there is no four-event limit. Fewer than
two eligible events stops joint search. No strong evidence stops independent
search before downloading flight history. **Include weak tonal fragments ·
diagnostic search** explicitly restores exploratory independent searches;
**Show weak fragments for review** only changes the visible list. Manual event
selection remains available. A false positive can make joint constraints
inconsistent; inspect the curves before interpreting a map.

The optional **Flyover detection** section exposes editable events. Uncheck
**Use automatically detected flyovers** to choose reviewed curves and
enter measured ranges yourself. For partial curves, enable **Partial flyover**
and leave CPA uncertainty at zero. Joint matching keeps a separate source frequency
for each flight but one shared clock; each event must pass and be assigned a
distinct track. Locations cannot pass by independently shifting their events.

**Reviewed silence intervals** can also reject locations. **Find gaps to review
for silence** suggests gaps without detected ridges, with ten-second margins;
it does not declare them silent. Listen first, then explicitly confirm review to
populate the intervals. The suggested 2 km audibility radius is an editable
assumption. Rejection covers *any part* of the flight within the specified slant
radius whose sound arrives during the interval, even if CPA is outside it.
Missing detections, masked aircraft and incomplete flight history remain limits.
Automatic silence inference without review is not implemented.

**Search settings** defaults to 200 m geometry, 100 m scoring in surviving
regions, then 50 → 25 → 12.5 → **10 m** refinement around up to eight competing
flight combinations/geographic peaks per pass. Finer passes also sample the
shared clock at up to 0.1 s and source tones at up to 0.05 Hz. All original time,
frequency, event and silence constraints remain in force. Change **Final grid
spacing** to choose a different stopping spacing (minimum 5 m), or set **Refined
grid** to 0 to disable refinement. Grid spacing is not a promised accuracy.

Flight history covers the entire recording plus clock uncertainty and
sound-travel margins. **I was walking or moving** selects independent screening:
receiver motion and a shared-clock walking route are **not implemented**. The
stationary joint model is disabled for walks. Independent screening still
processes every selected strong candidate, with per-event progress/maps and no early-event cap;
it does not establish one recording location. Those events share the bounded
CPU worker pool, but their fitted clocks and locations remain independent.

Old saved runs retain their settings and results. Start a new search to use the
new method; automatic search refreshes detection when its version changes.
Existing spectrogram caches keep their original frequency band. Upload the
audio again to use the wider band. Previously saved searches preserve explicit
jet-exclusion settings; uncheck the filter when rerunning them.
Changing a map's display does not rerun the analysis. Scores are uncalibrated fit
measures; competing flight combinations within 1 dB and more than 10 km apart
are flagged as ambiguous, not assigned statistical confidence.

For an example without flight/terrain downloads, click **Try a synthetic sample
recording**. Its audio, trajectories, and expected answer are generated locally;
it is a regression fixture, not a real-phone accuracy claim.

Flight acquisition first reuses prepared daily archives when available. Otherwise
it reads the public ADSB.lol half-hour replay indexes to discover aircraft near
the selected area/time, then downloads their full readsb traces. Replay pressure
altitudes are used neither for geometry nor as substitutes for geometric
altitudes. Same-day traces may be available before the daily archive. Missing
history leaves a retryable **Waiting for flight history** job; missing individual
traces are counted in the report. Coverage is never assumed complete.

The root URL opens a fresh upload screen. **New recording** on a results page
returns there; **Reopen previous recording** opens the last saved job explicitly.
Refreshing a URL containing `?job=…` reconnects to that specific job, and
**Adjust search** changes settings for its existing audio. Jobs and maps live
in `work/web/jobs/`; flight and elevation caches
live in `work/web/cache/`. Completed maps can be reopened at the job URL or as
standalone HTML files. Elevation data uses Mapzen Skadi at three arcseconds plus
the EGM96 geoid, with a default 1.5 m microphone height. Negative bathymetry is
clamped to sea level. A measured ellipsoidal height can override this.

Resource limits are 512 MiB per upload, 8 GB of new flight downloads per search,
and two million coarse candidate locations per event. Regional searches generate
points only around time/speed-compatible flight segments. They preserve the
requested grid spacing; if a nationwide search exceeds the limit, the job asks
for a narrower area or time window rather than silently skipping regions.
State selections use Census polygon boundaries, including a contiguous-US mask
when no state is specified. Bounds are used only for indexing and map framing.
These are ranked fit maps, not calibrated probability or accuracy estimates.

Data references: [ADSB.lol historical data](https://www.adsb.lol/docs/open-data/historical),
[tar1090 replay format implementation](https://github.com/wiedehopf/tar1090/blob/master/html/script.js),
[Census state boundaries](https://www.census.gov/geographies/mapping-files/time-series/geo/carto-boundary-file.html),
[Skadi elevation format](https://github.com/tilezen/joerd/blob/master/docs/formats.md),
and [PROJ EGM96 grid](https://github.com/OSGeo/PROJ-datumgrid/blob/master/egm96_15.gtx).


Optional server settings (all runtime flags use double hyphens):

```sh
just serve --addr 127.0.0.1:8081 --data-dir work/web
just serve --tracks /path/to/tracks.csv
just serve --trace-dir /path/to/extracted/readsb
just serve --max-upload-mb 1024 --max-download-gb 12
```

`--tracks` or `--trace-dir` adds a **Use configured local flight dataset** choice
in the page. The server binds to loopback and is intended for local use. Stop it
with Ctrl+C; interrupted jobs are marked accordingly when it restarts. The
browser can reconnect without interrupting a search, but restarting the server
stops it. Retrying currently repeats event scoring; completed event files are
retained, but scoring does not yet resume from those files.
The generated map can also be opened as a standalone file. Satellite imagery and
flight/elevation downloads need internet access. Uploaded tracks plus an explicit
height override and the plain map background work offline.

## Run the reproducible CLI experiment

Requires Go 1.24+, `just`, `ffmpeg`, and `ffprobe` on PATH.

```sh
just demo
```

This builds the executable and writes the experiment to `work/demo`. That
directory must not already exist; use `just demo --out work/demo-2` for another
run. The demo generates a 220-second
WAV containing two moving, turning aircraft with harmonics, background noise,
and stationary hum. Its track file includes two distractor aircraft. Audio is
synthesized from continuous emitted phase; it does not use the search engine's
piecewise trajectory or frequency predictor.

Open the interactive location map:

```sh
just map
```

It shows a colored grid over satellite imagery, with weaker fits in purple and
stronger fits in red. Click dots to inspect coordinates, scores, aircraft and
clock corrections. Switch between combined and per-flyover scores, coarse and
refined grids, passing locations, and flight-path overlays. Per-flyover scores
use the same shared recording clock as the combined fit.

The report is saved as `work/demo/results/map.html`; you can open that file
directly or share it. Code and search data are embedded. Satellite imagery is
loaded from Esri on demand and requires internet access; the plain-background
option works offline. Colors represent match scores, not probabilities. Gray
points have insufficient coverage for a complete fit, rather than a zero score.
The color scale stays fixed for a selected metric when filtering or panning.

Open `work/demo/results/best-overlay.svg` to compare predicted curves with
the spectrogram, and `work/demo/results/candidates.geojson` in QGIS for the
ranked locations. `truth.json` contains the known synthetic answer.

The initial full demo run recovered the location within approximately **20 m**
and the correct **+2 s** clock correction. That is a synthetic regression result,
not a promised accuracy or a confidence radius. The search used 250 m coarse
spacing and 50 m refinement; spacing and accuracy are different quantities.

Run `just` to list recipes, `just build` to build only, or `just test` to run
the test suite and static checks. Command recipes build automatically and forward
arguments unchanged, including quoted paths with spaces. Runtime options use
double hyphens, for example `just import --from ... --until ...`. To see options,
run `just analyze --help`, `just import --help`, or `just run --help`.

For an existing demo generated before map support was added, refresh its results
without regenerating the audio:

```sh
just locate --config work/demo/search.json --out work/demo/results
just map
```

## Advanced CLI workflow for a phone recording

Keep the phone stationary and preserve the original uninterrupted file. Record
the approximate start time including time zone. A known test location is useful
for evaluating error; do not feed that exact coordinate into the search.

### 1. Analyze the audio

```sh
just analyze --audio /path/to/recording.m4a --out work
```

Outputs:

| File | Purpose |
| --- | --- |
| `spectrogram.bin` | Local versioned analysis cache for the search |
| `spectrogram.svg` | Self-contained spectrogram with labeled axes |
| `spectrogram.png` | Raster spectrogram, time left to right, high frequency at top |
| `analysis.json` | Channel, frequency bins and exact frame timestamps |
| `events.json` | All unverified sound-event proposals, including measured ridges |
| `detection.json` | Detector version, full recording coverage, minute counts, and rejection counts |

The default STFT uses 4096 samples/s, a 2-second Hann window, 0.25-second hops,
and 40–1,800 Hz. Bin spacing is 0.5 Hz; effective spectral resolution is broader
because of the window. FFmpeg performs resampling with anti-aliasing. A single
audio channel is selected (`--channel 0`); try another channel if appropriate.
Stereo averaging can cancel useful sound. Only the first audio stream is used.
There is a 100-million-cell analysis limit; longer files should be processed as
separate clips, with their start times adjusted accordingly.

Review candidate curves in the SVG. Automatic detection uses smooth descending
tonal ridges, so it can miss faint aircraft or suggest cars, speech and harmonics.
Copy useful events into your search config. `start_s` and `end_s` delimit each
curve; `f0_min_hz`/`f0_max_hz` bound the emitted tone frequency, not the entire
observed frequency sweep. A visible harmonic can itself be treated as the modeled
tone with `harmonics: [1]`. Only request additional harmonics when they are visible
and inside the analyzed band. `cpa_s` is the measured delayed CPA time in the recording. A positive
`cpa_tolerance_s` constrains it against flight geometry and the shared recording
clock. Legacy configurations with zero tolerance retain the broader event-window
check; partial flyovers must leave this tolerance zero.

### 2. Obtain historical tracks

[adsb.lol publishes daily readsb archives](https://www.adsb.lol/docs/open-data/historical).
Archive data has its own ODbL license. Discovery uses the yearly GitHub release
repository and defaults to listing sizes and URLs:

```sh
just fetch --date 2025-09-08
just fetch --date 2025-09-08 --download --out data/archive --max-gb 8
```

Use the recording's UTC date; include the adjacent day if the recording or
sound propagation crosses midnight. `--instance prod-1` or `staging-0` selects
another archive instance if present. Archives can be several GB. Downloads check
size and GitHub SHA256 digests when supplied, use temporary files, and refuse
to overwrite existing completed parts. Downloads do not yet resume. The importer
can read the split archive directly, without extracting its contents.

Import just the time range needed for analysis, allowing for clock uncertainty,
sound propagation, and track interpolation. The times below are an example:

```sh
just import --archive 'data/archive/v2025.09.08-planes-readsb-prod-0.tar.??' \
  --from 2025-09-08T20:28:00Z --until 2025-09-08T20:37:00Z \
  --out work/tracks.csv
```

Quote the archive glob so the tool receives it as one argument. Include all parts
from exactly one release. To import already-extracted traces instead, use
`--input data/extracted` in place of `--archive`.

For large data, add `--bounds south,west,north,east` covering the search area **plus
the aircraft corridor**. Include generous padding, e.g. at least the maximum
slant range and additional trajectory context. Filtering aircraft to the listener
rectangle would incorrectly discard nearby flyovers. Archive import decodes up to
eight independent traces concurrently with bounded memory; location search loads
the filtered CSV into memory. Import row order can vary, but tracks are sorted by
ID and time before scoring.

The importer detects gzip by its contents, including the compressed `.json`
files in actual readsb archives, skips stale/ground positions,
and splits continuity at missing heights, filtered points, new legs and long gaps.
It consolidates same-timestamp position updates within 10 metres by retaining
the last update. Inconsistent duplicate groups (including missing geometric
heights) are discarded and break continuity; duplicate counts cover the archive.
Non-increasing timestamps also start a new segment.
The default maximum gap is 30 seconds: real readsb archives can have ordinary
20-second sampling intervals. Larger missing intervals still split tracks and
are not extrapolated by the scorer.
It reports skip counts. It accepts geometric altitude from trace column 10 or
the geometric-altitude flag, converts feet to metres, and **does not silently
substitute pressure altitude**. This conservative choice can exclude real flights
with insufficient geometric altitude. A meteorological altitude correction model
is a future extension.

For a large CLI experiment, `max_grid_points` can explicitly raise the default
250,000-location limit to at most 2,000,000. Setting `omit_unmodeled_points: true`
keeps locations with complete model scores in the output and omits unmodeled
locations, while still counting every evaluated point. These options do not
change scoring or remove below-threshold scores. A coarse grid can miss a narrow
peak entirely; refinement of the highest coarse scores does not guarantee recovery.

Other providers can be converted to this CSV schema:

```csv
track_id,time_unix_s,lat,lon,alt_ellipsoid_m
example-leg-1,1757363400,LAT_A,LON_A,1800
example-leg-1,1757363402,LAT_B,LON_B,1802
```

Coordinates are WGS84 degrees, heights are WGS84 ellipsoidal metres, and times
are UTC Unix seconds, including fractions. IDs identify contiguous flight legs,
not a day's unrelated flights. Duplicate conflicting timestamps are rejected.
An optional sixth column, `type_code`, preserves an ICAO aircraft type designator
such as `C172` or `B737`. New readsb imports include it; legacy five-column input
has unknown types and cannot support known-jet exclusion for those tracks.

### 3. Configure and search

Edit `examples/search.json` for your recording, region, heights and reviewed
events, then run:

```sh
just locate --config examples/search.json --out work/results
just map --results work/results
```

The example's coordinates, date, height and event are placeholders. File paths
in a configuration resolve relative to that file. `recording_start` is the time
of audio sample zero with a UTC offset or `Z`. File modification times are not
automatically trusted. Positive `clock_offset_s` means the fitted recording
started later than the supplied time. Every event shares the same correction.

`observer_height_ellipsoid_m` explicitly supplies the **microphone's** height
above the WGS84 ellipsoid, assumed constant across the search area. For terrain,
remove that field and instead supply:

```json
{
  "terrain_dir": "../data/srtm",
  "terrain_geoid_offset_m": -25.0,
  "microphone_height_agl_m": 1.5
}
```

The offset above is illustrative: use the actual local geoid undulation.
The terrain reader accepts unpacked 1201×1201 or 3601×3601 SRTM `.hgt` tiles
named e.g. `N35W101.hgt`, interpolates their heights, and rejects missing/void
terrain. Conversion is **ellipsoidal height = orthometric elevation + geoid
undulation + microphone height above ground**. A constant geoid correction is
only a local approximation. A raster geoid model is needed for larger regions.

Legacy/independent search evaluates a regular grid in parallel, then refines up
to eight separated coarse candidates. The staged stationary workflow instead
uses coarse geometry to preserve regions before any acoustic threshold and
scores them at 100 m before selecting peaks for progressive local refinement.
`final_grid_m` enables finer passes down to that spacing after `refine_m`; zero
preserves the single refinement pass for older configurations. Competing flight
combinations receive seeds before additional spatial peaks. Final passes constrain
the surviving flight IDs while retaining other flights for silence checks.
It sweeps source frequency per event and one recording clock
offset shared by all events. The threshold `min_event_score_db` must be met by
every event, with at least 95% modeled sample/harmonic coverage. Flight speed and
slant range are conservative filters, not precise estimates automatically
inferred from a short curve. Unreviewed proposals carry
`partial_flyover: true`: their closest approach need not fall inside the visible
sound fragment. Arrival-time geometry determines their nearest range during the
fragment. The measured `ridge` constrains trajectory shape and source-frequency
fitting; scoring cannot simply select an unrelated bright line elsewhere in the
frequency search range. Manually specified complete flyovers retain the
closest-approach timing constraint unless `partial_flyover` is enabled. Reviewed
joint searches accept overlapping events, ordered by start time. Automatic joint
selection retains strong predictive partial curves as well as bounded full
curves and preserves all other proposals for review. Every selected curve must
pass with one position and one clock; there is no separate shift for a partial
curve. Distinct-track assignment also applies to multi-event CLI searches outside
staged mode. Reports expose which events actually constrained the search.

Outputs include `map.html`, `results.json`, `score-grid.geojson`,
`candidates.geojson`, `best-curves.csv` and `best-overlay.svg`. Ranked results
contain up to 20 locations and the best location per winning flight combination.
The score grid retains every evaluated location (including low or missing scores)
and can also be loaded into QGIS. A location may appear in both coarse and refined
passes; these are grid evaluations, not independent probability samples. For
passing locations the map shows the best accepted fit; otherwise it shows the
best complete below-threshold fit. Locations with no complete fit have null
scores. No-match reports still show this evidence but have no best-candidate marker.
`candidate_matches` means these deserve inspection;
it is not proof of location. `no_match` means no tested combination passed the
configured requirements. Neither result establishes that archive coverage was
complete. A winner at a spatial, clock, or frequency boundary calls for expanding
that search range. In legacy/independent mode, weak hypotheses rejected on the coarse grid are not
refined. Staged joint mode does not use coarse acoustic scores to prune regions.

## Physical model and assumptions

For a stationary observer at position `q` and aircraft position `p(te)`:

```text
R(te) = |p(te) - q|
tr    = te + R(te) / c
vr    = dot(v(te), p(te) - q) / R(te)   # positive when receding
f(tr) = f0 * c / (c + vr)
```

Flight samples are converted to Earth-centered Earth-fixed coordinates and
interpolated piecewise linearly. Reception time is inverted to emission time
before computing the radial velocity. Simply shifting a Doppler curve by a
constant delay is incorrect. CPA means closest point of **approach**; its heard
time includes the slant-distance propagation delay.

Each predicted harmonic samples the whitened spectrogram; bounded scores are
averaged over time and harmonics. Per-frequency background subtraction reduces
stationary hum, while per-frame subtraction reduces broadband level changes.
These scores and their threshold are heuristics, not calibrated likelihoods.
The model assumes constant source tone per event, a stationary recorder, still
uniform air, constant sound speed, direct propagation and subsonic flight. It
omits wind, refraction, multipath, source directivity, RPM variation, microphone
clock drift, audio edits and compression artifacts. A single straight flight
can produce mirror ambiguity across its track. ADS-B errors and gaps can dominate
accuracy even with a very fine search grid.

Silence filtering is opt-in. Reviewed `silence_intervals` reject a candidate
when a retained flight has a delayed CPA inside the interval and within its
explicit `max_slant_m` audibility radius, at the same fitted recording clock.
Automatic detector misses are never converted into silence constraints. Missing
telemetry, masking and atmospheric conditions can invalidate this assumption.
Known jet types are excluded when `exclude_known_jets` is enabled; unknown types
remain eligible. Web searches enable this filter by default.

## Validation and next implementation stages

```sh
just test
```

Tests compare FFT output with a direct DFT, retarded Doppler with an independent
closed-form solution, check CSV/readsb altitude conversion and gap handling,
terrain interpolation/voids, recover a synthetic location from a decoded WAV,
and reject silence and inconsistent clocks. The audio integration test requires
FFmpeg and is skipped with `go test -short ./...`. CI installs FFmpeg.

Map tests verify full-grid export, missing-versus-low scores, agreement with the
ranked results, and safe embedding of recording names. Leaflet 1.9.4 is bundled
under its BSD-2-Clause license in `internal/mapview/assets/LEAFLET-LICENSE`;
the generated report includes that license and visible imagery attribution.

Web tests upload a synthesized WAV over HTTP, detect a flyover, upload flight
tracks, run a location search, and retrieve the map. They also exercise automatic
archive download/import through a small split-tar fixture, cache reuse,
cancellation, restart recovery, malformed uploads and cross-origin rejection.
The fixture verifies the archive pipeline without downloading a real daily dump.
Additional tests cover replay indexes and cache limits, Texas/Central time and
DST transitions, state polygon exclusion, geoid interpolation, automatic
independent event maps, and retryable unavailable history.

Next work needed to reach the full vision:

1. Collect phone recordings with ground-truth positions, times and held-out
   aircraft. Measure detection recall, false matches and actual location error.
2. Improve event/ridge fitting, harmonic identification, source-frequency drift,
   and calibrated confidence beyond the current distant-near-tie warning.
3. Scale beyond the current flight-corridor index with partitioned searches and
   checkpoints. The web workflow caps each event at two million candidate points
   and 2,001 clock samples; the CLI defaults to 250,000 points unless raised.
4. Extend the new staged delayed-CPA search to date-only nationwide acquisition.
   The current website still requires an approximate recording start time.
5. Fit receiver motion, add meteorological corrections and pressure-altitude
   uncertainty, and improve telemetry reconstruction and coverage diagnostics.
   Spatial EGM96 geoid corrections are already used by automatic terrain mode.

## Primary references

- [readsb trace and altitude schema](https://github.com/wiedehopf/readsb/blob/dev/README-json.md#trace-jsons)
- [adsb.lol archive format and download information](https://www.adsb.lol/docs/open-data/historical)
- [NASA Doppler equations](https://www.grc.nasa.gov/www/k-12/airplane/doppler.html)
- [USGS SRTM format and vertical reference](https://lpdaac.usgs.gov/documents/179/SRTM_User_Guide_V3.pdf)
- [FFmpeg audio decoding options](https://www.ffmpeg.org/ffmpeg.html)
- [OpenSky historical data interface, as a possible future provider](https://openskynetwork.github.io/opensky-api/trino)

## Real recording reproduction: September 8, 2026

The [source recording](https://www.youtube.com/watch?v=4CxVxuMVkMc) from
[colsto's demonstration](https://www.youtube.com/watch?v=UytYrhYLOKI) was tested
against the [September 8, 2026 adsb.lol archive](https://github.com/adsblol/globe_history_2026/releases/tag/v2026.09.08-planes-readsb-prod-0).
The user confirmed the year. The downloaded archive is 3,874,165,248 bytes; both
parts passed their published SHA-256 checks. The New York bounding rectangle
includes adjoining areas; aircraft were imported with an additional border
buffer for 19:30–22:30 UTC. This produced 1,166,464 positions and 9,280 usable
track segments.

The statewide run evaluated **1,618,568 locations** at 450 m spacing with 50 m
refinement. Its winning pair was **N22NY (PC12, `a1dfeb`)** and **N93CQ (B350,
`ace391`)**. Local refinement of clock and source frequency retained the winning
coordinate **40.876274, -73.524955**, approximately **21.4 m** from the video's
reported model point, **40.876466, -73.524951**. The fitted recording start was
**2026-09-08 20:17:29 UTC**. The statewide winning pair scored 12.35 dB versus
10.63 for the highest other flight pair in that pass. Local refinement improved
the winning score to 13.33 dB; competitors were not given that additional sweep.
Scores are heuristic evidence, not confidence probabilities.

Open the prepared maps on this workspace:

```sh
just map --results work/reproduction/final-results
just map --results work/reproduction/dense-state-results
```

Rerun using the prepared spectrogram, flight CSV and terrain tiles:

```sh
just locate --config examples/colsto-2026-09-08.json --out work/reproduction/state-rerun
just locate --config work/reproduction/search-final.json --out work/reproduction/final-rerun
```

The second configuration refines the first run's winning region. These commands
reuse local data under ignored `work/` and `data/` directories; a fresh checkout
does not contain those downloads. The verified flight archive is also linked into
`work/web/cache/2026-09-08/prod-0`, so the browser reuses it without downloading
another copy. To make the filtered CSV available in the upload page:

```sh
just serve --tracks work/reproduction/ny-tracks.csv
```

This reproduction manually selected audio windows 2100–2280 s and 3040–3140 s.
It used a 4 s FFT window, 0.5 s hop, channel 0, a 40–700 Hz band, a single shared
clock correction, and an independent source-frequency sweep per flyover.
Skadi terrain tiles were sampled at 3 arcseconds; underwater elevations were
clamped to the water surface. A statewide -33 m EGM96 geoid approximation and
5 m microphone height were used. Propagation assumes still air at 343 m/s.
The recording was the user's MP3 download, including YouTube's prior compression.

The supplied reference coordinate was used in diagnostic fits after a 2 km grid
missed the narrow peak. The successful 450 m pass searched the full New York
rectangle and refined its own winning candidates. This is a reproduction with
known-reference diagnostics, not a blinded accuracy benchmark. The reported
21.4 m is agreement with the video's model point, not surveyed camera accuracy.
The faint third flyover did **not** independently select this location in the
tested statewide configuration, so that check remains inconclusive.

Detailed provenance and settings are in `work/reproduction/experiment.json`.
The interactive maps, numerical results, and `event-overlays.png` are under
`work/reproduction/final-results/`; earlier statewide results are retained under
`work/reproduction/dense-state-results/`.

## Texas walking recording

The automatic web workflow was run against `2026_11_03_18_50_11_1.mp3`
(63 minutes, 32 seconds). The owner confirmed Texas, a walk, and a start time on
the evening of November 3, 2026 in America/Chicago. The search allowed a
±30-second clock error and used Texas state polygons, without a town or known
location as a prior.

One 20.5-second tonal event passed automatic screening.
The needed replay window yielded 739 aircraft traces and 27,231 usable geometric
positions. Terrain, the EGM96 geoid, a 450 m initial grid, and 50 m refinement
were applied automatically. The search evaluated 583,734 locations.

**The outcome is ambiguous.** The leading candidate is
a Texas-area coordinate (withheld, 9.012 dB), while a distant alternative at
another Texas-area coordinate (withheld) scores 8.694 dB. These are fit scores, not probabilities.
The selected sound is not independently confirmed as an aircraft. The true
recording position was not provided until after this run, and walking motion
was not fitted. This run
demonstrates the automatic pipeline; it does not establish the walk's location.

After `just serve`, reopen the saved run:

<http://127.0.0.1:8080/?job=6f75e80cfbd599c620fe38353e261504>

The full experiment record is `work/texas-walk/experiment.json`, and the map is
`work/web/jobs/6f75e80cfbd599c620fe38353e261504/event-01/map.html`.
These local artifacts are not distributed with the source code.


### Subsequent ground-truth check

The owner later identified the walk as a neighborhood park in Texas
(its approximate center is kept in the private, gitignored site labels).
The original top candidate is about **349 km from that park**, so the original
result is a failed localization, not a verified match.

A separate diagnostic preserved the blind search and reran its original model
on a dense grid of locations across the park. None reached
spectral scoring: no track met the original closest-approach/time/distance
requirements there. Relaxing the closest-approach gate at the approximate park
center also failed to support the selected fragment: the best of five nearby
stored tracks scored 3.857 dB, below the 5 dB threshold. This neither confirms the
fragment as an aircraft nor proves that the hour-long recording lacks other
usable flyovers. The original automatic detector selected only 20.5 seconds of that hour.

This exposed an implementation limitation in the original model: partial-ridge
proposals can occur away from closest approach, while the old scorer required
closest approach within the event window plus 10 seconds. The brightness score
also lacks calibration against chance matches across the large search space.
Walking motion is another unsupported model component, but is not established
as the primary cause of this failure.

Evidence is saved separately in
`work/texas-walk/park-validation/ground-truth-audit.json` and
`work/texas-walk/park-validation/model-comparison.png`.


### Whole-recording correction

The revised detector, CLI analysis, and automatic web search use one shared
whole-recording detector. Regression tests cover fourteen events, a quiet later
pass, concurrent passes, harmonic copies, long ridges, partial flyovers outside
CPA, full-duration flight-history selection, and continued processing after an
individual event cannot be saved. The web API serves event maps beyond event 8.
Candidate sound detection still cannot establish that a sound is an aircraft;
false positives and missed flyovers remain possible.

A fresh upload of the 63-minute Texas recording was checked with detector
`whole-recording-v3`: **143 candidate sound events**, all searched using the full
recording's available flight history. The known park bounds were supplied only
for this diagnostic, on a 50 m grid: 29 events had candidate fits, 114 did not,
and none failed to process. These counts describe sound-event windows, not
confirmed or distinct aircraft. This does not establish independent statewide
location recovery. The broader spectral neighborhood also recovers both known
aircraft in the synthetic regression, including rapid frequency changes.

After `just serve`, view the explicitly labeled validation run:

<http://127.0.0.1:8080/?job=4f1e499923687abb61988712b6b380c2>

The coverage timeline, every event outcome, and per-event maps are available
there. **Adjust search** returns from the map to coverage and settings. The
input settings, detection version, full flight-history interval, and outcome
counts are recorded in
`work/texas-walk/park-validation/whole-recording-experiment.json`.


## Map context and Doppler evidence

Both the search-area map and result maps show state borders from the bundled
Census cartographic boundaries. Display outlines are simplified by at most
0.001 degrees; the search still uses the unchanged full-resolution polygons.
Choose **Street map**, or keep satellite imagery and enable **Roads and labels**.
These tiles are provided by Esri; state borders remain available offline.
The reference layers are [World Transportation](https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Transportation/MapServer)
and [World Boundaries and Places](https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer).

Open **View detected Doppler curve** in a sound-event card, or **Compare Doppler
curves** in an event outcome/result map. The focused spectrogram shows measured
spectral power in white, the independently detected ridge in cyan, and the
candidate flight prediction in orange. Toggle either curve, adjust spectrogram
brightness, or choose another ranked flight combination. Coordinates, source
frequency, fit score, and recording-clock offset are displayed with the curves.
A joint result shows every selected event using the same candidate location and
clock offset; the viewer never independently shifts a curve for a prettier fit.
New exported maps embed these plots. Older web maps are displayed with current
controls, and their evidence is reconstructed on demand from saved inputs,
without rerunning the location search or changing its scores.

### Methodology audit against the supplied video transcript

The automatic walking workflow used for job
`de5ae6eafcde9e393572e3e8780d49a1` did **not** reproduce the video's complete
pipeline. Implementing propagation physics alone is not equivalent to its
geolocation method. Its crucial constraints come from measuring useful curves,
excluding known jets, narrowing speed and slant distance, aligning delayed CPA
times, and jointly testing distinct flights with one recording clock.

That job searched a custom approximately 100 km area inside Texas, not the
entire state. It retained 143 sound candidates and returned
94 single-event threshold passes. None of those events' top-ranked positions was
within 5 km of the user-supplied park center: minimum error 6.6 km,
median 32.3 km, maximum 65.2 km. Eighty-three events were marked ambiguous.
Twenty-three event maps contained a passing point within 1 km, which does not
establish the park as the answer among many competing positions.

Event 1 illustrates the failure: its 12.5-second ridge drops only 3 Hz. A simple
linear frequency trend fits the ridge with 0.60 Hz RMS error, compared with
1.26 Hz for the highest-scoring aircraft prediction. The scorer accepts the
curve within a broad error tolerance, then ranks spectral energy. It does not
test whether aircraft geometry explains the sound better than a generic tone
or changing pitch. Searching many locations, aircraft and clock offsets makes
accidental fits likely; the score is not a statistical significance measure.
The 1 km coarse grid and refinement of only eight accepted peaks can also miss
narrow correct peaks. Independent clocks and unconstrained positions across
events discard the video's strongest evidence.

These are **post-search diagnostics using a disclosed reference location**,
not a blind validation. The machine-readable audit is stored locally at
`work/texas-walk/search-audit/<area>-independent-audit.json`.

The implementation target is the transcript's ordered pipeline: reviewed tonal
events and measured parameter ranges; known-jet exclusion with unknown types
retained; terrain-aware coarse geometry and delayed-CPA constraints; optional
explicit silence constraints; dense scoring of all surviving regions using
retarded-time trajectories; locked multi-event timing with separate source
frequencies; ranking by distinct flight combinations; then local refinement.
Silence thresholds are not specified in the supplied transcript and must be
explicit assumptions, not presented as a recovered implementation detail.
Stationary validation must use the original sample with its answer withheld
from the search bounds. A walking recording additionally requires a shared
clock and constrained receiver motion; independent maps do not supply that.

| Video step | Implementation |
| --- | --- |
| Extract acoustic Doppler curves | Whole-recording STFT at two resolutions, coverage/contrast checks, held-out Doppler predictions versus a linear pitch trend, harmonic/resolution corroboration and padded parameter ranges; aircraft identity remains unconfirmed. |
| Download matching flight history | Available trajectories for the full recording interval, clock uncertainty, and propagation padding; geometry requires usable geometric altitude. |
| Terrain and slant distance | WGS84 3D geometry with terrain and EGM96 geoid correction. |
| Initial speed/distance and time filtering | Reviewed speed/slant bounds and delayed-CPA uncertainty constrain distinct flight combinations to a common recording-clock interval. Partial curves use conservative received-sound overlap intervals instead of assumed CPA times. The coarse geometry stage expands spatial/timing tolerances for the cell size. Automatic selection preserves broad trajectory bounds; exact shape and coverage are tested during scoring. |
| Actual flight-path Doppler shape | Interpolated 3D trajectories, radial velocity, and solving emission time for each arrival time. This includes the nonuniform sound-travel delay described in the video. |
| Source-frequency fitting | Measured ridges constrain fitted source frequency; reviewed events without ridges use a frequency sweep. The whitened spectrogram supplies the score. |
| Locked multi-flight timing | **Combine flyovers · stationary** fits one location and one recording-clock offset. Reviewed selection requires every selected complete/partial event. Automatic mode expands compatible strong-curve pairs into larger groups, fits on guarded training intervals, validates frozen predictions on reserved intervals, and repeats the group search at eight wrong recording times. Source frequency is fitted separately for each event. Curve comparisons show fixed interval offsets and, where measured, observed/predicted CPA gaps. |
| Walking/initial screening | **Screen each sound independently** gives separate candidate maps and clocks. It is explicitly not the video's joint verification, and does not infer a walking route. |
| Flight-pair ranking and refinement | Stationary joint mode applies geometry at <=200 m, scores every surviving region at 100 m, then progressively refines up to eight competing flight combinations/spatial peaks per pass, down to the requested final spacing. Reports rank winning distinct flight combinations. |
| Exclude known jets | Imported readsb type codes are preserved through CSV and event slicing; a pinned ICAO engine-class lookup optionally excludes known jets and retains unknown types. Jets are included by default. Reports count excluded and unknown tracks. |
| Reject candidates using silence | Optional reviewed intervals with explicit audibility radii; all modeled arrival intervals within range are checked using the candidate’s shared clock. Gaps can be suggested for review; no automatic declaration of silence or claim to reproduce undisclosed thresholds. |

For stationary joint verification, review the detected curves, clear the event
selection, select at least two distinct flyover curves, and choose **Combine
flyovers · stationary**. For complete flyovers, enter each measured delayed CPA,
its uncertainty, and conservative speed/slant-distance bounds. For partial curves,
keep the partial-flyover option and leave CPA uncertainty at zero. Uncheck that option
only after confirming that the window includes closest approach. The UI preserves
these CPA measurements when submitting; it no longer replaces them with window
midpoints. The UI and backend reject this mode when the recording
is marked as moving. This is a fixed-observer, still-air model; wind, receiver
velocity, variable engine/propeller source frequency, missing ADS-B coverage,
and false sound detections remain limitations. Scores are not probabilities.

### Revised stationary reproduction

The revised stationary workflow is reproducible with the prepared original
sample data in this workspace:

```sh
just locate --config examples/colsto-staged.json --out work/methodology/colsto-rerun
just map --results work/methodology/colsto-staged
```

The fixed configuration covers the New York state bounds and polygon, with a
200 m geometry grid, 100 m dense scoring, optional 50 m refinement, and the
previously reviewed windows at 2100–2280 s and 3040–3140 s. Measured delayed CPA
times are 2155 s and 3076 s, each with ±5 s uncertainty; both events share the
recording clock. Terrain uses the spatial EGM96 geoid. Silence is left disabled
because the original video's exact audibility thresholds were not supplied.

This pass tested 1,039,962 coarse locations; 1,676 survived geometry, generating
9,411 dense positions and 159 refinement positions. In the relevant flight
window, it excluded 498 known-jet track segments and retained 973 known nonjet
and 81 unknown-type segments. The winning pair remains `a1dfeb` (PC12) and
`ace391` (B350), at **40.875825, -73.525079**, approximately **72.2 m** from the
video's model coordinate. The recording start fits **20:17:29.5 UTC**. This is a
known-sample reproduction with previously reviewed windows and bounds derived
from the state, not an independent blind accuracy test or a validated phone-walk
result. The earlier 21.4 m local fit used a finer source-frequency/time sweep and
different terrain assumptions; its number should not be compared as a controlled
accuracy benchmark.

The legacy CSV was enriched using type fields from the same dated raw archive,
without changing its positions. Inputs and provenance are under
`work/methodology/`; these large local datasets are not included in a fresh
checkout. Fresh imports preserve the optional `type_code` CSV column. Old
five-column CSV files still work, with their aircraft types reported as unknown.
The pinned engine-class lookup and its source SHA-256 are in
`internal/tracks/data/type-engines.json`, derived from
[tar1090-db's ICAO type table](https://github.com/wiedehopf/tar1090-db/tree/master/db).

Remaining gaps include validation/calibration of automatic curve selection and
parameter ranges, calibrated false-match rejection, date-only nationwide acquisition,
and a shared-clock moving-receiver model. Staged coarse-cell padding is a
geometric approximation; tight measurements, unusual maneuvers or terrain
variation still require sensitivity checks. The transcript itself does not
establish that every phone recording contains enough identifiable evidence.

### Progressive refinement follow-up

`examples/colsto-staged.json` now requests a final 10 m grid. With the same
reviewed windows, New York search bounds and prepared data, the follow-up run
returned **40.876229, -73.525287**, about **38.7 m** from the supplied video model
coordinate, with the same PC12/B350 pair. The shared recording-clock correction
was +89.273 s. This pass evaluated 10,011 acoustic locations, including 600
refinement evaluations after 9,411 dense points. Results are in
`work/methodology-v2/colsto-refined-clock/`.

This remains a reference-assisted, reviewed stationary reproduction, with silence
disabled; it is not blind validation or evidence of walking accuracy. A spatial-only
10 m run initially moved *farther* from the reference (about 199 m). Refining the
source frequencies and shared clock as well improved the result. This experiment
illustrates why a small grid spacing alone is not a location-accuracy guarantee.
The automatic selection pipeline is separately exercised with synthesized audio;
its success is not claimed for the original recording or the phone walk.

### Parallel execution and measured performance

Audio decoding runs in FFmpeg. Spectrogram analysis, event detection, trajectory
processing, propagation physics, candidate search and scoring are Go code.
JavaScript renders maps and evidence; Python is not a runtime dependency.
Web scoring defaults to the configured Go CPU parallelism (up to 32 workers).
Two independent events can overlap, sharing one cancellable scoring-worker
budget. Spectra and trajectories are read-only; each event owns its terrain
reader, grid and output files. Terrain cache downloads are coordinated with
at most four in flight. Event concurrency is capped at four to bound concurrent
grid/terrain/report allocations. Joint events remain coupled in one search.
Progress shows completed events and the work underway in each active event.

```sh
just serve --workers 8 --event-workers 2
```

`--workers` is the total point-scoring budget, not a per-event multiplier.
`--event-workers 1` restores sequential event processing. Preparation and output
also use CPU; `GOMAXPROCS` limits overall Go CPU parallelism if needed. Flight
history, terrain and spectrograms are cached. Restart only after an active run
has finished; these settings cannot change a running process.

A four-event batch using a saved real recording and the same eight scoring
workers took a median 6.46 s sequentially and 3.38 s with two concurrent events
(three runs each). All score reports were byte-for-byte identical. This small
cached-area benchmark is not a promised speedup for an entire state; timing is
affected by other workloads. Reproduce using a completed small-area job:

```sh
PINPOINT_BENCH_JOB=/absolute/path/to/job go test ./internal/webapp --run '^$' --bench BenchmarkIndependentEvents --benchtime=1x --count=3 --benchmem
```

The scoring hot path now prepares observed ridge samples once and reuses bounded
scratch storage across frequency trials. A focused benchmark with three tracks,
a partial ridge and 61 clock samples improved from a median 4.69 ms to 1.62 ms
per point on this development machine; temporary allocations fell from about
192 KB to 6 KB per point. This is a microbenchmark, not a promised statewide
runtime: the machine was also running other workloads. A real saved event's
280-point score report was byte-for-byte identical before and after the change.
Reproduce the benchmark with:

```sh
go test ./internal/search --run '^$' --bench BenchmarkAutomatic --benchmem
```

### Acoustic screening and jet support update (November 4, 2026)

The saved upload `463ec91ee700df1cdb56b492ceea8cb1` contained **145 tonal
fragments**, of which only **two** passed the previous curve-shape/joint-timing
check. The walking workflow nevertheless searched all fragments independently.
That policy could produce many unrelated maps; neither 145 nor two was a count
of confirmed aircraft. Automatic independent selection now uses the same
strong-evidence gate as joint selection, with joint timing as an additional
requirement for stationary searches.

The earlier detector version was `whole-recording-v7`. The saved phone audio was
reanalyzed separately in `work/curve-quality/wide/` with the wider frequency band;
it retained **1,325 low-level fragments, zero strong candidates, and one curve
requiring review** late in the recording. The wider band retains more diagnostic
fragments; none automatically starts a flight search. This does not establish
that no aircraft were present. No flight-location conclusion is drawn from
acoustic screening alone. The test
suite includes an independently synthesized 800 Hz source moving at 180 m/s,
with retarded-time received phase, background noise and a stationary hum. It
must produce one strong, jointly usable curve after two-resolution analysis.
Additional checks reject a linear pitch sweep, a sparse ridge, broadband noise,
stationary tones and brief chirps. An HTTP upload-to-map test uses a known A320
type to verify that the default aircraft filter admits jets.

This validates those fixtures, not precision/recall on real airliners. The next
validation step is a labeled set of real flyovers and non-aircraft sounds,
separated by recording for calibration and held-out evaluation. No fixed target
of five or six detections is imposed on this walk. Walking motion, masking,
variable engine tones and track coverage remain unresolved accuracy limits.

### Reference-recording detection regression fix (November 4, 2026)

`whole-recording-v8` fixes a real-data regression missed by the synthetic tests.
Long-lived background tracks previously took priority over passing tones at
frequency crossings. The second reference flyover was fragmented, and the
evidence checks rejected those pieces. Tracking now prioritizes recent measured
strength, retraces promising seeds along spectral energy within a bounded
corridor, and joins overlapping pieces only when their measured frequencies
agree and the combined path passes the evidence checks again. Missing spectral
measurements are not filled with modeled frequencies. A longer supported pass
takes precedence over a short, nearly perfect fragment during deduplication.

Both complete input recordings were checked using their cached two- and
four-second spectra (40–1,800 Hz). The cached audio files have the same SHA-256
hashes as the supplied originals. There are no file names, timestamps, locations
or expected aircraft counts embedded in the detector.

| Recording | Whole-file result | Interpretation |
| --- | --- | --- |
| `Unknown location - raw audio [4CxVxuMVkMc].mp3` | 19 strong acoustic candidates; both known reference flyovers recovered | The known paths span 2124–2243 s and 3036–3133 s, with arrival CPAs about 2156.8 s and 3077.9 s. Both support joint timing. Other candidates are unconfirmed sounds, potentially including ground traffic. This is not 19 identified aircraft. |
| `walking_2026_11_03_18_50_11_1.mp3` | Six curves needing review; no strong candidates | Those curves lack sufficient corroboration at a second resolution or matching harmonics. An experimental eight-second low-frequency pass did not establish a strong curve either. This does not establish that the walk contained no aircraft. |

The real reference regression tests use numerical spectral excerpts around
both passes and assert the measured frequency paths and usable CPA estimates,
not merely a nonzero detection count. See
[`internal/acoustic/testdata/README.md`](../../internal/acoustic/testdata/README.md)
for provenance. Full-file results and a plot of both recovered paths are in
`work/detector-regression/`. The reference is now a development regression, not
an independent accuracy benchmark. Neither this detection check nor the prior
manually selected location reproduction establishes blind geolocation accuracy.

### Comparing Doppler extraction methods

`just experiment` runs a Go comparison workbench. It writes an interactive local
report with the original spectral energy, nine analysis variants, optional
detected-path overlays, frequency/contrast controls, and evaluation against known
paths. It does not replace the web application's detector or rerun location
searches. FFmpeg decodes the audio; the FFT, transformations, tracking and
evaluation run in Go.

```sh
# Run the reference excerpts, walking excerpts, injections and controls.
just experiment --workers 2
# Open the already generated report; this does not analyze again.
just experiment-open

# Repeat the injection experiment using a different walking-noise segment.
just experiment --config examples/curve-experiments-holdout.json \
  --out work/curve-experiments-holdout --workers 2

# Compare the promising variants across all three complete recordings.
just experiment --config examples/curve-experiments-full.json \
  --out work/curve-experiments-full --workers 2
just experiment-open work/curve-experiments-full/index.html

# A small, selectable comparison.
just experiment --only reference-2 \
  --methods production-2+4s,harmonic-2s,local-2s --out work/curve-excerpt
```

The manifests contain local paths and can be edited for another recording.
Each case specifies `audio`, `start_s`, `duration_s`, an identifier and label.
Use `kind: "unlabeled"` for real audio without timestamp annotations. Intervals
are in seconds relative to the original file. Optional truth anchors are in
seconds **relative to the excerpt** and are used only for evaluation and the
separately toggled yellow overlay; the detector never receives them. Reports
record the source SHA-256, selected methods, exact manifest, detailed detection
JSON and a CSV summary. Workers are bounded to 1–4 independent cases. Two workers
are useful for the full-recording suite; more workers increase memory use.

| Method | What changes |
| --- | --- |
| `stft-1s`, `stft-2s`, `stft-4s`, `stft-8s` | Hann-window duration; hop is one eighth of the window. Longer windows resolve nearby steady tones better but smear rapidly changing tones. |
| `local-2s` | Subtract nearby-frequency background after the existing temporal/frame median normalization. |
| `reassigned-1s`, `reassigned-2s` | Move measured spectral energy to estimated instantaneous frequencies using a derivative-window FFT. Frequency only; no time reassignment. |
| `harmonic-2s` | Pool contrast at 1×, 2× and 3× frequency with declining weights. This is an evidence projection, not raw audio energy, and can suppress a solitary tone. |
| `production-2+4s` | The current two-resolution detection and joining pipeline. |

The reassignment calculation follows the frequency formula documented by
[librosa](https://librosa.org/doc/0.11.0/generated/librosa.reassigned_spectrogram.html).
Low-energy bins are excluded before dividing by the original FFT. Numerical
tests check frequency accuracy, concentration and energy conservation on an
off-bin tone, and check that the ordinary spectrum matches production STFT
normalization. The existing detector and thresholds are held fixed across
representations; their evidence labels are **not calibrated probabilities**.
Transforming contrast changes its statistics, so a higher evidence count is
not by itself an improvement. Harmonic evidence computed on a pooled spectrum
is also not independent validation. The ordinary alternate-resolution spectrum
is supplied for corroboration, but that check does not invalidate every path
that already passed the existing harmonic gate.

The November 4 experiment evaluated two known reference excerpts, six unlabeled
walking excerpts, six synthetic flyovers injected into November 4 walking audio,
four disruption/noise controls, and one simulated passing vehicle. A follow-up
used the same injection parameters on a separate November 3 noise segment, plus
two newly seeded noise controls. The second set tests transfer to another noise
background; it is not a labeled real-aircraft test set.

The table counts known paths recovered **and** passing the existing strong
evidence gate. Recovery requires one measured ridge to match at least 80% of
the time-frequency anchors within `max(3 Hz, 1.5% of frequency)`, allowing one
fixed harmonic ratio. The reference anchors are sparse manual development
labels; the injection anchors are generated from the simulated source.

| Method | Reference excerpts (2) | Injections in Nov 4 noise (6) | Injections in Nov 3 noise (6) |
| --- | ---: | ---: | ---: |
| Production 2+4 s | 0 | 3 | 3 |
| 1 s STFT | 0 | 3 | 3 |
| 2 s STFT | 0 | 2 | 2 |
| 4 s STFT | 0 | 2 | 1 |
| 8 s STFT | 2 | 1 | 1 |
| Local background subtraction | 1 | 1 | 2 |
| 1 s frequency reassignment | 0 | 2 | 2 |
| 2 s frequency reassignment | 0 | 2 | 0 |
| Harmonic pooling | 2 | 4 | 2 |

**The excerpt baseline result does not mean the reference regression returned.**
The production detector recovers both flyovers when processing the whole
reference file. Short excerpts change temporal median normalization and the
track context, which fragmented their detections under this stricter anchor
coverage metric. That sensitivity is itself a finding and should be addressed
before generalizing excerpt performance to whole files.

Local background subtraction and harmonic pooling each recovered the entire
800 Hz simulated jet trace at −25 dB in the November 4 noise segment; the
production result covered only 41% of its anchors. However, neither enhancement
improved all injections or both noise backgrounds. Reassignment did not improve
overall recovery in this experiment. Eight-second windows helped the reference
propeller curves but failed to recover the rapidly changing jet injections.

No strong detections occurred in the six noise/disruption control cases across
the two suites. That small count is not a measured population false-alarm rate.
Several methods, including production, strongly detected the passing-vehicle
control: **a good Doppler fit is not proof of an aircraft**. All six unmodified
walking excerpts yielded no strong candidates. They are unlabeled, not verified
aircraft-free negatives; any reported overhead aircraft must not be counted as absent.

Synthetic signals use independent emission-time bisection, distance-dependent
amplitude and constant source tones. The propeller simulation has three harmonics;
the jet simulation has one stable 800 Hz tone. The −15/−25/−35 dB levels are RMS
ratios against the entire decoded noise segment, not narrowband SNR or sound
pressure levels. This experiment does not model broadband jet roar, changing
engine RPM, wind propagation, phone noise processing or listener motion, and its
40–1,800 Hz band does not cover every possible aircraft tone. It does not establish
reliable detection of real airliners.

The interactive displays are quantized/max-pooled for portability; detection
uses full-resolution floating-point spectra. Raw dB values are unnormalized FFT
power, not calibrated SPL. Timings exclude shared 2/4-second FFT setup and are
not comparable end-to-end speed benchmarks. Use `--render-only --out DIRECTORY`
to rebuild displays and anchor evaluation from saved detections without rerunning
analysis. Experimental passes remain separate until labeled real flyovers and
non-aircraft examples support a calibrated selection rule.

The complete-recording follow-up used the original audio from time zero, with
durations rounded down to whole seconds. Counts below are strong/review acoustic
candidates, **not aircraft counts**:

| Recording | Production 2+4 s | Harmonic pooling | Local subtraction | 8 s STFT |
| --- | ---: | ---: | ---: | ---: |
| Reference hour | 19 / 30 | 5 / 0 | 1 / 1 | 21 / 137 |
| November 3 walk | 0 / 6 | 0 / 0 | 0 / 0 | 0 / 54 |
| November 4 walk | 0 / 21 | 0 / 0 | 0 / 0 | 0 / 84 |

On the full reference, production recovers 100% of both sets of anchors. The
eight-second pass recovers 100% and 83%, respectively, both with strong evidence.
Harmonic pooling recovers only the first at the 80% threshold; local subtraction
recovers neither. Thus the normalization/context sensitivity affects enhanced
methods too. Fewer detections cannot be interpreted as better precision without
labels. Results are in `work/curve-experiments-full/`, including a separate
reference-anchor check. Neither complete walking recording yielded a strong
candidate in these four passes. The next detector work should address stable
background estimation across clip boundaries and validate proposed paths on
unmodified spectral measurements, with real aircraft/non-aircraft labels before
changing the website's automatic selection policy.

### Review of the `samples/` collection

The collection has seven MP3 files and six unique SHA-256 hashes. The two files
named `2026_11_04_13_01_27_1.mp3` and `desk_2026_11_04_13_01_27_1.mp3` are identical.
The three newly supplied November 4 recordings starting at 15:05, 15:43 and 21:49
were recorded indoors with the phone stationary, as confirmed by the user.
Existing experiment manifests now reference the copies in `samples/`.

The three new recordings were analyzed in full, rounded down to whole seconds,
with all nine workbench variants. Previous full-recording results for the
reference, November 3 walk and 13:01 recording were reused after matching hashes.
The collection review, measured-curve exports and diagnostic plots are available
locally:

```sh
just experiment-open work/sample-review/index.html

# Reproduce the nine-method scans on the three new recordings.
just experiment --config examples/sample-review-new.json \
  --out work/sample-review/new-recordings --workers 2

# Reproduce seven focused follow-ups around proposed curves/changing tones.
just experiment --config examples/sample-review-clips.json \
  --out work/sample-review/focused --workers 2
```

| File | Result of this review |
| --- | --- |
| YouTube reference | Two known aircraft curves at 35:24–37:23 and 50:36–52:13. Measured ridge samples are exported to `work/sample-review/reference-curves.csv` and `.json`. |
| November 3 walk | No validated aircraft curve; hash-verified prior four-method full-file comparison. |
| November 4 13:01, including the `desk_` duplicate | No validated aircraft curve; hash-verified prior four-method full-file comparison. |
| November 4 15:05 | No strong candidate in any of the nine full-file methods. |
| November 4 15:43 | Seven eight-second proposals labeled strong by the heuristic, but none accepted after inspecting their transitions in the measured spectra. All other full-file methods yielded zero strong proposals. |
| November 4 21:49 | No strong candidate in any of the nine full-file methods. FFmpeg reports a damaged MP3 packet while completing decoding; precise timing continuity is not validated. |

The 15:43 file exposes a concrete failure in the current experimental evidence
checks: a tracked path can leave one persistent horizontal background tone,
bridge weak/noisy bins, and settle on another tone. Several proposals repeatedly
connect approximately 208 Hz and 177 Hz at different times while both actual
tones continue horizontally. A fitted Doppler curve, harmonic energy, and even
coarse alternate-resolution support can pass despite the missing transition.
These are likely spurious paths, not seven identified airplanes. The first
three also overlap, so they do not represent independent flyovers.

The report includes two- and eight-second raw spectra with and without each of
these proposed paths. Seven targeted excerpts yielded zero strong proposals,
but excerpt sensitivity alone is not the rejection criterion; the unsupported
transitions in the original measured spectra are the central evidence. Separate
changing tones around 5:00–7:30 in the 15:43 file remain unclassified.

Independent scientific visualizations cover 40–6,000 Hz and include overview
and five-minute panels. They are generated with NumPy/SciPy/Matplotlib by the
scripts saved in `work/sample-review/`; the actual detector comparisons use Go
and FFmpeg. `review.json` records provenance, context, method counts and decoder
warnings. No new flight-location inference or automatic selection policy change
was made from these unvalidated paths. No validated curve in a personal recording
does **not** establish absence of aircraft. The results motivate checking the
measured frequency transition against persistent-tone alternatives before
assigning strong evidence.

### Independent public flyover samples

Eight real smartphone flyovers from [Praveen Pathak's public dataset](https://zenodo.org/records/23424278)
are downloaded to `samples/public/pathak-2026/`, with original WAV audio,
timestamped aircraft tracks and published frequency measurements. See the
[sample notes](../../samples/public/pathak-2026/README.md) for attribution, checksums,
test results and recorder-position/audio-clock limitations. The first assisted
California-wide search ranks the Livermore area and the correct aircraft highest,
but a competing location scores almost as well, so the result remains ambiguous.
This is not a verified position-error or fully automatic detection benchmark.

```sh
# Open the comparison already generated for all eight recordings.
just experiment-open work/public-flyover-curves/index.html

# Reproduce six analysis variants with the Go workbench.
just experiment --config examples/public-flyover-curves.json \
  --out work/public-flyover-curves --workers 2

# Open the combined California-wide trial for the first recording.
just map --results work/pathak-california/results
```

## Reference dataset and regression suite

The [reference suite](../../references/README.md) pins source files, independent curve
labels and explicit acceptance checks. It records all 40 Pathak passages (eight
downloaded), the Colsto recording and the personal sample inventory, keeping
reported locations, inferred reference estimates and search predictions separate.

```sh
just reference-check                     # quick known-curve checks
just reference-check --suite audio       # fresh analysis of the full reference audio
just reference-check --suite location    # repeat the statewide California search
```

For the private November 5 desk recording, `just desk-reference-check` reruns
audio detection and the regional competing-flight search from frozen local
inputs. It checks both the tracked-aircraft association and unresolved geographic
ambiguity; it does not claim a successful precise location. See the
[desk replay findings](jet-validation.md#regional-replay-and-frozen-desk-regression-november-5-2026).

Reports are written under `work/reference-check/`; failures return a nonzero
exit code. CI runs the quick suite. The [research source notes](sources-and-validation.md)
document the paper search, third-party implementations, source URLs, local data
paths, attribution, and reproducibility limits. The associated paper has not
been located; the dataset's analysis PDFs and tables were available.

The inferred Livermore site reported by dopplerprint is 37.663216, -121.781876,
about 1.925 km from our earlier top prediction. This is additional evidence
against treating that prediction as precise GPS, although it ranked the correct
aircraft first in the Livermore area. See the research notes for provenance and
the distinction between residual error and surveyed position accuracy.

### Flightradar24 playback and phone recording review

The November 7 review collected 261 actual anonymous FR24 playback snapshots
for the six stationary November 4–6 desk recordings. Its pages compare
callsigns and positions with geometric ADSB.lol tracks, show predicted arrival
times and relative gaps, play the corresponding audio, and display cropped
spectrograms and automatic ridge/model comparisons.

The walking recording has a separate 32-snapshot FR24 inventory linked from
the same report. Its timestamps describe aircraft observations; the phone's
route and sound arrival times have not been inferred.

```sh
just sample-audit-serve
# Open http://127.0.0.1:8081/fr24-review-2026-11-07/

# Rebuild the local comparisons, then their report.
just flight-history-review \
  --config work/fr24-review-2026-11-07/analysis-config.json \
  --out work/fr24-review-2026-11-07 --ridges --plots
just flight-history-report
just flight-history-test
```

The tracked turboprop association is consistent across both flight sources. A
flight-conditioned Texas search ranks a point 330 m from the supplied desk
highest, with alternatives more than 10 km away nearly tied. The association
had already used the desk position, so this remains a conditional development
experiment. Additional direct acoustic searches at the winning candidate and
the supplied receiver found no corroborating aircraft passing the controls.
No reliable jet localization or unique recording position is established.

The [research notes](sources-and-validation.md#flightradar24-playback-cross-check-november-7-2026)
describe acquisition, source hashes, UTC handling and limitations. The Go
tool performs geometry and curve diagnostics; Python handles browser capture
and report generation. FR24 displayed altitude is not used as geometric height.

For a recording with a supplied receiver location, `just flight-audio-review`
tests predicted flight windows directly against the measured spectrum, including
windows without an automatic proposal. It holds the receiver and clock fixed,
compares separate spectral resolutions, and checks reserved audio, steady tones
and shifted-time alternatives. It is a recognition diagnostic with an explicit
location prior. The November 7 recording's report is at
`http://127.0.0.1:8081/nov07-desk/` when `just sample-audit-serve` is running.


### Receiver-model and regional-search research (November 8)

Guided checks now consider jet source tones through 6 kHz, select pitches against
stationary training explanations, handle partial flyovers and distinguish missing
validation from a failed match. Location comparisons lock the audio partition
across candidates. The website's first-pass detector remains separate from the
experimental model and regional-search tools below; these are not verified GPS.

```sh
just test
just receiver-recovery-check --out work/receiver-recovery-check
just improvements-report
just sample-audit-serve
```

Open `http://127.0.0.1:8081/overnight-improvements-2026-11-08/` for the preserved
spectrograms, FFT comparisons, schedule failures and candidate maps. The research
server serves local files; original recordings are not uploaded. If it is already
running on port 8081, open that URL without starting a second server.

The private recovery checks require the pinned recordings and previously acquired
tracks. They fail when inputs are missing/changed and are development scoring
checks, not aircraft/GPS truth labels. New experiment commands use two-hyphen flags:

```sh
just curve-model-study --help
just receiver-schedule-study --help
just receiver-region-study --help
```

The regional experiment uses explicit `--bounds south,west,north,east`, all supplied
tracks and training-only candidate selection, then refines separated peaks with
`--grid`, `--refine` and `--final-grid`. A 10 m grid is not evidence of 10 m accuracy.
See [the research record](sources-and-validation.md) for assumptions,
limits, source hashes and reproducible case commands.

### Stationary desk development case

The complete-curve November 5 experiment ranks a point 316 m from the supplied
desk across a supplied 40 km region. Six complete incorrect-time searches are
weaker. Nearby alternatives remain competitive; this is a regional development
regression with a selected sound, not verified unique GPS or successful
localization of every desk recording.

```sh
just desk-localization-check --out work/desk-location-new
just sample-audit-serve
```

Open `http://127.0.0.1:8081/desk-model-improvements-2026-11-08/` for the map,
full observed/predicted curves, comparisons and all six desk diagnostics. The
replay requires pinned private audio, cached tracks and the measured spectrum.
It verifies hashes and excludes receiver truth from the search configuration.
See the [desk experiment record](desk-model-improvements-2026-11-08.md)
for the method, failed joint checks and limits. The website's automatic
first-pass detector remains separate from this experiment.

### Full-Texas accuracy results

The preserved [statewide evaluation](texas-statewide-evaluation-2026-11-08.md)
does **not** meet the two-mile target automatically: none of ten primary location
estimates from eleven Texas phone recordings is within two miles; one file has
no strong curve. The separately reviewed November 5 tracked-flyover curve reaches 321 m
only when its filename start is treated as exact in that baseline. Allowing ±6 s selects a point
360 miles away. Narrow-area success did not establish statewide accuracy.

```sh
just texas-evaluate --out work/texas-recheck
just texas-evaluate --out work/texas-exact-recheck --exact-clock
just texas-evaluation-report
just sample-audit-serve
```

Open `http://127.0.0.1:8081/texas-evaluation-2026-11-08/` for both timing modes,
all offsets, curve overlays and candidate maps with Texas borders and streets.
Private media and cached statewide tracks/spectra are required. These are
development outcomes; reviewed intervals and exact-start assumptions remain
separate from automatic selection and approximate timing.

### Experimental trajectory reconstruction

The [follow-up model study](trajectory-reconstruction-2026-11-08.md)
adds fixed-uncertainty position/velocity Kalman + RTS reconstruction. Across Texas,
the same reviewed tracked-flyover curve now selects a point **426.5 m** from the desk with
the **±6 s** clock allowance retained. The position and reported velocity are
reconciled before retarded-time acoustic modeling; no receiver or audio enters
that reconstruction. This assisted success does not establish automatic accuracy;
the park cases and longest-curve selection remain separate acceptance gates.

Exact geometry optimizations are enabled in ordinary Go grid searches. The new
trajectory model remains opt-in in the research scanner while its broader
behavior is evaluated; it is not the website's default model.

```sh
# Recheck just the reviewed desk case using cached statewide inputs.
just texas-evaluate --out work/texas-reconstructed-check --reconstruct-track --only nov05-prior-turboprop-curve
# Remove --only to evaluate every declared case; the full accuracy target remains separate.
just model-improvements-report
just sample-audit-serve
```

Open `http://127.0.0.1:8081/model-improvements-2026-11-08/` for the preserved
comparison, candidate maps, full-curve overlays and runtime measurements.
