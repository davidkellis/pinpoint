# Reference dataset and regression checks

Run these commands from the project root. Analysis and search run in Go;
FFmpeg decodes audio. Checks never download data or update their own expectations.

```sh
just reference-check                      # two spectral excerpts + original R04 WAV
just reference-check --suite audio        # full YouTube hour + eight public WAVs
just reference-check --suite location     # R04 against competing California flights
just reference-check --suite all          # all of the above
```

Reports go to `work/reference-check/{quick,audio,location,all}.json`.
Use `--out work/my-check` to keep a separate run. Failures return a nonzero exit
code, including absent or changed inputs. Go-source, manifest and input hashes,
decoder version, timings, curve metrics and aircraft rankings make runs traceable.
The location suite requires the cached California tracks, terrain and geoid;
it reruns all three bands and does not read previously generated results.

To restore the eight original public passages, use `just reference-fetch`.
It downloads the pinned 23 MB Zenodo archive only if needed, verifies its MD5
and each extracted member's SHA-256, and refuses to replace differing files.
The private phone recordings and YouTube MP3 are not downloaded by that recipe.

## What is labeled

| Reference | Independently available evidence | Check |
| --- | --- | --- |
| Colsto, Oyster Bay | Two visually inspected frequency paths; video-reported location with visual camera match | Recover both continuous strong curves, including after fresh full-recording whitening |
| Pathak N784MB_R04 | Publisher's frequency trace, aircraft registration, Livermore city; audio-only timing alignment | Recover the labeled curve; rank `aaa077` first in a California search; retain distant near-tie warning |
| Seven other downloaded Pathak passages | Publisher's identified aircraft, UTC frequency traces and city | Decode/detect and report counts; audio-relative curve labels still pending |
| Challenger R21 and ERJ-145 R50 | Publisher frequency traces, registrations and Livermore city label | Separate partial-recovery regression and unresolved full-curve/location challenges |
| Other 30 Pathak passages | Publisher catalog and measurements | Cataloged, not downloaded or evaluated by this suite |
| Five unique personal recordings (six filenames) | User context; no timestamped aircraft labels | Cataloged only; never treated as aircraft-free negatives |

The two compressed Colsto excerpts retain whole-file-whitened spectra. The
`audio` suite additionally recomputes both 2 s and 4 s spectra from the full
MP3 using the website's channel, analysis band and detector combination.
One continuous detected ridge must follow the anchors with a consistent
harmonic ratio. A high total candidate count cannot substitute for recovering
the known curve. R04's recovered ridge lies at half the frequency of the
published trace; matching through that consistent harmonic factor is permitted.

Requirements live in [suite.json](suite.json). The initial measured outputs are
in [baseline-2026-11-05.json](baseline-2026-11-05.json). Requirements are explicit
minimum coverage/hit rates, maximum frequency error, aircraft identity, regional
agreement and ambiguity retention; the report is not an automatic threshold
update. Changes to labels, inputs or tolerances require an explained review.
R04's initial measured baseline sets a minimum 95% anchor hit rate, full
anchor coverage, strong evidence and at most 3 Hz RMSE. Colsto requires all
of its independently read anchors to match within the stated tolerances.
The seven unlabeled observations do not count as successful recognition tests.
Their changes still appear in the report for review.

These are development regressions, not an untouched test set or an accuracy
certification. Existing `go test ./...` checks cover synthetic stationary tones,
confusers, timing, assignment, silence and refinement. Real-world false-positive
rates remain unmeasured: annotations are incomplete. Candidate counts include
unconfirmed sounds and possibly harmonics of one aircraft.

## Data and location evidence

The separate [jet suite](jets.json) runs with `just jet-reference-check`.
Use `just jet-reference-check --suite location` for the assisted Phenom
California replay, which reruns curve extraction and uses the supplied target
track. See [jet validation](../docs/research/jet-validation.md) for source licenses,
known failures, background controls and the distinction between regional fit
and verified GPS accuracy. The main quick suite remains a propeller regression guard.

Additional jet cases are documented in
[additional-jet-samples.md](../docs/research/additional-jet-samples.md).
`just jet-sample-fetch` restores their eight public files.
`just jet-sample-check --suite all` repeats the passing partial-Challenger and
supplied-track gates. `just jet-challenge-check --suite all` deliberately retains
the currently failing whole-curve and competing-aircraft requirements. The
Challenger's real aircraft ranks second statewide; a near-correct alternative
does not make a wrong winner pass. Neither new recording has verified microphone
GPS. The partial audio gate runs in CI; challenges remain explicitly failing
local integration evaluations.

[jets-tracking.json](jets-tracking.json), run with `just jet-tracking-check`, adds
a stronger gate for the November 6 tracker: at least 68% of Challenger publisher
anchors matched, 70% coverage and at most 6 Hz RMSE on one strong curve. The
measured result is 70.6% hits, 72.7% coverage and 4.48 Hz RMSE. It uses the same
source bytes and independent labels. The original partial baseline and the
75% whole-curve challenge remain unchanged. Both passing regression gates run
in CI. This is a development improvement, not an untouched test-set evaluation.

- [catalog.json](catalog.json): 40 publisher passages, Colsto, five unique personal
  files, duplicate aliases, availability, session and evidence status.
- [sources.json](sources.json): source URLs, versions, local paths and attribution.
- [suite.json](suite.json): SHA-256 for every exercised input, including terrain.
- [provenance/](provenance/): original metadata, publisher results, archive digests,
  inferred audio alignment and previous search protocol.
- [Research notes](../docs/research/sources-and-validation.md): paper investigation,
  external implementation findings and the limits of the location reference.

Reported city, video-reported coordinate, inferred track-offset reference and
our predicted location are separate fields. None is silently promoted to surveyed
microphone GPS. The region used to assess R04 is evaluation-only and is never
passed to the California search. The target KML and registration are also withheld
from search; the published trace is explicitly assisted input.

The checked-in geographic configurations use 200 m spacing across California
and progressive refinement down to 10 m. The latter is sampling resolution,
not a claim of 10 m accuracy. Repeat the input preparation using the source
paths and commands in the research notes if the local cache is absent.

CI runs the quick suite after restoring/verifying the public files. The full
audio and statewide suites are local integration checks requiring their larger
inputs. Neither personal audio nor its unverified location labels enter CI.

## Automatic multi-flyover regression

`references/sequence-failures.json` freezes the original reference-area
`unresolved` result and known-desk-position `no_match` result with their report
hashes. `just sequence-regression-check` verifies those snapshots and evaluates
new reports without changing acoustic labels or location tolerances. It performs
no audio analysis. Reproduce the Go searches first, or use `--reference-report`
and `--desk-report` to select completed sequence reports.

The current sequence search uses guarded training/reserved blocks, training-only
frequency normalization, distinct aircraft and one shared recording clock. Larger
groups expand only from compatible training parents, and each group searches the
original supplied region. Every selected event is mandatory on reserved audio;
validation cannot choose a replacement group. The worst reserved margin across
all groups in eight full wrong-time searches is retained as the competitor.

A conditional reference promotion requires at least three events, both previously
identified reference aircraft, all reserved checks, eight controls, sufficient
control separation and evaluation within 500 m of the video's model coordinate.
The coordinate and expected aircraft are read only after the Go search. This is
a disclosed known-area development experiment, not blind statewide localization
or verified microphone GPS. The private desk replay is a known-position
association diagnostic and must remain `no_match` unless its frozen policy is
reviewed with additional evidence.

Go integration tests separately synthesize emitted acoustic phase with an
independent closed-form sound-delay solution, decode a WAV, detect three flyovers,
fit their shared location/clock, check reserved audio and time controls, and
refine to 10 m spacing. Aircraft type codes exercise eligibility only; synthetic
narrow tones do not model realistic jet engines. Tests also cover unrelated
sounds, aircraft identity reuse, guarded FFT support, training isolation,
validation-only input binding, error/budget handling and web artifact delivery.

## Private stationary desk location regression

`just desk-localization-check --out work/desk-location-new` replays the selected
November 5 curve across a supplied 40 km region, then repeats six incorrect-time
searches. [desk-localization.json](private/desk-localization.json) pins original audio,
enriched tracks, measured spectrum and the selected measured ridge. Receiver
truth is omitted from the Go configuration and evaluated after the training
winner is frozen. The result is 316 m from the supplied desk, with nearby
competitors. The 750 m tolerance is a development regression threshold, not a
confidence radius or demonstrated method accuracy. This does not change the
older unresolved multi-event policy or establish all desk files' locations.

See [the experiment record](../docs/research/desk-model-improvements-2026-11-08.md)
for data preparation, new provisional matches, failed schedule checks and limits.
Private inputs are not downloaded or included in CI. Unit/race tests cover motion
field units, missing geometric climb, legacy CSVs, preservation of original
positions/gaps, full-curve shared-clock assignment and long-recording chirps.
