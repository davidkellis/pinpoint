# Real spectral regression fixtures

Source: the user-supplied MP3 `Unknown location - raw audio [4CxVxuMVkMc].mp3`
from https://www.youtube.com/watch?v=4CxVxuMVkMc, discussed in the supplied
transcript of https://youtu.be/UytYrhYLOKI.

Source MP3 SHA-256:
`89512e9c28454029059cc3cf7bc79ca77811c9bc931bf17584481ad897e71487`

These are gzip-compressed Go `Spectrum` values: numerical, whitened log-power
measurements, not synthesized curves or raw audio. They contain all spectral
bins from 40 through 400 Hz in two excerpts, including competing/background
tones. They were cropped **after whole-recording whitening**, preserving the
values used by the web detector. No aircraft tracks or location labels enter
detection.

| Fixture prefix | Source frame-time offset | Excerpt duration |
| --- | --- | --- |
| `colsto-1` | 2000 s | 300 s |
| `colsto-2` | 2940 s | 260 s |

Both use 4096 Hz resampled mono audio, channel 0. `long` uses a 16384-sample
Hann window and 2048-sample hop (4 s / 0.5 s). `short` uses 8192 / 1024
(2 s / 0.25 s). Crop row indices are `[offset/hop, (offset+duration)/hop)`;
`Spectrum.Time` includes the half-window center offset. The test adds the source
offset when comparing with the original recording.

The frequency anchors in `real_recording_test.go` were read from the spectrogram.
They require one strong, jointly usable path through each known flyover, with
an interior closest approach and at least 80 seconds of support. A random
unrelated strong tone cannot satisfy the test. Other candidate sounds are not
labeled as aircraft or negatives by this fixture. Cropped tests cannot substitute
for full-file detection checks; both full recordings were also checked during
the v8 fix.

Run:

```sh
go test ./internal/acoustic --run TestRealReferenceCurves --v
```
