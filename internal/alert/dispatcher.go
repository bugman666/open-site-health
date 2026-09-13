// Package alert is the notification placeholder (email or Webhook).
//
// TODO(#3): deliver at least one channel (SMTP or HTTP POST), include target
// URL and fault kind, and apply dedupe/cooldown so the same unresolved
// incident does not flood the inbox. Recovery notices are optional for MVP.
package alert

import (
	"context"
	"errors"
	"log"

	"github.com/bugman666/open-site-health/internal/config"
)

// ErrNotImplemented is returned by Notify until #3 is done.
var ErrNotImplemented = errors.New("alert: email/webhook delivery not implemented yet; see issue #3")

// Kind distinguishes outage alerts from certificate alerts.
type Kind string

const (
	KindDown        Kind = "down"
	KindCertWarn    Kind = "cert_warn"
	KindCertExpired Kind = "cert_expired"
	KindRecovered   Kind = "recovered"
)

// Event is one candidate notification.
type Event struct {
	TargetID string
	URL      string
	Kind     Kind
	Message  string
}

// Dispatcher will send mail or webhooks after cooldown checks.
type Dispatcher struct {
	cfg config.Config
}

// New keeps alert settings on hand for later delivery work.
func New(cfg config.Config) *Dispatcher {
	return &Dispatcher{cfg: cfg}
}

// Ready reports whether any delivery channel is configured.
// The skeleton never sends; this only helps /healthz describe the stub.
func (d *Dispatcher) Ready() bool {
	return d.cfg.Alert.WebhookURL != "" || d.cfg.Alert.SMTPHost != ""
}

// Notify will send (or suppress) one event.
//
// TODO(#3): pick webhook or SMTP, skip if the same target+kind is still
// inside cfg.Alert.Cooldown, and allow a new send on kind change or recovery.
func (d *Dispatcher) Notify(_ context.Context, _ Event) error {
	return ErrNotImplemented
}

// LogStub notes at startup that alerting is not wired yet.
func (d *Dispatcher) LogStub() {
	channel := "none"
	if d.cfg.Alert.WebhookURL != "" {
		channel = "webhook"
	} else if d.cfg.Alert.SMTPHost != "" {
		channel = "smtp"
	}
	log.Printf("alert stub idle: channel=%s cooldown=%s (see #3)", channel, d.cfg.Alert.Cooldown)
}
