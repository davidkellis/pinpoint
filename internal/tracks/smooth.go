package tracks

import (
	"fmt"
	"math"

	"pinpoint/internal/geo"
)

type SmoothingOptions struct {
	Step            float64 `json:"step_s"`
	MaxGap          float64 `json:"max_gap_s"`
	MaxAcceleration float64 `json:"max_acceleration_mps2"`
	UseKinematics   bool    `json:"use_reported_kinematics,omitempty"`
}
type SmoothingReport struct {
	Input              int     `json:"input_points"`
	Output             int     `json:"output_points"`
	Smoothed           int     `json:"smoothed_segments"`
	Rejected           int     `json:"segments_kept_linear"`
	Gaps               int     `json:"unbridged_gaps"`
	MaxDeviation       float64 `json:"maximum_sampled_displacement_from_linear_m"`
	Note               string  `json:"note"`
	ReportedHorizontal int     `json:"reported_horizontal_tangents"`
	ReportedVertical   int     `json:"reported_vertical_tangents"`
}

// Smooth densifies a trajectory with time-aware cubic Hermite interpolation.
// Original positions/timestamps are preserved; no receiver or audio enters the
// interpolation. Interior tangents come from adjacent secants, so velocity is
// continuous where both adjoining segments pass the acceleration bound. Long
// telemetry gaps are never filled. Suspect segments remain linear and explicit.
// This cannot recover actual ADS-B velocity fields or remove position errors.
func (t Track) Smooth(o SmoothingOptions) (Track, SmoothingReport, error) {
	r := SmoothingReport{Input: len(t.Points), Note: "Experimental interpolation from position samples only. Recorded nodes are preserved. No gap is filled or audio/receiver used. Acceleration-rejected segments stay linear; they can retain velocity discontinuities. This is not new telemetry or a guarantee of a more accurate flight path."}
	if o.UseKinematics {
		r.Note = "Experimental Hermite interpolation using reported ground speed/true track and geometric climb rate where available; missing components use position-derived tangents. Recorded nodes and gaps are preserved. No audio/receiver enters interpolation. Rejected acceleration segments stay linear; reported velocities can themselves be noisy or stale."
	}
	if !geo.Finite(o.Step) || !geo.Finite(o.MaxGap) || !geo.Finite(o.MaxAcceleration) || o.Step < .05 || o.Step > 2 || o.MaxGap <= 0 || o.MaxGap > 120 || o.MaxAcceleration <= 0 || o.MaxAcceleration > 50 || len(t.Points) < 2 {
		return Track{}, r, fmt.Errorf("invalid smoothing limits")
	}
	for i, p := range t.Points {
		for _, v := range []float64{p.Time, p.Position.X, p.Position.Y, p.Position.Z} {
			if !geo.Finite(v) {
				return Track{}, r, fmt.Errorf("nonfinite telemetry point")
			}
		}
		if i > 0 && p.Time <= t.Points[i-1].Time {
			return Track{}, r, fmt.Errorf("telemetry times are not strictly ordered")
		}
	}
	n := len(t.Points)
	tangents := make([]geo.Vec, n)
	secant := func(i int) geo.Vec {
		return t.Points[i+1].Position.Sub(t.Points[i].Position).Scale(1 / (t.Points[i+1].Time - t.Points[i].Time))
	}
	for i := 0; i < n; i++ {
		left := i > 0 && t.Points[i].Time-t.Points[i-1].Time <= o.MaxGap
		right := i < n-1 && t.Points[i+1].Time-t.Points[i].Time <= o.MaxGap
		switch {
		case left && right:
			a, b := t.Points[i].Time-t.Points[i-1].Time, t.Points[i+1].Time-t.Points[i].Time
			tangents[i] = secant(i - 1).Scale(b / (a + b)).Add(secant(i).Scale(a / (a + b)))
		case left:
			tangents[i] = secant(i - 1)
		case right:
			tangents[i] = secant(i)
		}
	}
	if o.UseKinematics {
		motion := map[float64]KinematicPoint{}
		for _, m := range t.Motion {
			motion[m.Time] = m
		}
		for i, p := range t.Points {
			m, ok := motion[p.Time]
			if !ok {
				continue
			}
			if _, e := readKinematics(kinematicFields(m), m.Time); e != nil {
				return Track{}, r, e
			}
			upVector := geo.ENUVector(p.Lat, p.Lon, 0, 0, 1)
			up := tangents[i].Dot(upVector)
			if m.Horizontal {
				angle := m.TrueTrack * math.Pi / 180
				tangents[i] = geo.ENUVector(p.Lat, p.Lon, m.GroundSpeed*math.Sin(angle), m.GroundSpeed*math.Cos(angle), up)
				r.ReportedHorizontal++
			}
			if m.Vertical {
				tangents[i] = tangents[i].Add(upVector.Scale(m.Up - up))
				r.ReportedVertical++
			}
		}
	}
	out := Track{ID: t.ID, TypeCode: t.TypeCode, Points: []Point{t.Points[0]}, Motion: append([]KinematicPoint(nil), t.Motion...)}
	for i := 0; i < n-1; i++ {
		a, b := t.Points[i], t.Points[i+1]
		h := b.Time - a.Time
		if h > o.MaxGap {
			r.Gaps++
			out.Points = append(out.Points, b)
			continue
		}
		v, m0, m1 := secant(i), tangents[i], tangents[i+1]
		acc0 := v.Scale(6).Sub(m0.Scale(4)).Sub(m1.Scale(2)).Scale(1 / h)
		acc1 := m0.Scale(2).Add(m1.Scale(4)).Sub(v.Scale(6)).Scale(1 / h)
		if math.Max(acc0.Norm(), acc1.Norm()) > o.MaxAcceleration {
			r.Rejected++
			out.Points = append(out.Points, b)
			continue
		}
		pieces := int(math.Ceil(h / o.Step))
		if pieces > 2000000-len(out.Points) {
			return Track{}, r, fmt.Errorf("smoothing exceeds two million points")
		}
		r.Smoothed++
		for k := 1; k < pieces; k++ {
			u := float64(k) / float64(pieces)
			// Use a local displacement to preserve precision of ECEF positions.
			delta := b.Position.Sub(a.Position)
			p := a.Position.Add(delta.Scale(3*u*u - 2*u*u*u)).Add(m0.Scale(h * (u - 2*u*u + u*u*u))).Add(m1.Scale(h * (-u*u + u*u*u)))
			linear := a.Position.Add(delta.Scale(u))
			r.MaxDeviation = math.Max(r.MaxDeviation, p.Sub(linear).Norm())
			lat, lon, alt := geo.Geodetic(p)
			out.Points = append(out.Points, Point{Time: a.Time + u*h, Lat: lat, Lon: lon, Alt: alt, Position: p})
		}
		out.Points = append(out.Points, b)
	}
	r.Output = len(out.Points)
	return out, r, nil
}
