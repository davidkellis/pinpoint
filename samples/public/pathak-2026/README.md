# Public aircraft flyover samples

The versioned [reference suite](../../../references/README.md) now checks the
original R04 audio and replays its statewide search. The
[source review](../../../docs/research/sources-and-validation.md) records an
additional **inferred** main recording site reported by dopplerprint:
37.663216, -121.781876. Our earlier statewide top point is about 1.925 km from
that estimate. Neither point is surveyed microphone GPS; the regional success
reported below does not establish precise recording location.

Eight unchanged WAV recordings from Praveen Pathak's **Aeroplane data full
analysis and raw files**, published October 16, 2026:

- Dataset: https://zenodo.org/records/23424278
- DOI: https://doi.org/10.5281/zenodo.23424278
- License: [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/)
- Downloaded archive: `Raw data_V_constant_H_constant.zip`
- Verified archive MD5: `d34b32ae57477074d6e8563300a7fc19`

The publisher describes smartphone recordings over Livermore, California, in
May 2026. Each directory contains the original `audio.wav`, a timestamped KML
aircraft track, selected harmonic frequency measurements in CSV, and the
publisher's analysis PDF. `manifest.json` records individual audio SHA-256
hashes, durations, attribution and the corresponding published result rows.
Audio has not been normalized, cropped, denoised or resynthesized.

Start with `N784MB_R04/audio.wav` (85.23 seconds) or `N788BM_R03/audio.wav`
(68.03 seconds). Both show descending tonal curves and harmonics in the measured
spectra. A curve count is not an aircraft count: harmonics and fragments can
produce multiple candidates for one passage.

From the project root, open the already generated comparison:

```sh
just experiment-open work/public-flyover-curves/index.html
xdg-open work/public-flyover-curves/flyover-previews.png
```

Reproduce the Go detector comparison:

```sh
just experiment --config examples/public-flyover-curves.json \
  --out work/public-flyover-curves --workers 2
```

The manifest compares production 2+4-second analysis, 1/2/4-second STFTs,
local background subtraction and harmonic pooling. On November 5, 2026,
the production comparison returned at least one heuristic strong candidate
in seven of these eight files. This is **not** a measured aircraft recall rate:
the candidates have not been matched against independently time-aligned labels.
The case kind is `unlabeled` for this reason, despite the publisher's aircraft
identifications. No detector thresholds were adjusted for these samples.

The `analyze` CLI uses a single FFT window by default; its output is not the
same as the workbench's production 2+4-second comparison. The existing website
can also accept these WAV files as uploads.

## What remains before scoring geographic accuracy

1. The downloaded files do not supply a verified microphone latitude,
   longitude and elevation. The published city is insufficient to measure
   position error in meters. This does **not** prevent a statewide search or
   checking whether the highest-ranked region is near Livermore.
2. `received_start_utc` in `final_results_40_cases.csv` equals the first
   **selected ridge measurement**. It is not a verified timestamp for sample
   zero of the WAV. Do not silently use it as `recording_start`.
3. The KML altitude basis is identified by the publisher as EGM96 geometric
   height above mean sea level. Import requires conversion to the ellipsoidal
   height used by Pinpoint; simply renaming the altitude field is incorrect.
4. Each supplied track identifies the target aircraft. Testing against only
   this track would test known-flight position fitting. A blind aircraft search
   also needs competing tracks from the same time and region.

These samples provide independent real audio and telemetry for further
development. Keep the published position-related result columns out of search
inputs. The previously documented Colsto reproduction remains the local example
with a reported microphone coordinate; it is a development benchmark, not a
held-out accuracy test.

The preview image is a measured two-second Hann STFT (4096 Hz analysis rate,
0.125 s hop, 60–1200 Hz), with display limits −70 to −10 dB of unnormalized FFT
magnitude and no curve overlays. Its rendering script is saved at
`work/public-flyover-source/render_previews.py`. Those dB values are not SPL.

## First California-wide search: November 5, 2026

The first trial used `N784MB_R04/audio.wav` and its published UTC frequency
measurements. The search was restricted to **California's state boundary**,
without Livermore coordinates, the supplied target KML, aircraft registration,
or the publisher's inferred speed/height/distance results as search inputs.

The highest-ranked aircraft was independently identified as ICAO24 `aaa077`.
After completing the rankings, its archive metadata was checked: it is
**N784MB**, matching the source recording's aircraft. The highest-ranked
position is **37.647510, −121.791066**, in the Livermore area.

| Flight hypothesis | Candidate latitude, longitude | Spectral score |
| --- | --- | ---: |
| `aaa077` / N784MB | 37.647510, −121.791066 | 19.0089 dB |
| `a50358` / N422D | 38.997717, −121.135153 | 18.7462 dB |
| `a8c7b8` / N665KL | 35.070817, −120.154310 | 18.3915 dB |

This is a promising regional result, **not a uniquely established location**.
The second hypothesis is approximately 160.7 km away and only 0.2627 dB below
the winner. The existing ambiguity check correctly leaves the location
unresolved. Scores are not probabilities, and a 10 m final grid does not
establish 10 m accuracy. Only this one recording has been searched so far.

```sh
# Open the combined statewide map and its curve comparisons.
just map --results work/pathak-california/results
```

The trial used the independent [ADS-B archive for May 14, 2026](https://github.com/adsblol/globe_history_2026/releases/tag/v2026.05.14-planes-readsb-prod-0).
Both archive parts were verified against their published SHA-256 hashes. A
padded California bounding box, from 17:39 to 17:46 UTC, retained 51,278
positions in 1,387 usable track segments. This includes competing aircraft;
there was no target-aircraft filter and no exclusion of known jets. Acoustic
speed/slant limits and available geometric-altitude telemetry still limit
which flights and locations can be modeled.

Aligning the published frequency trace against the original WAV's measured
spectrum places the first CSV measurement 4.975 s into the audio, implying
sample zero at `2026-05-14T17:41:32.264Z`. This uses no flight geometry or
location information. The search allows an additional ±5 s clock offset;
it does not independently verify the publisher's absolute clock.

Broad speed, slant-distance and timing bounds were fitted from the observed
CSV curve alone. The Go matcher scored the original audio spectrum subject to
agreement with that curve, using retarded sound propagation and terrain plus
the EGM96 geoid. This is an **assisted curve-input test**, not a test of fully
automatic aircraft detection from an unannotated WAV.

To preserve 200 m spacing while respecting the two-million-point per-run limit,
California was partitioned into three contiguous latitude bands. All three
were searched and their rankings merged. The search evaluated 2,604,556
locations including refinement, retaining 4,322 modeled points for the map.
Refinement progressed through 50, 25, 12.5 and 10 m. The three Go searches,
including report generation, took approximately 17 seconds on this machine;
archive download, import, terrain preparation and audio work are additional.
Each band retains up to eight refinement seeds per stage. Coarse rejection,
archive gaps, source-frequency variation, weather and single-flight ambiguity
remain limitations.

These files are separate passages from different recording sessions. They were
not combined as though they shared one continuous audio clock, and no silence
constraint was invented for this trial. A stronger validation would use another
independent passage, or multiple flyovers known to share one recorder position.

Inputs, frozen protocol, inferred audio alignment, per-band results, runtimes
and post-search aircraft verification are saved in `work/pathak-california/`.
The original `search.json` describes the complete region but exceeds the current
per-run grid limit. To repeat the executable partitioned search with the cached
inputs:

```sh
just locate --config work/pathak-california/search-band-1.json --out work/pathak-california/results-band-1
just locate --config work/pathak-california/search-band-2.json --out work/pathak-california/results-band-2
just locate --config work/pathak-california/search-band-3.json --out work/pathak-california/results-band-3
go run ./work/pathak-california/merge
```
