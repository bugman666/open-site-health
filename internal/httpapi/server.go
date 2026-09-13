// Package httpapi serves process health, the target registry, and probe results.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/probe"
	"github.com/bugman666/open-site-health/internal/targets"
)

// Server is the HTTP front door of the process.
type Server struct {
	cfg     config.Config
	store   *targets.Store
	results *probe.ResultStore
	alerts  *alert.Dispatcher
	mux     *http.ServeMux
}

// New registers health, target CRUD, and probe query routes.
func New(cfg config.Config, store *targets.Store, results *probe.ResultStore, alerts *alert.Dispatcher) *Server {
	s := &Server{cfg: cfg, store: store, results: results, alerts: alerts, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /targets", s.handleListTargets)
	s.mux.HandleFunc("POST /targets", s.handleCreateTarget)
	s.mux.HandleFunc("GET /targets/{id}", s.handleGetTarget)
	s.mux.HandleFunc("PUT /targets/{id}", s.handleUpdateTarget)
	s.mux.HandleFunc("DELETE /targets/{id}", s.handleDeleteTarget)
	s.mux.HandleFunc("GET /targets/{id}/probes", s.handleTargetProbes)
	s.mux.HandleFunc("GET /targets/{id}/status", s.handleTargetStatus)
	s.mux.HandleFunc("GET /probes", s.handleListProbes)
	return s
}

// Handler exposes the mux for tests, with auth applied to sensitive routes.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if needsAuth(r.URL.Path) && !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="open-site-health"`)
			writeJSON(w, http.StatusUnauthorized, errorBody{Error: "unauthorized"})
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func needsAuth(path string) bool {
	return path == "/targets" || path == "/probes" ||
		strings.HasPrefix(path, "/targets/") || strings.HasPrefix(path, "/probes/")
}

func (s *Server) authorized(r *http.Request) bool {
	want := s.cfg.APIToken
	if want == "" {
		return true
	}
	got := requestToken(r)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		const prefix = "Bearer "
		if len(h) >= len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
			return strings.TrimSpace(h[len(prefix):])
		}
	}
	if t := strings.TrimSpace(r.Header.Get("X-API-Key")); t != "" {
		return t
	}
	return ""
}

// ListenAndServe starts the HTTP server and shuts it down when ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	httpSrv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if s.cfg.APIToken != "" {
			log.Printf("http listening on %s (API token required for /targets and /probes)", s.cfg.Listen)
		} else {
			log.Printf("http listening on %s (no API token; loopback-only trust)", s.cfg.Listen)
		}
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

type probeListResponse struct {
	Results []probe.Result `json:"results"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if _, err := s.store.List(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{
			Error: err.Error(),
		})
		return
	}
	if s.results != nil {
		if _, err := s.results.ListRecent(1); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				Error: err.Error(),
			})
			return
		}
	}

	alertState := "disabled"
	if s.alerts != nil && s.alerts.Ready() {
		alertState = "ok"
	}
	probeState := "ok"
	if s.results == nil {
		probeState = "stub"
	}

	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Service: "open-site-health",
		Modules: map[string]string{
			"targets": "ok",
			"probe":   probeState,
			"alert":   alertState,
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
		"probes":  "/probes",
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

func (s *Server) handleTargetProbes(w http.ResponseWriter, r *http.Request) {
	if s.results == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "probe store unavailable"})
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.Get(id); err != nil {
		writeStoreError(w, err)
		return
	}
	list, err := s.results.ListByTarget(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
		return
	}
	if list == nil {
		list = []probe.Result{}
	}
	writeJSON(w, http.StatusOK, probeListResponse{Results: list})
}

func (s *Server) handleTargetStatus(w http.ResponseWriter, r *http.Request) {
	if s.results == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "probe store unavailable"})
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.Get(id); err != nil {
		writeStoreError(w, err)
		return
	}
	got, err := s.results.Latest(id)
	if err != nil {
		if errors.Is(err, probe.ErrNoResults) {
			writeJSON(w, http.StatusNotFound, errorBody{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleListProbes(w http.ResponseWriter, r *http.Request) {
	if s.results == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "probe store unavailable"})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "limit must be a positive integer"})
			return
		}
		limit = n
	}

	var (
		list []probe.Result
		err  error
	)
	if id := r.URL.Query().Get("target_id"); id != "" {
		list, err = s.results.ListByTarget(id)
	} else {
		list, err = s.results.ListRecent(limit)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
		return
	}
	if list == nil {
		list = []probe.Result{}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	writeJSON(w, http.StatusOK, probeListResponse{Results: list})
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
	case errors.Is(err, probe.ErrNoResults):
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
