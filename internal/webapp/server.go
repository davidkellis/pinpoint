// Package webapp serves Pinpoint's local upload, analysis and map workflow.
package webapp

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"pinpoint/internal/acoustic"
	"pinpoint/internal/geo"
	"pinpoint/internal/mapview"
	"pinpoint/internal/tracks"
)

//go:embed assets/*
var assets embed.FS

type Options struct {
	DataDir, TracksFile, TraceDir    string
	MaxUploadBytes, MaxDownloadBytes int64
	Workers, EventWorkers            int
}
type JobInfo struct {
	FragmentCount   int                       `json:"total_fragments,omitempty"`
	EventIndices    []int                     `json:"event_indices,omitempty"`
	EventsFiltered  bool                      `json:"events_filtered,omitempty"`
	DetectorProfile string                    `json:"detector_profile,omitempty"`
	Detection       *acoustic.DetectionReport `json:"detection,omitempty"`
	Sounds          *acoustic.SoundInventory  `json:"sound_inventory,omitempty"`
	EventMaps       []EventMap                `json:"event_maps,omitempty"`
	MissingTraces   int                       `json:"missing_traces,omitempty"`
	ID              string                    `json:"id"`
	Filename        string                    `json:"filename"`
	State           string                    `json:"state"`
	Phase           string                    `json:"phase"`
	Message         string                    `json:"message"`
	Error           string                    `json:"error,omitempty"`
	Events          []acoustic.Event          `json:"events"`
	FirstTime       float64                   `json:"first_time_s"`
	LastTime        float64                   `json:"last_time_s"`
	HasTracks       bool                      `json:"has_tracks"`
	Synthetic       bool                      `json:"synthetic"`
	Completed       int64                     `json:"completed"`
	Total           int64                     `json:"total"`
	Params          *SearchRequest            `json:"params,omitempty"`
	ResultURL       string                    `json:"result_url,omitempty"`
	ResultStatus    string                    `json:"result_status,omitempty"`
}
type job struct {
	mu     sync.Mutex
	info   JobInfo
	dir    string
	cancel context.CancelFunc
}

func (j *job) snapshot() JobInfo { j.mu.Lock(); defer j.mu.Unlock(); return j.info }
func (j *job) update(change func(*JobInfo)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	change(&j.info)
	j.persistLocked()
}
func (j *job) persistLocked() {
	// Detailed measurements already live in events.json/detection.json. Progress
	// must not rewrite hundreds of megabytes of ridges on every status change.
	info := compactMetadata(j.info)
	info.FragmentCount = len(info.Events)
	info.Events = nil
	info.EventIndices = nil
	b, err := json.MarshalIndent(info, "", "  ")
	if err == nil {
		tmp := filepath.Join(j.dir, ".job.json")
		err = os.WriteFile(tmp, b, 0600)
		if err == nil {
			err = os.Rename(tmp, filepath.Join(j.dir, "job.json"))
		}
	}
	if err != nil {
		log.Printf("saving job %s: %v", j.info.ID, err)
	}
}
func (j *job) phase(name, message string) {
	j.update(func(i *JobInfo) { i.Phase = name; i.Message = message; i.Completed = 0; i.Total = 0 })
}

type Server struct {
	evidenceMu   sync.Mutex
	evidencePath string
	evidenceTime time.Time
	evidenceData *acoustic.Spectrum
	options      Options
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	jobs         map[string]*job
	work         chan struct{}
	terrainWork  chan struct{}
	wg           sync.WaitGroup
	handler      http.Handler
}

func New(ctx context.Context, o Options) (*Server, error) {
	if o.Workers == 0 {
		o.Workers = min(32, runtime.GOMAXPROCS(0))
	}
	if o.EventWorkers == 0 {
		o.EventWorkers = 2
	}
	if o.Workers < 1 || o.Workers > 256 || o.EventWorkers < 1 || o.EventWorkers > 4 {
		return nil, fmt.Errorf("workers must be 1–256 and event-workers must be 1–4")
	}
	if o.DataDir == "" {
		o.DataDir = "work/web"
	}
	if o.MaxUploadBytes == 0 {
		o.MaxUploadBytes = 512 << 20
	}
	if o.MaxDownloadBytes == 0 {
		o.MaxDownloadBytes = 8_000_000_000
	}
	if o.MaxUploadBytes < 1 || o.MaxDownloadBytes < 1 {
		return nil, fmt.Errorf("upload and download limits must be positive")
	}
	root, err := filepath.Abs(o.DataDir)
	if err != nil {
		return nil, err
	}
	o.DataDir = root
	if err = os.MkdirAll(filepath.Join(root, "jobs"), 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{options: o, ctx: ctx, cancel: cancel, jobs: map[string]*job{}, work: make(chan struct{}, 1), terrainWork: make(chan struct{}, 1)}
	entries, err := os.ReadDir(filepath.Join(root, "jobs"))
	if err != nil {
		cancel()
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, "jobs", e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "job.json"))
		if err != nil {
			continue
		}
		var info JobInfo
		if json.Unmarshal(b, &info) != nil || info.ID != e.Name() {
			continue
		}
		if len(info.Events) == 0 && info.FragmentCount > 0 {
			data, err := os.ReadFile(filepath.Join(dir, "events.json"))
			if err == nil {
				err = json.Unmarshal(data, &info.Events)
			}
			if err != nil {
				info.State = "error"
				info.Error = "Could not restore the recording's detailed curves. Reanalyze the recording."
				info.Message = info.Error
			}
		}
		j := &job{info: info, dir: dir}
		s.jobs[info.ID] = j
		if busy(info.State) {
			j.update(func(i *JobInfo) {
				i.State = "error"
				i.Phase = "interrupted"
				i.Error = "The server stopped during this run. Retry the search or upload the recording again."
				i.Message = i.Error
			})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("POST /api/jobs", s.upload)
	mux.HandleFunc("POST /api/demo", s.demo)
	mux.HandleFunc("GET /api/jobs/{id}", s.status)
	mux.HandleFunc("GET /api/jobs/{id}/fragments", s.fragments)
	mux.HandleFunc("GET /api/jobs/{id}/detected/{event}", s.detectedEvidence)
	mux.HandleFunc("POST /api/jobs/{id}/tracks", s.uploadTracks)
	mux.HandleFunc("POST /api/jobs/{id}/search", s.search)
	mux.HandleFunc("POST /api/jobs/{id}/reanalyze", s.reanalyze)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/jobs/{id}/files/{name}", s.file)
	mux.HandleFunc("GET /api/jobs/{id}/events/{event}/{name}", s.file)
	mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "leaflet.js" || name == "leaflet.css" || name == "map-context.js" || name == "state-borders.json" || name == "evidence.js" || name == "evidence.css" {
			b, err := mapview.LibraryAsset(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(name, ".js") {
				w.Header().Set("Content-Type", "text/javascript")
			} else if strings.HasSuffix(name, ".css") {
				w.Header().Set("Content-Type", "text/css")
			} else {
				w.Header().Set("Content-Type", "application/geo+json")
				w.Header().Set("Cache-Control", "public, max-age=86400")
			}
			w.Write(b)
			return
		}
		sub, _ := fs.Sub(assets, "assets")
		http.StripPrefix("/assets/", http.FileServer(http.FS(sub))).ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("assets/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	s.handler = s.guard(mux)
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) Close()                                           { s.cancel(); s.wg.Wait() }
func busy(state string) bool {
	return state == "queued" || state == "analyzing" || state == "searching"
}
func reply(w http.ResponseWriter, status int, value any) {
	if info, ok := value.(JobInfo); ok {
		value = clientInfo(info)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
func localHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	return h == "localhost" || (net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback())
}
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !localHost(r.Host) {
			problem(w, 403, "This application is served on localhost.")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				problem(w, 403, "Cross-origin requests are not allowed.")
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			problem(w, 403, "Cross-site requests are not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	reply(w, 200, map[string]any{"states": geo.States(), "max_upload_bytes": s.options.MaxUploadBytes, "max_download_bytes": s.options.MaxDownloadBytes, "local_tracks_available": s.options.TracksFile != "" || s.options.TraceDir != ""})
}
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) *job {
	s.mu.Lock()
	j := s.jobs[r.PathValue("id")]
	s.mu.Unlock()
	if j == nil {
		problem(w, 404, "Recording not found. Upload the file to begin a new analysis.")
	}
	return j
}
func (s *Server) newJob(filename string) (*job, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b)
	dir := filepath.Join(s.options.DataDir, "jobs", id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	j := &job{dir: dir, info: JobInfo{ID: id, Filename: filename, State: "queued", Phase: "queued", Message: "Waiting to analyze audio", Events: []acoustic.Event{}}}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
	return j, nil
}

func (s *Server) start(j *job, state string, fn func(context.Context) error) bool {
	j.mu.Lock()
	if j.cancel != nil {
		j.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(s.ctx)
	j.cancel = cancel
	j.mu.Unlock()
	j.update(func(i *JobInfo) {
		i.State = "queued"
		i.Phase = "queued"
		i.Message = "Waiting for the analysis worker"
		i.Error = ""
		i.ResultURL = ""
		i.ResultStatus = ""
		i.EventMaps = nil
		i.MissingTraces = 0
	})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { j.mu.Lock(); j.cancel = nil; j.mu.Unlock() }()
		var err error
		select {
		case s.work <- struct{}{}:
			defer func() { <-s.work }()
			j.update(func(i *JobInfo) { i.State = state })
			err = fn(ctx)
		case <-ctx.Done():
			err = ctx.Err()
		}
		if err != nil {
			j.update(func(i *JobInfo) {
				i.State = "error"
				i.Phase = "error"
				i.Error = err.Error()
				i.Message = i.Error
				var unavailable *tracks.HistoryUnavailable
				if errors.As(err, &unavailable) {
					i.State = "waiting_data"
					i.Phase = "waiting_data"
					i.Error = ""
				}
				if ctx.Err() != nil {
					i.State = "cancelled"
					i.Phase = "cancelled"
					i.Error = ""
					i.Message = "Run cancelled. You can adjust the inputs and search again."
				}
			})
		}
	}()
	return true
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.options.MaxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		problem(w, 400, "Could not read upload. Check the file and upload size limit.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	profile, err := detectorProfile(r.FormValue("detector_profile"))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	f, h, err := r.FormFile("audio")
	if err != nil {
		problem(w, 400, "Choose an audio or video file.")
		return
	}
	defer f.Close()
	if h.Size == 0 {
		problem(w, 400, "The recording is empty.")
		return
	}
	name := filepath.Base(strings.ReplaceAll(h.Filename, "\\", "/"))
	j, err := s.newJob(name)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	j.update(func(i *JobInfo) { i.DetectorProfile = profile })
	path := filepath.Join(j.dir, "audio")
	out, err := os.Create(path)
	if err == nil {
		_, err = io.Copy(out, f)
		ce := out.Close()
		if err == nil {
			err = ce
		}
	}
	if err != nil {
		j.update(func(i *JobInfo) { i.State = "error"; i.Error = err.Error() })
		problem(w, 500, err.Error())
		return
	}
	s.start(j, "analyzing", func(ctx context.Context) error { return s.analyze(ctx, j, nil) })
	reply(w, http.StatusAccepted, j.snapshot())
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if j := s.getJob(w, r); j != nil {
		reply(w, 200, j.snapshot())
	}
}
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if j := s.getJob(w, r); j != nil {
		j.mu.Lock()
		if j.cancel != nil {
			j.cancel()
		}
		j.mu.Unlock()
		reply(w, 200, j.snapshot())
	}
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	name := r.PathValue("name")
	allowed := map[string]bool{"validation.json": true, "automatic.html": true, "automatic.json": true, "spectrogram.svg": true, "spectrogram.png": true, "events.json": true, "map.html": true, "results.json": true, "score-grid.geojson": true, "candidates.geojson": true, "best-overlay.svg": true, "best-curves.csv": true, "flight-data.json": true, "detection.json": true, "flight-window.json": true, "evidence.json": true, "consensus.html": true, "consensus.json": true}
	if !allowed[name] {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	r.Header.Del("If-Modified-Since")
	dir := j.dir
	if event := r.PathValue("event"); event != "" {
		n, err := strconv.Atoi(event)
		if err != nil || n < 1 || n > 1000000 {
			http.NotFound(w, r)
			return
		}
		dir = filepath.Join(dir, fmt.Sprintf("event-%02d", n))
	}
	if name == "evidence.json" {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			s.savedEvidence(w, r, j, dir)
			return
		}
	}
	if name == "map.html" {
		page, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err == nil {
			page, err = mapview.Refresh(page)
		}
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
		return
	}
	http.ServeFile(w, r, filepath.Join(dir, name))
}
func (s *Server) uploadTracks(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		problem(w, 409, "Wait for analysis to finish before adding flight data.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		problem(w, 400, "Could not read the flight-data upload (maximum 128 MiB).")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("tracks")
	if err != nil {
		problem(w, 400, "Choose a normalized flight-track CSV.")
		return
	}
	defer f.Close()
	ts, err := tracks.Read(f)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	out, err := os.Create(filepath.Join(j.dir, "tracks.csv"))
	if err == nil {
		err = tracks.Write(out, ts)
		ce := out.Close()
		if err == nil {
			err = ce
		}
	}
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	j.info.HasTracks = true
	j.persistLocked()
	reply(w, 200, map[string]any{"tracks": len(ts), "has_tracks": true})
}

// Listen runs a local-only service and gracefully cancels worker jobs on exit.
func Listen(ctx context.Context, addr string, o Options) error {
	if !localHost(addr) {
		return fmt.Errorf("--addr must be a loopback address such as 127.0.0.1:8080")
	}
	s, err := New(ctx, o)
	if err != nil {
		return err
	}
	defer s.Close()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			httpServer.Shutdown(shutdown)
		case <-done:
		}
	}()
	fmt.Printf("Pinpoint is ready at http://%s\nDrop a recording into the page. Press Ctrl+C to stop.\n", listener.Addr())
	err = httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
