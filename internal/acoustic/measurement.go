package acoustic

import (
	"fmt"
	"math"
)

// RidgeMeasurement is an ideal-flight screening fit. Its ranges are a padded
// profile of plausible fits, not confidence intervals or an aircraft identity.
type RidgeMeasurement struct {
	ShapePass bool    `json:"shape_pass"`
	FitSpeed  float64 `json:"fit_speed_mps"`
	FitSlant  float64 `json:"fit_slant_m"`
	// Historical JSON name: this marks a bounded complete-pass CPA. Strong
	// partial curves may also constrain joint matching through their timestamps.
	Usable       bool    `json:"usable_for_joint"`
	Reason       string  `json:"reason"`
	RMSE         float64 `json:"doppler_rmse_hz"`
	LinearRMSE   float64 `json:"linear_rmse_hz"`
	ConstantRMSE float64 `json:"constant_rmse_hz"`
	CPA          float64 `json:"cpa_s"`
	CPATolerance float64 `json:"cpa_tolerance_s"`
	F0           float64 `json:"f0_hz"`
	FMin         float64 `json:"f0_min_hz"`
	FMax         float64 `json:"f0_max_hz"`
	MinSpeed     float64 `json:"min_speed_mps"`
	MaxSpeed     float64 `json:"max_speed_mps"`
	MinSlant     float64 `json:"min_slant_m"`
	MaxSlant     float64 `json:"max_slant_m"`
}
type ridgeTrial struct{ speed, distance, cpa, f0, rmse float64 }

func measureFit(s *Spectrum, points []ridgePoint, best ridgeTrial, trials []ridgeTrial) RidgeMeasurement {
	a, b := s.Time(points[0].frame), s.Time(points[len(points)-1].frame)
	duration := b - a
	policy := s.settings()
	n := float64(len(points))
	mt, mf := 0.0, 0.0
	for _, p := range points {
		mt += s.Time(p.frame) - a
		mf += p.hz
	}
	mt /= n
	mf /= n
	xy, xx := 0.0, 0.0
	for _, p := range points {
		dt := s.Time(p.frame) - a - mt
		xy += dt * (p.hz - mf)
		xx += dt * dt
	}
	linear, constant := 0.0, 0.0
	for _, p := range points {
		constant += (p.hz - mf) * (p.hz - mf)
		err := p.hz - mf - xy/xx*(s.Time(p.frame)-a-mt)
		linear += err * err
	}
	m := RidgeMeasurement{FitSpeed: best.speed, FitSlant: best.distance, RMSE: best.rmse, LinearRMSE: math.Sqrt(linear / n), ConstantRMSE: math.Sqrt(constant / n), CPA: best.cpa, F0: best.f0}
	df := float64(s.SampleRate) / float64(s.Window)
	tolerance := math.Max(df*.6, best.rmse*.35)
	lo, hi := best.cpa, best.cpa
	m.MinSpeed, m.MaxSpeed = best.speed, best.speed
	m.MinSlant, m.MaxSlant = best.distance, best.distance
	m.FMin, m.FMax = best.f0, best.f0
	for _, f := range trials {
		if f.rmse > best.rmse+tolerance {
			continue
		}
		lo = math.Min(lo, f.cpa)
		hi = math.Max(hi, f.cpa)
		m.MinSpeed = math.Min(m.MinSpeed, f.speed)
		m.MaxSpeed = math.Max(m.MaxSpeed, f.speed)
		m.MinSlant = math.Min(m.MinSlant, f.distance)
		m.MaxSlant = math.Max(m.MaxSlant, f.distance)
		m.FMin = math.Min(m.FMin, f.f0)
		m.FMax = math.Max(m.FMax, f.f0)
	}
	m.CPATolerance = math.Max(policy.cpaFloor, math.Max(best.cpa-lo, hi-best.cpa)+2*s.Step())
	m.MinSpeed = math.Max(15, m.MinSpeed*.8)
	m.MaxSpeed = math.Min(300, m.MaxSpeed*1.2)
	m.MinSlant = math.Max(0, m.MinSlant*.65-200)
	m.MaxSlant = math.Min(15000, m.MaxSlant*1.35+200)
	m.FMin = math.Max(1, m.FMin-2*df)
	m.FMax += 2 * df
	m.ShapePass = duration >= policy.shape && m.RMSE <= math.Max(1.5, m.F0*.015) && m.RMSE <= m.LinearRMSE*.5 && m.LinearRMSE >= df
	switch {
	case duration < policy.shape:
		m.Reason = fmt.Sprintf("Less than %g seconds of coherent ridge; retained for review.", policy.shape)
	case m.RMSE > math.Max(1.5, m.F0*.015) || m.RMSE > m.LinearRMSE*.5 || m.LinearRMSE < df:
		m.Reason = "A generic pitch trend explains this ridge too well to use as an automatic aircraft constraint."
	case m.CPA-m.CPATolerance < a+duration*.1 || m.CPA+m.CPATolerance > b-duration*.1:
		m.Reason = "Closest approach is not bounded inside the observed curve; retained as a partial flyover."
	case m.CPATolerance > 30:
		m.Reason = "Closest-approach timing is too uncertain to use as a CPA constraint."
	default:
		m.Usable = true
		m.Reason = "Curve shape bounds closest approach; acoustic evidence checks and flight matching are still required."
	}
	return m
}

// JointEvents keeps every usable event across the recording, never merely the
// loudest events or the first seconds. Rejected proposals remain in detection.
// A strong predictive partial curve still has measured timestamps: all its
// samples can share the recording clock without an invented CPA measurement.
// An ideal straight-flight fit establishes acoustic curvature, not hard bounds
// on a real trajectory. Turning/climbing flights can fit that ideal curve well
// while lying outside ALL its parameter ranges. Preserve the proposal's broad
// speed, distance and frequency limits; constrain delayed CPA only to the
// observed complete flyover. Actual telemetry and one shared recording clock
// still determine every scored curve. Reviewed configurations may use tighter
// bounds explicitly, but automatic selection must not invent that certainty.
func JointEvents(events []Event) []Event {
	var out []Event
	for _, e := range events {
		m := e.Measurement
		if m == nil || !m.Usable {
			if m != nil && m.ShapePass && e.Evidence != nil && e.Evidence.Level == "strong" && e.Evidence.Predictive && len(e.Ridge) >= 8 {
				e.Partial = true
				e.CPATime = (e.Start + e.End) / 2 // placeholder, not used as a CPA constraint
				e.CPATolerance = 0
				out = append(out, e)
			}
			continue
		}
		e.Partial = false
		e.CPATime = (e.Start + e.End) / 2
		e.CPATolerance = (e.End - e.Start) / 2
		out = append(out, e)
	}
	return out
}
