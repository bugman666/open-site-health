// Package targets persists the list of URLs the service will probe.
//
// The store is a JSON file (data/targets.json) so the binary stays
// CGO-free and single-process. URLs are normalized before write:
// required http(s) scheme, lower-cased host, default ports stripped,
// duplicates rejected. The in-memory list is always sorted so List
// is stable across restarts.
package targets

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

const filename = "targets.json"

var (
	// ErrInvalidURL is returned when a URL is empty, unparseable, or
	// missing an http/https scheme and host.
	ErrInvalidURL = errors.New("invalid URL")
	// ErrDuplicate is returned when the normalized URL is already stored.
	ErrDuplicate = errors.New("duplicate target")
	// ErrNotFound is returned when an id is not in the store.
	ErrNotFound = errors.New("target not found")
)

// Target is one URL the service will probe.
type Target struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type filePayload struct {
	Targets []Target `json:"targets"`
}

// Store is a file-backed, restart-safe list of monitor targets.
type Store struct {
	path string
	mu   sync.RWMutex
}

// Open creates dataDir if needed and ensures an empty targets file exists.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, filename)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeFile(path, []Target{}); err != nil {
			return nil, fmt.Errorf("init targets file: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// Path returns the backing file (useful in tests and /healthz).
func (s *Store) Path() string {
	return s.path
}

// List returns a stable copy of the current targets (created_at, then id).
func (s *Store) List() ([]Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Target, len(list))
	copy(out, list)
	sortTargets(out)
	return out, nil
}

// Get returns one target by id.
func (s *Store) Get(id string) (Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.loadLocked()
	if err != nil {
		return Target{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return Target{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Add persists a new URL after validation and duplicate checks.
func (s *Store) Add(rawURL string) (Target, error) {
	normalized, err := normalizeURL(rawURL)
	if err != nil {
		return Target{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadLocked()
	if err != nil {
		return Target{}, err
	}
	for _, t := range list {
		if t.URL == normalized {
			return Target{}, fmt.Errorf("%w: %s", ErrDuplicate, normalized)
		}
	}

	id, err := newID()
	if err != nil {
		return Target{}, err
	}
	now := time.Now().UTC()
	t := Target{
		ID:        id,
		URL:       normalized,
		CreatedAt: now,
		UpdatedAt: now,
	}
	list = append(list, t)
	if err := s.saveLocked(list); err != nil {
		return Target{}, err
	}
	return t, nil
}

// Update changes an existing target's URL. Illegal or duplicate URLs
// leave the store unchanged.
func (s *Store) Update(id, rawURL string) (Target, error) {
	if id == "" {
		return Target{}, fmt.Errorf("%w: empty id", ErrNotFound)
	}
	normalized, err := normalizeURL(rawURL)
	if err != nil {
		return Target{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadLocked()
	if err != nil {
		return Target{}, err
	}

	idx := -1
	for i, t := range list {
		if t.ID == id {
			idx = i
			continue
		}
		if t.URL == normalized {
			return Target{}, fmt.Errorf("%w: %s", ErrDuplicate, normalized)
		}
	}
	if idx < 0 {
		return Target{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	now := time.Now().UTC()
	list[idx].URL = normalized
	list[idx].UpdatedAt = now
	if err := s.saveLocked(list); err != nil {
		return Target{}, err
	}
	return list[idx], nil
}

// Delete removes a target so later List (and the probe loop) no longer see it.
func (s *Store) Delete(id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty id", ErrNotFound)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.loadLocked()
	if err != nil {
		return err
	}
	kept := make([]Target, 0, len(list))
	found := false
	for _, t := range list {
		if t.ID == id {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return s.saveLocked(kept)
}

func (s *Store) loadLocked() ([]Target, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse targets: %w", err)
	}
	if payload.Targets == nil {
		payload.Targets = []Target{}
	}
	return payload.Targets, nil
}

func (s *Store) saveLocked(list []Target) error {
	sortTargets(list)
	return writeFile(s.path, list)
}

func writeFile(path string, list []Target) error {
	if list == nil {
		list = []Target{}
	}
	payload, err := json.MarshalIndent(filePayload{Targets: list}, "", "  ")
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

func sortTargets(list []Target) {
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		}
		if list[i].ID != list[j].ID {
			return list[i].ID < list[j].ID
		}
		return list[i].URL < list[j].URL
	})
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
