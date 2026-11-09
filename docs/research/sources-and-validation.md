# Sources and validation record — November 5, 2026

The subsequent [jet experiments](jet-validation.md) add AeroSonicDB airliner
recordings, the DLR A320 microphone-array benchmark, a Phenom curve regression
and an assisted regional location replay. Their licenses, coordinate uncertainty
and incomplete results are documented separately.

## Primary dataset and associated paper

Praveen Pathak's [Zenodo v1 record](https://doi.org/10.5281/zenodo.23424278)
provides 40 May 2026 aircraft passages from Livermore, with original audio,
UTC frequency measurements, target KML tracks and analysis pages. Its metadata
declares CC BY 4.0. Eight original passage folders are present in
`samples/public/pathak-2026/`; the remaining 32 are cataloged, not yet evaluated.
The source ZIP and supporting files are in `work/public-flyover-source/`.

The record names **Aircraft speed and height from Doppler-shifted sound and
flight-track data** as its associated paper. I did not locate the paper itself:
the November 5 metadata contains no paper DOI or related-paper URL; exact-title
and author searches did not identify it; the inspected
[author publication list](https://www.hbcse.tifr.res.in/people/scientific/praveen-p-pathak)
does not list it. The ORCID webpage requires JavaScript and did not supply text.
This is an unresolved retrieval issue, not evidence that the paper is unpublished.
Do not cite the dataset DOI as a journal-paper DOI or claim that we read the paper.

The supplied `results_for_all40_flights.pdf`, `frequency_vs_time_40_cases.pdf`
and `final_results_40_cases.csv` are usable supporting material. The following
details affect validation:

- KML altitudes are geometric heights relative to EGM96 mean sea level; our
  WGS84 ellipsoid geometry requires a geoid conversion.
- The table's `received_start_utc` is the first selected frequency measurement,
  not independently verified WAV sample zero. R04's trace was aligned to audio
  without flight geometry, giving a 4.975 s offset and inferred sample zero
  `2026-05-14T17:41:32.264Z`. The search retains ±5 s uncertainty.
- The result pages use phone-centered ground-track diagrams and report the
  horizontal CPA offset `L`. Height estimates use that offset with the acoustic
  slant distance. These are useful consistency checks, not blind geolocation.
- The four motion categories allow us to evaluate steady-flight approximations
  separately from changing speed or height. The eight current samples all come
  from the constant-speed/constant-height group; they are not representative of
  all aircraft or noisy walking recordings.

For N784MB_R04, the published ADS-B CPA speed is 327.237 km/h and slant distance
is 2264.486 m. Its published Method II fit gives 327.959 km/h, 2227.886 m and
2.702 Hz residual. Our prior audio-only fit gave 330.007 km/h, 2251.253 m and
2.710 Hz residual. These figures support the curve/model interpretation, but
do not establish receiver GPS. Publisher values are evaluation data and were
not used to choose the statewide search bounds or target aircraft.

## Coordinates found in other implementations

The inspected revisions are pinned in [sources.json](../../references/sources.json).

| Project | What its Livermore reference means |
| --- | --- |
| [dopplerprint](https://github.com/Hexay/dopplerprint/blob/f4ead9a52519562af0a50be12eecf0d70f54573b/data/MANIFEST.md) | Reports **37.663216, -121.781876**, about 153 m MSL, reconstructed from 37 tracks and published horizontal offsets; reported RMS residual 4.7 m. Three May 9 passages receive a separate, low-confidence fit at 37.695539, -121.845009. |
| [DOPPLERGEIST](https://github.com/1337isnot1337/DOPPLERGEIST/blob/7d74fc2d6f4be4a1df747a13ded0fabeb9a31148/docs/LIVERMORE_DEMO.md) | Uses a reference inferred from other passages, excluding R04 before evaluating R04; 36 retained passages after outlier rejection, reported residual 4.76 m. Explicitly describes this as inferred, not surveyed. |

We inspected their reference-building code and documentation, without running
their pipelines or copying their implementation. Track-offset reconstruction
is additional evidence about the likely recorder site. Residuals describe model
consistency, not GPS uncertainty; shared track errors, site changes, and selection
of inliers can bias the fit. A leave-one-passage-out check is preferable to
allowing the tested passage to influence its own reference. These estimates
remain separate from primary location labels in our catalog.

Our earlier R04 statewide prediction **37.647510, -121.791066** is approximately
**1.925 km** from dopplerprint's inferred main site. This is distance to an
external estimate, not measured microphone error. The previous screenshot
confirmed that a coordinate could be plotted, not that it was the recorder's
position. The correct aircraft and general city do not prove a precise solution.

Useful ideas to evaluate from these projects are independent automatic-versus-
assisted benchmarks, leave-one-out references, explicit search/telemetry coverage,
and competing-flight rankings. Pinpoint already has retarded sound propagation,
shared-clock multi-event scoring, silence constraints and dense refinement;
their existence does not establish adequate detection or calibrated accuracy.
Any scoring/detection change should run our frozen suite and preserve the
distinction between missing data and rejected hypotheses.

dopplerprint's inspected code is MIT licensed. DOPPLERGEIST's inspected revision
uses PolyForm Noncommercial 1.0.0; older documents elsewhere describing it as
MIT do not describe this revision. Third-party datasets retain their own terms.

## Data locations and transformations

The machine-readable [source catalog](../../references/sources.json) records URLs,
local paths, attribution and revisions. [suite.json](../../references/suite.json)
pins SHA-256 for exercised assets; [provenance](../../references/provenance/)
preserves the source metadata and ADS-B archive hashes.

| Input | Local location | Purpose and provenance |
| --- | --- | --- |
| [Colsto raw audio](https://www.youtube.com/watch?v=4CxVxuMVkMc) and [methodology video](https://www.youtube.com/watch?v=UytYrhYLOKI) | `samples/Unknown location - raw audio [4CxVxuMVkMc].mp3`; `internal/acoustic/testdata/` | User-downloaded MP3; derived spectral fixtures. Camera coordinate 40.876466, -73.524951 is video-reported, not surveyed. No redistribution license verified. |
| Pathak original WAV/CSV/KML/PDF | `samples/public/pathak-2026/` | Unchanged publisher files; CC BY 4.0; manual/published labels kept separate from predictions. |
| [ADS-B lol May 14 archive](https://github.com/adsblol/globe_history_2026/releases/tag/v2026.05.14-planes-readsb-prod-0) | `data/2026-05-14/`; `work/pathak-california/california-tracks.csv` | Two SHA-256-verified archive parts. California import: 51,278 positions, 1,387 usable segments, competing aircraft included. Database ODbL; retain upstream attribution and contents terms. |
| [ADS-B lol September 8 archive](https://github.com/adsblol/globe_history_2026/releases/tag/v2026.09.08-planes-readsb-prod-0) | `data/2026-09-08/`; `work/methodology/ny-tracks-typed.csv` | Prior Oyster Bay development reproduction. |
| [Skadi terrain](https://github.com/tilezen/joerd/blob/master/docs/attribution.md) | `data/terrain-ca-3arc/`, `data/terrain-ny-3arc/` | 1 arcsecond HGT source sampled at 3 arcseconds; negative elevations clamped to zero, voids retained. Preserve applicable provider attribution. |
| [PROJ EGM96 grid](https://github.com/OSGeo/PROJ-datumgrid) | `work/web/cache/egm96_15.gtx` | Orthometric-to-ellipsoid conversion; checksum pinned. |
| [Census 2024 state boundaries](https://www2.census.gov/geo/tiger/GENZ2024/shp/cb_2024_us_state_500k.zip) | `internal/geo/states.json`, `internal/geo/state-masks.json.gz`; `tools/census_states.py` | Search masks and border display; no acoustic location evidence. |
| [tar1090-db aircraft types](https://github.com/wiedehopf/tar1090-db) | `internal/tracks/data/type-engines.json` | Upstream revision and SHA included in file. Optional jet exclusion; disabled for this California trial. |
| Personal phone recordings | `samples/`, inventory in `references/catalog.json` | Unlabeled indoor/walking data. Duplicate files are aliases, not independent examples. No permission to redistribute raw recordings is implied. |

Esri imagery/roads and OpenStreetMap/Leaflet display attribution belong with
published map screenshots; those services are not acoustic search inputs.
Do not label terrain simply “SRTM” without recording the Skadi provider mixture
and transformations actually used. Do not copy private or unlicensed media into
a public release just because it appears in this local inventory.

## Repeat the checks and prepare missing inputs

```sh
just reference-fetch
just reference-check
just reference-check --suite audio
just reference-check --suite location
```

The first successful replays recovered both labeled YouTube curves from the
complete MP3. R04 recovered 95.8% of the 403 published anchors, with 2.739 Hz
RMSE after a consistent harmonic conversion. Seven other WAVs were analyzed
but have no aligned acceptance labels yet; those observations are not passes
in a recognition-rate calculation. See the frozen baseline for all outputs.

The statewide replay again evaluated 2,604,556 locations, ranked ICAO `aaa077`
(publisher registration N784MB) first and retained its ambiguity warning.
The next aircraft is only 0.263 dB lower and roughly 161 km away. The location
suite is an **assisted, development regression**, not an automatic location
accuracy benchmark. Scores are not probabilities.

If the California cache is missing, the original archive import parameters were:

```sh
just fetch --date 2026-05-14 --download --out data/2026-05-14
just import --archive 'data/2026-05-14/*.tar.*' \
  --from 2026-05-14T17:39:00Z --until 2026-05-14T17:46:00Z \
  --bounds 32.2,-124.8,42.35,-113.75 \
  --out work/pathak-california/california-tracks.csv
```

Verify the archive against `references/provenance/adsblol-20260514.json` and the
derived CSV against the suite manifest. Terrain/geoid preparation uses the
existing Go download functions in `internal/geo/download.go`; required tiles
and their hashes are enumerated in the suite. The initial preparation scripts,
audio alignment plots and per-band maps remain in `work/pathak-california/`.
The runner computes the audio spectrum again, so no old spectrum cache is needed.

All exercised cases are development data. Future held-out evaluation should split
by recording session and site, keep target tracks and derived receiver estimates
out of scoring, and label aircraft intervals plus genuine confusers. Independent
microphone GPS and time are still needed for defensible meter-level accuracy.


### November 6 combined-profile and automatic-selection artifacts

The new work reuses the audio, telemetry, terrain, geoid and annotation sources
registered above; it introduces no new independent recorder GPS evidence.
`references/hybrid.json` copies existing propeller/jet/background acoustic gates.
`references/hybrid-location.json` is an explicitly assisted, known-single-passage
Phenom replay using the longest usable measured ridge. Its regional expectation
is not microphone GPS.

Current derived artifacts are under `work/automatic-sequences/`: the combined
acoustic regression (`hybrid-validation-v2/`), supplied-track regional replay
(`hybrid-location-v2/`), complete 19-curve reference pair/control experiment
within a disclosed known-area rectangle (`reference-conditional/`), full private
November 6 analysis (`desk-full-v2/`), known-position pair diagnostics
(`desk-fixed-position-v2/`), the private tracked-flyover regression and original quick regressions.
The reference pair/control experiment remains unresolved. The desk GPS is an
input to its diagnostic, not a recovered coordinate.

Reference runners record source/manifest/input digests and decoder versions.
New automatic reports include effective search settings, all actual/control pair
outcomes, exclusions, model-file digests and an executable/spectrum/telemetry/model
fingerprint. Successful cached pair results are reusable only with matching
inputs and settings. Details and validation limits are recorded in
[the November 6 research entry](additional-jet-samples.md#automatic-sound-pairs-and-combined-analysis-november-6-2026).

### November 7 automatic sequence provenance

The new multi-flyover work reuses the registered recordings and flight/terrain
sources. It adds no independently known jet recorder GPS.
[sequence-search-2026-11-07.json](../../references/provenance/sequence-search-2026-11-07.json)
pins the original audio, detection reports, spectrum, competing tracks, search
settings, preserved training executables, validation executable and evaluation
policy. Each automatic report additionally lists DEM/geoid paths and digests.

Current derived files are under `work/sequence-search/`. The original Colsto
standard-profile proposals and spectrum come from `work/traced-joint/reference/`.
Its automatic sequence replay uses `reference-final/config.json`: a disclosed
known-reference-area rectangle with 12 scoring workers. This is not an unknown
statewide location test. `reference-final/training-binary` preserves the original
search executable; validation-only replay records both executable hashes and
never changes training positions, clocks, tones, aircraft or group selection.

The private desk replay imports the existing 388 cached November 6 readsb trace
files into `desk-expanded-tracks.csv`, using the same regional bounds. This replaces an initial import truncated before the final
positive control could finish. The complete cached-source inventory is stored
in `work/sequence-search/desk-trace-inventory.json`, whose SHA-256 is pinned in
the provenance record. Cached trace discovery and a continuous timestamp span
do not prove complete receiver-network coverage. No extra flight download or
new acoustic data is implied by this broader import.

`desk-final/` holds the completed known-position training search;
`desk-revalidated/` preserves its newer validation overview. The supplied desk
coordinate remains an input to this association diagnostic. Repeated negative
training matches are not evidence that no aircraft were audible.

The new hybrid acoustic, quick spectral, supplied-track Phenom and private
tracked-flyover regression outputs are in `hybrid-regression/`, `quick-regression/`,
`phenom-regression/` and a private tracked-flyover regression directory. Reference runners record original
input and manifest hashes, Go-source digests, decoder versions, label coverage,
residuals, timings and aircraft rankings. The frozen historical failure policy
and subsequent sequence reports are checked after inference; evaluation
coordinates and aircraft identities are never fed back into the fit.

### Complete sample audit artifacts

The full November 7 collection audit is pinned in
[full-sample-audit-2026-11-07.json](../../references/provenance/full-sample-audit-2026-11-07.json).
It inventories all 39 unique audio inputs and their alias, plus fresh detection
outputs, competing trajectory CSVs, acquisition coverage records, preserved Go
executables, completed fits, explicitly separate reference-area diagnostics and
the collection reports. Per-recording inference inputs contain no receiver GPS;
the reference-area diagnostic configurations deliberately disclose that prior.
Private reference coordinates remain in local work/evaluation records.

Local results are `work/sample-audit-2026-11-07/{index.html,report.json,summary.json,results.csv}`.
Run `just sample-audit-serve`, then open
`http://127.0.0.1:8081/sample-audit-2026-11-07/` to keep map/evidence assets on
HTTP. The simple report server performs no analysis. All audio analysis and
flight matching were performed by Go with FFmpeg decoding.

Full hybrid scans completed for every unique input. Five independent California
competing-flight searches and two Texas searches completed; the reviewed
New York replay completed separately. Three stationary phone cases have
known-reference-area diagnostics only, with no completed full-state ranking.
Four older California replay acquisitions failed with HTTP 502 on three passes.
Those outcomes, unverified public GPS and the unsupported walking route are
material limits, not successful geolocation tests. No acceptance thresholds or
labels were weakened to improve the reported result.

### Flightradar24 playback cross-check November 7, 2026

The user's screenshot is an actual FR24 global playback view from November 6.
That particular frame is outside the audio interval of the November 6 desk
recording. A late-evening November 4 recording (Central time) instead spans into
November 5 UTC. Filename times and the America/Chicago zone were confirmed by the
user.

The [FR24 playback help](https://support.fr24.com/support/solutions/articles/3000115499-is-it-possible-to-see-playback-of-past-flights-)
describes historical global playback and the free recent-history window.
Direct HTTP access encountered Cloudflare, but an ordinary anonymous Firefox
session loaded playback normally. Selenium read the public map's historical
aircraft state and used the same playback timestamp setters as the timeline.
No authentication, subscription flags, browser cookies or access checks were
modified. `tools/capture_fr24_playback.py` is an experimental website capture
adapter; its frontend state interface is undocumented and can change.

There are 261 saved snapshots, spaced two minutes apart: 13, 11, 24, 23, 85 and
105 for the six stationary recordings in chronological order. Saved observations
contain FR24 flight-leg IDs, callsigns, historical position timestamps and
horizontal positions. They are genuinely observed FR24 records, separately
identified from the ADSB.lol trajectories. Sampling and viewport limits omit
some flights and do not establish complete coverage. A live global data/server
timestamp can coexist with historical positions; only each aircraft's
historical observation timestamp is used for matching.

An additional 32 snapshots cover the moving November 3 walking recording
(evening of November 3, Central time). The same surrounding map viewport is
used only to inventory visible aircraft; it is not a stationary microphone
coordinate assumption. Its separate page records telemetry observation times
and callsigns without converting them to predicted audio arrivals, inferring
a route, or claiming acoustic aircraft matches. The stationary-location model
does not support this moving receiver. Older public reference recordings lie
outside the anonymous recent-playback window and were not re-acquired from
FR24 in this experiment.

`tools/flight_timeline.go` performs the Go geometry and recognition diagnostics:

- Geometric ADSB.lol trajectories provide ECEF geometry; SRTM and EGM96 provide
  receiver height. A 343 m/s propagation model gives arrival CPA times and gaps.
  A common clock correction moves every event together.
- Repeated local minima are retained for circuits. Same-airframe fragments
  within 120 seconds are merged, and incomplete trace edges are marked.
- FR24 positions are compared at their own observation times with valid
  geometric segments, without bridging gaps longer than 30 seconds. Matches
  require horizontal separation no greater than 1 km and retain ambiguity when
  another airframe is similarly close. Time-local callsign agreement is reported
  separately from positional proximity.
- Registration, aircraft type, operator and description come from the pinned
  ADSB.lol trace header. Callsigns are selected near emission time rather than
  borrowed from another flight later that day. FR24 altitude is not substituted
  for ellipsoidal height. FR24's [altitude explanation](https://www.flightradar24.com/blog/inside-flightradar24/understanding-altitude-on-flightradar24/)
  distinguishes barometric and GPS altitude, so an unverified map readout is
  insufficient to establish the needed datum.
- Existing automatic strong ridges are compared with actual retarded-time
  flight curves at one shared clock grid. Source pitch is fitted on alternating
  ten-second training blocks, with two-second boundary guards. Other blocks
  compare shape errors against constant and linear frequency alternatives.
  This descriptive diagnostic does not replace production spectral scoring,
  full sequence validation or wrong-time search controls.

Across the six files there are 661 predicted encounters within 15 km, including
55 complete closest approaches within 5 km. These are geometry counts, not
numbers of audible or detected planes. Fresh replay/trace import was also run
for the 15:05 recording despite its zero automatic strong proposals: 111
aircraft discovered and 111 traces available, yielding 19,022 imported points.
Other recordings reuse the fresh full-state track imports from the earlier
collection audit, including their recorded missing traces. Provider agreement
can reflect shared underlying ADS-B reports and is not independent acoustic
ground truth.

The private November 5 tracked-turboprop curve is the only automatic strong
ridge passing this review's shape agreement checks. An FR24 flight record with a
matching callsign has two historical observations that agree with the archived
ADSB.lol trajectory to within a few metres horizontally; the aircraft type and
registration come from the archive. The curve
has training/reserved RMS errors 0.618/0.673 Hz with a 100.928 Hz source tone at
zero clock correction; constant and linear alternatives have reserved errors
13.614 and 1.860 Hz. This reinforces an existing development association;
it does not independently prove the acoustic airframe identity.

A separately disclosed **flight-conditioned** Texas experiment supplies that
aircraft's geometric track and its existing automatic audio curve, the filename
start time ±30 s, and Texas boundaries. It evaluates 59,571 grid points at
200 m coarse, 50 m refinement and 10 m final spacing. The highest fit is
330.3 m from the user-reported desk, with score 12.36 dB
and clock correction −2.277 s. Locations more than 10 km away are within 1 dB.
The search config contains no recorder coordinate, but the aircraft/window
association had already used the known desk position. This is neither a blind
test nor a unique location result. A fine grid is not accuracy or confidence.

Production `corroborate` then searches measured spectral energy outside the
tracked-flyover anchor, without requiring automatic proposals. One trial freezes the top
candidate and its clock; a second explicitly supplies the user-reported desk
and filename clock. They examine 67 and 63 additional usable 120-second
trajectory windows over source frequencies 40–6000 Hz. These are two hypothesis
trials, not 130 independent aircraft. Source tones are chosen on training blocks;
other blocks, stationary-tone and shifted-time controls assess support. Both
anchor rechecks pass; no additional window passes the support checks. Some
80–90 Hz fits have positive energy but fail against competing controls. The
November 6 existing full-spectrum telemetry-led review remains available under
`work/nov06-desk/adsb-led/`; no new reliable jet acoustic association or unique
receiver location is established by this cross-check.

Local artifacts:

- `work/fr24-review-2026-11-07/{index.html,summary.json,results.csv,provenance.json}`
- Per recording: `fr24/*.json`, `encounters.{json,csv}`,
  `fr24-comparison.json`, `ridge-comparison.json`, cropped spectrogram SVGs and
  audio playback links. Original MP3s are unchanged.
- The November 5 recording's `flight-conditioned-location/` directory contains the single-flight
  search, map, post-inference evaluation and both direct spectral follow-ups.
- `references/private/provenance/fr24-playback-review-2026-11-07.json` pins original
  audio, raw observations, geometric inputs, helper sources and immutable Go
  executable snapshots. Exact initial browser executor snapshots were not
  retained for every capture; the raw observations and request/aircraft times
  are pinned, and the final helper source version is recorded separately.

Use `just sample-audit-serve` and open
`http://127.0.0.1:8081/fr24-review-2026-11-07/`.
`just flight-history-review --config ... --out ... --ridges --plots` runs the Go
review; `just flight-history-report` builds the HTML. `just flight-history-test`
checks retarded CPA, fragment deduplication, historical-versus-live timestamps,
time-local callsigns and rejection of ambiguous or unverified assignments.
These offline checks also run in `just test` and CI. They do not assert
aircraft absence or upgrade the current development labels to independent truth.

### November 7 recording and direct receiver review

The November 7 afternoon desk-context recording is 4,499.252125 seconds long
(74:59.252), mono MP3 at 44.1 kHz. Its original SHA-256 is
`cb4eeb2dd2c99236470bb96b499743e2b92d87230098736af0d1875910e0a1be`.
The prior desk location, indoor stationary conditions and Central filename
start-time convention are **working assumptions pending confirmation for this
file**. On that timing assumption it falls entirely within November 7 UTC.
It remains unlabeled development audio and is not a negative-aircraft example.

Full Go hybrid analysis covers the entire file. There are 14,973 tonal fragments,
one brief strong acoustic proposal,
63 review proposals and zero proposals with bounded closest approach usable
for joint timing. The brief strong proposal lacks separate temporal test blocks
for the existing ridge/flight diagnostic. It is not assigned an aircraft identity.

Fresh regional ADSB.lol discovery covers the supplied receiver region and
recording window with 1,710 seconds of trajectory padding on each side. All
156 discovered trace files were available. The import retains 26,626 usable
geometric points, with missing-geometric, stale, ground and conflicting updates
recorded in `fetch.json`. Discovery is regional, not a blind Texas search, and
available files do not establish complete surveillance coverage. FR24 acquisition
saves 38 public playback snapshots at two-minute spacing: 39 airborne flight-leg
IDs appear during the audio interval, with 27 airframes having consistent callsign
and position associations with the geometric source.

At the assumed receiver there are 39 modeled encounters within 15 km. Only one
is a complete, comparatively close approach. These are telemetry predictions, not counts of planes heard in the recording.

The new Go `search.ReviewReceiver` entry point tests the supplied receiver and
one shared clock without inventing an acoustic anchor. It reuses the same
spectral fitting and steady-tone/shifted-time controls as anchored corroboration.
The anchored workflow still rechecks its seeds before entering the common
schedule scorer. No position or clock optimization, detection promotion or
location candidate is performed by the receiver review.

The November 7 experiment fixes the filename clock correction at zero and
examines source tones 40–6000 Hz in 2 Hz steps. A saved scoring spectrum and a
fresh 0.5-second FFT spectrum each test 22 usable predicted flight windows.
For each flight, either a 40- or 120-second window and its tone are chosen from
training blocks, then checked on other blocks and against stationary-tone and
wrong-time alternatives. **Neither resolution yields a passing window.** The
44 evaluations are alternative measurements of the same predictions, not 44
independent aircraft. No reliable acoustic flight identity, multi-aircraft
sequence or receiver location is established. This does not establish absence
of audible aircraft: source-pitch changes, weak signals, short passages,
microphone processing, geometry gaps and uncertain recording context remain
limitations.

Whitening statistics use the full recording. Reserved spectral blocks are not
independent recording sessions, and the experimental support thresholds are not
calibrated probabilities. The report's playback-clock control changes listening
times; it does not refit the stored spectral checks.

Local outputs are `work/nov07-desk/`: original-source metadata, analysis inputs,
full-file detection/spectrum, capture configuration, and a per-recording folder
with flight traces, acquisition coverage, FR24 snapshots, encounter timelines,
ridge diagnostics, direct receiver reviews for both resolutions and measured
spectrograms. `references/private/provenance/nov07-desk-review.json` pins the artifacts
and executable versions. Original MP3 bytes are unchanged. Open
`http://127.0.0.1:8081/nov07-desk/` with `just sample-audit-serve`.

Reusable commands are `just flight-history-fetch --config ... --out ... --cache ...`
(`--state TX` opts into state-wide discovery), `just flight-audio-review --config ...
--out ... --mode both`, and `just flight-history-report work/nov07-desk`.
Receiver-review regressions cover discovery without an invented anchor, missing
sound, invalid context, cancellation and one clock shared across every flight;
existing anchored controls and seed-recheck tests also remain required.

### Receiver-model diagnosis and three park recordings November 8 UTC

The user corrected the stationary desk reference and supplied an approximate
park centre. Those private coordinates and per-file assignments are recorded
in `work/personal-samples-2026-11-08/receiver-context.json`. The desk correction
is 1.93 m, which cannot explain the earlier large location errors. Historical
experiment inputs remain preserved.
The user explicitly identifies the three new November 7 recordings as park
walks. The earlier November 7 afternoon file's individual
assignment remains unconfirmed; it must not silently become surveyed desk truth.

Full Go hybrid analysis of the three new originals gives:

| Recording (November 7 park walk) | Duration | Strong acoustic proposals | Review proposals | Bounded-CPA proposals |
| --- | ---: | ---: | ---: | ---: |
| First | 73.926 s | 2 | 0 | 1 |
| Second | 90.244 s | 0 | 1 | 0 |
| Third | 1051.189 s | 2 | 4 | 1 |

Overlapping proposals are not independent aircraft. These are development
measurements, without positive/negative acoustic identity labels. Fresh regional
ADSB.lol traces cover the windows, including the November 7/8 UTC rollover.
The park centre is a **static proxy** for forward-model diagnosis; no phone
trajectory has been reconstructed. No FR24 playback acquisition was made for
this batch. `--include-review-curves` explicitly permits lower-tier curves in
the diagnostic helper without changing detector labels or production thresholds.

There is a useful real association candidate in the 73.926 s file. At the park
centre, one observed event is consistent with a regional turboprop track.
Training-selected clock correction +2 s and source tone
135.03 Hz give training/reserved ridge RMS errors 1.345/0.679 Hz. This is a
conditional forward-model association, not independently confirmed audio identity.

A deliberate desk alternative also passes the same descriptive shape gates:
clock 0 s, source tone 133.04 Hz, training/reserved RMS 1.515/0.841 Hz. Both
locations therefore remain compatible under the current nuisance parameters
and static proxy. The microphone was walking, so neither supplied coordinate
is a known per-sample microphone trajectory. A slightly smaller residual does
not establish a unique GPS location or calibrated confidence.

The 90.244 s clip overlaps a regional jet trajectory passing near the park
centre. Its review-tier ridge is retained for diagnosis rather
than being declared an aircraft. The best static-centre constant-pitch fit has
reserved RMS error about 208 Hz, so that model does not explain the extracted
ridge well. The cause is unresolved: extraction error, source-pitch variation,
different sound identity and receiver geometry must be separated rather than
assuming the recording is unusable.

Two implementation limitations were reproduced with **synthetic spectrograms**,
using an independent closed-form retarded-time formula for the injected curve:

1. A perfect 80-second flyover at 160 Hz yields 100% modeled coverage and about
   18.99 dB reserved contrast, yet is rejected solely for `insufficient_controls`.
   The direct checker requires at least three shifted-audio controls at offsets
   120–480 s. Short clips cannot supply them. Symmetric 40/120-second windows
   also omit flyovers close to recording boundaries. Neither condition is a
   physical failure of the observed Doppler curve.
2. An explicitly present 160 Hz curve with 6 dB peak plus an 80 Hz steady hum
   with 20 dB peak causes unrestricted training selection to choose 66 Hz.
   That winner fails the controls. Examining the injected tone's neighborhood
   in the **identical spectrum** recovers the 160 Hz curve with 4.89 dB reserved
   contrast and passes. The scorer freezes one strongest training pitch before
   its noise checks, so failure of that winner does not exhaust alternative
   source pitches. The narrow search uses synthetic truth solely to demonstrate
   recoverability; it is not a proposed production shortcut or accuracy estimate.

Reproduce those illustrations with `just model-diagnosis --out work/receiver-limit-diagnosis`.
Their source is `tools/diagnose_receiver_limits.go`; results are saved under
`work/personal-samples-2026-11-08/model-diagnosis/`. No acceptance threshold was
relaxed and these diagnosed limitations are not yet fixed in production.

Current acoustic templates assume a stationary receiver and a constant source
tone within each selected window. They are generic Doppler templates, rather
than complete propeller/jet engine-noise models. Jet fans can produce several
tonal orders plus broadband components, and frequencies change with rotational
speed ([IHI fan-noise research](https://www.ihi.co.jp/en/technology/techinfo/contents_no/1202032_13586.html),
[NASA fan-noise model](https://ntrs.nasa.gov/citations/20150000884)). ADS-B alone
does not supply engine RPM or the phone's walking trajectory. These omissions
are established model restrictions; their causal contribution to each failed
real clip has not been measured. Indoor processing/noise and telemetry coverage
are additional possible limitations, not demonstrated explanations for every miss.

The immediate engineering priorities are multiple training-selected pitch/ridge
hypotheses checked against stationary alternatives, validation controls that
exist for short recordings, explicit skipped-window reasons, bounded receiver
motion, and constrained source-pitch/harmonic models. Distinct aircraft and one
shared clock must then disambiguate locations before dense-grid refinement.
Silence can exclude locations only when detection/audibility is supported.

The local overview is `work/personal-samples-2026-11-08/index.html`, with per-file
timelines, measured spectra, conditional curve overlays and original audio links.
`diagnosis-summary.json` separates reproduced failures, real associations and
remaining hypotheses. Source hashes and frozen executables are pinned in
`references/private/provenance/receiver-model-diagnosis-2026-11-08.json`.


### Model recovery, FFT studies and regional scans, November 8

The implementation changes and experiments are preserved separately from the
historical failed runs under `work/overnight-improvements-2026-11-08/`.
The overview is `index.html`, and the machine-readable result is
`improvements-summary.json`. These are research improvements, not a completed
claim of reliable automatic aircraft identification or blind statewide GPS.

Implemented in the Go follow-up/direct-review path:

- Each source pitch competes with the strongest stationary explanation of its
  own observed frequency band **during training selection**. The selection
  objective is `min(curve_training, curve_training - stationary_training)`.
  An immutable range-maximum index avoids rescanning all stationary pitches
  separately for every curved hypothesis. Reserved power never chooses pitch.
- Source tones up to 6 kHz are considered by default, within the available
  measured band. The spectral/geometry bounds cap the actual sweep and retain
  the requested low-frequency pitch-grid precision.
- Partial windows at recording boundaries are evaluated when sufficient guarded
  training/reserved samples exist. Missing eligible measurements are reported
  as `validation_status=unavailable`, rather than evidence of no aircraft.
- Short clips with insufficient distant shifted-audio controls instead compare
  reversed/shifted trajectory hypotheses on the **same audio**, with the same
  pitch search and guarded folds, plus stationary and linear-tone competitors.
  These are correlated hypothesis comparisons; their count is not a number of
  independent null trials, a p-value, or calibrated confidence.
- Comparisons of candidate positions lock a recording-wide fold origin. Otherwise
  moving a candidate's CPA also moves its training/validation boundary, letting
  other candidates train on nominally reserved audio. A regression mutates all
  globally reserved power while moving the candidate window and checks that
  training pitch and selection remain unchanged.

The unchanged synthetic 80-second flyover and the 160 Hz curve under louder
80 Hz hum now pass the experimental checks. New unit cases reject absent sound,
steady hum and a straight chirp, and verify cancellation, missing telemetry,
reserved-data isolation and source/microphone modeling. These fixtures exercise
specific logic, not end-to-end real-aircraft sensitivity or false-positive rates.
Reproduce with `just model-diagnosis --out work/model-recovery-replay`.

#### Jet FFT and extraction results

For the 90.244 s November 7 park-walk clip, at the supplied park-centre proxy and
filename clock, the jet trajectory gives:

| FFT window | Reserved contrast | Margin over competitors | Experimental support |
| --- | ---: | ---: | --- |
| 0.5 s | 1.63 dB | -1.27 dB | No |
| 0.25 s | 6.02 dB | 2.98 dB | Yes |
| 0.125 s | 2.67 dB | -1.92 dB | No |
| 0.0625 s | 2.34 dB | -1.20 dB | No |

All resolutions are retained. The quarter-second choice is a development
finding selected after inspection, not independent validation. Four fixed-point
comparisons reject that target at the desk and at three other alternative points
at the nominal clock. Those few alternatives do not establish uniqueness.
The conditional turboprop association in the 73-second file also passes direct
spectral checks without an automatic acoustic anchor.

The conditional jet model predicts a steep frequency slope near closest approach,
so the inferred sweep within a 0.5 s FFT spans many two-Hz bins. A
0.25 s FFT trades reduced sweep against coarser bins and less integration time.
These are finite differences of a hypothesized flight curve, not measurements
of an independently confirmed aircraft. `chirp-smear-estimates.json` preserves
all estimates. Shorter FFT is not monotonically better in the real clip.

The standalone quarter-second acoustic detector still returns **zero strong and
zero review** curves for this file. The positive experiment is trajectory-guided
spectral matching. This separates a demonstrated extraction limitation from the
assumption that the Doppler equations alone must be wrong. No detector labels or
thresholds were relaxed to manufacture a new strong curve.

#### Source, receiver and telemetry models

`Track.ReceivedMoving` solves the retarded-source problem at the receiver's
arrival position, with the still-air frequency factor
`(c + n dot receiver_velocity) / (c + n dot source_velocity)`, where `n` points
from receiver to the emitted source position. An independent forward-wavefront
arrival calculation verifies this factor, its emission time and its zero-motion
limit. Subtracting observer velocity only in the denominator would be wrong.
See [OpenStax Doppler formulation](https://openstax.org/books/university-physics-volume-1/pages/17-7-the-doppler-effect).

`FitRidgeCurve` compares constant emitted pitch with bounded linear changes in
**emission time**. One supplied receiver trajectory and clock apply to every
curve in a trial. A training-only complexity criterion chooses the source model;
reserved ridge values cannot choose source pitch/slope, clock or motion. These
ridges were discovered using full recordings, so reserved residuals are descriptive
and do not constitute unbiased aircraft detection validation. Jet fan sound can
include several tonal orders and broadband components ([NASA fan-noise model](https://ntrs.nasa.gov/citations/20150000884)); the new linear-pitch option is not
an implementation of that complete acoustic source model.

Experiments include constant pitch, 5% pitch variation, eight 2 m/s horizontal
motion directions plus stationary, and a 15% variation sensitivity case. Greater
flexibility sometimes improves training while worsening reserved residuals or
improving wrong-location fits. These options remain experimental and do not
become default automatic location decisions.

`Track.Smooth` optionally uses time-aware cubic Hermite interpolation of ECEF
positions, with receiver/audio-independent tangents from neighboring secants.
It preserves every recorded node, refuses to bridge long gaps, and retains
segments linearly when the implied acceleration exceeds the configured limit.
The study uses 0.25 s samples and a 20 m/s² bound; reports retain displacement,
gap and rejected-segment counts. Independent known quadratic motion improves
Doppler error, and constant-velocity/gap regressions remain intact. No new ADS-B
measurements are invented. Actual ground-speed/track/vertical-rate fields are
not yet retained in the normalized CSV; position interpolation cannot replace
them or establish the true path between reports.

#### Regional scan and geographic uncertainty

The new matched-filter regional tool does not require an acoustic proposal or
fixed aircraft identity. It examines all supplied regional tracks, source tones,
window choices and one shared clock per candidate, with one recording-wide fold
partition. Coordinates in the old receiver config are ignored by the scorer;
the explicit rectangle is a disclosed geographic prior around the known area.
Candidate selection uses training evidence only. Reserved checks do not replace
an unsuccessful training winner with a different heldout winner.

The jet study rectangle is approximately 4 km by 4 km. Coarse 250 m cells are
followed by 50 m cells around separated peaks, then 10 m cells around the leading
refined peaks. The first scan evaluates 875 locations in 88.0 s on four workers.
Its training winner lies near the private park center, with clock correction 0 s
and the jet track selected from the supplied tracks. Reserved checks pass. It is
155.6 m from the approximate supplied park centre. Because the user was walking,
this is **not** a measured GPS error, surveyed microphone position, statewide
blind test or proof of 10 m accuracy.

A second separated peak is only 0.52 dB behind. Using the optional smooth
telemetry path moves the winner by roughly 330 m; that winner also passes local
checks. Both maps, all candidate scores and source-model variants are retained.
This sensitivity is material uncertainty, not a reason to select whichever point
is nearer the expected park centre. `improvements-summary.json` records both
coordinates and their exact separation. Location-search look-elsewhere effects,
receiver motion and full-search false-positive calibration remain unresolved.

#### Complete stationary recordings and schedule controls

All six confirmed stationary desk recordings were reviewed at the corrected
longitude and Central filename start times. The first overlapping quarter-second
FFT attempt hit the existing 100-million-cell bound on long files. The completed
scoring pass uses adjacent quarter-second FFTs (`--hop-fraction 1`), covering the
**entire** recordings without truncation. It produces six experimental supporting
windows in total, including four in the November 6 file. These remain unverified
acoustic associations, not six independently identified planes.

The new anchor-free schedule audit retimes **every flight together** under seven
wrong-clock controls, preserving relative flyover gaps. Every trial searches
source tones/windows/distinct flights using training evidence only. All trials
share one eligible encounter cohort and recording-wide folds. Selected failures
cannot be rescued by another flight that scores better on reserved audio.

None of the six desk recordings' training-selected two-aircraft schedules beats
the best retimed schedule. Margins range from approximately -0.68 to -7.25 dB.
Thus the individual positive windows cannot be promoted to a validated joint
sequence or a recording location. Failure is not proof of aircraft absence or
silence. The controls are correlated and these results are not calibrated
false-positive probabilities. See `desk-schedule-audit/`.

#### Regression and provenance

- The full Go race suite and vet pass. The archive of incomplete source excerpts
  is isolated in `_source/`; an ignored directory symlink preserves historical
  provenance paths and exact bytes without adding a broken build package.
- All ten frozen public hybrid acoustic regressions pass, including both original
  YouTube curves, the propeller/jet reference cases and five background cases.
  Expectations in `references/hybrid.json` were not changed. An exact benchmark
  source archive was captured and its fingerprint verified before later test-only
  additions; use that archive rather than the changing working tree for replay.
- `references/private/receiver-recovery.json` pins four **private development** scoring
  cases: the park turboprop, quarter-second jet and two wrong receiver alternatives.
  `just receiver-recovery-check` recomputes spectra from unchanged original audio
  and tests the conditional implementation behavior. Cases were chosen after
  seeing the recordings; they are not independent recognition or GPS truth.
- Frozen executables, source snapshots, inputs, logs and result hashes are listed
  in `references/private/provenance/model-improvements-2026-11-08.json`. Personal audio,
  coordinates and traces are private research inputs; no redistribution permission
  or publication of the original recordings is asserted.

The website's first-pass acoustic detector is unchanged. The production changes
apply to guided follow-up and supplied-receiver review; the new model, schedule
and regional-map workflows are explicit experimental tools. The next useful
work is improving raw ridge extraction, using retained ADS-B velocity telemetry,
and integrating a validated geometry-guided method into bounded website searches.

Reproduce individual experimental workflows (all are local and explicitly scoped):

```sh
just receiver-recovery-check --out work/replayed-receiver-fixtures

just curve-model-study \
  --config work/personal-samples-2026-11-08/config.json \
  --out work/replayed-curve-models \
  --compare-receiver "$DESK_LAT,$DESK_LON" \
  --max-source-change 0.05 --motion-speed 2 --include-review

just receiver-region-study \
  --config work/personal-samples-2026-11-08/config.json \
  --recording <november-7-park-walk-clip> \
  --spectrum work/overnight-improvements-2026-11-08/fft-study-final/w025/<november-7-park-walk-clip>/receiver-short-spectrum.bin \
  --bounds "$PARK_BOUNDS" \
  --out work/replayed-jet-region \
  --grid 250 --refine 50 --final-grid 10 \
  --clock-window 2 --clock-step 2 --workers 4

just receiver-schedule-study \
  --config work/overnight-improvements-2026-11-08/desk-config.json \
  --spectra-root work/overnight-improvements-2026-11-08/desk-quarter-scoring-review \
  --out work/replayed-desk-schedule --controls 7 --flights 2
```

The optional `--smooth-track-step 0.25` can be added to the model or regional
study for the interpolation sensitivity test. It is not an accuracy upgrade
established by the known park-centre comparison.

### Stationary desk full-curve study, November 8, 2026

The [desk experiment record](desk-model-improvements-2026-11-08.md) documents
retained ADS-B ground motion, original source/cache locations, full-curve joint
assignment, the new private regional regression and all six desk diagnostics.
Its report/provenance root is `work/desk-model-improvements-2026-11-08/`; the
pinned source index is
`references/private/provenance/desk-model-improvements-2026-11-08.json`. The selected
November 5 curve ranks a point 316 m from the supplied desk across a supplied
40 km region, without consuming that coordinate in search. This remains assisted
development evidence with nearby alternatives. The broader multi-flight schedule
still fails controls; original unresolved policies and private-data handling
remain unchanged.
