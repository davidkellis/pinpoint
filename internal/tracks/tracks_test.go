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

func TestRetardedDopplerAgainstClosedForm(t *testing.T) {
	const c = 343.0
	const v = 90.0
	const d = 1300.0
	tr := Track{ID: "linear", Points: []Point{{Time: -200, Position: geo.Vec{X: -200 * v, Y: d}}, {Time: 200, Position: geo.Vec{X: 200 * v, Y: d}}}}
	for _, heard := range []float64{-60, -10, 0, 10, 60} {
		emission := (c*c*heard - math.Sqrt(c*c*v*v*heard*heard+(c*c-v*v)*d*d)) / (c*c - v*v)
		distance := math.Hypot(v*emission, d)
		want := c / (c + v*v*emission/distance)
		factor, gotDistance, ok := tr.Received(geo.Vec{}, heard, c, 500)
		if !ok || math.Abs(factor-want) > 1e-8 || math.Abs(gotDistance-distance) > 1e-5 {
			t.Fatalf("t=%v factor=%v distance=%v ok=%v; want %v,%v", heard, factor, gotDistance, ok, want, distance)
		}
	}
	a, ok := tr.CPA(geo.Vec{}, c, 500)
	if !ok || math.Abs(a.Emitted) > 1e-9 || math.Abs(a.Heard-d/c) > 1e-9 || a.Distance != d {
		t.Fatalf("incorrect delayed CPA: %+v", a)
	}
	if _, _, ok := tr.Received(geo.Vec{}, 0, c, 15); ok {
		t.Fatal("bridged a telemetry gap")
	}
	if _, _, ok := tr.Received(geo.Vec{}, 500, c, 500); ok {
		t.Fatal("extrapolated beyond coverage")
	}
}

func TestRetardedDopplerAtUnixTimestampVertices(t *testing.T) {
	const epoch = 1700000000.0
	const c = 343.0
	observer := geo.ECEF(35.25, -100.5, 900)
	tr := Track{ID: "linear-at-real-epoch"}
	for sec := -60.0; sec <= 180; sec += 5 {
		tr.Points = append(tr.Points, Point{Time: epoch + sec, Position: observer.Add(geo.Vec{X: 120 * (sec - 60), Y: 1500, Z: 2000})})
	}
	for te := 0.0; te <= 120; te += 2 {
		x := 120 * (te - 60)
		distance := math.Sqrt(x*x + 1500*1500 + 2000*2000)
		// Forward emission-to-arrival mapping is independent of the inverse solver.
		heard := epoch + te + distance/c
		want := c / (c + 120*x/distance)
		factor, gotDistance, ok := tr.Received(observer, heard, c, 30)
		if !ok || math.Abs(factor-want) > 1e-7 || math.Abs(gotDistance-distance) > .001 {
			t.Fatalf("emission %.1f: factor %.12f distance %.6f ok %v; want %.12f %.6f", te, factor, gotDistance, ok, want, distance)
		}
	}
}

func TestReceiverCachePreservesEpochSolverAtDifferentPositions(t *testing.T) {
	p := geo.ECEF(35.25, -100.5, 900)
	tr := Track{ID: "turn-and-climb"}
	for sec := -120.; sec <= 120; sec += 2 {
		tr.Points = append(tr.Points, Point{Time: 1700000000 + sec, Position: p.Add(geo.Vec{X: 90 * sec, Y: 1000 + sec*sec*.01, Z: 2000 + sec*2})})
	}
	for _, receiver := range []geo.Vec{p, p.Add(geo.Vec{X: 700, Y: -900, Z: 10})} {
		cached := tr.ForObserver(receiver, 343)
		for sec := -80.; sec < 100; sec += .375 {
			a, d, ok := tr.Received(receiver, 1700000000+sec, 343, 15)
			b, e, yes := cached.Received(receiver, 1700000000+sec, 343, 15)
			if ok != yes || ok && (math.Abs(a-b) > 1e-10 || math.Abs(d-e) > 1e-6) {
				t.Fatal("cached receiver changed physics")
			}
			// A different receiver must bypass this cache, without mutating it.
			other := receiver.Add(geo.Vec{Y: 100})
			a, d, ok = tr.Received(other, 1700000000+sec, 343, 15)
			b, e, yes = cached.Received(other, 1700000000+sec, 343, 15)
			if ok != yes || ok && (a != b || d != e) {
				t.Fatal("reused wrong receiver cache")
			}
		}
	}
	if tr.receiver != nil {
		t.Fatal("mutated shared trajectory")
	}
}

func TestCSVRejectsBadData(t *testing.T) {
	header := strings.Join(Header, ",") + "\n"
	for _, body := range []string{"a,1,NaN,0,100\na,2,0,0,100\n", "a,1,0,0,100\na,1,1,0,100\n", "a,1,91,0,100\na,2,0,0,100\n"} {
		if _, e := Read(strings.NewReader(header + body)); e == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
func TestReadsbAltitudeFlagsAndGaps(t *testing.T) {
	// Geometric field 10 takes priority over pressure altitude; bit 8 marks
	// geometric altitude in legacy rows. Stale, ground and missing heights break legs.
	fixture := `{"icao":"abc123","timestamp":1000,"trace":[
 [0,35,-100,1000,100,90,0,0,null,"adsb_icao",2000],
 [1,35,-99.999,1000,100,90,0,0,null,"adsb_icao",2000],
 [2,35,-99.998,1000,100,90,0,0],
 [3,35,-99.997,2100,100,90,8,0],
 [4,35,-99.996,2100,100,90,8,0],
 [5,35,-99.995,2100,100,90,9,0],
 [6,35,-99.994,"ground",0,0,0,0]]}`
	path := filepath.Join(t.TempDir(), "trace.json")
	if e := os.WriteFile(path, []byte(fixture), 0644); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	stats, e := Import(path, &out, ImportOptions{Start: 0, End: 2000, MaxGap: 15})
	if e != nil {
		t.Fatal(e)
	}
	if stats.Points != 4 || stats.MissingGeometric != 1 || stats.Stale != 1 || stats.Ground != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	ts, e := Read(&out)
	if e != nil {
		t.Fatal(e)
	}
	if len(ts) != 2 || math.Abs(ts[0].Points[0].Alt-609.6) > 1e-6 || math.Abs(ts[1].Points[0].Alt-640.08) > 1e-6 {
		t.Fatalf("wrong conversion or gap: %+v", ts)
	}
}

func TestReadsbDuplicateUpdatesDoNotInventMotion(t *testing.T) {
	fixture := `{"icao":"abc123","timestamp":1000,"trace":[
 [0,41,-74,1000,100,90,0,0,null,"adsb_icao",1200],
 [1,41,-73.999,1000,100,90,0,0,null,"adsb_icao",1200],
 [1,41,-73.99898,1000,100,90,0,0,null,"adsb_icao",1200],
 [2,41,-73.998,1000,100,90,0,0,null,"adsb_icao",1200],
 [2,41,-73.99,1000,100,90,0,0,null,"adsb_icao",1200],
 [3,41,-73.997,1000,100,90,0,0,null,"adsb_icao",1200],
 [4,41,-73.996,1000,100,90,0,0,null,"adsb_icao",1200]]}`
	p := filepath.Join(t.TempDir(), "trace.json")
	os.WriteFile(p, []byte(fixture), 0600)
	var out bytes.Buffer
	stats, err := Import(p, &out, ImportOptions{Start: 1000, End: 1010, MaxGap: 10})
	if err != nil || stats.Points != 4 || stats.DuplicateUpdates != 2 || stats.ConflictingUpdates != 1 {
		t.Fatalf("duplicate policy failed: %+v %v", stats, err)
	}
	ts, err := Read(&out)
	if err != nil || len(ts) != 2 {
		t.Fatalf("conflicting updates did not break continuity: %v", err)
	}
	if ts[0].Points[1].Lon != -73.99898 || ts[1].Points[0].Time != 1003 {
		t.Fatal("wrong repeated update retained or conflicting timestamp survived")
	}
}

func TestApproachWithinUsesArrivalRatherThanEmissionTime(t *testing.T) {
	const c = 343.0
	const v = 90.0
	const d = 1300.0
	tr := Track{ID: "partial", Points: []Point{{Time: -200, Position: geo.Vec{X: -200 * v, Y: d}}, {Time: 200, Position: geo.Vec{X: 200 * v, Y: d}}}}
	// The closest position in this arrival interval is the right endpoint;
	// actual CPA is heard well after it, at d/c.
	a, ok := tr.ApproachWithin(geo.Vec{}, -60, -30, c, 500)
	te := (c*c*(-30) - math.Sqrt(c*c*v*v*30*30+(c*c-v*v)*d*d)) / (c*c - v*v)
	if !ok || math.Abs(a.Heard+30) > 1e-6 || math.Abs(a.Emitted-te) > 1e-6 || math.Abs(a.Distance-math.Hypot(v*te, d)) > 1e-5 {
		t.Fatalf("wrong partial approach: %+v", a)
	}
	if _, ok = tr.ApproachWithin(geo.Vec{}, 500, 510, c, 500); ok {
		t.Fatal("extrapolated a nonexistent partial pass")
	}
}
