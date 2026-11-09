package tracks

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// KinematicPoint retains measured ground motion independently of positions.
// Missing geometric climb rate never falls back to barometric climb rate.
type KinematicPoint struct {
	Time        float64
	GroundSpeed float64
	TrueTrack   float64
	Up          float64
	Horizontal  bool
	Vertical    bool
}

var KinematicHeader = append(append([]string(nil), MetadataHeader...), "ground_speed_mps", "true_track_deg", "geom_vertical_rate_mps")

func traceKinematics(row []json.RawMessage, flags int) []string {
	fields := []string{"", "", ""}
	speed, sok := number(row[4])
	track, tok := number(row[5])
	if sok && tok && speed >= 0 && speed <= 800 && track >= 0 && track < 360 {
		fields[0] = strconv.FormatFloat(speed*1852/3600, 'f', 8, 64)
		fields[1] = strconv.FormatFloat(track, 'f', 8, 64)
	}
	up, uok := 0., false
	if len(row) > 11 {
		up, uok = number(row[11])
	}
	if !uok && flags&4 != 0 {
		up, uok = number(row[7])
	}
	if uok && math.Abs(up) <= 15000 {
		fields[2] = strconv.FormatFloat(up*.3048/60, 'f', 8, 64)
	}
	return fields
}

func readKinematics(fields []string, time float64) (KinematicPoint, error) {
	m := KinematicPoint{Time: time}
	if (fields[0] == "") != (fields[1] == "") {
		return m, fmt.Errorf("ground speed and true track must both be present or absent")
	}
	if fields[0] != "" {
		speed, e := strconv.ParseFloat(fields[0], 64)
		if e != nil || math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 0 || speed > 800*1852./3600 {
			return m, fmt.Errorf("invalid ground speed")
		}
		track, e := strconv.ParseFloat(fields[1], 64)
		if e != nil || math.IsNaN(track) || math.IsInf(track, 0) || track < 0 || track >= 360 {
			return m, fmt.Errorf("invalid true track")
		}
		m.GroundSpeed, m.TrueTrack, m.Horizontal = speed, track, true
	}
	if fields[2] != "" {
		up, e := strconv.ParseFloat(fields[2], 64)
		if e != nil || math.IsNaN(up) || math.IsInf(up, 0) || math.Abs(up) > 15000*.3048/60+1e-8 {
			return m, fmt.Errorf("invalid geometric climb rate")
		}
		m.Up, m.Vertical = up, true
	}
	return m, nil
}

func kinematicFields(m KinematicPoint) []string {
	fields := []string{"", "", ""}
	if m.Horizontal {
		fields[0] = strconv.FormatFloat(m.GroundSpeed, 'f', 8, 64)
		fields[1] = strconv.FormatFloat(m.TrueTrack, 'f', 8, 64)
	}
	if m.Vertical {
		fields[2] = strconv.FormatFloat(m.Up, 'f', 8, 64)
	}
	return fields
}
