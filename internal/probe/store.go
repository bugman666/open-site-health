package probe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	resultsFilename     = "probes.json"
	maxResultsPerTarget = 50
	defaultRecentLimit  = 50
	maxRecentLimit      = 200
)

// ErrNoResults is returned when a target has no stored probe records yet.
var ErrNoResults = errors.New("no probe results")

type filePayload struct {
	Results []Result `json:"results"`
}

// ResultStore is a file-backed ring of recent probe records.
type ResultStore struct {
	path string
	mu   sync.RWMutex
}

// OpenResults creates dataDir if needed and ensures an empty results file exists.
func OpenResults(dataDir string) (*ResultStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, resultsFilename)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeResultsFile(path, []Result{}); err != nil {
			return nil, fmt.Errorf("init probes file: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	return &ResultStore{path: path}, nil
}

// Path returns the backing file (useful in tests and /healthz).
func (s *ResultStore) Path() string {
	return s.path
}

// Append persists one result and drops older rows for that target
// once maxResultsPerTarget is exceeded.
func (s *ResultStore) Append(r Result) (Result, error) {
	if r.ID == "" {
		id, err := newID()
		if err != nil {
			return Result{}, err
		}
		r.ID = id
	}
	if r.CheckedAt.IsZero() {
		r.CheckedAt = time.Now().UTC()
	} else {
		r.CheckedAt = r.CheckedAt.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadLocked()
	if err != nil {
		return Result{}, err
	}
	list = append(list, r)
	list = trimPerTarget(list, maxResultsPerTarget)
	if err := s.saveLocked(list); err != nil {
		return Result{}, err
	}
	return r, nil
}

// ListByTarget returns records for one target, newest first.
func (s *ResultStore) ListByTarget(targetID string) ([]Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0)
	for _, r := range list {
		if r.TargetID == targetID {
			out = append(out, r)
		}
	}
	sortResultsNewestFirst(out)
	return out, nil
}

// Latest returns the most recent result for a target.
func (s *ResultStore) Latest(targetID string) (Result, error) {
	list, err := s.ListByTarget(targetID)
	if err != nil {
		return Result{}, err
	}
	if len(list) == 0 {
		return Result{}, fmt.Errorf("%w: %s", ErrNoResults, targetID)
	}
	return list[0], nil
}

// ListRecent returns the newest results across all targets, capped at limit.
func (s *ResultStore) ListRecent(limit int) ([]Result, error) {
	if limit <= 0 {
		limit = defaultRecentLimit
	}
	if limit > maxRecentLimit {
		limit = maxRecentLimit
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(list))
	copy(out, list)
	sortResultsNewestFirst(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *ResultStore) loadLocked() ([]Result, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read probes: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse probes: %w", err)
	}
	if payload.Results == nil {
		payload.Results = []Result{}
	}
	return payload.Results, nil
}

func (s *ResultStore) saveLocked(list []Result) error {
	return writeResultsFile(s.path, list)
}

func writeResultsFile(path string, list []Result) error {
	if list == nil {
		list = []Result{}
	}
	payload, err := json.MarshalIndent(filePayload{Results: list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func trimPerTarget(list []Result, maxPer int) []Result {
	if maxPer <= 0 {
		return list
	}
	counts := make(map[string]int, len(list))
	sortResultsNewestFirst(list)
	kept := make([]Result, 0, len(list))
	for _, r := range list {
		if counts[r.TargetID] >= maxPer {
			continue
		}
		counts[r.TargetID]++
		kept = append(kept, r)
	}
	// Persist oldest-first so a file read looks chronological.
	sort.SliceStable(kept, func(i, j int) bool {
		if !kept[i].CheckedAt.Equal(kept[j].CheckedAt) {
			return kept[i].CheckedAt.Before(kept[j].CheckedAt)
		}
		return kept[i].ID < kept[j].ID
	})
	return kept
}

func sortResultsNewestFirst(list []Result) {
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CheckedAt.Equal(list[j].CheckedAt) {
			return list[i].CheckedAt.After(list[j].CheckedAt)
		}
		return list[i].ID > list[j].ID
	})
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
