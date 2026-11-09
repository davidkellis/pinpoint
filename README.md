# Pinpoint

Estimate where a phone recording was made by matching the Doppler curves of
passing aircraft in the audio against historical ADS-B flight tracks. Written in
Go with no third-party Go dependencies; FFmpeg decodes the audio. The idea comes
from [this video](https://youtu.be/UytYrhYLOKI).

The audio contains aircraft *sound*, not ADS-B messages. Flight telemetry is
downloaded separately (ADSB.lol public archives and replay indexes).

## Status

Pinpoint is a **research prototype**, not a dependable locator.

- **Consensus search (November 9)** finds locations where at least two
  automatically detected curves are explained by different aircraft under one
  clock, and checks each area against the recording's loudness. On the YouTube
  recording, it searched all of New York from 16 automatically selected curves
  and a ±120 s clock. It ranked the true area first (**35 m** from the video's
  coordinate, with the correct two aircraft) and reported it as *ambiguous*
  because a 27 km alternative is close. In a 40 km box the same search is
  *resolved* at 43 m. The Texas desk recordings contain at most one usable
  tracked flyover, so consensus correctly returns no location instead of a
  wrong one. See
  [consensus-search-2026-11-09.md](docs/research/consensus-search-2026-11-09.md).
- The synthetic demo recovers its known location and clock offset (about 20 m).
  That is a regression fixture, not an accuracy claim.
- **No personal phone recording has yet been located automatically.** In the
  November 8 full-Texas evaluation (single-curve protocol), 10 automatic attempts across 11 recordings produced
  locations and none was within two miles of its supplied site; one recording
  had no strong curve. See
  [texas-statewide-evaluation-2026-11-08.md](docs/research/texas-statewide-evaluation-2026-11-08.md).
- The only statewide Texas success is **assisted**: one hand-reviewed tracked-flyover curve
  selects a point about **427 m** from the desk, using fixed-uncertainty
  trajectory reconstruction and a +/-6 s clock allowance. Without that
  reconstruction, the same curve selected a point 360 miles away. It does not
  show automatic accuracy. See
  [trajectory-reconstruction-2026-11-08.md](docs/research/trajectory-reconstruction-2026-11-08.md).
- The New York reference has now been reproduced without hand-reviewed windows
  (see consensus search above). It is a development case, since the thresholds
  were tuned on it, not a held-out benchmark.
- Public Livermore, California samples (Pathak dataset) are regression labels.
  In assisted California searches the correct aircraft ranks first for the R04
  propeller passage (with a near-tied distant alternative) and only second for the
  Challenger jet (behind a wrong location). No microphone GPS is verified, and jet
  support is experimental.
- Scores are uncalibrated fit measures, not probabilities. A fine grid spacing
  (down to 10 m) is a sampling resolution, not demonstrated accuracy.
- Not implemented: moving-receiver (walking) reconstruction, date-only nationwide
  search (an approximate recording start time is required), calibrated confidence.

## How it works

1. **Detect.** FFmpeg decodes one audio channel. Two spectrograms (2 s and 4 s
   windows, 40-1,800 Hz; optional short-sweep profile to 6.5 kHz) are whitened and
   scanned for smooth tonal ridges. Candidates must pass coverage, contrast,
   Doppler-shape and cross-resolution checks. Candidates are sounds, not
   confirmed aircraft.
2. **Fetch tracks.** Flights near the search area and recording time are
   downloaded (or imported from a readsb archive) as geometric-altitude tracks.
   Pressure altitude is never substituted.
3. **Predict and score.** For each candidate location and recording-clock offset,
   the receive time is inverted to emission time and the Doppler curve
   `f(tr) = f0 * c / (c + vr)` is predicted for each aircraft, with a fitted
   source tone `f0`. The score compares predicted and measured frequency on the
   whitened spectrogram.
4. **Combine flyovers.** Stationary recordings are searched for several flyovers
   that share one location and one clock, with distinct aircraft. Part of the
   audio is reserved for validation, and the search is repeated at eight wrong
   recording times as controls. Unresolved results stay unresolved.
   The newer **consensus search** offers every strong curve and needs at least
   two to agree. A curve counts only when an aircraft explains most of its bend
   (Doppler versus straight-line residual), so nearly flat curves cannot fit
   everywhere. Each candidate area is also checked against the recording's
   loudness ("silence is a signal"): aircraft predicted to pass close by should
   be heard.
5. **Refine.** Geometry at 200 m, scoring at 100 m, then progressive refinement
   (50, 25, 12.5, 10 m) around competing peaks.

Assumptions: a stationary microphone, still air at 343 m/s, direct propagation,
a constant source tone per flyover and subsonic flight. Wind, refraction,
multipath, RPM changes, clock drift and ADS-B gaps are not modeled.

## Requirements

- Go 1.24 or newer (`go.mod` declares `go 1.24`)
- [`just`](https://github.com/casey/just) (1.52 tested; the justfile uses `[group]` attributes)
- FFmpeg and `ffprobe` on `PATH`
- Python 3, only for dataset-restore and research-report scripts in `tools/`
  (not needed by the app); `jet-reference-fetch` can also need `h5py` and `scipy`
- Internet access for flight and terrain downloads and satellite map tiles. The
  synthetic demo and plain-background maps work offline.

## Quick start

```sh
just            # list recipes, grouped: core, checks, data, research, reports
just demo       # builds, then writes a synthetic experiment to work/demo
just map        # opens work/demo/results/map.html
just serve      # web app at http://127.0.0.1:8080
```

`just demo` needs `work/demo` not to exist; pass `--out work/demo-2` for another
run. Runtime flags always use double hyphens, and recipes forward them unchanged.

### Web app (drag and drop)

1. Open <http://127.0.0.1:8080> and choose a **Sound analysis** profile: long
   tonal curves, short sweeps, or both. Drag an audio or video file onto the
   upload box; analysis starts automatically.
2. Enter the approximate recording **start time**, **time zone** and
   **uncertainty**. A date is required; audio does not encode one. Filenames such
   as `2026_11_03_18_50_11_1.mp3` prefill a suggestion to check.
3. Optionally pick a **state** or draw a search rectangle. Mark walks with
   **I was walking or moving**; walking uses independent per-sound screening, not
   a reconstructed route.
4. Click **Find likely locations**. Stationary uploads default to **Several
   flyovers agree** (consensus search). It returns a map of every separated
   candidate area with its aircraft, clock and loudness check, plus a status:
   resolved, ambiguous, contradicted or no match. **Combine flyovers** (sequence
   groups with reserved-audio and wrong-time controls) and **Screen each sound
   independently** (needed for walks) remain available.

Leaving the state blank searches eligible flight corridors across the contiguous
US, within resource limits (512 MiB uploads, 8 GB of flight downloads per search).
Jobs live in `work/web/jobs/` and caches in `work/web/cache/`. The server is for
local use only. Options:

```sh
just serve --addr 127.0.0.1:8081 --data-dir work/web --workers 8 --event-workers 2
just serve --tracks /path/to/tracks.csv      # or --trace-dir /path/to/readsb
just serve --max-upload-mb 1024 --max-download-gb 12
```

## CLI workflow

Keep the phone stationary, keep the original file, and note the start time with
its time zone. Do not feed a known test coordinate into the search. Check
options with `just analyze --help`, `just import --help`, and so on.

```sh
# 1. Analyze: spectrograms, events.json (unverified proposals), detection.json
just analyze --audio /path/to/recording.m4a --out work

# 2. Fetch: list adsb.lol archive parts for a UTC date, then optionally download
just fetch --date 2025-09-08
just fetch --date 2025-09-08 --download --out data/archive --max-gb 8

# 3. Import the needed time range (quote the glob) into a normalized CSV
just import --archive 'data/archive/v2025.09.08-planes-readsb-prod-0.tar.??' \
  --from 2025-09-08T20:28:00Z --until 2025-09-08T20:37:00Z --out work/tracks.csv
  # optional: --bounds south,west,north,east (pad well beyond the listener area)

# 4. Locate: edit examples/search.json (recording_start, bounds, events), then
just locate --config examples/search.json --out work/results
just map --results work/results
```

For a stationary recording, the **consensus search** needs no reviewed windows
and is the recommended statewide method. It offers every strong curve, requires
at least two of them to be explained by different aircraft under one clock, and
checks each candidate area against the recording's loudness:

```sh
just consensus --audio /path/to/recording.m4a --tracks work/tracks.csv \
  --start 2026-11-05T<HH:MM:SS>-06:00 --clock-window 6 --state TX \
  --download-terrain --out work/consensus
# open work/consensus/consensus.html; consensus.json lists every region
```

Use `--detection detection.json` to reuse an analysis, `--bounds` for a smaller
area, and a wider `--clock-window` (with `--clock-step 4`) when the start time is
only known roughly. Tracks with reported motion are reconstructed and
interpolated by default (`--reconstruct-track=false --reported-motion=false
--smooth-track-step 0` for plain position tracks).

- `fetch` options: `--date`, `--instance`, `--download`, `--out`, `--max-gb`.
  `import` options: `--input` (extracted traces) or `--archive`, `--from`,
  `--until`, `--out`, `--bounds`, `--max-gap`. `--bounds` must cover aircraft
  corridors, not just the listener area, or nearby flyovers are discarded.
- Track CSV columns: `track_id,time_unix_s,lat,lon,alt_ellipsoid_m` and optional
  `type_code`; WGS84 degrees, ellipsoidal metres, UTC Unix seconds.
- Paths in a search config resolve relative to the config file. `events` give
  reviewed `start_s`/`end_s`, `cpa_s` and source-tone bounds from the
  `analyze` output.

Other commands:

| Recipe | Purpose |
| --- | --- |
| `jet-analyze`, `hybrid-analyze` | Short-sweep and combined long-tone/short-sweep detection |
| `joint-locate` | Re-trace reviewed sound windows, search jointly, then refine |
| `auto-locate` | Automatic multi-flyover groups with reserved audio and wrong-time controls (`--profile hybrid`, `--controls 0/2/4/8`, `--detection`, `--method pairs`) |
| `corroborate` | Look for additional flyovers at existing candidate locations |
| `diagnose` | Explain one location hypothesis (`--lat --lon --track`) and plot residuals |
| `run` | Pass any `pinpoint` subcommand through |

The `research`, `reports` and some `data` recipes in `just --list` drive studies
and local reports; their usage is in the [research notes](#documentation) below.

## Testing and regression suites

```sh
just test                       # go test -race (cmd, internal, four tools) + go vet
just reference-fetch            # restore the eight CC BY public passages first
just reference-check            # quick known-curve checks
just reference-check --suite audio      # full YouTube hour + public WAVs
just reference-check --suite location   # statewide California replay
just jet-reference-check        # jet positives and background controls
just jet-sample-check           # partial Challenger regression
just jet-tracking-check         # jet ridge-coverage regression
```

`just test` limits Go packages to `./cmd/... ./internal/...` so it does not
descend into the gitignored `work/`. Audio integration tests need FFmpeg and are
skipped by `go test -short`. Reference checks never download data or rewrite
their own expectations; see [references/README.md](references/README.md) for
labels, baselines and acceptance gates.

CI ([.github/workflows/test.yml](.github/workflows/test.yml)) runs `go test -race`,
the tool tests, `go vet`, a build, the quick reference suite, and the jet audio,
Challenger-partial and tracking regressions. Other recipes need data CI does not
have:

- `jet-challenge-check` keeps unresolved jet failures visible and is expected to
  fail.
- `hybrid-reference-check`, `jet-test-check`, `sequence-regression-check` and the
  `--suite location` replays are local integration checks.
- `desk-reference-check`, `desk-localization-check`, `receiver-recovery-check` and
  `texas-evaluate` replay private recordings and run only on the owner's machine.

These are development regressions, not an untouched test set or an accuracy
certification.

## Data and privacy

The following are gitignored and not published, so private checks cannot run
from a fresh checkout:

- personal phone recordings (`samples/*.mp3`, `*.m4a`, `*.wav`), including the
  walking and desk recordings
- the YouTube reference rip `samples/Unknown location - raw audio [4CxVxuMVkMc].mp3`
- `references/private/` (private manifests, labels and provenance)
- `work/`, `data/`, `bin/` and `demo-output/` (caches, jobs, archives, results)

Owner locations (the desk and a park) belong only in those private labels; do not
add them to tracked files or docs. Openly licensed samples under
`samples/public/` are committed with attribution and keep their upstream licenses:
Pathak 2026 and DLR RECORD 139 are CC BY 4.0, AeroSonicDB is CC BY-NC 4.0
(noncommercial). Flight data comes from
[ADSB.lol](https://www.adsb.lol/docs/open-data/historical) (ODbL).

## Documentation

Research notes in `docs/research/`:

| File | Contents |
| --- | --- |
| [development-log.md](docs/research/development-log.md) | Chronological development log moved from the old README; many findings, some since superseded |
| [sources-and-validation.md](docs/research/sources-and-validation.md) | Pathak dataset and paper search, other implementations, data locations, FR24 playback cross-check, receiver-model and regional-scan research, repeat commands |
| [jet-validation.md](docs/research/jet-validation.md) | Jet curve experiments: short-sweep profile, A320/Phenom/AeroSonicDB/DLR data, assisted California replay, frozen desk regression |
| [additional-jet-samples.md](docs/research/additional-jet-samples.md) | Challenger and ERJ-145 samples, tracking diagnostics, guided discovery, sequence controls, joint refinement, multi-flyover groups with reserved-audio validation |
| [desk-model-improvements-2026-11-08.md](docs/research/desk-model-improvements-2026-11-08.md) | Full-curve joint fit and regional search for the stationary desk case (conditional 316 m result) |
| [texas-statewide-evaluation-2026-11-08.md](docs/research/texas-statewide-evaluation-2026-11-08.md) | Full-Texas evaluation: no automatic result within two miles; one conditional success |
| [trajectory-reconstruction-2026-11-08.md](docs/research/trajectory-reconstruction-2026-11-08.md) | Kalman and RTS trajectory reconstruction; the reviewed tracked-flyover curve reaches 426.5 m |
| [consensus-search-2026-11-09.md](docs/research/consensus-search-2026-11-09.md) | Why single curves cannot localize statewide; consensus search with curvature and loudness evidence; New York and Texas results |

Also: [references/README.md](references/README.md) (reference dataset and
regression gates), [samples/public/pathak-2026/README.md](samples/public/pathak-2026/README.md)
(public samples and limits) and
[internal/acoustic/testdata/README.md](internal/acoustic/testdata/README.md)
(spectral fixtures).

External references: [readsb trace schema](https://github.com/wiedehopf/readsb/blob/dev/README-json.md#trace-jsons),
[NASA Doppler equations](https://www.grc.nasa.gov/www/k-12/airplane/doppler.html),
[USGS SRTM guide](https://lpdaac.usgs.gov/documents/179/SRTM_User_Guide_V3.pdf)
and [FFmpeg](https://www.ffmpeg.org/ffmpeg.html).
