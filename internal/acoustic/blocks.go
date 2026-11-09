package acoustic

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type AnalysisProgress struct {
	Profile, Stage string
	Block, Blocks  int
	From, Until    float64
}
type analysisProgressKey struct{}

// WithAnalysisProgress reports bounded-memory block work on the caller's
// goroutine. The callback must not mutate analysis inputs.
func WithAnalysisProgress(ctx context.Context, fn func(AnalysisProgress)) context.Context {
	return context.WithValue(ctx, analysisProgressKey{}, fn)
}

func reportAnalysis(ctx context.Context, p AnalysisProgress) {
	if fn, ok := ctx.Value(analysisProgressKey{}).(func(AnalysisProgress)); ok && fn != nil {
		fn(p)
	}
}

// needsBlocks measures duration only to choose a bounded-memory analysis plan.
// Failed probes fall back to the ordinary decoder, which reports its own error.
func needsBlocks(ctx context.Context, path string, o Options, budget float64) (float64, bool) {
	b, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0, false
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || !finite(d) || d <= 0 {
		return 0, false
	}
	bins := (o.MaxHz-o.MinHz)*float64(o.Window)/float64(o.Rate) + 1
	return d, d*float64(o.Rate)/float64(o.Hop)*bins > budget
}

// Each block owns curves whose midpoint lies in its core. The overlap exceeds
// the detector's 120-second maximum curve extent, so crossings can be measured
// with context on both sides. Times stay relative to the uncut original audio.
func analyzeBlocks(ctx context.Context, path string, o Options, duration float64, short bool, matchingMinHz float64) (*Spectrum, DetectionReport, error) {
	return analyzeBlocksSize(ctx, path, o, duration, short, 480, 120, matchingMinHz)
}

func analyzeBlocksSize(ctx context.Context, path string, o Options, duration float64, short bool, core, overlap, matchingMinHz float64) (*Spectrum, DetectionReport, error) {
	profile := "long curves"
	if short {
		profile = "short sweeps"
	}
	blocks := int(math.Ceil(duration / core))
	reports := make([]DetectionReport, blocks)
	type result struct {
		index       int
		from, until float64
		report      DetectionReport
		err         error
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	workers := max(1, min(4, runtime.GOMAXPROCS(0)/4))
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				start := float64(index) * core
				from, until := math.Max(0, start-overlap), math.Min(duration, start+core+overlap)
				var d DetectionReport
				var err error
				if short {
					_, d, err = analyzeShortSpan(workCtx, path, o, from, until-from)
				} else {
					_, _, d, err = analyzeStandardSpan(workCtx, path, from, until-from)
				}
				if err == nil {
					var owned []Event
					for _, e := range d.Events {
						mid := (e.Start + e.End) / 2
						if mid >= start && mid < start+core {
							owned = append(owned, e)
						}
					}
					d.Events = owned
				}
				results <- result{index, from, until, d, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for k := 0; k < blocks; k++ {
			select {
			case jobs <- k:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	reportAnalysis(ctx, AnalysisProgress{profile, "curves", 1, blocks + 1, 0, math.Min(duration, core+overlap)})
	done := 0
	var failure error
	for v := range results {
		done++
		if v.err != nil && failure == nil {
			failure = fmt.Errorf("audio block %.1f–%.1f s: %w", v.from, v.until, v.err)
			cancel()
		}
		reports[v.index] = v.report
		reportAnalysis(ctx, AnalysisProgress{profile, "curves", done, blocks + 1, v.from, v.until})
	}
	if err := ctx.Err(); err != nil {
		return nil, DetectionReport{}, err
	}
	if failure != nil {
		return nil, DetectionReport{}, failure
	}
	// Localization already samples at about one second. Keep the full band in
	// the scoring cache with fewer overlapping frames; detection above retains
	// its original finer temporal resolution, independently of this cache.
	scoring := o
	scoring.MinHz = matchingMinHz
	scoring.Hop = min(o.Window, o.Rate)
	reportAnalysis(ctx, AnalysisProgress{profile, "scoring spectrum", blocks + 1, blocks + 1, 0, duration})
	s, err := Analyze(ctx, path, scoring)
	if err != nil {
		return nil, DetectionReport{}, err
	}
	d := MergeProfiles(reports...)
	d.Version = DetectorVersion
	if short {
		d.Version = ShortPassVersion
	}
	d.ScannedStart, d.ScannedEnd = 0, duration
	d.SetEvents(d.Events)
	return s, d, ctx.Err()
}
