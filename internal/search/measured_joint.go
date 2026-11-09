package search

import (
	"fmt"
	"pinpoint/internal/acoustic"
)

// MeasuredJointConfig starts again from the user's complete search area. It
// never takes a single-event shortlist, seed position, or selected aircraft.
func MeasuredJointConfig(c Config, events []acoustic.Event) (Config, error) {
	if len(events) < 2 || len(events) != len(c.Events) {
		return c, fmt.Errorf("every reviewed flyover must supply a measured curve for joint search")
	}
	for i, e := range events {
		if len(e.Ridge) < 8 || e.Evidence == nil || e.Evidence.Level != "strong" || !e.Evidence.Predictive || e.Measurement == nil || !e.Measurement.ShapePass {
			return c, fmt.Errorf("event %d lacks strong measured ridge evidence", i+1)
		}
	}
	c.Events = append([]acoustic.Event(nil), events...)
	c.Staged, c.Corridors = true, true
	c.Grid = min(c.Grid, 200)
	if c.DenseGrid == 0 {
		c.DenseGrid = 100
	}
	c.DenseGrid = min(c.DenseGrid, min(c.Grid, 100))
	c.Refine = min(c.Refine, c.DenseGrid)
	if c.Refine == 0 {
		c.FinalGrid = 0
	} else {
		c.FinalGrid = min(c.FinalGrid, c.Refine)
	}
	c.clockOffsets = nil
	c.eventTrackIDs = nil
	return c, nil
}
