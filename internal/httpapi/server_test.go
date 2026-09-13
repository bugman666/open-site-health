package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := targets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	return New(cfg, store, alert.New(cfg))
}

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
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
	if body.Modules["targets"] != "ok" || body.Modules["probe"] != "stub" {
		t.Fatalf("modules: %#v", body.Modules)
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
