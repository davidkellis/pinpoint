set positional-arguments

# List available commands.
[group('core')]
default:
    @just --list

# Build the Go executable.
[group('core')]
build:
    go build -o bin/pinpoint ./cmd/pinpoint

# Start the drag-and-drop web app at http://127.0.0.1:8080.
[group('core')]
serve *args: build
    ./bin/pinpoint serve "$@"

# Run tests with the race detector and static checks.
[group('checks')]
test:
    go test -race ./cmd/... ./internal/...
    go test -race tools/flight_timeline.go tools/flight_timeline_test.go
    go test -race tools/audit_receiver_schedules.go tools/audit_receiver_schedules_test.go
    go test -race tools/enrich_track_motion.go tools/enrich_track_motion_test.go
    go test -race tools/scan_receiver_region.go tools/scan_receiver_region_test.go
    go vet ./cmd/... ./internal/...

# Run the synthetic experiment; override the new output directory with --out.
[group('core')]
demo *args: build
    ./bin/pinpoint demo --out work/demo "$@"

# Analyze a recording: just analyze --audio recording.m4a --out work
[group('core')]
analyze +args: build
    ./bin/pinpoint analyze "$@"

# Experimental jet/short-pass analysis with 0.5 + 1 second FFTs up to 6.5 kHz.
[group('core')]
jet-analyze +args: build
    ./bin/pinpoint analyze --sample-rate 16384 --window 8192 --hop 1024 --min-hz 100 --max-hz 6500 --detector-profile short-pass "$@"

# Check long tones and short higher-frequency sweeps across the entire recording.
[group('core')]
hybrid-analyze +args: build
    ./bin/pinpoint analyze --detector-profile hybrid "$@"

# List or download flight archives: just fetch --date YYYY-MM-DD [--download]
[group('core')]
fetch +args: build
    ./bin/pinpoint fetch "$@"

# Import extracted traces using --input, --from, --until and --out.
[group('core')]
import +args: build
    ./bin/pinpoint import "$@"

# Search for a recording's location using --config and --out.
[group('core')]
locate +args: build
    ./bin/pinpoint locate "$@"

# Trace reviewed sound windows afresh, jointly search the region, then refine.
[group('core')]
joint-locate +args: build
    ./bin/pinpoint joint-locate "$@"

# Jointly search sound groups, reserve audio for validation, run eight time controls.
[group('core')]
auto-locate +args: build
    ./bin/pinpoint auto-locate "$@"

# Example: just consensus --audio rec.mp3 --tracks tracks.csv --start 2026-11-05T10:17:58-06:00 --state TX --out work/c1
# Find locations where several strong curves agree under one clock, with a loudness check.
[group('core')]
consensus +args: build
    ./bin/pinpoint consensus "$@"

# Example: just sounds --audio rec.mp3
# Explain what a recording contains and which sounds can help locate it.
[group('core')]
sounds +args: build
    ./bin/pinpoint sounds "$@"

# Recheck frozen training winners; requires the original report and search binary.
[group('checks')]
revalidate-sequences +args: build
    ./bin/pinpoint revalidate-sequences "$@"

# Check unchanged historical failures and freshly computed sequence reports.
[group('checks')]
sequence-regression-check *args:
    python3 tools/check_sequence_regressions.py "$@"

# Inventory/deduplicate every sample and run fresh Go hybrid sound analysis.
[group('research')]
sample-audit *args: build
    python3 tools/audit_samples.py "$@"

# Acquire state-wide flight discovery/traces; --family pathak uses California.
[group('data')]
sample-audit-tracks *args:
    go run tools/fetch_sample_tracks.go "$@"

# Run and evaluate location experiments after sample analysis/track acquisition.
[group('research')]
sample-audit-locations *args:
    go build -o bin/locate-sample tools/locate_sample.go
    python3 tools/audit_sample_locations.py "$@"

# Serve the research audit and its relative map assets over local HTTP.
[group('reports')]
sample-audit-serve port="8081":
    python3 -m http.server "$1" --bind 127.0.0.1 --directory work

# Capture sparse anonymous FR24 playback observations for supplied recording times.
[group('data')]
flight-history-capture +args:
    python3 tools/capture_fr24_playback.py "$@"

# Predict arrival times and compare FR24 identities/automatic curves in Go.
[group('research')]
flight-history-review +args:
    go run tools/flight_timeline.go "$@"

# Acquire geometric tracks for configured windows; --state TX selects statewide discovery.
[group('data')]
flight-history-fetch +args:
    go run tools/fetch_flight_review.go "$@"

# Test predicted flights directly in audio at a supplied receiver/shared clock.
[group('research')]
flight-audio-review +args:
    go run tools/review_receiver_audio.go "$@"

# Build the local flight/audio report after the Go comparisons complete.
[group('reports')]
flight-history-report directory="work/fr24-review-2026-11-07":
    python3 tools/report_flight_history.py --input "$1"

# Verify historical timestamps, propagation delays and ambiguous identity handling.
[group('checks')]
flight-history-test:
    go test tools/flight_timeline.go tools/flight_timeline_test.go

# Replay the exact historical pitch-selection and short-clip failure fixtures.
[group('research')]
model-diagnosis *args:
    go run tools/diagnose_receiver_limits.go "$@"

# Compare bounded changing source pitch and hypothetical receiver motion.
[group('research')]
curve-model-study +args:
    go run tools/study_curve_models.go "$@"

# Regional matched-filter location experiment, with common folds and dense refinement.
[group('research')]
receiver-region-study +args:
    go run tools/scan_receiver_region.go "$@"

# Retain measured ground motion from cached traces without changing old geometry.
[group('data')]
enrich-track-motion +args:
    go run tools/enrich_track_motion.go "$@"

# Replay the private desk case across a supplied 40 km region; requires cached data.
[group('checks')]
desk-localization-check *args:
    python3 tools/check_desk_localization.py "$@"

# Render the desk maps, full-curve overlays and saved experiment comparisons.
[group('reports')]
desk-improvements-report directory="work/desk-model-improvements-2026-11-08":
    python3 tools/report_desk_improvements.py --input "$1"

# Full-Texas development evaluation; private media and statewide data are required.
[group('research')]
texas-evaluate *args:
    go build -o bin/scan-receiver-region tools/scan_receiver_region.go
    python3 tools/evaluate_texas_samples.py --binary bin/scan-receiver-region "$@"

# Render both timing protocols, candidate maps and the two-mile accuracy table.
[group('reports')]
texas-evaluation-report directory="work/texas-evaluation-2026-11-08":
    python3 tools/report_texas_evaluation.py --input "$1"

# Compare fixed-uncertainty telemetry reconstruction with preserved statewide cases.
[group('reports')]
model-improvements-report directory="work/model-improvements-2026-11-08":
    python3 tools/report_model_improvements.py --input "$1"

# Replay private conditional scoring cases; these are not aircraft/GPS truth labels.
[group('checks')]
receiver-recovery-check *args:
    python3 tools/check_receiver_recovery.py "$@"

# Compare complete retimed flight schedules at a disclosed receiver (no acoustic anchor).
[group('research')]
receiver-schedule-study +args:
    go run tools/audit_receiver_schedules.go "$@"

# Build the local model/FFT/location experiment report from saved results.
[group('reports')]
improvements-report directory="work/overnight-improvements-2026-11-08":
    python3 tools/report_model_improvements.py --input "$1"

# Explain a specific flight/location hypothesis and export residual plots.
[group('core')]
diagnose +args: build
    ./bin/pinpoint diagnose "$@"

# Search unused sound for additional flights at existing candidate locations/clocks.
[group('core')]
corroborate +args: build
    ./bin/pinpoint corroborate "$@"

# Open the demo's location map; use --results for another search directory.
[group('core')]
map *args: build
    ./bin/pinpoint map --results work/demo/results --open "$@"

# Pass any command and arguments to the executable.
[group('core')]
run +args: build
    ./bin/pinpoint "$@"

# Compare experimental Doppler extraction methods and write a local review report.
[group('research')]
experiment *args:
    go run ./cmd/curvebench "$@"

# Open an existing experiment report in the browser (does not rerun analysis).
[group('reports')]
experiment-open report="work/curve-experiments/index.html":
    xdg-open "$1"

# Check frozen known curves; use --suite audio, location, or all for full replays.
[group('checks')]
reference-check *args:
    go run ./cmd/refcheck "$@"

# Restore the eight CC BY public reference passages; verifies existing bytes.
[group('data')]
reference-fetch:
    python3 tools/fetch_reference.py

# Jet positives and background controls; use --suite location for the assisted replay.
[group('checks')]
jet-reference-check *args:
    go run ./cmd/refcheck --manifest references/jets.json --suite audio --out work/jet-reference-check "$@"

# Evaluate additional jet sessions and background controls without changing the detector.
[group('checks')]
jet-test-check *args:
    go run ./cmd/refcheck --manifest references/jets-test.json --suite audio --out work/jet-test-check "$@"

# New Challenger partial-curve regression; --suite location uses its supplied track.
[group('checks')]
jet-sample-check *args:
    go run ./cmd/refcheck --manifest references/jets-challenger.json --suite audio --out work/additional-jets/regression "$@"

# Stronger regression for the measured improvement in jet ridge coverage.
[group('checks')]
jet-tracking-check *args:
    go run ./cmd/refcheck --manifest references/jets-tracking.json --suite audio --out work/jet-improvements/tracking-regression "$@"

# Combined profile with unchanged propeller, jet, and background acceptance gates.
[group('checks')]
hybrid-reference-check *args:
    go run ./cmd/refcheck --manifest references/hybrid.json --suite audio --out work/hybrid-reference-check "$@"

# Stricter jet challenges, currently failing; --suite all also reruns California.
[group('checks')]
jet-challenge-check *args:
    go run ./cmd/refcheck --manifest references/jets-additional.json --suite audio --out work/additional-jets/challenge "$@"

# Restore/check two additional Pathak jet recordings, labels, tracks and PDFs.
[group('data')]
jet-sample-fetch:
    python3 tools/fetch_additional_jets.py

# Inspect the combined statewide Challenger search (already generated locally).
[group('reports')]
jet-sample-map: build
    ./bin/pinpoint map --results work/additional-jets/california-combined --open

# Private desk recording: fresh detection, competing-flight replay, ambiguity checks.
# Requires the frozen local assets in work/desk-regression (not published).
[group('checks')]
desk-reference-check *args:
    go run ./cmd/refcheck --manifest work/desk-regression/suite.json --suite all --out work/desk-regression/check "$@"

# Restore/check jet audio; missing DLR WAVs require h5py and scipy for preparation.
[group('data')]
jet-reference-fetch:
    python3 tools/fetch_jet_reference.py
