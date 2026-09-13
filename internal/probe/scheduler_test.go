package probe

import (
	"context"
	"errors"
	"testing"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

func TestCheckAllIsStub(t *testing.T) {
	store, err := targets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	s := New(cfg, store, alert.New(cfg))
	if err := s.CheckAll(context.Background()); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("got %v", err)
	}
}
