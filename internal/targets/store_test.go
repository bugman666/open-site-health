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

func TestMutatorsAreStubs(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("https://example.org"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Add: %v", err)
	}
	if _, err := store.Update("id", "https://example.org"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Update: %v", err)
	}
	if err := store.Delete("id"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Delete: %v", err)
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
