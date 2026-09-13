package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

func TestHealthzOK(t *testing.T) {
	store, err := targets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	srv := New(cfg, store, alert.New(cfg))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body %s", rec.Code, rec.Body.String())
	}
	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Service != "open-site-health" {
		t.Fatalf("body: %#v", body)
	}
	if body.Modules["targets"] != "stub" || body.Modules["probe"] != "stub" {
		t.Fatalf("modules: %#v", body.Modules)
	}
}

func TestRoot(t *testing.T) {
	store, err := targets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(config.Defaults(), store, alert.New(config.Defaults()))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}
