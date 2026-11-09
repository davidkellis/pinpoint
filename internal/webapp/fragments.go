package webapp

import (
	"net/http"
	"pinpoint/internal/acoustic"
	"strconv"
)

// Keep ordinary status responses small. Full ridges remain available for every
// event through the original index and the paged fragment endpoint.
func clientInfo(info JobInfo) JobInfo {
	info = compactMetadata(info)
	info.FragmentCount = len(info.Events)
	if len(info.Events) <= 1000 {
		return info
	}
	info.EventsFiltered = true
	var events []acoustic.Event
	for i, e := range info.Events {
		if e.Evidence != nil && e.Evidence.Level == "weak" {
			continue
		}
		events = append(events, e)
		info.EventIndices = append(info.EventIndices, i)
	}
	info.Events = events
	return info
}

func compactMetadata(info JobInfo) JobInfo {
	if info.Detection != nil {
		d := *info.Detection
		d.Events = nil
		info.Detection = &d
	}
	// Automatic events are derived again from the saved detector report.
	if info.Params != nil && info.Params.Automatic {
		p := *info.Params
		p.Events = nil
		info.Params = &p
	}
	return info
}

type fragmentPage struct {
	Events  []acoustic.Event `json:"events"`
	Indices []int            `json:"event_indices"`
	Next    int              `json:"next"`
	End     bool             `json:"end"`
	Total   int              `json:"total_fragments"`
}

func (s *Server) fragments(w http.ResponseWriter, r *http.Request) {
	j := s.getJob(w, r)
	if j == nil {
		return
	}
	from, limit := 0, 200
	var err error
	if v := r.URL.Query().Get("from"); v != "" {
		from, err = strconv.Atoi(v)
	}
	if err != nil || from < 0 {
		problem(w, 400, "Invalid fragment offset.")
		return
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
	}
	if err != nil || limit < 1 || limit > 500 {
		problem(w, 400, "Request 1–500 fragments per page.")
		return
	}
	info := j.snapshot()
	p := fragmentPage{Events: []acoustic.Event{}, Indices: []int{}, Total: len(info.Events)}
	p.Next = min(from, len(info.Events))
	for p.Next < len(info.Events) && len(p.Events) < limit {
		i := p.Next
		p.Next++
		e := info.Events[i]
		if e.Evidence == nil || e.Evidence.Level != "weak" {
			continue
		}
		p.Events = append(p.Events, e)
		p.Indices = append(p.Indices, i)
	}
	p.End = p.Next == len(info.Events)
	reply(w, 200, p)
}
