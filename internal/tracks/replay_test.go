package tracks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pinpoint/internal/geo"
	"testing"
)

func TestReplayUsesTimestampAndPositionNotMetadata(t *testing.T) {
	var b bytes.Buffer
	put := func(a, b1, c, d uint32) {
		for _, v := range []uint32{a, b1, c, d} {
			binary.Write(&b, binary.LittleEndian, v)
		}
	}
	const millis uint64 = 1793753700000
	put(180, 0, 0, 0) // Optional replay slice index before timestamp records.
	put(9174, 0, 0, 0)
	put(0xe7f7c9d, uint32(millis>>32), uint32(millis&0xffffffff), 10000)
	lon := int32(-99000000)
	put(0xabc123, 31000000, uint32(lon), 0)
	put(0xdef123, 0x40000000, 0, 0)          // Callsign/squawk record, not a position.
	put(0x123456, 51000000, uint32(lon), 0)  // Outside Texas.
	put(0x1abc123, 31000000, uint32(lon), 0) // Non-ICAO identifier.
	region, _ := geo.StateBounds("TX")
	ids, e := replayIDs(b.Bytes(), region, float64(millis)/1000-1, float64(millis)/1000+1)
	if e != nil || len(ids) != 1 || !ids["abc123"] {
		t.Fatalf("%v, %v", ids, e)
	}
	ids, e = replayIDs(b.Bytes(), region, 0, 10)
	if e != nil || len(ids) != 0 {
		t.Fatal("ignored replay time filter")
	}
	if _, e = replayIDs(b.Bytes()[1:], region, 0, 10); e == nil {
		t.Fatal("accepted malformed replay")
	}
}

func TestHistoryCacheGzipBudgetAndNotYetPublished(t *testing.T) {
	var data bytes.Buffer
	g := gzip.NewWriter(&data)
	g.Write([]byte("trace"))
	g.Close()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		w.Write(data.Bytes())
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "trace")
	budget := int64(1000)
	b, e := historyFile(context.Background(), path, srv.URL, &budget, false)
	if e != nil || string(b) != "trace" {
		t.Fatal(e, string(b))
	}
	if budget != 1000-int64(data.Len()) {
		t.Fatal("download budget ignored")
	}
	historyFile(context.Background(), path, srv.URL, &budget, false)
	if calls != 1 {
		t.Fatal("cache not reused")
	}
	_, e = historyFile(context.Background(), path+"x", srv.URL+"/missing", &budget, false)
	var pending *HistoryUnavailable
	if !errors.As(e, &pending) {
		t.Fatalf("expected unavailable history, got %v", e)
	}
}
