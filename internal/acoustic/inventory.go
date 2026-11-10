package acoustic

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Sound kinds in an inventory, from most to least useful for location.
const (
	SoundUsable   = "usable"    // clear Doppler curve offered to location search
	SoundPossible = "possible"  // Doppler-shaped but uncorroborated; manual use only
	SoundTooHigh  = "too_high"  // clear curve above the locatable pitch limit
	SoundLoudOnly = "loud_only" // loud pass with no traceable tone (jets, traffic)
)

// SoundItem is one sound a listener could point to, explained in plain language.
type SoundItem struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Start       float64  `json:"start_s"`
	End         float64  `json:"end_s"`
	Detector    int      `json:"detector_index,omitempty"` // 1-based detection event
	StartHz     float64  `json:"start_hz,omitempty"`
	EndHz       float64  `json:"end_hz,omitempty"`
	MedianHz    float64  `json:"median_hz,omitempty"`
	RiseDB      float64  `json:"loudness_rise_db,omitempty"`
	Partial     bool     `json:"partial_pass,omitempty"`
	Related     []int    `json:"related_detector_indices,omitempty"`
	Explanation []string `json:"explanation"`
}

type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// SoundInventory says what the detector found in a recording and whether each
// sound can help locate it. It describes sound patterns; it does not identify
// aircraft.
type SoundInventory struct {
	Verdict         string         `json:"verdict"`
	Items           []SoundItem    `json:"items"`
	Counts          map[string]int `json:"counts"`
	Rejected        int            `json:"rejected_fragments"`
	RejectedReasons []ReasonCount  `json:"rejected_reasons"`
	Note            string         `json:"note"`
}

// Clock formats audio seconds as m:ss or h:mm:ss.
func Clock(t float64) string {
	s := int(math.Round(math.Max(0, t)))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func span(d float64) string {
	if d < 90 {
		return fmt.Sprintf("%.0f seconds", d)
	}
	return fmt.Sprintf("%.1f minutes", d/60)
}

func distance(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%.0f m", 50*math.Round(m/50))
	}
	return fmt.Sprintf("%.1f km", m/1000)
}

func overlap(a, b SoundItem) float64 {
	o := math.Min(a.End, b.End) - math.Max(a.Start, b.Start)
	shorter := math.Min(a.End-a.Start, b.End-b.Start)
	if o <= 0 || shorter <= 0 {
		return 0
	}
	return o / shorter
}

// pitchHint describes what kind of source usually makes a tone at this pitch.
func pitchHint(hz float64) string {
	switch {
	case hz <= 400:
		return "Low pitch: typical of a propeller's blade tone or a helicopter rotor (road vehicles can sound similar)."
	case hz <= 1200:
		return "Middle pitch: could be a propeller harmonic, a jet, or a ground vehicle."
	default:
		return "High-pitched whine: typical of a jet engine's fan. Such tones fade quickly with distance and shift with throttle, so they often fit flight paths poorly."
	}
}

// geometryNote reads the detector's best straight-flyby fit. It is a rough
// guide: real aircraft turn and climb, and partial passes fit loosely.
func geometryNote(e Event) string {
	m := e.Measurement
	if m == nil || !(m.FitSlant > 0) || !(m.FitSpeed > 0) {
		return ""
	}
	if m.FitSlant < 500 {
		return fmt.Sprintf("Its bend is very sharp: the best fit puts the source within about %s of the microphone. That is unusually close for an aircraft. It can be an aircraft turning (a turn also bends the pitch quickly) or one landing or taking off nearby, but nearby cars, birds and people make sounds like this too.", distance(m.FitSlant))
	}
	speed := fmt.Sprintf("about %.0f km/h", 10*math.Round(m.FitSpeed*3.6/10))
	if m.FitSpeed < 30 {
		speed += ", slower than almost any aircraft"
	}
	return fmt.Sprintf("Best straight-line fit: closest approach about %s away, moving at %s (a rough guide; aircraft turn and climb).", distance(m.FitSlant), speed)
}

func partialPass(e Event) bool {
	return e.Measurement != nil && strings.Contains(e.Measurement.Reason, "not bounded")
}

var plainReasons = []struct{ match, plain string }{
	{"generic pitch trend explains", "its pitch held steady or changed in a straight line, without the bend of something passing by"},
	{"covers less than", "the tone kept dropping out"},
	{"coherent ridge", "too short to measure a pass"},
	{"held-out audio", "its bend did not hold up when checked against held-back parts of the audio"},
	{"not consistently separated", "too faint against the background noise"},
}

func plainReason(r string) string {
	for _, p := range plainReasons {
		if strings.Contains(r, p.match) {
			return p.plain
		}
	}
	return r
}

// Inventory explains every strong or review curve, loud passes without a
// curve, and why the remaining fragments were rejected. maxHz is the pitch
// limit for location search; minCurves the curves the consensus search needs.
func Inventory(events []Event, humps []Hump, maxHz float64, minCurves int) SoundInventory {
	inv := SoundInventory{Counts: map[string]int{}, Items: []SoundItem{}, Note: "These are sound patterns, not aircraft identifications. Whether a curve belongs to a real aircraft is decided later, by matching it to recorded flight paths."}
	type ranked struct {
		e     Event
		index int
		rank  int
	}
	var curves []ranked
	reasons := map[string]int{}
	for i, e := range events {
		if e.Evidence == nil || len(e.Ridge) < 8 || e.End <= e.Start {
			continue
		}
		switch e.Evidence.Level {
		case "strong":
			curves = append(curves, ranked{e, i + 1, 0})
		case "review":
			curves = append(curves, ranked{e, i + 1, 1})
		default:
			inv.Rejected++
			r := "no reason recorded"
			if len(e.Evidence.Reasons) > 0 {
				r = plainReason(e.Evidence.Reasons[0])
			}
			reasons[r]++
		}
	}
	// Strong curves first, longer first, so overlapping review curves attach
	// to the clearest version of the same sound.
	sort.SliceStable(curves, func(a, b int) bool {
		if curves[a].rank != curves[b].rank {
			return curves[a].rank < curves[b].rank
		}
		return curves[a].e.End-curves[a].e.Start > curves[b].e.End-curves[b].e.Start
	})
	for _, c := range curves {
		e := c.e
		m := medianHz(e.Ridge)
		it := SoundItem{Start: e.Start, End: e.End, Detector: c.index, StartHz: e.Ridge[0].Hz, EndHz: e.Ridge[len(e.Ridge)-1].Hz, MedianHz: m, Partial: partialPass(e)}
		switch {
		case c.rank == 0 && m <= maxHz:
			it.Kind, it.Title = SoundUsable, "Clear Doppler curve · used for location"
		case c.rank == 0:
			it.Kind, it.Title = SoundTooHigh, "Clear curve, but too high-pitched to use"
		default:
			it.Kind, it.Title = SoundPossible, "Possible Doppler curve · not used automatically"
		}
		if it.Kind == SoundPossible {
			merged := false
			for k := range inv.Items {
				if overlap(inv.Items[k], it) >= .5 {
					inv.Items[k].Related = append(inv.Items[k].Related, c.index)
					merged = true
					break
				}
			}
			if merged {
				continue
			}
		}
		slide := "rose"
		if it.EndHz < it.StartHz {
			slide = "fell"
		}
		it.Explanation = append(it.Explanation, fmt.Sprintf("A steady tone %s from %.0f to %.0f Hz over %s: the pitch slide of a source moving past (the Doppler effect).", slide, it.StartHz, it.EndHz, span(e.End-e.Start)))
		it.Explanation = append(it.Explanation, pitchHint(m))
		if g := geometryNote(e); g != "" {
			it.Explanation = append(it.Explanation, g)
		}
		if it.Partial {
			it.Explanation = append(it.Explanation, "Only one side of the pass was captured (the closest approach is outside the curve), so it pins the location less tightly.")
		}
		kind := it.Kind
		if kind == SoundPossible && m > maxHz {
			kind += "-high"
		}
		switch kind {
		case SoundUsable:
			it.Explanation = append(it.Explanation, "Used by the location search: each candidate place is tested for a tracked aircraft that would have produced this exact pitch slide.")
		case SoundTooHigh:
			it.Explanation = append(it.Explanation, fmt.Sprintf("Its median pitch, %.0f Hz, is above %.0f Hz. Air absorbs tones this high within a few kilometres, and in tests such curves were not explained by aircraft, so the search ignores it.", m, maxHz))
		case SoundPossible + "-high":
			it.Explanation = append(it.Explanation, fmt.Sprintf("Its shape fits a passing source, but it was confirmed in only one analysis view, and its median pitch, %.0f Hz, is above the %.0f Hz limit for location search (air absorbs such tones within a few kilometres). It is not used.", m, maxHz))
		default:
			it.Explanation = append(it.Explanation, "Its shape fits a passing source, but it was confirmed in only one analysis view and has no matching harmonic, so it may be noise. It is not used automatically; you can select it yourself under Flyover detection.")
		}
		inv.Items = append(inv.Items, it)
	}
	for _, h := range humps {
		covered := false
		for k := range inv.Items {
			if h.Time >= inv.Items[k].Start-10 && h.Time <= inv.Items[k].End+10 {
				inv.Items[k].RiseDB = math.Max(inv.Items[k].RiseDB, h.Rise)
				covered = true
			}
		}
		if covered {
			continue
		}
		inv.Items = append(inv.Items, SoundItem{Kind: SoundLoudOnly, Title: "Loud pass, no traceable tone", Start: h.Time - h.Width/2, End: h.Time + h.Width/2, RiseDB: h.Rise, Explanation: []string{
			fmt.Sprintf("The recording got %.0f dB louder for about %.0f seconds, peaking at %s, but no steady tone could be traced through it.", h.Rise, h.Width, Clock(h.Time)),
			"Jets often sound like this (mostly roar), as do cars, wind gusts and voices.",
			"Without a tone there is no Doppler curve, so it cannot be used to locate the recording.",
		}})
	}
	sort.SliceStable(inv.Items, func(a, b int) bool { return inv.Items[a].Start < inv.Items[b].Start })
	for i := range inv.Items {
		it := &inv.Items[i]
		inv.Counts[it.Kind]++
		if it.Kind != SoundUsable {
			continue
		}
		for _, o := range inv.Items {
			if o.Kind == SoundUsable && o.Detector != it.Detector && overlap(*it, o) > 0 {
				it.Explanation = append(it.Explanation, fmt.Sprintf("It overlaps the curve at %s; they may be one aircraft (two tones) or two. The search never uses overlapping curves together.", Clock(o.Start)))
				break
			}
		}
	}
	for r, n := range reasons {
		inv.RejectedReasons = append(inv.RejectedReasons, ReasonCount{r, n})
	}
	sort.Slice(inv.RejectedReasons, func(a, b int) bool {
		if inv.RejectedReasons[a].Count != inv.RejectedReasons[b].Count {
			return inv.RejectedReasons[a].Count > inv.RejectedReasons[b].Count
		}
		return inv.RejectedReasons[a].Reason < inv.RejectedReasons[b].Reason
	})
	inv.Verdict = verdict(inv.Counts, minCurves)
	return inv
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func verdict(c map[string]int, minCurves int) string {
	u := c[SoundUsable]
	var v string
	switch {
	case u >= minCurves && u > 1:
		v = fmt.Sprintf("%s. That can be enough for the \"several flyovers agree\" search if they come from different aircraft that appear in flight data.", plural(u, "clear Doppler curve", "clear Doppler curves"))
	case u == 1 && minCurves <= 1:
		v = "1 clear Doppler curve. A single-curve search can use it, but one curve alone usually fits many places about equally well."
	case u == 1:
		v = fmt.Sprintf("Only 1 clear Doppler curve. The \"several flyovers agree\" search needs at least %d from different aircraft; one curve alone usually fits many places about equally well.", minCurves)
	default:
		v = "No clear Doppler curve, so aircraft sounds cannot locate this recording."
	}
	if n := c[SoundPossible]; n > 0 {
		v += fmt.Sprintf(" %s could be checked by hand.", plural(n, "possible curve", "possible curves"))
	}
	if n := c[SoundLoudOnly]; n > 0 {
		v += fmt.Sprintf(" %s without a traceable tone (often jets) cannot be used.", plural(n, "loud pass", "loud passes"))
	}
	if u < max(2, minCurves) {
		v += " Better recordings: stand still, keep the phone out of a pocket, and record while propeller planes pass."
	}
	return v
}

// Text renders the inventory for a terminal.
func (inv SoundInventory) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "What Pinpoint heard\n  %s\n", inv.Verdict)
	for _, it := range inv.Items {
		fmt.Fprintf(&b, "\n  %s–%s  %s", Clock(it.Start), Clock(it.End), it.Title)
		if it.Detector > 0 {
			fmt.Fprintf(&b, " (detector curve %d)", it.Detector)
		}
		if it.RiseDB > 0 && it.Kind != SoundLoudOnly {
			fmt.Fprintf(&b, ", %.0f dB louder", it.RiseDB)
		}
		b.WriteString("\n")
		for _, line := range it.Explanation {
			fmt.Fprintf(&b, "    · %s\n", line)
		}
		if len(it.Related) > 0 {
			fmt.Fprintf(&b, "    · Also traced as weaker copies (curves %v), for example a harmonic or a second engine.\n", it.Related)
		}
	}
	if inv.Rejected > 0 {
		fmt.Fprintf(&b, "\n  %d other tonal fragments were rejected:\n", inv.Rejected)
		for _, r := range inv.RejectedReasons {
			fmt.Fprintf(&b, "    · %d: %s\n", r.Count, r.Reason)
		}
	}
	fmt.Fprintf(&b, "\n  %s\n", inv.Note)
	return b.String()
}
