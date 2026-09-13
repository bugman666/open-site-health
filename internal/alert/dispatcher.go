// Package alert delivers downtime and certificate notifications.
//
// A webhook POST is the required channel; SMTP is used when host and
// recipient are set. The same target+kind is suppressed until the
// cooldown elapses or the kind changes (including recovery).
package alert

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bugman666/open-site-health/internal/config"
)

// Kind distinguishes outage alerts from certificate alerts.
type Kind string

const (
	KindDown        Kind = "down"
	KindCertWarn    Kind = "cert_warn"
	KindCertExpired Kind = "cert_expired"
	KindRecovered   Kind = "recovered"
	webhookTimeout       = 10 * time.Second
)

// Event is one candidate notification.
type Event struct {
	TargetID string
	URL      string
	Kind     Kind
	Message  string
}

// Payload is the JSON body posted to a webhook.
type Payload struct {
	Service  string `json:"service"`
	TargetID string `json:"target_id"`
	URL      string `json:"url"`
	Kind     Kind   `json:"kind"`
	Message  string `json:"message,omitempty"`
	SentAt   string `json:"sent_at"`
}

// Dispatcher sends mail or webhooks after cooldown checks.
type Dispatcher struct {
	cfg    config.Config
	state  *stateStore
	now    func() time.Time
	client *http.Client
	// Mailer, when set, replaces the default SMTP sender (tests).
	Mailer func(ctx context.Context, ev Event) error
}

// Open loads last-send state from cfg.DataDir so cooldown survives restarts.
func Open(cfg config.Config) (*Dispatcher, error) {
	st, err := openState(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return newDispatcher(cfg, st), nil
}

// New keeps cooldown in memory only. Tests that do not reopen the
// process can use this; production uses Open.
func New(cfg config.Config) *Dispatcher {
	return newDispatcher(cfg, newMemState())
}

func newDispatcher(cfg config.Config, st *stateStore) *Dispatcher {
	return &Dispatcher{
		cfg:   cfg,
		state: st,
		now:   func() time.Time { return time.Now().UTC() },
		client: &http.Client{
			Timeout: webhookTimeout,
		},
	}
}

// Ready reports whether any delivery channel is configured.
func (d *Dispatcher) Ready() bool {
	if d == nil {
		return false
	}
	return d.cfg.Alert.WebhookURL != "" || d.smtpReady()
}

func (d *Dispatcher) smtpReady() bool {
	return d.cfg.Alert.SMTPHost != "" && d.cfg.Alert.To != ""
}

func (d *Dispatcher) channel() string {
	switch {
	case d.cfg.Alert.WebhookURL != "" && d.smtpReady():
		return "webhook+smtp"
	case d.cfg.Alert.WebhookURL != "":
		return "webhook"
	case d.smtpReady():
		return "smtp"
	default:
		return "none"
	}
}

// LogReady notes at startup which channels will fire.
func (d *Dispatcher) LogReady() {
	log.Printf("alert ready: channel=%s cooldown=%s", d.channel(), d.cfg.Alert.Cooldown)
}

// Notify sends ev, or suppresses it when the same target+kind is still
// inside the cooldown window. A kind change (including recovery) always
// sends. Recovery is skipped if nothing was previously alerted.
func (d *Dispatcher) Notify(ctx context.Context, ev Event) error {
	if d == nil {
		return nil
	}
	if ev.Kind == "" {
		return fmt.Errorf("alert: empty kind")
	}
	if !d.Ready() {
		return nil
	}

	now := d.now()
	ok, err := d.shouldSend(ev, now)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := d.deliver(ctx, ev, now); err != nil {
		return err
	}
	return d.state.record(ev.TargetID, ev.Kind, now)
}

func (d *Dispatcher) shouldSend(ev Event, now time.Time) (bool, error) {
	last, found, err := d.state.last(ev.TargetID)
	if err != nil {
		return false, err
	}
	if ev.Kind == KindRecovered {
		if !found || last.Kind == KindRecovered {
			return false, nil
		}
		return true, nil
	}
	if !found {
		return true, nil
	}
	if last.Kind == ev.Kind && d.cfg.Alert.Cooldown > 0 && now.Sub(last.SentAt) < d.cfg.Alert.Cooldown {
		return false, nil
	}
	return true, nil
}

func (d *Dispatcher) deliver(ctx context.Context, ev Event, now time.Time) error {
	var first error
	sent := false
	if d.cfg.Alert.WebhookURL != "" {
		if err := d.postWebhook(ctx, ev, now); err != nil {
			first = fmt.Errorf("webhook: %w", err)
		} else {
			sent = true
		}
	}
	if d.smtpReady() {
		if err := d.sendMail(ctx, ev); err != nil {
			if first == nil {
				first = fmt.Errorf("smtp: %w", err)
			}
		} else {
			sent = true
		}
	}
	if sent {
		return nil
	}
	if first != nil {
		return first
	}
	return nil
}
