# Trajectory reconstruction after the statewide failures

Private-case details (aircraft, timings, coordinates) are kept in the gitignored references/private/ directory.

The reviewed November 5 tracked-flyover curve now selects a point **426.5 m** from
the supplied desk across Texas, retaining the **±6-second** recording-clock
allowance. The baseline winner was 360.36 miles away. This is an assisted
development result, not an automatic localization accuracy rate. The observed
curve was previously investigated; no aircraft identity or observer coordinates
were supplied to this statewide rerun. The automatic primary corpus results remain
preserved separately.

## What changed

Previously the reported-motion Hermite interpolator preserved every measured
position/altitude node. Endpoint tangents could agree with reported speed and
climb rate while interior derivatives absorbed position noise and altitude
quantization. Those derivatives directly affect modeled Doppler frequency.

`internal/tracks/reconstruct.go` adds a six-state position/velocity Kalman filter
and Rauch–Tung–Striebel backward smoother in relative ECEF coordinates. It combines
position observations with reported ground speed/true track and geometric climb
rate in local east/north/up axes. Its state estimates then supply the existing
Hermite interpolator. Original audio and raw trajectory measurements are retained.
Neither audio, receiver coordinates, aircraft identity nor expected source pitch
enters the reconstruction.

One preset was fixed before the statewide reconstruction test:

| Setting | Value |
| --- | ---: |
| Horizontal position standard deviation | 15 m |
| Vertical position standard deviation | 20 m |
| Horizontal/vertical velocity standard deviation | 1.5 m/s |
| White acceleration process density, square-root parameter | 0.8 m/s/√s |
| Maximum independent-chunk gap | 30 s |
| Maximum accepted node displacement | 120 m |
| Hermite sampling step | 0.25 s |

The process covariance per axis is `a² × [[dt³/3, dt²/2], [dt²/2, dt]]`.
These errors are engineering assumptions, not calibrated GPS confidence.
Missing velocity components remain missing measurements. Barometric climb rate
is never substituted for geometric climb rate. Gaps split independent chunks;
the smoother does not learn across them. Chunks with numerical problems,
unrepresentable inferred motion or excessive displacement retain their original
geometry and motion. Rejection counts and node displacements are reported.
The first full-state reconstruction retained 67 original chunks among 1,836
screened track segments; the largest accepted displacement was 118.49 m.

The algorithm follows the standard linear Kalman/RTS equations in
[Särkkä and Svensson, *Bayesian Filtering and Smoothing*](https://users.aalto.fi/~ssarkka/pub/bfs_book_2023_online.pdf).
Units and geometric fields follow the
[readsb trace schema](https://github.com/wiedehopf/readsb/blob/dev/README-json.md).
The optional reconstruction is a state estimate, not additional measured telemetry.

## Experiments and limits

All 111 measured ridge observations of the reviewed curve remain available;
guarded training/reserved blocks use the same fixed partition. Statewide
searches retain all eligible aircraft types, the Texas polygon, available flight
corridors, terrain/geoid elevations, 1,000 → 100 → 20 m grids, 20 coarse
flight-combination seeds, 10 final seeds and the same ±6 s clock grid. There is
no supplied desk-area bound or supplied aircraft ID.

The reconstruction selects the tracked aircraft's leg, about 426.5 m from the
desk, with a +2-second common clock correction. Training ridge RMSE is 0.335 Hz
and reserved ridge RMSE is 0.580 Hz. The second aircraft group is about 426 km
from the desk and has a 0.374 Hz training RMSE. These residuals are descriptive
development metrics, not a calibrated probability or proof of unique GPS. Grid
spacing does not establish location accuracy.

A fixed-eight-candidate ablation preceded the full-state reconstruction run.
Full-spectrum scoring and one through four harmonics did not make the original
near-desk candidate the training winner. Reconstruction moved it from fourth to
first among those frozen candidates, motivating the statewide rerun. The global
rerun then evaluated 498,964 positions and independently selected the near-desk
group. These examined cases are not untouched test data. One preset was used;
parameters were not swept to minimize supplied-site error.

The automatic November 5 longest-curve choice and the park propeller and jet
protocols are rerun using the same preset and six-second clock allowance. Their
top-winner offsets are:

| Case | Preserved baseline | Reconstruction |
| --- | ---: | ---: |
| Reviewed desk tracked-flyover curve | 360.359 mi | **0.265 mi / 426.5 m** |
| Desk automatic longest-curve choice | 259.624 mi | 259.624 mi |
| Park propeller case | 241.811 mi | 74.409 mi, shape check fails |
| Park jet case | 229.563 mi | 229.781 mi |

The automatic choice remains event 42,752, rather than the previously reviewed
event 13,973. Its distant location survives the current local checks.
Reconstruction alone therefore does not establish reliable automatic event
selection or calibrate statewide false matches.

The complete outcomes, including failures, are in
`work/model-improvements-2026-11-08/comparison.json`. The jet comparison additionally
introduces reported-motion Hermite interpolation relative to its earlier
position-linear protocol. Neither walk has a measured receiver trajectory, and
the jet association remains trajectory-guided rather than an independently
detected acoustic ridge. Do not generalize the tracked-flyover result into a claim
that the walking cases or automatic aircraft selection have been solved.

## Runtime changes

`PrepareSegments` caches receiver-independent segment velocity/speed in immutable
tracks before scoring workers start. With ordered subsonic motion, sound-arrival
time is monotonic, allowing binary-search interval bounds. Suspect geometry keeps
exhaustive checks. Range-only screening returns as soon as a qualifying segment
is found; it no longer computes an unnecessary global distance minimum. A further
causality bound for range `R` restricts emission time to `[arrival_start − R/c,
arrival_end]`, including tracks where a different bad segment prevents monotone
arrival indexing. Telemetry gaps and speed exclusions remain identical.

The dense arrival-interval benchmark improves about 20×. The combined full-curve
microbenchmark improves roughly 3× relative to the initial implementation; these
are different workloads and are not application-wide speedups. A paired statewide
replay of the reviewed case drops from **927.2 to 567.5 seconds**, an observed
**1.63×** wall-time ratio, while preserving **the exact same winner, eight
leading groups and 498,964 evaluated positions**. Both runs shared CPU with other
tests/searches, so these wall times are operational observations rather than a
controlled benchmark. Executables are pinned separately. The final causality
bound and report track IDs were added after that paired replay and are covered by
equivalence/unit tests.

Analytic noisy-telemetry tests independently generate positions, velocities and
received frequency. Forward-model RMSE improves from 0.729 to 0.025 Hz for a
130 Hz propeller tone and from 17.54 to 0.618 Hz for a 1,500 Hz jet tone. Tests
also protect gap independence, raw-input immutability, rejected corrections,
bearing wraparound, exact arrival vertices, suspect-speed fallback and complete
joint-fit statistics. Synthetic forward-model improvement is not real-world
aircraft recognition accuracy.

## Reproduction and artifacts

```sh
# Private media and cached state-wide traces/spectra are required.
just texas-evaluate --out work/texas-reconstructed-check --reconstruct-track --only nov05-prior-turboprop-curve
just model-improvements-report
just sample-audit-serve
# http://127.0.0.1:8081/model-improvements-2026-11-08/
```

Remove `--only` for the entire declared corpus. The full accuracy gate remains
separate; one reviewed success does not make that gate pass. The reconstruction
option requires a new output directory, preserving the earlier baseline.
Ordinary Go grid searches use the exact geometry optimizations. The reconstruction
is opt-in in the research scanner; it is not the website's default model.

The study directory retains comparison JSON, maps with state borders and streets,
complete overlays of the reviewed curve, fixed-peak spectral ablations, all
evaluated grid points, benchmark logs, executable hashes and a source archive.
`references/private/provenance/trajectory-reconstruction-2026-11-08.json` indexes
them. Original phone audio remains private and unchanged.

The largest remaining gains require better automatic curve selection, jointly
supported flyovers with one receiver path/clock, and full-search false-positive
calibration. A missing tone in a noisy or indoor recording does not establish
silence. Weather and receiver motion remain model limitations. A denser grid
cannot supply evidence that is absent from the recording.
