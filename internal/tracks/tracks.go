package tracks

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"pinpoint/internal/geo"
)

type Point struct {
	Time, Lat, Lon, Alt float64
	Position            geo.Vec
}
type Track struct {
	receiver *receiverCache
	segments *trackSegments
	ID       string
	Points   []Point
	TypeCode string
	Motion   []KinematicPoint
}

type receiverCache struct {
	observer geo.Vec
	sound    float64
	arrivals []float64
}

// ForObserver returns a private immutable arrival-time index for repeated
// queries at one candidate position. The original trajectory is unchanged.
func (t Track) ForObserver(observer geo.Vec, sound float64) Track {
	if t.receiver != nil && t.receiver.observer == observer && t.receiver.sound == sound {
		return t
	}
	r := &receiverCache{observer: observer, sound: sound, arrivals: make([]float64, len(t.Points))}
	for i, p := range t.Points {
		r.arrivals[i] = p.Time + p.Position.Sub(observer).Norm()/sound
	}
	t.receiver = r
	return t
}

func (t Track) ArrivalSpan(observer geo.Vec, sound float64) (float64, float64, bool) {
	if len(t.Points) < 2 {
		return 0, 0, false
	}
	if t.receiver != nil && t.receiver.observer == observer && t.receiver.sound == sound {
		return t.receiver.arrivals[0], t.receiver.arrivals[len(t.receiver.arrivals)-1], true
	}
	a, b := t.Points[0], t.Points[len(t.Points)-1]
	return a.Time + a.Position.Sub(observer).Norm()/sound, b.Time + b.Position.Sub(observer).Norm()/sound, true
}

var Header = []string{"track_id", "time_unix_s", "lat", "lon", "alt_ellipsoid_m"}
var MetadataHeader = append(append([]string(nil), Header...), "type_code")

func Read(r io.Reader) ([]Track, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	kinematics := strings.Join(header, ",") == strings.Join(KinematicHeader, ",")
	metadata := kinematics || strings.Join(header, ",") == strings.Join(MetadataHeader, ",")
	if !metadata && strings.Join(header, ",") != strings.Join(Header, ",") {
		return nil, fmt.Errorf("expected CSV header %s", strings.Join(Header, ","))
	}
	groups := map[string][]Point{}
	types := map[string]string{}
	motion := map[string]map[float64]KinematicPoint{}
	for line := 2; ; line++ {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) != len(header) || row[0] == "" {
			return nil, fmt.Errorf("invalid track at line %d", line)
		}
		if metadata {
			typeCode := strings.ToUpper(strings.TrimSpace(row[5]))
			if types[row[0]] != "" && typeCode != "" && types[row[0]] != typeCode {
				return nil, fmt.Errorf("conflicting aircraft types for %s", row[0])
			}
			if typeCode != "" {
				types[row[0]] = typeCode
			}
		}
		var v [4]float64
		for i := range v {
			v[i], err = strconv.ParseFloat(row[i+1], 64)
			if err != nil || !geo.Finite(v[i]) {
				return nil, fmt.Errorf("invalid number at line %d", line)
			}
		}
		if math.Abs(v[1]) > 90 || math.Abs(v[2]) > 180 || v[3] < -1000 || v[3] > 30000 {
			return nil, fmt.Errorf("invalid position at line %d", line)
		}
		p := Point{v[0], v[1], v[2], v[3], geo.ECEF(v[1], v[2], v[3])}
		groups[row[0]] = append(groups[row[0]], p)
		if kinematics {
			m, e := readKinematics(row[6:9], v[0])
			if e != nil {
				return nil, fmt.Errorf("line %d: %w", line, e)
			}
			if m.Horizontal || m.Vertical {
				if motion[row[0]] == nil {
					motion[row[0]] = map[float64]KinematicPoint{}
				}
				motion[row[0]][v[0]] = m
			}
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Track
	for _, id := range ids {
		p := groups[id]
		sort.Slice(p, func(i, j int) bool { return p[i].Time < p[j].Time })
		clean := p[:0]
		for _, v := range p {
			if len(clean) > 0 && v.Time == clean[len(clean)-1].Time {
				if v.Position.Sub(clean[len(clean)-1].Position).Norm() > 1 {
					return nil, fmt.Errorf("conflicting positions for %s at %f", id, v.Time)
				}
				continue
			}
			clean = append(clean, v)
		}
		if len(clean) >= 2 {
			t := Track{ID: id, Points: clean, TypeCode: types[id]}
			for _, p := range clean {
				if m, ok := motion[id][p.Time]; ok {
					t.Motion = append(t.Motion, m)
				}
			}
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tracks with at least two positions")
	}
	return out, nil
}

func Write(w io.Writer, ts []Track) error {
	cw := csv.NewWriter(w)
	metadata := false
	kinematics := false
	for _, t := range ts {
		if t.TypeCode != "" {
			metadata = true
		}
		if len(t.Motion) > 0 {
			kinematics = true
			metadata = true
		}
	}
	header := Header
	if metadata {
		header = MetadataHeader
	}
	if kinematics {
		header = KinematicHeader
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, t := range ts {
		motion := map[float64]KinematicPoint{}
		for _, m := range t.Motion {
			motion[m.Time] = m
		}
		for _, p := range t.Points {
			row := []string{t.ID}
			for _, v := range []float64{p.Time, p.Lat, p.Lon, p.Alt} {
				row = append(row, strconv.FormatFloat(v, 'f', 8, 64))
			}
			if metadata {
				row = append(row, t.TypeCode)
			}
			if kinematics {
				row = append(row, kinematicFields(motion[p.Time])...)
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// Received solves tr = te + |p(te)-observer|/c on a piecewise linear ECEF
// trajectory. Radial velocity is positive for recession. No extrapolation,
// bridging long gaps, or supersonic segments is allowed.
func (t Track) Received(observer geo.Vec, tr, c, maxGap float64) (factor, distance float64, ok bool) {
	p := t.Points
	arrival := func(i int) float64 {
		if t.receiver != nil && t.receiver.observer == observer && t.receiver.sound == c && len(t.receiver.arrivals) == len(p) {
			return t.receiver.arrivals[i]
		}
		return p[i].Time + p[i].Position.Sub(observer).Norm()/c
	}
	i := sort.Search(len(p), func(i int) bool { return arrival(i) >= tr })
	if i == 0 || i == len(p) {
		return 0, 0, false
	}
	a, b := p[i-1], p[i]
	dt := b.Time - a.Time
	if dt <= 0 || dt > maxGap {
		return 0, 0, false
	}
	velocity, speed := t.segmentMotion(i - 1)
	if speed >= c {
		return 0, 0, false
	}
	// Newton iteration within the bracket, in local time to preserve precision.
	lo, hi := 0.0, dt
	u := dt / 2
	target := tr - a.Time
	// Contemporary Unix timestamps have coarser float64 spacing than 1e-7 s.
	// An arrival rounded onto a telemetry vertex can otherwise select a bracket
	// whose endpoint residual never meets that fixed tolerance. Account for the
	// input timestamp precision without extrapolating beyond the bracket.
	tolerance := math.Max(1e-7, 2*(math.Nextafter(tr, math.Inf(1))-tr))
	// Solve the constant-velocity segment's retarded-time quadratic. The
	// rationalized root avoids subtracting nearly equal positive numbers.
	r0 := a.Position.Sub(observer)
	v2 := velocity.Dot(velocity)
	qa := c*c - v2
	qb := c*c*target + r0.Dot(velocity)
	qc := c*c*target*target - r0.Dot(r0)
	disc := qb*qb - qa*qc
	if disc >= 0 {
		candidate := qc / (qb + math.Sqrt(disc))
		if geo.Finite(candidate) && candidate >= -tolerance && candidate <= dt+tolerance {
			candidate = math.Max(0, math.Min(dt, candidate))
			r := r0.Add(velocity.Scale(candidate))
			d := r.Norm()
			if d >= 1 && math.Abs(candidate+d/c-target) <= tolerance {
				return c / (c + r.Dot(velocity)/d), d, true
			}
		}
	}
	for k := 0; k < 32; k++ {
		r := a.Position.Add(velocity.Scale(u)).Sub(observer)
		d := r.Norm()
		if d < 1 {
			return 0, 0, false
		}
		radial := r.Dot(velocity) / d
		f := u + d/c - target
		nearEndpoint := u <= tolerance || dt-u <= tolerance
		if math.Abs(f) < 1e-7 || nearEndpoint && math.Abs(f) <= tolerance {
			return c / (c + radial), d, true
		}
		if f > 0 {
			hi = u
		} else {
			lo = u
		}
		next := u - f/(1+radial/c)
		if next <= lo || next >= hi {
			next = (lo + hi) / 2
		}
		u = next
	}
	return 0, 0, false
}

type Approach struct{ Emitted, Heard, Distance, Speed float64 }

func (t Track) CPA(observer geo.Vec, c, maxGap float64) (Approach, bool) {
	return t.ApproachWithin(observer, math.Inf(-1), math.Inf(1), c, maxGap)
}

// ApproachWithin finds the nearest modeled position whose sound ARRIVES in
// the supplied interval. A partial flyover need not contain the flight's CPA.
func (t Track) ApproachWithin(observer geo.Vec, from, to, c, maxGap float64) (Approach, bool) {
	return t.approachWithin(observer, from, to, c, maxGap, -1)
}

// WithinRange answers the same distance gate as ApproachWithin without finding
// an unnecessary global minimum after a qualifying segment is found. Arrival
// bounds, subsonic checks and telemetry-gap rules remain identical.
func (t Track) WithinRange(observer geo.Vec, from, to, radius, c, maxGap float64) bool {
	if !geo.Finite(radius) || radius < 0 {
		return false
	}
	a, ok := t.approachWithin(observer, from, to, c, maxGap, radius)
	return ok && a.Distance <= radius
}

func (t Track) approachWithin(observer geo.Vec, from, to, c, maxGap, stopDistance float64) (Approach, bool) {
	best := Approach{Distance: math.Inf(1)}
	first, last := 1, len(t.Points)-1
	// For a qualifying range <= R, causality gives from-R/c <= emission
	// time <= to. This bound is safe even when a different segment is
	// supersonic or discontinuous and arrival monotonicity is unavailable.
	if index := t.segments; stopDistance >= 0 && c > 0 && index != nil && index.count == len(t.Points) && index.ordered {
		if !math.IsInf(from, -1) {
			first = max(first, sort.Search(len(t.Points), func(i int) bool { return t.Points[i].Time >= from-stopDistance/c }))
		}
		if !math.IsInf(to, 1) {
			last = min(last, sort.Search(len(t.Points), func(i int) bool { return t.Points[i].Time > to }))
		}
	}
	// Strictly subsonic ordered motion makes arrival time strictly increasing
	// for every fixed receiver. Only segments overlapping this arrival interval
	// can contribute. Suspect/supersonic geometry keeps the exhaustive path.
	if index := t.segments; index != nil && index.count == len(t.Points) && index.ordered && index.maxSpeed < c {
		arrival := func(i int) float64 {
			if t.receiver != nil && t.receiver.observer == observer && t.receiver.sound == c && len(t.receiver.arrivals) == len(t.Points) {
				return t.receiver.arrivals[i]
			}
			p := t.Points[i]
			return p.Time + p.Position.Sub(observer).Norm()/c
		}
		if !math.IsInf(from, -1) {
			first = max(first, sort.Search(len(t.Points), func(i int) bool { return arrival(i) >= from }))
		}
		if !math.IsInf(to, 1) {
			last = min(last, sort.Search(len(t.Points), func(i int) bool { return arrival(i) > to }))
		}
	}
	for i := first; i <= last; i++ {
		a, b := t.Points[i-1], t.Points[i]
		dt := b.Time - a.Time
		if dt <= 0 || dt > maxGap {
			continue
		}
		delta := b.Position.Sub(a.Position)
		_, speed := t.segmentMotion(i - 1)
		if speed < 1 || speed >= c {
			continue
		}
		arrival := func(u float64) float64 { return a.Time + u*dt + a.Position.Add(delta.Scale(u)).Sub(observer).Norm()/c }
		left, right := 0.0, 1.0
		bounded := !math.IsInf(from, -1) || !math.IsInf(to, 1)
		if bounded && (arrival(1) < from || arrival(0) > to) {
			continue
		}
		fraction := func(target float64) float64 {
			lo, hi := 0.0, 1.0
			for k := 0; k < 35; k++ {
				mid := (lo + hi) / 2
				if arrival(mid) < target {
					lo = mid
				} else {
					hi = mid
				}
			}
			return (lo + hi) / 2
		}
		if bounded && arrival(0) < from {
			left = fraction(from)
		}
		if bounded && arrival(1) > to {
			right = fraction(to)
		}
		u := max(left, min(right, observer.Sub(a.Position).Dot(delta)/delta.Dot(delta)))
		d := a.Position.Add(delta.Scale(u)).Sub(observer).Norm()
		if d < best.Distance {
			te := a.Time + u*dt
			best = Approach{te, te + d/c, d, speed}
			if d <= stopDistance {
				return best, true
			}
		}
	}
	return best, !math.IsInf(best.Distance, 1)
}
