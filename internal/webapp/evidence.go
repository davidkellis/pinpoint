package webapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"pinpoint/internal/acoustic"
	"pinpoint/internal/search"
	"pinpoint/internal/tracks"
	"strconv"
)

func (s *Server) evidenceSpectrum(j *job) (*acoustic.Spectrum, error) {
	path := filepath.Join(j.dir, "spectrogram.bin")
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	s.evidenceMu.Lock()
	defer s.evidenceMu.Unlock()
	if s.evidencePath == path && s.evidenceTime.Equal(stat.ModTime()) {
		return s.evidenceData, nil
	}
	spectrum, err := acoustic.Load(path)
	if err != nil {
		return nil, err
	}
	s.evidencePath, s.evidenceTime, s.evidenceData = path, stat.ModTime(), spectrum
	return spectrum, nil
}

func (s *Server) detectedEvidence(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	n, err := strconv.Atoi(r.PathValue("event"))
	info := j.snapshot()
	if err != nil || n < 1 || n > len(info.Events) {
		problem(w, 404, "Detected sound event not found.")
		return
	}
	spectrum, err := s.evidenceSpectrum(j)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	c := search.DefaultConfig()
	c.Events = []acoustic.Event{info.Events[n-1]}
	evidence, err := search.BuildEvidence(search.Report{}, c, spectrum, nil)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	evidence.Method = "Detected sound · not yet confirmed as an aircraft"
	reply(w, 200, evidence)
}

// Older saved maps did not embed focused evidence. Build it on demand without
// rerunning the location search. New reports already contain evidence.json.
func (s *Server) savedEvidence(w http.ResponseWriter, r *http.Request, j *job, dir string) {
	var c search.Config
	var report search.Report
	for name, dst := range map[string]any{"search.json": &c, "results.json": &report} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			problem(w, 409, "Search evidence is not ready for this event.")
			return
		}
		if err = json.Unmarshal(raw, dst); err != nil {
			problem(w, 500, fmt.Sprintf("Could not read %s: %v", name, err))
			return
		}
	}
	spectrum, err := s.evidenceSpectrum(j)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	f, err := os.Open(c.Tracks)
	if err != nil {
		problem(w, 409, "Saved flight trajectories are unavailable.")
		return
	}
	ts, err := tracks.Read(f)
	f.Close()
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	evidence, err := search.BuildEvidence(report, c, spectrum, search.TracksForEvents(c, ts))
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	reply(w, 200, evidence)
}
