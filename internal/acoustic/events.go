package acoustic

type Event struct {
	Evidence     *CurveEvidence    `json:"acoustic_evidence,omitempty"`
	Resolution   float64           `json:"resolution_window_s,omitempty"`
	Measurement  *RidgeMeasurement `json:"measurement,omitempty"`
	Partial      bool              `json:"partial_flyover,omitempty"`
	Ridge        []RidgeSample     `json:"ridge,omitempty"`
	Start        float64           `json:"start_s"`
	End          float64           `json:"end_s"`
	CPATime      float64           `json:"cpa_s"`
	CPATolerance float64           `json:"cpa_tolerance_s,omitempty"`
	FMin         float64           `json:"f0_min_hz"`
	FMax         float64           `json:"f0_max_hz"`
	FStep        float64           `json:"f0_step_hz"`
	Harmonics    []int             `json:"harmonics"`
	MinSpeed     float64           `json:"min_speed_mps"`
	MaxSpeed     float64           `json:"max_speed_mps"`
	MinSlant     float64           `json:"min_slant_m"`
	MaxSlant     float64           `json:"max_slant_m"`
}
type RidgeSample struct {
	Time float64 `json:"time_s"`
	Hz   float64 `json:"hz"`
}

// Propose is a compatibility alias for the whole-recording detector.
func (s *Spectrum) Propose() []Event { return s.Discover() }
