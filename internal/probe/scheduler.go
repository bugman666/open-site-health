// Package probe runs scheduled HTTP(S) availability and TLS expiry checks.
//
// Each pass lists registered targets, records up/down independently of
// certificate ok/warn/expired, and keeps a recent result history so the
// HTTP API can query it. Notifications stay with the alert stub (#3).
package probe

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

const maxConcurrentProbes = 8

// Scheduler owns the probe ticker and writes results to a ResultStore.
type Scheduler struct {
	cfg     config.Config
	store   *targets.Store
	results *ResultStore
	alerts  *alert.Dispatcher
	// Timeout overrides the 10s per-target HTTP timeout. Tests set a
	// shorter value so downtime cases do not wait on the default.
	Timeout time.Duration
}

// New wires config, the target list, the result store, and the alert hook.
func New(cfg config.Config, store *targets.Store, results *ResultStore, alerts *alert.Dispatcher) *Scheduler {
	return &Scheduler{cfg: cfg, store: store, results: results, alerts: alerts}
}

// Results exposes the backing store for HTTP handlers and tests.
func (s *Scheduler) Results() *ResultStore {
	return s.results
}

// Start probes immediately, then again on every configured interval
// until ctx is cancelled. Default interval is 5m.
func (s *Scheduler) Start(ctx context.Context) {
	log.Printf("probe started: interval=%s tls_warn_days=%d", s.cfg.ProbeInterval, s.cfg.TLSWarnDays)
	ticker := time.NewTicker(s.cfg.ProbeInterval)
	defer ticker.Stop()

	if ctx.Err() == nil {
		s.runPass(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("probe stopped")
			return
		case <-ticker.C:
			s.runPass(ctx)
		}
	}
}

func (s *Scheduler) runPass(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if err := s.CheckAll(ctx); err != nil {
		log.Printf("probe pass: %v", err)
	}
}

// CheckAll runs one probe pass over every registered target and
// persists a Result for each. A failure against one URL does not
// skip the rest.
func (s *Scheduler) CheckAll(ctx context.Context) error {
	if s.store == nil {
		return errors.New("probe: target store is nil")
	}
	if s.results == nil {
		return errors.New("probe: result store is nil")
	}

	list, err := s.store.List()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}

	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup
	for _, t := range list {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t targets.Target) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			r := s.checkOne(ctx, t)
			if _, err := s.results.Append(r); err != nil {
				log.Printf("probe: persist %s: %v", t.ID, err)
				return
			}
			s.maybeNotify(ctx, r)
		}(t)
	}
	wg.Wait()
	return nil
}

func (s *Scheduler) maybeNotify(ctx context.Context, r Result) {
	if s.alerts == nil {
		return
	}
	var kind alert.Kind
	switch {
	case r.Availability == Down:
		kind = alert.KindDown
	case r.CertStatus == CertExpired:
		kind = alert.KindCertExpired
	case r.CertStatus == CertWarn:
		kind = alert.KindCertWarn
	default:
		return
	}
	ev := alert.Event{
		TargetID: r.TargetID,
		URL:      r.URL,
		Kind:     kind,
		Message:  r.Message,
	}
	if err := s.alerts.Notify(ctx, ev); err != nil && !errors.Is(err, alert.ErrNotImplemented) {
		log.Printf("probe: alert: %v", err)
	}
}
