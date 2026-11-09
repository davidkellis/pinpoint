package webapp

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pinpoint/internal/acoustic"
)

func TestCombinedUploadPreservesProfileAndScoringBand(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	s, server := testServer(t)
	audio, _ := recordingFixture(t)
	var info JobInfo
	decodeResponse(t, multipartUpload(t, server.URL+"/api/jobs", "audio", "combined.wav", audio, map[string]string{"detector_profile": "hybrid"}), 202, &info)
	info = waitJob(t, server.URL+"/api/jobs/"+info.ID, "ready")
	if info.DetectorProfile != "hybrid" || info.Detection == nil || info.Detection.Version != acoustic.HybridVersion {
		t.Fatal("profile was lost or silently analyzed as standard")
	}
	s.mu.Lock()
	j := s.jobs[info.ID]
	s.mu.Unlock()
	sp, err := acoustic.Load(filepath.Join(j.dir, "spectrogram.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Hz(0) > 40 || sp.Hz(len(sp.Rows[0])-1) < 6500 || float64(sp.Window)/float64(sp.SampleRate) > 1 {
		t.Fatal("scoring cache cannot represent the chosen detector's observations")
	}
}

func TestInvalidReanalysisProfileDoesNotDiscardResults(t *testing.T) {
	s, server := testServer(t)
	j, err := s.newJob("cached.wav")
	if err != nil {
		t.Fatal(err)
	}
	j.update(func(i *JobInfo) { i.State = "complete"; i.ResultURL = "keep-this-result" })
	r, err := http.Post(server.URL+"/api/jobs/"+j.snapshot().ID+"/reanalyze", "application/json", strings.NewReader(`{"detector_profile":"typo"}`))
	if err != nil {
		t.Fatal(err)
	}
	decodeResponse(t, r, 400, nil)
	if j.snapshot().ResultURL != "keep-this-result" || j.snapshot().State != "complete" {
		t.Fatal("invalid request changed the job")
	}
}
