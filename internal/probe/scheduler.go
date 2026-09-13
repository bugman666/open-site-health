// Package probe is the scheduled HTTP(S) + TLS certificate check placeholder.
//
// TODO(#2): on each tick, list targets, probe availability (timeout / 5xx → down,
// 2xx → up), inspect certificate expiry (warn vs expired, distinct from down),
// and persist results so they can be queried.
package probe

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

// ErrNotImplemented is returned by CheckAll until #2 is done.
var ErrNotImplemented = errors.New("probe: HTTP(S) and TLS checks not implemented yet; see issue #2")

// Availability is the connectivity outcome of one probe.
type Availability string

const (
	Up   Availability = "up"
	Down Availability = "down"
)

// CertStatus is independent of Availability so a live site with an
// expiring certificate is not reported as down.
type CertStatus string

const (
	CertOK      CertStatus = "ok"
	CertWarn    CertStatus = "warn"
	CertExpired CertStatus = "expired"
	CertNA      CertStatus = "n/a"
)

// Result is one check against one target. Not produced yet.
type Result struct {
	TargetID     string
	URL          string
	CheckedAt    time.Time
	Availability Availability
	CertStatus   CertStatus
	Message      string
}

// Scheduler will own the probe ticker. The stub only logs the configured
// interval so the process stays up without hitting the network.
type Scheduler struct {
	cfg    config.Config
	store  *targets.Store
	alerts *alert.Dispatcher
}

// New wires the pieces that #2 will use.
func New(cfg config.Config, store *targets.Store, alerts *alert.Dispatcher) *Scheduler {
	return &Scheduler{cfg: cfg, store: store, alerts: alerts}
}

// Start keeps the process alive and records that probing is still a stub.
// Default interval is 5m, matching the documented 50–200 URL load.
func (s *Scheduler) Start(ctx context.Context) {
	log.Printf("probe stub idle: interval=%s tls_warn_days=%d (see #2)", s.cfg.ProbeInterval, s.cfg.TLSWarnDays)
	<-ctx.Done()
}

// CheckAll will run one probe pass over every registered target.
//
// TODO(#2): implement HTTP(S) GET with timeout, TLS expiry vs cfg.TLSWarnDays,
// store results, and call alerts.Notify on state changes.
func (s *Scheduler) CheckAll(_ context.Context) error {
	return ErrNotImplemented
}
