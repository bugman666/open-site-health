package targets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndListEmpty(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %#v", list)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("backing file missing: %v", err)
	}
}

func TestListReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filename)
	body := `{"targets":[{"id":"t1","url":"https://example.org"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].URL != "https://example.org" {
		t.Fatalf("got %#v", list)
	}
}

// TC1.1: a valid URL is created, listed, and still present after reopen.
func TestAddValidTargetPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Add("https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.URL != "https://example.org" {
		t.Fatalf("created: %#v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps should be set: %#v", got)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != got.ID || list[0].URL != "https://example.org" {
		t.Fatalf("list after add: %#v", list)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].ID != got.ID || again[0].URL != "https://example.org" {
		t.Fatalf("list after reopen: %#v", again)
	}
}

// TC1.2: empty / missing scheme / junk URLs are rejected and the list is unchanged.
func TestAddRejectsInvalidURL(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("https://example.org"); err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{"", "   ", "not-a-url", "example.org", "ftp://example.org", "http://"} {
		_, err := store.Add(raw)
		if !errors.Is(err, ErrInvalidURL) {
			t.Fatalf("Add(%q): want ErrInvalidURL, got %v", raw, err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].URL != "https://example.org" {
		t.Fatalf("list should be unchanged: %#v", list)
	}
}

// TC1.3: the same URL after normalization is rejected; no second row.
func TestAddRejectsDuplicateNormalizedURL(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("https://example.org"); err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{
		"https://example.org",
		"HTTPS://EXAMPLE.ORG",
		"https://example.org/",
		"https://example.org:443",
		"  https://example.org  ",
	} {
		_, err := store.Add(raw)
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("Add(%q): want ErrDuplicate, got %v", raw, err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one target, got %#v", list)
	}
}

// TC1.4: a legal update is persisted; an illegal one is refused.
func TestUpdateTarget(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Add("https://example.org")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.Update(a.ID, "https://example.net/status")
	if err != nil {
		t.Fatal(err)
	}
	if updated.URL != "https://example.net/status" {
		t.Fatalf("updated url: %#v", updated)
	}
	if !updated.UpdatedAt.After(updated.CreatedAt) && !updated.UpdatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("updated_at should not precede created_at: %#v", updated)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].URL != "https://example.net/status" {
		t.Fatalf("persisted update: %#v", list)
	}

	if _, err := store.Update(a.ID, "not-a-url"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("illegal update: %v", err)
	}
	list, err = store.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].URL != "https://example.net/status" {
		t.Fatalf("illegal update mutated store: %#v", list)
	}

	if _, err := store.Update("missing", "https://example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestUpdateRejectsDuplicateURL(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Add("https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Add("https://example.net")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Update(b.ID, "https://EXAMPLE.ORG"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("update into existing url: %v", err)
	}
	// Same URL on the same row is a no-op success.
	same, err := store.Update(a.ID, "https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	if same.URL != "https://example.org" {
		t.Fatalf("got %#v", same)
	}
}

// TC1.5: delete drops the row from List (what the probe loop reads).
func TestDeleteRemovesTarget(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Add("https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("https://example.net"); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].URL != "https://example.net" {
		t.Fatalf("after delete: %#v", list)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].URL != "https://example.net" {
		t.Fatalf("after reopen: %#v", again)
	}
	if _, err := reopened.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted id still gettable: %v", err)
	}
}

// List order is insertion (created_at) then id, and the same after reopen.
func TestListOrderIsStable(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	urls := []string{
		"https://c.example.org",
		"https://a.example.org",
		"https://b.example.org",
	}
	var ids []string
	for _, u := range urls {
		got, err := store.Add(u)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, got.ID)
	}

	assertOrder := func(t *testing.T, list []Target) {
		t.Helper()
		if len(list) != 3 {
			t.Fatalf("len=%d %#v", len(list), list)
		}
		for i, u := range urls {
			if list[i].URL != u || list[i].ID != ids[i] {
				t.Fatalf("index %d: got id=%s url=%s", i, list[i].ID, list[i].URL)
			}
		}
	}

	first, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, first)

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, second)
}
