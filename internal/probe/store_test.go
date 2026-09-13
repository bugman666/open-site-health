package probe

import (
	"errors"
	"testing"
	"time"
)

func TestResultStoreAppendListLatest(t *testing.T) {
	store, err := OpenResults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t0 := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	first, err := store.Append(Result{
		TargetID:     "aaa",
		URL:          "https://a.example",
		CheckedAt:    t0,
		Availability: Up,
		CertStatus:   CertOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" {
		t.Fatal("expected id")
	}

	second, err := store.Append(Result{
		TargetID:     "aaa",
		URL:          "https://a.example",
		CheckedAt:    t0.Add(time.Minute),
		Availability: Down,
		CertStatus:   CertNA,
		Message:      "HTTP 502",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Append(Result{
		TargetID:     "bbb",
		URL:          "https://b.example",
		CheckedAt:    t0.Add(30 * time.Second),
		Availability: Up,
		CertStatus:   CertWarn,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListByTarget("aaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %#v", list)
	}
	if list[0].ID != second.ID || list[0].Availability != Down {
		t.Fatalf("newest first: %#v", list[0])
	}

	latest, err := store.Latest("aaa")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != second.ID {
		t.Fatalf("latest: %#v", latest)
	}

	if _, err := store.Latest("missing"); !errors.Is(err, ErrNoResults) {
		t.Fatalf("missing: %v", err)
	}
}

func TestResultStoreReopenByDir(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	written, err := store.Append(Result{
		TargetID:     "t1",
		URL:          "http://127.0.0.1/healthz",
		Availability: Up,
		CertStatus:   CertNA,
	})
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Latest("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != written.ID {
		t.Fatalf("got %#v want %#v", got, written)
	}
}

func TestResultStoreTrimsPerTarget(t *testing.T) {
	store, err := OpenResults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxResultsPerTarget+10; i++ {
		if _, err := store.Append(Result{
			TargetID:     "only",
			URL:          "https://example.org",
			CheckedAt:    base.Add(time.Duration(i) * time.Minute),
			Availability: Up,
			CertStatus:   CertOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListByTarget("only")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != maxResultsPerTarget {
		t.Fatalf("len=%d", len(list))
	}
	// Newest kept; the first 10 minutes should have been dropped.
	oldestKept := list[len(list)-1].CheckedAt
	if !oldestKept.After(base.Add(9 * time.Minute)) {
		t.Fatalf("oldest kept %s; expected trim of earliest rows", oldestKept)
	}
}

func TestListRecentCapsAndOrders(t *testing.T) {
	store, err := OpenResults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := store.Append(Result{
			TargetID:     "x",
			CheckedAt:    base.Add(time.Duration(i) * time.Second),
			Availability: Up,
			CertStatus:   CertOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListRecent(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d", len(list))
	}
	if !list[0].CheckedAt.After(list[1].CheckedAt) {
		t.Fatalf("not newest first: %#v", list)
	}
}
