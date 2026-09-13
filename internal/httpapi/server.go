// Package httpapi serves process health and the target registry API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// New registers health and target CRUD routes.
func New(cfg config.Config, store *targets.Store, alerts *alert.Dispatcher) *Server {
	s := &Server{cfg: cfg, store: store, alerts: alerts, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /targets", s.handleListTargets)
	s.mux.HandleFunc("POST /targets", s.handleCreateTarget)
	s.mux.HandleFunc("GET /targets/{id}", s.handleGetTarget)
	s.mux.HandleFunc("PUT /targets/{id}", s.handleUpdateTarget)
	s.mux.HandleFunc("DELETE /targets/{id}", s.handleDeleteTarget)
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

type errorBody struct {
	Error string `json:"error"`
}

type targetRequest struct {
	URL string `json:"url"`
}

type targetListResponse struct {
	Targets []targets.Target `json:"targets"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if _, err := s.store.List(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{
			Error: err.Error(),
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
			"targets": "ok",
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
		"targets": "/targets",
		"docs":    "https://github.com/bugman666/open-site-health",
	})
}

func (s *Server) handleListTargets(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targetListResponse{Targets: list})
}

func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	req, ok := readTargetRequest(w, r)
	if !ok {
		return
	}
	t, err := s.store.Add(req.URL)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Location", "/targets/"+t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTarget(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	req, ok := readTargetRequest(w, r)
	if !ok {
		return
	}
	t, err := s.store.Update(r.PathValue("id"), req.URL)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readTargetRequest(w http.ResponseWriter, r *http.Request) (targetRequest, bool) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "could not read body"})
		return targetRequest{}, false
	}
	var req targetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
		return targetRequest{}, false
	}
	return req, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, targets.ErrInvalidURL):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	case errors.Is(err, targets.ErrDuplicate):
		writeJSON(w, http.StatusConflict, errorBody{Error: err.Error()})
	case errors.Is(err, targets.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
