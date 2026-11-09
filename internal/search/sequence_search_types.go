package search

import (
	"math"
	"pinpoint/internal/acoustic"
)

type SequenceOptions struct {
	CacheDir      string    `json:"-"`
	AudioPath     string    `json:"-"`
	Channel       int       `json:"audio_channel"`
	ControlShifts []float64 `json:"control_shifts_s"`
	MaxGroups     int       `json:"max_groups_per_trial"`
	Margin        float64   `json:"required_validation_margin_db"`
	Workers       int       `json:"spectral_workers"`
}

func DefaultSequenceOptions(c Config) SequenceOptions {
	d := math.Max(600, 2*c.ClockUncertainty+300)
	o := SequenceOptions{MaxGroups: 2048, Margin: 1, Workers: 2}
	for _, m := range []float64{1, 1.5, 2, 2.5} {
		o.ControlShifts = append(o.ControlShifts, -d*m, d*m)
	}
	return o
}

type SequenceProgress struct {
	Trial, Trials, Size, Group, Groups int
	Shift                              float64
	Stage                              string
	Done, Total                        int
}
type Rejection struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Count  int    `json:"count,omitempty"`
}
type SequenceCheck struct {
	EventIndex   int         `json:"event_index"`
	Track        string      `json:"track_id"`
	F0           float64     `json:"training_f0_hz"`
	TrainSamples int         `json:"training_samples"`
	TestSamples  int         `json:"validation_samples"`
	Coverage     float64     `json:"validation_coverage"`
	Train        float64     `json:"training_score_db"`
	Test         *float64    `json:"validation_score_db,omitempty"`
	Flat         *float64    `json:"steady_tone_validation_db,omitempty"`
	Linear       *float64    `json:"linear_pitch_validation_db,omitempty"`
	RMSE         *float64    `json:"validation_ridge_rmse_hz,omitempty"`
	Margin       *float64    `json:"validation_margin_db,omitempty"`
	Plan         FoldPlan    `json:"partition"`
	Pass         bool        `json:"pass"`
	Reasons      []Rejection `json:"rejections"`
}
type SequenceGroup struct {
	ID                int             `json:"id"`
	Trial             int             `json:"trial"`
	Shift             float64         `json:"time_shift_s"`
	Indices           []int           `json:"event_indices"`
	Status            string          `json:"status"`
	Best              *Candidate      `json:"training_candidate,omitempty"`
	Locations         int             `json:"locations_evaluated"`
	TrainingAmbiguous bool            `json:"training_ambiguous"`
	Checks            []SequenceCheck `json:"event_checks"`
	Validation        *float64        `json:"weakest_validation_margin_db,omitempty"`
	Pass              bool            `json:"passes_validation"`
	Reasons           []Rejection     `json:"rejections"`
}
type SequenceReport struct {
	Version      string                `json:"version"`
	Status       string                `json:"status"`
	Note         string                `json:"note"`
	Started      string                `json:"started_utc"`
	Finished     string                `json:"finished_utc"`
	Fingerprint  string                `json:"search_inputs_sha256"`
	AudioSHA     string                `json:"audio_sha256,omitempty"`
	ModelInputs  []ModelInput          `json:"elevation_inputs,omitempty"`
	Options      SequenceOptions       `json:"options"`
	Settings     *Config               `json:"search_settings,omitempty"`
	Curves       []acoustic.Event      `json:"curves"`
	Excluded     []CurveExclusion      `json:"excluded_curves,omitempty"`
	Groups       []SequenceGroup       `json:"groups"`
	Selected     *int                  `json:"selected_group_id,omitempty"`
	ControlMax   *float64              `json:"maximum_wrong_time_validation_margin_db,omitempty"`
	Margin       *float64              `json:"separation_db,omitempty"`
	Reasons      []Rejection           `json:"rejections"`
	Error        string                `json:"error,omitempty"`
	Revalidation *SequenceRevalidation `json:"revalidation,omitempty"`
}

// Revalidation preserves training winners and records both implementations.
type SequenceRevalidation struct {
	ReportSHA           string `json:"original_report_sha256"`
	TrainingBinarySHA   string `json:"training_binary_sha256"`
	ValidationBinarySHA string `json:"validation_binary_sha256"`
	Finished            string `json:"finished_utc"`
}

type SequenceCompleted func(SequenceGroup, Config, Report) error
