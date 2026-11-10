package acoustic

import (
	"strings"
	"testing"
)

func curveEvent(level string, start, end, fromHz, toHz float64, reason string) Event {
	e := Event{Start: start, End: end, Evidence: &CurveEvidence{Level: level, Reasons: []string{reason}}, Measurement: &RidgeMeasurement{FitSlant: 3000, FitSpeed: 70, Reason: "Curve shape bounds closest approach"}}
	for t := start; t <= end; t++ {
		u := (t - start) / (end - start)
		e.Ridge = append(e.Ridge, RidgeSample{Time: t, Hz: fromHz + u*(toHz-fromHz)})
	}
	return e
}

func TestInventoryExplainsEachKindOfSound(t *testing.T) {
	events := []Event{
		curveEvent("strong", 10, 100, 150, 120, ""),                                                   // 1: usable propeller-like tone
		curveEvent("review", 20, 80, 450, 360, ""),                                                    // 2: weaker copy of 1
		curveEvent("review", 200, 230, 600, 500, ""),                                                  // 3: possible curve on its own
		curveEvent("strong", 300, 330, 3200, 2800, ""),                                                // 4: too high-pitched
		curveEvent("weak", 400, 410, 500, 500, "A generic pitch trend explains this ridge too well."), // 5: rejected
	}
	humps := []Hump{{Time: 60, Rise: 8, Width: 20}, {Time: 600, Rise: 6, Width: 25}}
	inv := Inventory(events, humps, DefaultLocatableMaxHz, 2)
	kinds := map[string]int{}
	for _, it := range inv.Items {
		kinds[it.Kind]++
	}
	if kinds[SoundUsable] != 1 || kinds[SoundPossible] != 1 || kinds[SoundTooHigh] != 1 || kinds[SoundLoudOnly] != 1 || len(inv.Items) != 4 {
		t.Fatalf("wrong items: %+v", inv.Items)
	}
	first := inv.Items[0]
	if first.Kind != SoundUsable || first.Detector != 1 || first.RiseDB != 8 || len(first.Related) != 1 || first.Related[0] != 2 {
		t.Fatalf("usable curve should absorb its weaker copy and the overlapping loud pass: %+v", first)
	}
	last := inv.Items[len(inv.Items)-1]
	if last.Kind != SoundLoudOnly || !strings.Contains(strings.Join(last.Explanation, " "), "no steady tone") {
		t.Fatalf("an uncovered loud pass should be explained as having no curve: %+v", last)
	}
	if inv.Rejected != 1 || len(inv.RejectedReasons) != 1 || !strings.Contains(inv.RejectedReasons[0].Reason, "straight line") {
		t.Fatalf("rejected fragments should be counted with plain reasons: %+v", inv.RejectedReasons)
	}
	if !strings.HasPrefix(inv.Verdict, "Only 1 clear Doppler curve") || !strings.Contains(inv.Verdict, "stand still") {
		t.Fatalf("verdict: %s", inv.Verdict)
	}
	if text := inv.Text(); !strings.Contains(text, "0:10–1:40") || !strings.Contains(text, "Loud pass, no traceable tone") {
		t.Fatalf("text rendering:\n%s", text)
	}
}

func TestInventoryFlagsVerySharpBends(t *testing.T) {
	e := curveEvent("strong", 0, 20, 2000, 1500, "")
	e.Measurement.FitSlant = 120
	inv := Inventory([]Event{e}, nil, DefaultLocatableMaxHz, 2)
	if !strings.Contains(strings.Join(inv.Items[0].Explanation, " "), "unusually close for an aircraft") {
		t.Fatalf("missing close-source note: %v", inv.Items[0].Explanation)
	}
}

func TestFindHumpsFindsBroadRisesOnly(t *testing.T) {
	l := Loudness{Step: 1}
	for i := 0; i < 600; i++ {
		v := -50.
		if i >= 200 && i < 230 { // a 30 s pass, 10 dB up
			v = -40
		}
		if i == 400 { // a one-second click
			v = -30
		}
		l.Level = append(l.Level, v)
	}
	hs := FindHumps(l, DefaultHumpOptions())
	if len(hs) != 1 || hs[0].Time < 200 || hs[0].Time > 230 || hs[0].Rise < 9 {
		t.Fatalf("humps: %+v", hs)
	}
}
