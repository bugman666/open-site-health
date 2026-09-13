package probe

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

type capturedPayload struct {
	mu   sync.Mutex
	list []alert.Payload
}

func (c *capturedPayload) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var p alert.Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.list = append(c.list, p)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func (c *capturedPayload) got() []alert.Payload {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]alert.Payload, len(c.list))
	copy(out, c.list)
	return out
}

func newAlertingScheduler(t *testing.T, cfg config.Config) (*Scheduler, *targets.Store, *ResultStore, *capturedPayload) {
	t.Helper()
	cap := &capturedPayload{}
	hook := httptest.NewServer(cap.handler())
	t.Cleanup(hook.Close)

	if cfg.Listen == "" {
		cfg = config.Defaults()
	}
	cfg.Alert.WebhookURL = hook.URL
	if cfg.Alert.Cooldown == 0 {
		cfg.Alert.Cooldown = time.Hour
	}
	dir := t.TempDir()
	store, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	alerts := alert.New(cfg)
	s := New(cfg, store, results, alerts)
	s.Timeout = time.Second
	return s, store, results, cap
}

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

// TC3.2: up → down produces one downtime webhook.
func TestDownTriggersWebhook(t *testing.T) {
	s, store, _, cap := newAlertingScheduler(t, config.Defaults())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	tgt := mustAdd(t, store, "http://"+addr)
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := cap.got()
	if len(got) != 1 {
		t.Fatalf("want 1 down alert, got %#v", got)
	}
	if got[0].Kind != alert.KindDown {
		t.Fatalf("kind: %s", got[0].Kind)
	}
	if got[0].URL != tgt.URL || got[0].TargetID != tgt.ID {
		t.Fatalf("payload: %#v", got[0])
	}
}

// TC3.3: a cert warn is posted as cert_warn, not down.
func TestCertWarnTriggersDistinctAlert(t *testing.T) {
	cfg := config.Defaults()
	cfg.TLSWarnDays = 14
	s, store, _, cap := newAlertingScheduler(t, cfg)

	now := time.Now()
	cert := leafCert(t, now.Add(-time.Hour), now.Add(7*24*time.Hour))
	srv := startTLS(t, cert, okHandler())
	tgt := mustAdd(t, store, srv.URL)

	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := cap.got()
	if len(got) != 1 {
		t.Fatalf("want 1 cert alert, got %#v", got)
	}
	if got[0].Kind != alert.KindCertWarn {
		t.Fatalf("kind should be cert_warn, got %s", got[0].Kind)
	}
	if got[0].URL != tgt.URL {
		t.Fatalf("url: %s", got[0].URL)
	}
}

func TestCertExpiredTriggersDistinctAlert(t *testing.T) {
	cfg := config.Defaults()
	cfg.TLSWarnDays = 14
	s, store, _, cap := newAlertingScheduler(t, cfg)

	now := time.Now()
	cert := leafCert(t, now.Add(-48*time.Hour), now.Add(-time.Hour))
	srv := startTLS(t, cert, okHandler())
	mustAdd(t, store, srv.URL)

	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := cap.got()
	if len(got) != 1 || got[0].Kind != alert.KindCertExpired {
		t.Fatalf("want cert_expired, got %#v", got)
	}
}

// TC3.4: repeated downs inside the cooldown do not spam the webhook.
func TestRepeatedDownDoesNotSpam(t *testing.T) {
	s, store, _, cap := newAlertingScheduler(t, config.Defaults())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	mustAdd(t, store, "http://"+addr)

	for i := 0; i < 3; i++ {
		if err := s.CheckAll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(cap.got()); n != 1 {
		t.Fatalf("cooldown should keep a single down alert, got %d %#v", n, cap.got())
	}
}

// TC3.5: a downed target that comes back sends one recovery notice.
func TestRecoveryAfterDown(t *testing.T) {
	s, store, _, cap := newAlertingScheduler(t, config.Defaults())

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	tgt := mustAdd(t, store, fail.URL)
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail.Close()

	ok := httptest.NewServer(okHandler())
	t.Cleanup(ok.Close)
	if _, err := store.Update(tgt.ID, ok.URL); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := cap.got()
	if len(got) != 2 {
		t.Fatalf("want down + recovery, got %#v", got)
	}
	if got[0].Kind != alert.KindDown || got[1].Kind != alert.KindRecovered {
		t.Fatalf("kinds: %#v", got)
	}
}
