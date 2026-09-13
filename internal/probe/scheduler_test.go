package probe

import (
	"context"
	"testing"
	"time"

	"github.com/bugman666/open-site-health/internal/config"
	"net/http/httptest"
)

// TC2.1: after one configured interval, every registered target has a
// result whose timestamps follow the period.
func TestScheduledProbeCreatesResults(t *testing.T) {
	cfg := config.Defaults()
	cfg.ProbeInterval = 80 * time.Millisecond
	s, store, results := newTestScheduler(t, cfg)
	s.Timeout = 500 * time.Millisecond

	srv := httptest.NewServer(okHandler())
	t.Cleanup(srv.Close)

	one := mustAdd(t, store, srv.URL+"/one")
	two := mustAdd(t, store, srv.URL+"/two")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)

	deadline := time.Now().Add(800 * time.Millisecond)
	var r1, r2 []Result
	for time.Now().Before(deadline) {
		var err error
		r1, err = results.ListByTarget(one.ID)
		if err != nil {
			t.Fatal(err)
		}
		r2, err = results.ListByTarget(two.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(r1) >= 2 && len(r2) >= 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	cancel()

	if len(r1) < 1 || len(r2) < 1 {
		t.Fatalf("expected a result per target, got %d and %d", len(r1), len(r2))
	}
	if r1[0].Availability != Up || r2[0].Availability != Up {
		t.Fatalf("scheduled results: %#v %#v", r1[0], r2[0])
	}

	if len(r1) >= 2 {
		delta := r1[0].CheckedAt.Sub(r1[1].CheckedAt)
		if delta < 40*time.Millisecond || delta > 300*time.Millisecond {
			t.Fatalf("timestamp gap %s not near interval %s", delta, cfg.ProbeInterval)
		}
	}
}

func TestStartReturnsWhenCancelled(t *testing.T) {
	cfg := config.Defaults()
	cfg.ProbeInterval = time.Hour
	s, _, _ := newTestScheduler(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}
