package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrackerRatesAndSamples(t *testing.T) {
	tr := NewTracker(2)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }

	tr.Add(100, 200)
	tr.Tick()
	s := tr.Snapshot()
	if s.TotalRead != 100 || s.TotalWrite != 200 || s.ReadRate != 100 || s.WriteRate != 200 {
		t.Fatalf("snapshot = %+v", s)
	}

	tr.Add(50, 25)
	tr.Tick()
	tr.Add(1, 1)
	tr.Tick()
	s = tr.Snapshot()
	if s.ReadRate != 1 || s.WriteRate != 1 {
		t.Fatalf("rates = %+v", s)
	}
	if len(s.Samples) != 2 {
		t.Fatalf("samples = %d，期望上限 2", len(s.Samples))
	}
	if s.TotalRead != 151 || s.TotalWrite != 226 {
		t.Fatalf("totals = %+v", s)
	}
}

func TestTrackerRunStops(t *testing.T) {
	tr := NewTracker(10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		tr.Run(ctx, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run 未退出")
	}
}

func TestRingOverwritesAndSubscribes(t *testing.T) {
	r := NewRing(2)
	ch, cancel := r.Subscribe()
	defer cancel()

	r.Add(Entry{Domain: "a"})
	r.Add(Entry{Domain: "b"})
	r.Add(Entry{Domain: "c"})
	recent := r.Recent()
	if len(recent) != 2 || recent[0].Domain != "b" || recent[1].Domain != "c" {
		t.Fatalf("recent = %+v", recent)
	}
	for _, want := range []string{"a", "b", "c"} {
		select {
		case e := <-ch:
			if e.Domain != want {
				t.Fatalf("event = %q，期望 %q", e.Domain, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("未收到 %q", want)
		}
	}
}

func TestLoggerWritesFileAndTracker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "fastgithub.log")
	tr := NewTracker(10)
	ring := NewRing(10)
	l, err := NewLogger(ring, tr, path)
	if err != nil {
		t.Fatal(err)
	}
	l.Log(Entry{Time: time.Unix(1000, 0), Domain: "github.com", Method: "GET", Status: 200, Duration: 5 * time.Millisecond, Read: 3, Write: 7})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "github.com GET 200") || !strings.Contains(text, " 3 7") {
		t.Fatalf("log = %q", text)
	}
	if s := tr.Snapshot(); s.TotalRead != 3 || s.TotalWrite != 7 {
		t.Fatalf("tracker = %+v", s)
	}
	if got := ring.Recent(); len(got) != 1 || got[0].Domain != "github.com" {
		t.Fatalf("ring = %+v", got)
	}
}
