# Audit against the published Doppler method, November 9

This note compares Pinpoint with the method described in the methodology
video for the reference recording
([UytYrhYLOKI](https://www.youtube.com/watch?v=UytYrhYLOKI)). For each step it
says whether Pinpoint implements the same idea, a measured improvement, or
nothing yet. It also records one gap that was closed today and one jet
observable that is promising but not yet validated.

## Step-by-step comparison

| # | Video step | Pinpoint | Status |
| --- | --- | --- | --- |
| 1 | Download every ADS-B flight for the day (one large scrape) | Daily adsb.lol archives (`pinpoint fetch`, `internal/tracks/archive.go`) or replay indexes plus full traces for a time window and area (`internal/tracks/replay.go`). Uses geometric (GNSS) altitude only; pressure altitude is never used for acoustic geometry. | Same idea; more accurate altitude basis |
| 2 | Drop known jet types via a type lookup; keep unknown types | `tracks.ExcludeKnownJets` with the embedded `type-engines.json`, keeping unknown types. Off by default, so jets are searched too. **Newly available in consensus search** (`--exclude-known-jets`, web checkbox), where it removes jets only as candidate explanations; jets still count in the loudness check. | Same; now in every search mode |
| 3 | 200 m grid within 10 km of each flight path | `GridLocations` corridor grids within the audible slant radius (15 km by default), then coarse-to-fine refinement (consensus: 1 km → 100 m → 20 m with overlapping refinement boxes and finer clock steps) | Same idea, with refinement |
| 4 | Delayed CPA: closest approach plus slant-distance sound travel, using a DEM for ground height | `Track.ApproachWithin` / `Track.Received` solve the retarded-time equation exactly on each segment; receiver height from SRTM terrain plus EGM96 geoid (`internal/geo/heights.go`) | Same |
| 5 | Time-offset filter: second CPA 920 ± 5 s after the first | All curves share one recording clock that is searched (consensus `--clock-window`, joint matching). The offset between flyovers is fixed by flight geometry, not a tolerance window. | Same constraint, exact rather than windowed |
| 6 | Fit speed and CPA distance from each curve; keep only flight segments and grid points that match | The detector measures speed and slant bounds for each curve (`acoustic` measurement). Independent and joint modes gate on them (`search.go`). Consensus instead fits every candidate flight's full predicted curve, which is stricter than speed/distance gates. | Same, or stricter |
| 7 | Silence filter: reject points that would have heard another flight between the two events | (a) User-reviewed silence intervals are enforced as a hard filter over each flight's full audible span (joint and independent modes). (b) Consensus adds an automatic loudness check: aircraft predicted to pass close should coincide with louder audio (`internal/search/loudness.go`). | Same (reviewed) plus an automatic, softer version |
| 8 | Model the exact Doppler curve from real flight geometry, using emission-time radial velocity | Same retarded-time model, on trajectories reconstructed with a Kalman/RTS smoother and Hermite interpolation of reported motion rather than straight lines between ADS-B points | Same; better trajectory model |
| 9 | Score by summing spectrogram brightness along the modeled curve; sweep event 1's time and both source frequencies; lock event 2 to event 1 | Spectral modes sum brightness along predicted curves with a swept shared clock and per-curve source frequency. Consensus fits measured ridges with per-curve source frequency under one shared clock, and ranks by the share of each curve's bend left unexplained. | Same locking; additional metric |
| 10 | Refine 200 m → 100 m around survivors | 1 km → 100 m → 20 m in consensus; configurable elsewhere | Same idea, finer |
| 11 | Group candidates by flight pair, keep each pair's best, and look for a clear winner | Regions separated by two miles, each with its explaining aircraft; status *resolved* only when the best separated competitor is at least 1.5× worse, otherwise *ambiguous* with all regions listed | Same, with an explicit ambiguity rule |
| 12 | Known recording time, one faint curve: trace it by hand and score candidates by distance to the traced line, searched nationwide | Ridge tracing and single-curve search (`consensus --min-curves 1` with a narrow `--clock-window`, or the independent web mode). On the reference recording the automatic detector rates that faint curve as weak; the video traced it by hand. The single-curve demonstration has **not yet been reproduced**. | Implemented; this case untested |
| 13 | Confirm with Street View | Out of scope | — |

Beyond the video, Pinpoint:

- searches jets as well as propeller aircraft (see [jet-validation.md](jet-validation.md));
- selects curves automatically;
- runs wrong-time controls and reserved-ridge checks;
- supports a moving-receiver model;
- reports ambiguity instead of a single answer.

## Reproducing the video's result

With no coordinate supplied, consensus search on the reference recording ranks
the published location's area first. It is **43 m** from the video's coordinate
in a 40 km box (resolved), and **35 m** across all of New York, using the same
two turboprops the video identifies. The statewide run was reported as
*ambiguous*. Every competing region close to the winner explained the 112 Hz
propeller curve with a **jet** (E75L + A319, C172 + F2TH, F2TH + P28A,
C172 + B739, ...). The video avoids those competitors with its jet filter
(step 2), which consensus search did not previously offer.

Rerun with the jet filter (`--exclude-known-jets`), otherwise identical:

| Run | Status | Top region | Aircraft | Runner-up ratio | Points / time |
| --- | --- | --- | --- | ---: | ---: |
| All of New York, ±120 s, all aircraft (previous) | ambiguous | 35 m | PC12 + B350 | 1.35 (jet pair, 27 km away) | 209,123 / 1,475 s |
| All of New York, ±120 s, known jets excluded | **resolved** | **46 m**, clock +90 s | PC12 + B350 | 1.53 (P28A pair, 24 km away) | 170,664 / 520 s |

The filter removed 823 jet track segments and kept 2,115 known non-jets and
159 unknown types. The margin over the 1.5 resolve threshold is small, and
this recording is a development case, so it is not evidence of general
statewide reliability. Use the filter only when the sound was heard as a
propeller; jets remain searched by default.

## A jet observable the video does not use: ground-reflection fringes

Jets often produce no tonal Doppler line that a detector can follow. They do
produce loud broadband noise, and a microphone at height *h* above the ground
hears both the direct sound and its ground reflection. Their interference
produces spectral notches whose spacing corresponds to a delay
τ = 2*h*·sin(elevation)/c ("Lloyd's mirror"). The notch pattern forms U-shaped
fringes in the spectrogram, and the delay appears as a peak in the cepstrum.
The delay rises to its maximum when the aircraft is at its highest elevation,
so τ(t) carries pass geometry: the time of maximum elevation, and how fast the
elevation falls (altitude-to-speed ratio).

Observations so far:

- **Public jets.** The Pathak jet clips and the AeroSonicDB airliner clips show
  rising cepstral delay tracks as the aircraft approaches.
- **The reference recording's camera.** It did not show a ground-reflection
  arch. During an overhead jet pass its cepstral peaks are at ≈ 0.6–1.2 ms,
  which suggests a reflection from the camera mount rather than the ground.
  The broadband loudness peak did arrive at the predicted delayed-CPA time
  (within about one second).
- **One private handheld walking recording.** It shows a clear arch during a
  jet pass. The arch peak and the loudness peak arrived within a few seconds
  of the aircraft's predicted delayed-CPA arrival at the receiver position the
  owner reported afterwards. The delay magnitude, however, did not give the
  elevation. Assuming a ≈ 1 m handheld microphone over flat ground, the arch
  implied a nearly overhead pass, which put the receiver about 1.8 km from
  where it really was. At the reported position the aircraft peaked near 31°
  elevation, so the measured delay corresponds to an effective reflector about
  2 m below the microphone. Sloping or uneven ground near the reflection
  point, or the walker's own body, can change that.

So far the arch's **timing** agrees with geometry, but its **magnitude**
cannot be converted to elevation without knowing the reflecting surface.
The delay-magnitude part is therefore not validated, and a camera or indoor
microphone may not show the arch at all. The observable is **not** used in
any search yet. A useful validation case is a
handheld or tripod recording at a surveyed point under a jet approach path.

Two jet-friendly additions follow from this, in order of evidence:

1. Treat a loud broadband hump as a timing event: "heard an aircraft at
   T ± a few seconds". This is the video's first filter, and it works for
   jets that leave no tonal line.
2. Use the arch only as a timing event (time of maximum elevation), which
   needs no microphone height. Use its magnitude only after validation at
   surveyed positions.

## Reproduction

```sh
just consensus --detection work/traced-joint/reference/detection.json \
  --tracks work/methodology/ny-tracks-typed.csv --start 2026-09-08T20:16:00Z \
  --clock-window 120 --clock-step 4 --fine-clock-step 1 --state NY --download-terrain \
  --terrain-dir data/terrain-ny-3arc --smooth-track-step 0 --reported-motion=false \
  --reconstruct-track=false --exclude-known-jets \
  --loudness-audio "samples/Unknown location - raw audio [4CxVxuMVkMc].mp3" \
  --out work/consensus-ny-props
```
