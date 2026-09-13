// Package httpapi serves the process health endpoint and (later) target APIs.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

// Server is the HTTP front door of the process.
type Server struct {
	cfg    config.Config
	store  *targets.Store
	alerts *alert.Dispatcher
	mux    *http.ServeMux
}

// New registers the routes that exist in this skeleton.
func New(cfg config.Config, store *targets.Store, alerts *alert.Dispatcher) *Server {
	s := &Server{cfg: cfg, store: store, alerts: alerts, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /", s.handleRoot)
	return s
}

// Handler exposes the mux for tests.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAndServe starts the HTTP server and shuts it down when ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	httpSrv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("http listening on %s", s.cfg.Listen)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		err := <-errCh
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

type healthResponse struct {
	Status  string            `json:"status"`
	Service string            `json:"service"`
	Modules map[string]string `json:"modules"`
	Store   string            `json:"store"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if _, err := s.store.List(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	alertState := "stub"
	if s.alerts != nil && s.alerts.Ready() {
		alertState = "stub_configured"
	}

	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Service: "open-site-health",
		Modules: map[string]string{
			"targets": "stub",     // TODO(#1)
			"probe":   "stub",     // TODO(#2)
			"alert":   alertState, // TODO(#3)
		},
		Store: s.store.Path(),
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "open-site-health",
		"health":  "/healthz",
		"docs":    "https://github.com/bugman666/open-site-health",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
