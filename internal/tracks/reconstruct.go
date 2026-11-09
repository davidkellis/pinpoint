package tracks

import (
	"fmt"
	"math"

	"pinpoint/internal/geo"
)

// ReconstructionOptions describe measurement/process uncertainty, not GPS
// confidence. They must be fixed before comparing acoustic receiver candidates.
type ReconstructionOptions struct {
	MaxGap             float64 `json:"max_gap_s"`
	HorizontalPosition float64 `json:"horizontal_position_sigma_m"`
	VerticalPosition   float64 `json:"vertical_position_sigma_m"`
	HorizontalVelocity float64 `json:"horizontal_velocity_sigma_mps"`
	VerticalVelocity   float64 `json:"vertical_velocity_sigma_mps"`
	Acceleration       float64 `json:"white_acceleration_density_mps_per_sqrt_s"`
	MaxDisplacement    float64 `json:"maximum_node_displacement_m"`
}

func DefaultReconstructionOptions() ReconstructionOptions {
	return ReconstructionOptions{MaxGap: 30, HorizontalPosition: 15, VerticalPosition: 20, HorizontalVelocity: 1.5, VerticalVelocity: 1.5, Acceleration: .8, MaxDisplacement: 120}
}

type ReconstructionReport struct {
	Track      string                `json:"track_id"`
	Options    ReconstructionOptions `json:"options"`
	Nodes      int                   `json:"nodes"`
	Chunks     int                   `json:"chunks"`
	Rejected   int                   `json:"chunks_kept_original"`
	Horizontal int                   `json:"reported_horizontal_measurements"`
	Vertical   int                   `json:"reported_vertical_measurements"`
	MaxShift   float64               `json:"maximum_node_displacement_m"`
	RMSShift   float64               `json:"rms_node_displacement_m"`
	Note       string                `json:"note"`
}

type trajectoryState [6]float64
type trajectoryMatrix [6][6]float64

func trajectoryTransition(dt float64) trajectoryMatrix {
	var a trajectoryMatrix
	for i := range a {
		a[i][i] = 1
		if i < 3 {
			a[i][i+3] = dt
		}
	}
	return a
}

func trajectoryProduct(a, b trajectoryMatrix) (r trajectoryMatrix) {
	for i := range r {
		for k := range r {
			for j := range r {
				r[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return
}

func trajectoryTranspose(a trajectoryMatrix) (r trajectoryMatrix) {
	for i := range r {
		for j := range r {
			r[i][j] = a[j][i]
		}
	}
	return
}

// Solve one SPD system by Cholesky; no explicit inverse is formed.
func trajectorySolve(a trajectoryMatrix, b trajectoryState) (x trajectoryState, ok bool) {
	var l trajectoryMatrix
	for i := range l {
		for j := 0; j <= i; j++ {
			v := a[i][j]
			for k := 0; k < j; k++ {
				v -= l[i][k] * l[j][k]
			}
			if i == j {
				if !geo.Finite(v) || v <= 0 {
					return x, false
				}
				l[i][j] = math.Sqrt(v)
			} else {
				l[i][j] = v / l[j][j]
			}
		}
	}
	var y trajectoryState
	for i := range y {
		v := b[i]
		for k := 0; k < i; k++ {
			v -= l[i][k] * y[k]
		}
		y[i] = v / l[i][i]
	}
	for i := 5; i >= 0; i-- {
		v := y[i]
		for k := i + 1; k < 6; k++ {
			v -= l[k][i] * x[k]
		}
		x[i] = v / l[i][i]
	}
	return x, true
}

func trajectoryMeasurement(x *trajectoryState, p *trajectoryMatrix, direction geo.Vec, velocity bool, value, variance float64) {
	var h, u trajectoryState
	k := 0
	if velocity {
		k = 3
	}
	h[k], h[k+1], h[k+2] = direction.X, direction.Y, direction.Z
	predicted, s := 0., variance
	for i := range h {
		predicted += h[i] * x[i]
		for j := range h {
			u[i] += p[i][j] * h[j]
		}
		s += h[i] * u[i]
	}
	for i := range h {
		x[i] += u[i] / s * (value - predicted)
		for j := range h {
			p[i][j] -= u[i] * u[j] / s
		}
	}
}

// Reconstruct estimates a coherent position/velocity trajectory using a linear
// Kalman filter and Rauch–Tung–Striebel smoother in relative ECEF coordinates.
// Unlike Smooth, recorded nodes may move within disclosed measurement errors.
// Neither audio nor receiver position enters this operation. Gaps split the
// filter/smoother; they are never filled. Original measurements remain unchanged.
// This is an opt-in state estimate, not additional measured telemetry.
func (t Track) Reconstruct(o ReconstructionOptions) (Track, ReconstructionReport, error) {
	r := ReconstructionReport{Track: t.ID, Options: o, Nodes: len(t.Points), Note: "Experimental position/velocity Kalman + RTS state estimate. Measurement errors and acceleration density are fixed inputs, not calibrated GPS confidence. No audio, receiver, aircraft label or source pitch is used. Raw inputs are unchanged; gaps split independent chunks. Chunks exceeding the node displacement limit retain original geometry and motion."}
	for _, v := range []float64{o.MaxGap, o.HorizontalPosition, o.VerticalPosition, o.HorizontalVelocity, o.VerticalVelocity, o.Acceleration, o.MaxDisplacement} {
		if !geo.Finite(v) || v <= 0 {
			return Track{}, r, fmt.Errorf("invalid trajectory reconstruction uncertainty")
		}
	}
	if o.MaxGap > 120 || o.HorizontalPosition > 100 || o.VerticalPosition > 100 || o.HorizontalVelocity > 30 || o.VerticalVelocity > 30 || o.Acceleration > 10 || o.MaxDisplacement > 1000 || len(t.Points) < 2 {
		return Track{}, r, fmt.Errorf("invalid trajectory reconstruction limits")
	}
	motion := map[float64]KinematicPoint{}
	for _, m := range t.Motion {
		if _, err := readKinematics(kinematicFields(m), m.Time); err != nil {
			return Track{}, r, err
		}
		motion[m.Time] = m
	}
	for i, p := range t.Points {
		for _, v := range []float64{p.Time, p.Lat, p.Lon, p.Position.X, p.Position.Y, p.Position.Z} {
			if !geo.Finite(v) {
				return Track{}, r, fmt.Errorf("nonfinite reconstruction telemetry")
			}
		}
		if math.Abs(p.Lat) > 90 || math.Abs(p.Lon) > 180 || i > 0 && p.Time <= t.Points[i-1].Time {
			return Track{}, r, fmt.Errorf("invalid reconstruction coordinates/times")
		}
	}
	out := Track{ID: t.ID, TypeCode: t.TypeCode, Points: append([]Point(nil), t.Points...)}
	var squared float64
	for first := 0; first < len(t.Points); {
		last := first + 1
		for last < len(t.Points) && t.Points[last].Time-t.Points[last-1].Time <= o.MaxGap {
			last++
		}
		r.Chunks++
		nodes := t.Points[first:last]
		estimates, ok := reconstructChunk(nodes, motion, o, &r)
		if !ok {
			r.Rejected++
		}
		origin := nodes[0].Position
		for j, p := range nodes {
			if !ok {
				if m, exists := motion[p.Time]; exists {
					out.Motion = append(out.Motion, m)
				}
				continue
			}
			x := estimates[j]
			position := origin.Add(geo.Vec{X: x[0], Y: x[1], Z: x[2]})
			lat, lon, alt := geo.Geodetic(position)
			out.Points[first+j] = Point{Time: p.Time, Lat: lat, Lon: lon, Alt: alt, Position: position}
			shift := position.Sub(p.Position).Norm()
			r.MaxShift = math.Max(r.MaxShift, shift)
			squared += shift * shift
			v := geo.Vec{X: x[3], Y: x[4], Z: x[5]}
			east, north, up := v.Dot(geo.ENUVector(lat, lon, 1, 0, 0)), v.Dot(geo.ENUVector(lat, lon, 0, 1, 0)), v.Dot(geo.ENUVector(lat, lon, 0, 0, 1))
			// Motion CSV validation uses eight decimal places. Normalize AFTER
			// that rounding so a tiny negative bearing cannot become 360.00000000.
			direction := math.Mod(math.Round(math.Atan2(east, north)*180/math.Pi*1e8)/1e8+360, 360)
			out.Motion = append(out.Motion, KinematicPoint{Time: p.Time, GroundSpeed: math.Hypot(east, north), TrueTrack: direction, Up: up, Horizontal: true, Vertical: true})
		}
		first = last
	}
	r.RMSShift = math.Sqrt(squared / float64(len(t.Points)))
	return out, r, nil
}

func reconstructChunk(nodes []Point, motion map[float64]KinematicPoint, o ReconstructionOptions, report *ReconstructionReport) ([]trajectoryState, bool) {
	if len(nodes) < 2 {
		return nil, false
	}
	n := len(nodes)
	filtered, predicted := make([]trajectoryState, n), make([]trajectoryState, n)
	filteredP, predictedP := make([]trajectoryMatrix, n), make([]trajectoryMatrix, n)
	origin := nodes[0].Position
	v := nodes[1].Position.Sub(origin).Scale(1 / (nodes[1].Time - nodes[0].Time))
	x := trajectoryState{0, 0, 0, v.X, v.Y, v.Z}
	var p trajectoryMatrix
	for i := range p {
		p[i][i] = 10000 // deliberately broad initial position/velocity prior
	}
	for i, node := range nodes {
		if i > 0 {
			dt := node.Time - nodes[i-1].Time
			a := trajectoryTransition(dt)
			for k := 0; k < 3; k++ {
				x[k] += dt * x[k+3]
			}
			p = trajectoryProduct(trajectoryProduct(a, p), trajectoryTranspose(a))
			q := o.Acceleration * o.Acceleration
			for k := 0; k < 3; k++ {
				p[k][k] += q * dt * dt * dt / 3
				p[k][k+3] += q * dt * dt / 2
				p[k+3][k] += q * dt * dt / 2
				p[k+3][k+3] += q * dt
			}
		}
		predicted[i], predictedP[i] = x, p
		axes := [3]geo.Vec{geo.ENUVector(node.Lat, node.Lon, 1, 0, 0), geo.ENUVector(node.Lat, node.Lon, 0, 1, 0), geo.ENUVector(node.Lat, node.Lon, 0, 0, 1)}
		d := node.Position.Sub(origin)
		for j, axis := range axes {
			sigma := o.HorizontalPosition
			if j == 2 {
				sigma = o.VerticalPosition
			}
			trajectoryMeasurement(&x, &p, axis, false, d.Dot(axis), sigma*sigma)
		}
		if m, exists := motion[node.Time]; exists {
			if m.Horizontal {
				report.Horizontal++
				theta := m.TrueTrack * math.Pi / 180
				trajectoryMeasurement(&x, &p, axes[0], true, m.GroundSpeed*math.Sin(theta), o.HorizontalVelocity*o.HorizontalVelocity)
				trajectoryMeasurement(&x, &p, axes[1], true, m.GroundSpeed*math.Cos(theta), o.HorizontalVelocity*o.HorizontalVelocity)
			}
			if m.Vertical {
				report.Vertical++
				trajectoryMeasurement(&x, &p, axes[2], true, m.Up, o.VerticalVelocity*o.VerticalVelocity)
			}
		}
		filtered[i], filteredP[i] = x, p
	}
	smoothed := append([]trajectoryState(nil), filtered...)
	for i := n - 2; i >= 0; i-- {
		a := trajectoryTransition(nodes[i+1].Time - nodes[i].Time)
		b := trajectoryProduct(filteredP[i], trajectoryTranspose(a))
		for j := 0; j < 6; j++ {
			gain, ok := trajectorySolve(predictedP[i+1], b[j])
			if !ok {
				return nil, false
			}
			for k := 0; k < 6; k++ {
				smoothed[i][j] += gain[k] * (smoothed[i+1][k] - predicted[i+1][k])
			}
		}
	}
	for i, x := range smoothed {
		for _, v := range x {
			if !geo.Finite(v) {
				return nil, false
			}
		}
		if origin.Add(geo.Vec{X: x[0], Y: x[1], Z: x[2]}).Sub(nodes[i].Position).Norm() > o.MaxDisplacement {
			return nil, false
		}
		v := geo.Vec{X: x[3], Y: x[4], Z: x[5]}
		up := v.Dot(geo.ENUVector(nodes[i].Lat, nodes[i].Lon, 0, 0, 1))
		if math.Sqrt(math.Max(0, v.Dot(v)-up*up)) > 800*1852./3600 || math.Abs(up) > 15000*.3048/60 {
			return nil, false
		}
	}
	return smoothed, true
}
