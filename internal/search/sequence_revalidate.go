package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/tracks"
)

// RevalidateSequences checks saved training winners without searching, changing
// source tones or selecting a different winner using reserved measurements.
// The original executable binds the saved input fingerprint; it is hashed only,
// never executed. Original reports are preserved by the caller.
func RevalidateSequences(ctx context.Context, data []byte, trainingBinary, audio, cache string, s *acoustic.Spectrum, ts []tracks.Track) (SequenceReport, error) {
	var r SequenceReport
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Version != "automatic-sequences-v2" || r.Settings == nil || r.Status == "incomplete" || r.Error != "" || r.Revalidation != nil {
		return r, fmt.Errorf("need an original, completed automatic-sequences-v2 report")
	}
	if err := validateSequenceOptions(*r.Settings, r.Options); err != nil {
		return r, err
	}
	audioSHA, err := sequenceAudioDigest(ctx, audio)
	if err != nil {
		return r, err
	}
	base, err := automaticFingerprintForBinary(ctx, s, ts, trainingBinary)
	if err != nil {
		return r, err
	}
	for _, m := range r.ModelInputs {
		if _, err := os.Stat(m.Path); os.IsNotExist(err) && m.Missing {
			continue
		}
		digest, err := sequenceAudioDigest(ctx, m.Path)
		if err != nil || m.Missing || digest != m.SHA256 {
			return r, fmt.Errorf("elevation input changed: %s", m.Path)
		}
	}
	sum := sha256.Sum256([]byte(bindModelInputs(base, r.ModelInputs) + audioSHA))
	if audioSHA != r.AudioSHA || hex.EncodeToString(sum[:]) != r.Fingerprint {
		return r, fmt.Errorf("original audio, spectrum, trajectories or training executable changed")
	}
	o := r.Options
	o.AudioPath, o.CacheDir = audio, cache
	bank, paths, err := sequenceSpectra(ctx, *r.Settings, o, r.Fingerprint+"-revalidation", nil)
	if err != nil {
		return r, err
	}
	c := *r.Settings
	c.eventSpectra, c.EventSpectra = bank, paths
	selected := r.Selected
	for i := range r.Groups {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		g := &r.Groups[i]
		if g.ID != i || g.Trial < 0 || g.Trial > len(o.ControlShifts) || g.Status == "error" {
			return r, fmt.Errorf("invalid or incomplete training group %d", i)
		}
		if g.Best == nil {
			continue
		}
		if len(g.Indices) < 2 || len(g.Indices) != len(g.Best.Events) {
			return r, fmt.Errorf("training group %d has inconsistent aircraft assignments", i)
		}
		for k, n := range g.Indices {
			if n < 0 || n >= len(c.Events) || k > 0 && n <= g.Indices[k-1] {
				return r, fmt.Errorf("invalid curve index in training group %d", i)
			}
		}
		q := subsetSequenceConfig(c, g.Indices)
		start, err := time.Parse(time.RFC3339Nano, c.RecordingStart)
		if err != nil {
			return r, err
		}
		q.RecordingStart = start.Add(time.Duration(g.Shift * float64(time.Second))).Format(time.RFC3339Nano)
		g.Checks, g.Validation, g.Pass = fixedSequenceChecks(q, s, ts, *g.Best, g.Indices)
		g.Reasons = nil
		g.Status = "validation_pass"
		if !g.Pass {
			g.Status = "validation_failed"
			g.Reasons = []Rejection{{Code: "reserved_audio_failure", Detail: "A frozen training assignment fails reserved-audio validation; no event or parameter was changed."}}
		}
	}
	r.Selected, r.ControlMax, r.Margin, r.Reasons = nil, nil, nil, nil
	finishSequenceSearch(&r, false)
	if !reflect.DeepEqual(selected, r.Selected) {
		return r, fmt.Errorf("revalidation changed training selection")
	}
	trainingSHA, err := sequenceAudioDigest(ctx, trainingBinary)
	if err != nil {
		return r, err
	}
	executable, err := os.Executable()
	if err != nil {
		return r, err
	}
	if _, err := os.Stat("/proc/self/exe"); err == nil {
		executable = "/proc/self/exe"
	}
	validationSHA, err := sequenceAudioDigest(ctx, executable)
	if err != nil {
		return r, err
	}
	reportSHA := sha256.Sum256(data)
	r.Revalidation = &SequenceRevalidation{hex.EncodeToString(reportSHA[:]), trainingSHA, validationSHA, time.Now().UTC().Format(time.RFC3339Nano)}
	r.Settings = &c
	return r, nil
}
