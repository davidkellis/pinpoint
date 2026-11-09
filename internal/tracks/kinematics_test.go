package tracks

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pinpoint/internal/geo"
)

func TestImportRetainsGroundMotionAndOnlyGeometricClimbRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	data := `{"icao":"abc123","t":"C172","timestamp":1000,"trace":[
 [0,29,-98,2000,100,90,0,-9000,null,"adsb_icao",2100,600],
 [1,29,-97.999,2000,100,90,0,-9000,null,"adsb_icao",2100,null],
 [2,29,-97.998,2000,null,null,4,900,null,"adsb_icao",2100,null]]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := Import(path, &b, ImportOptions{Start: 1000, End: 1002, MaxGap: 15}); err != nil {
		t.Fatal(err)
	}
	r, err := Read(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || len(r[0].Motion) != 3 {
		t.Fatal("lost kinematic samples", r)
	}
	m := r[0].Motion
	if !m[0].Horizontal || !m[0].Vertical || math.Abs(m[0].GroundSpeed-100*1852./3600) > 1e-7 || m[0].TrueTrack != 90 || math.Abs(m[0].Up-3.048) > 1e-8 {
		t.Fatal("wrong units or velocity fields", m[0])
	}
	if !m[1].Horizontal || m[1].Vertical {
		t.Fatal("barometric rate substituted for missing geometric rate", m[1])
	}
	if m[2].Horizontal || !m[2].Vertical || math.Abs(m[2].Up-4.572) > 1e-8 {
		t.Fatal("lost flag-qualified geometric rate or fabricated missing horizontal motion", m[2])
	}
	var out bytes.Buffer
	if err := Write(&out, r); err != nil {
		t.Fatal(err)
	}
	again, err := Read(&out)
	if err != nil || len(again[0].Motion) != 3 {
		t.Fatal("kinematic CSV roundtrip failed", err)
	}
	legacy := strings.Join(MetadataHeader, ",") + "\na,1,29,-98,500,C172\na,2,29,-97.999,500,C172\n"
	old, err := Read(strings.NewReader(legacy))
	if err != nil || len(old[0].Motion) != 0 {
		t.Fatal("legacy CSV compatibility changed", err)
	}
}

func TestKinematicCSVRejectsPartialOrNonfiniteFields(t *testing.T) {
	for _, tail := range []string{"NaN,90,0", "30,,0", "30,360,0", "30,90,Inf"} {
		text := strings.Join(KinematicHeader, ",") + "\na,1,29,-98,500,C172," + tail + "\na,2,29,-97.999,500,C172,30,90,0\n"
		if _, err := Read(strings.NewReader(text)); err == nil {
			t.Fatal("accepted invalid motion", tail)
		}
	}
}

func TestReportedVelocityImprovesBoundaryInterpolationWithoutMovingRecordedNodes(t *testing.T) {
	origin := geo.ECEF(0, 0, 1500)
	position := func(sec float64) geo.Vec { return origin.Add(geo.Vec{Y: 90*sec + .5*sec*sec}) }
	velocity := func(sec float64) geo.Vec { return geo.Vec{Y: 90 + sec} }
	tr := Track{ID: "measured-motion"}
	for sec := -10.; sec <= 10; sec += 10 {
		p := position(sec)
		lat, lon, alt := geo.Geodetic(p)
		tr.Points = append(tr.Points, Point{Time: sec, Lat: lat, Lon: lon, Alt: alt, Position: p})
		v := velocity(sec)
		east, north, up := geo.ENUVector(lat, lon, 1, 0, 0), geo.ENUVector(lat, lon, 0, 1, 0), geo.ENUVector(lat, lon, 0, 0, 1)
		angle := math.Atan2(v.Dot(east), v.Dot(north)) * 180 / math.Pi
		if angle < 0 {
			angle += 360
		}
		tr.Motion = append(tr.Motion, KinematicPoint{Time: sec, GroundSpeed: math.Hypot(v.Dot(east), v.Dot(north)), TrueTrack: angle, Up: v.Dot(up), Horizontal: true, Vertical: true})
	}
	o := SmoothingOptions{Step: .1, MaxGap: 15, MaxAcceleration: 20}
	plain, _, err := tr.Smooth(o)
	if err != nil {
		t.Fatal(err)
	}
	o.UseKinematics = true
	measured, report, err := tr.Smooth(o)
	if err != nil || report.ReportedHorizontal != 3 || report.ReportedVertical != 3 {
		t.Fatal("did not use measured motion", report, err)
	}
	obs := geo.ECEF(0, 0, 0)
	oldError, newError := 0., 0.
	for te := -9.; te <= 9; te += .37 {
		d := position(te).Sub(obs)
		heard := te + d.Norm()/343
		want := 343 / (343 + d.Dot(velocity(te))/d.Norm())
		a, _, yes := plain.Received(obs, heard, 343, 15)
		b, _, ok := measured.Received(obs, heard, 343, 15)
		if !yes || !ok {
			t.Fatal("lost acoustic coverage")
		}
		oldError += math.Pow(a-want, 2)
		newError += math.Pow(b-want, 2)
	}
	if newError >= oldError*.01 {
		t.Fatalf("measured kinematics did not improve known motion: %g / %g", newError, oldError)
	}
}
