package alert

import (
	"context"
	"errors"
	"testing"

	"github.com/bugman666/open-site-health/internal/config"
)

func TestNotifyIsStub(t *testing.T) {
	d := New(config.Defaults())
	if d.Ready() {
		t.Fatal("empty config should not be ready")
	}
	if err := d.Notify(context.Background(), Event{Kind: KindDown}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("got %v", err)
	}
}

func TestReadyWhenWebhookSet(t *testing.T) {
	cfg := config.Defaults()
	cfg.Alert.WebhookURL = "https://example.org/hook"
	if !New(cfg).Ready() {
		t.Fatal("expected ready")
	}
}
