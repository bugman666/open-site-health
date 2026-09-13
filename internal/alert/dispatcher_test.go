package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bugman666/open-site-health/internal/config"
)

func testCfg(t *testing.T, webhook string) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Alert.WebhookURL = webhook
	cfg.Alert.Cooldown = time.Hour
	return cfg
}

type hookRecorder struct {
	mu       sync.Mutex
	payloads []Payload
	status   int32
}

func (h *hookRecorder) handler() http.Handler {
	if atomic.LoadInt32(&h.status) == 0 {
		atomic.StoreInt32(&h.status, http.StatusOK)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var p Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.payloads = append(h.payloads, p)
		h.mu.Unlock()
		w.WriteHeader(int(atomic.LoadInt32(&h.status)))
	})
}

func (h *hookRecorder) got() []Payload {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Payload, len(h.payloads))
	copy(out, h.payloads)
	return out
}

func newHook(t *testing.T) (*hookRecorder, *httptest.Server) {
	t.Helper()
	rec := &hookRecorder{}
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)
	return rec, srv
}

func downEvent() Event {
	return Event{
		TargetID: "tgt-1",
		URL:      "https://example.org/donate",
		Kind:     KindDown,
		Message:  "HTTP 502",
	}
}

func TestReadyWhenWebhookSet(t *testing.T) {
	cfg := config.Defaults()
	if New(cfg).Ready() {
		t.Fatal("empty config should not be ready")
	}
	cfg.Alert.WebhookURL = "https://example.org/hook"
	if !New(cfg).Ready() {
		t.Fatal("expected ready")
	}
}

func TestReadyWhenSMTPSet(t *testing.T) {
	cfg := config.Defaults()
	cfg.Alert.SMTPHost = "smtp.example.org"
	cfg.Alert.To = "ops@example.org"
	if !New(cfg).Ready() {
		t.Fatal("expected ready with smtp host and to")
	}
}

func TestNotifyNoopWithoutChannel(t *testing.T) {
	d := New(config.Defaults())
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
}

// TC3.1: a configured webhook receives the target URL and fault kind.
func TestWebhookDeliversTargetAndKind(t *testing.T) {
	rec, srv := newHook(t)
	d := New(testCfg(t, srv.URL))

	ev := downEvent()
	if err := d.Notify(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	got := rec.got()
	if len(got) != 1 {
		t.Fatalf("want 1 delivery, got %#v", got)
	}
	if got[0].URL != ev.URL {
		t.Fatalf("url: %q", got[0].URL)
	}
	if got[0].Kind != KindDown {
		t.Fatalf("kind: %s", got[0].Kind)
	}
	if got[0].TargetID != ev.TargetID {
		t.Fatalf("target_id: %s", got[0].TargetID)
	}
	if got[0].Service != "open-site-health" {
		t.Fatalf("service: %s", got[0].Service)
	}
	if got[0].Message != ev.Message {
		t.Fatalf("message: %s", got[0].Message)
	}
}

// TC3.3: certificate alerts are a distinct kind from downtime.
func TestCertKindsDistinctFromDown(t *testing.T) {
	rec, srv := newHook(t)
	cfg := testCfg(t, srv.URL)
	cfg.Alert.Cooldown = time.Hour
	d := New(cfg)

	base := Event{TargetID: "tgt-cert", URL: "https://docs.example.org"}
	for _, kind := range []Kind{KindCertWarn, KindCertExpired, KindDown} {
		ev := base
		ev.Kind = kind
		ev.Message = string(kind)
		if err := d.Notify(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	got := rec.got()
	if len(got) != 3 {
		t.Fatalf("want 3 deliveries (kind changes), got %#v", got)
	}
	if got[0].Kind != KindCertWarn || got[1].Kind != KindCertExpired || got[2].Kind != KindDown {
		t.Fatalf("kinds: %#v", got)
	}
	for _, p := range got {
		if p.URL != base.URL {
			t.Fatalf("url missing: %#v", p)
		}
		if p.Kind == "" {
			t.Fatal("empty kind")
		}
	}
	if got[0].Kind == KindDown {
		t.Fatal("cert warn must not look like downtime")
	}
}

// TC3.4: the same unresolved fault is not resent inside the cooldown.
func TestCooldownSuppressesRepeat(t *testing.T) {
	rec, srv := newHook(t)
	cfg := testCfg(t, srv.URL)
	cfg.Alert.Cooldown = 30 * time.Minute
	d := New(cfg)

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }

	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 1 {
		t.Fatalf("repeat inside cooldown: got %d", n)
	}

	now = now.Add(25 * time.Minute)
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 2 {
		t.Fatalf("after cooldown should send again: got %d", n)
	}
}

func TestKindChangeBypassesCooldown(t *testing.T) {
	rec, srv := newHook(t)
	d := New(testCfg(t, srv.URL))
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }

	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	cert := downEvent()
	cert.Kind = KindCertExpired
	cert.Message = "leaf expired"
	if err := d.Notify(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	got := rec.got()
	if len(got) != 2 {
		t.Fatalf("kind change should send: %#v", got)
	}
	if got[1].Kind != KindCertExpired {
		t.Fatalf("second kind: %s", got[1].Kind)
	}
}

// TC3.5: a previously alerted target emits one recovery notice, then stays quiet.
func TestRecoveryNotice(t *testing.T) {
	rec, srv := newHook(t)
	d := New(testCfg(t, srv.URL))

	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	recov := Event{TargetID: "tgt-1", URL: "https://example.org/donate", Kind: KindRecovered, Message: "target recovered"}
	if err := d.Notify(context.Background(), recov); err != nil {
		t.Fatal(err)
	}
	if err := d.Notify(context.Background(), recov); err != nil {
		t.Fatal(err)
	}
	got := rec.got()
	if len(got) != 2 {
		t.Fatalf("want down + one recovery, got %#v", got)
	}
	if got[1].Kind != KindRecovered {
		t.Fatalf("recovery kind: %s", got[1].Kind)
	}

	// A new fault after recovery must send immediately.
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 3 {
		t.Fatalf("fault after recovery: got %d", n)
	}
}

func TestRecoverySkippedWithoutPriorAlert(t *testing.T) {
	rec, srv := newHook(t)
	d := New(testCfg(t, srv.URL))
	ev := Event{TargetID: "never-down", URL: "https://ok.example", Kind: KindRecovered}
	if err := d.Notify(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 0 {
		t.Fatalf("unexpected recovery: %#v", rec.got())
	}
}

func TestFailedDeliveryDoesNotConsumeCooldown(t *testing.T) {
	rec, srv := newHook(t)
	atomic.StoreInt32(&rec.status, http.StatusBadGateway)
	d := New(testCfg(t, srv.URL))

	if err := d.Notify(context.Background(), downEvent()); err == nil {
		t.Fatal("expected webhook error")
	}
	if n := len(rec.got()); n != 1 {
		t.Fatalf("receiver saw %d", n)
	}

	atomic.StoreInt32(&rec.status, http.StatusOK)
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 2 {
		t.Fatalf("retry after failure: got %d", n)
	}
}

func TestCooldownPersistsAcrossReopen(t *testing.T) {
	rec, srv := newHook(t)
	cfg := testCfg(t, srv.URL)
	cfg.Alert.Cooldown = time.Hour

	d1, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 15, 0, 0, 0, time.UTC)
	d1.now = func() time.Time { return now }
	if err := d1.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}

	d2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d2.now = func() time.Time { return now.Add(5 * time.Minute) }
	if err := d2.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.got()); n != 1 {
		t.Fatalf("reopened dispatcher should still be in cooldown, got %d", n)
	}
}

func TestSMTPChannelWithInjectedMailer(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Alert.SMTPHost = "smtp.example.org"
	cfg.Alert.SMTPFrom = "osh@example.org"
	cfg.Alert.To = "ops@example.org"
	cfg.Alert.Cooldown = time.Hour

	var got []Event
	d := New(cfg)
	d.Mailer = func(_ context.Context, ev Event) error {
		got = append(got, ev)
		return nil
	}
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if err := d.Notify(context.Background(), downEvent()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("smtp cooldown: %#v", got)
	}
	if got[0].Kind != KindDown || got[0].URL != downEvent().URL {
		t.Fatalf("mail event: %#v", got[0])
	}

	subject, body := formatMail(downEvent())
	if subject == "" || body == "" {
		t.Fatal("empty mail text")
	}
	if !strings.Contains(subject, "down") || !strings.Contains(subject, "example.org") {
		t.Fatalf("subject: %q", subject)
	}
	if !strings.Contains(body, "down") || !strings.Contains(body, "https://example.org/donate") || !strings.Contains(body, "tgt-1") {
		t.Fatalf("body: %q", body)
	}
}
