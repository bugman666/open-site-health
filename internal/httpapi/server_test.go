package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/probe"
	"github.com/bugman666/open-site-health/internal/targets"
)

func newTestEnv(t *testing.T) (*Server, *targets.Store, *probe.ResultStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := probe.OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	store.AllowPrivate = true
	return New(cfg, store, results, alert.New(cfg)), store, results
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, _, _ := newTestEnv(t)
	return srv
}

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONAuth(t, srv, method, path, "", body)
}

func doJSONAuth(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthzOK(t *testing.T) {
	srv := newTestServer(t)

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
	if body.Modules["targets"] != "ok" || body.Modules["probe"] != "ok" {
		t.Fatalf("modules: %#v", body.Modules)
	}
	if body.Modules["alert"] != "disabled" {
		t.Fatalf("alert without channel: %#v", body.Modules)
	}
}

func TestHealthzAlertReady(t *testing.T) {
	dir := t.TempDir()
	store, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := probe.OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Alert.WebhookURL = "http://127.0.0.1:9/hook"
	srv := New(cfg, store, results, alert.New(cfg))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Modules["alert"] != "ok" {
		t.Fatalf("alert: %#v", body.Modules)
	}
}

func TestRoot(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TC1.1 / TC1.4 / TC1.5 over HTTP: create, list, update, delete.
func TestTargetsCRUD(t *testing.T) {
	srv := newTestServer(t)

	create := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": "https://example.org"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var created targets.Target
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.URL != "https://example.org" {
		t.Fatalf("created: %#v", created)
	}

	listRec := doJSON(t, srv, http.MethodGet, "/targets", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", listRec.Code, listRec.Body.String())
	}
	var listed targetListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Targets) != 1 || listed.Targets[0].ID != created.ID {
		t.Fatalf("list: %#v", listed)
	}

	getRec := doJSON(t, srv, http.MethodGet, "/targets/"+created.ID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", getRec.Code, getRec.Body.String())
	}

	update := doJSON(t, srv, http.MethodPut, "/targets/"+created.ID, map[string]string{"url": "https://example.net"})
	if update.Code != http.StatusOK {
		t.Fatalf("update: %d %s", update.Code, update.Body.String())
	}
	var updated targets.Target
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.URL != "https://example.net" {
		t.Fatalf("updated: %#v", updated)
	}

	badUpdate := doJSON(t, srv, http.MethodPut, "/targets/"+created.ID, map[string]string{"url": "not-a-url"})
	if badUpdate.Code != http.StatusBadRequest {
		t.Fatalf("illegal update status: %d %s", badUpdate.Code, badUpdate.Body.String())
	}

	del := doJSON(t, srv, http.MethodDelete, "/targets/"+created.ID, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", del.Code, del.Body.String())
	}

	empty := doJSON(t, srv, http.MethodGet, "/targets", nil)
	var after targetListResponse
	if err := json.Unmarshal(empty.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Targets) != 0 {
		t.Fatalf("list after delete: %#v", after)
	}

	missing := doJSON(t, srv, http.MethodGet, "/targets/"+created.ID, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get deleted: %d %s", missing.Code, missing.Body.String())
	}
}

// TC1.2: invalid URLs return a clear 400 and do not change the list.
func TestCreateRejectsInvalidURL(t *testing.T) {
	srv := newTestServer(t)
	if rec := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": "https://example.org"}); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	for _, raw := range []string{"", "not-a-url", "example.org"} {
		rec := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": raw})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("url %q: status %d body %s", raw, rec.Code, rec.Body.String())
		}
		var errBody errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
			t.Fatal(err)
		}
		if errBody.Error == "" {
			t.Fatalf("url %q: empty error body", raw)
		}
	}

	listRec := doJSON(t, srv, http.MethodGet, "/targets", nil)
	var listed targetListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Targets) != 1 {
		t.Fatalf("list should be unchanged: %#v", listed)
	}
}

// TC1.3: a second create of the same normalized URL is 409.
func TestCreateRejectsDuplicate(t *testing.T) {
	srv := newTestServer(t)
	if rec := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": "https://example.org"}); rec.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}

	dup := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": "HTTPS://EXAMPLE.ORG/"})
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate status: %d %s", dup.Code, dup.Body.String())
	}
	var errBody errorBody
	if err := json.Unmarshal(dup.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error == "" {
		t.Fatal("expected duplicate error text")
	}

	listRec := doJSON(t, srv, http.MethodGet, "/targets", nil)
	var listed targetListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Targets) != 1 {
		t.Fatalf("expected one target, got %#v", listed)
	}
}

func TestListOrderIsReproducible(t *testing.T) {
	srv := newTestServer(t)
	urls := []string{"https://c.example.org", "https://a.example.org", "https://b.example.org"}
	for _, u := range urls {
		if rec := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": u}); rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", u, rec.Code, rec.Body.String())
		}
	}

	first := doJSON(t, srv, http.MethodGet, "/targets", nil)
	second := doJSON(t, srv, http.MethodGet, "/targets", nil)
	if first.Body.String() != second.Body.String() {
		t.Fatalf("list not stable:\n%s\n%s", first.Body.String(), second.Body.String())
	}

	var listed targetListResponse
	if err := json.Unmarshal(first.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Targets) != 3 {
		t.Fatalf("got %#v", listed)
	}
	for i, u := range urls {
		if listed.Targets[i].URL != u {
			t.Fatalf("index %d: got %s want %s", i, listed.Targets[i].URL, u)
		}
	}
}

// TC2.6: after probes have run, history and latest status expose time,
// availability, and certificate fields.
func TestProbeResultsQueryable(t *testing.T) {
	srv, store, results := newTestEnv(t)
	cfg := config.Defaults()
	cfg.AllowPrivateTargets = true
	sched := probe.New(cfg, store, results, alert.New(cfg))
	sched.Timeout = time.Second

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(upstream.Close)

	create := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": upstream.URL})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var created targets.Target
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	emptyStatus := doJSON(t, srv, http.MethodGet, "/targets/"+created.ID+"/status", nil)
	if emptyStatus.Code != http.StatusNotFound {
		t.Fatalf("status before probe: %d %s", emptyStatus.Code, emptyStatus.Body.String())
	}

	if err := sched.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sched.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	hist := doJSON(t, srv, http.MethodGet, "/targets/"+created.ID+"/probes", nil)
	if hist.Code != http.StatusOK {
		t.Fatalf("history: %d %s", hist.Code, hist.Body.String())
	}
	var listed probeListResponse
	if err := json.Unmarshal(hist.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Results) < 2 {
		t.Fatalf("expected at least two results, got %#v", listed)
	}
	for _, r := range listed.Results {
		if r.CheckedAt.IsZero() || r.Availability == "" || r.CertStatus == "" {
			t.Fatalf("missing fields: %#v", r)
		}
		if r.Availability != probe.Up {
			t.Fatalf("availability: %s", r.Availability)
		}
		if r.CertStatus != probe.CertNA {
			t.Fatalf("http cert: %s", r.CertStatus)
		}
		if r.TargetID != created.ID {
			t.Fatalf("target id: %s", r.TargetID)
		}
	}

	statusRec := doJSON(t, srv, http.MethodGet, "/targets/"+created.ID+"/status", nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", statusRec.Code, statusRec.Body.String())
	}
	var latest probe.Result
	if err := json.Unmarshal(statusRec.Body.Bytes(), &latest); err != nil {
		t.Fatal(err)
	}
	if latest.ID != listed.Results[0].ID {
		t.Fatalf("status should be newest history row: %#v vs %#v", latest, listed.Results[0])
	}

	global := doJSON(t, srv, http.MethodGet, "/probes?target_id="+created.ID, nil)
	if global.Code != http.StatusOK {
		t.Fatalf("global: %d %s", global.Code, global.Body.String())
	}

	missing := doJSON(t, srv, http.MethodGet, "/targets/does-not-exist/probes", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing target: %d %s", missing.Code, missing.Body.String())
	}
}

func TestCreateRejectsBlockedDestination(t *testing.T) {
	srv, store, _ := newTestEnv(t)
	store.AllowPrivate = false

	for _, raw := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.8/",
		"http://localhost/",
	} {
		rec := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": raw})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("url %q: status %d body %s", raw, rec.Code, rec.Body.String())
		}
	}

	ok := doJSON(t, srv, http.MethodPost, "/targets", map[string]string{"url": "https://example.org"})
	if ok.Code != http.StatusCreated {
		t.Fatalf("public url: %d %s", ok.Code, ok.Body.String())
	}
}

func newAuthedEnv(t *testing.T, token string) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.AllowPrivate = true
	results, err := probe.OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.APIToken = token
	return New(cfg, store, results, alert.New(cfg))
}

func TestAuthRequiredOnSensitiveRoutes(t *testing.T) {
	const token = "test-token"
	srv := newAuthedEnv(t, token)

	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(healthRec, health)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("healthz should stay public: %d", healthRec.Code)
	}

	root := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rootRec, root)
	if rootRec.Code != http.StatusOK {
		t.Fatalf("GET / should stay public: %d", rootRec.Code)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/targets", nil},
		{http.MethodPost, "/targets", map[string]string{"url": "https://example.org"}},
		{http.MethodGet, "/targets/abc", nil},
		{http.MethodPut, "/targets/abc", map[string]string{"url": "https://example.net"}},
		{http.MethodDelete, "/targets/abc", nil},
		{http.MethodGet, "/probes", nil},
		{http.MethodGet, "/targets/abc/probes", nil},
		{http.MethodGet, "/targets/abc/status", nil},
	} {
		rec := doJSON(t, srv, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	wrong := doJSONAuth(t, srv, http.MethodPost, "/targets", "nope", map[string]string{"url": "https://example.org"})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: %d %s", wrong.Code, wrong.Body.String())
	}

	created := doJSONAuth(t, srv, http.MethodPost, "/targets", token, map[string]string{"url": "https://example.org"})
	if created.Code != http.StatusCreated {
		t.Fatalf("bearer create: %d %s", created.Code, created.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("X-API-Key list: %d %s", rec.Code, rec.Body.String())
	}
}
