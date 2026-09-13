// Package targets is the persistence placeholder for registered monitor URLs.
//
// TODO(#1): implement add / update / delete with validation (scheme required,
// reject duplicates after normalization) and keep the list restart-safe.
// SQLite is the intended store; this file-backed JSON file is a stand-in so
// later work has a data directory and a stable type to build on.
package targets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const filename = "targets.json"

// ErrNotImplemented is returned by mutating methods until #1 is done.
var ErrNotImplemented = errors.New("targets: CRUD not implemented yet; see issue #1")

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

// Store reads a JSON list from dataDir. Writes are not implemented yet.
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
		payload, err := json.MarshalIndent(filePayload{Targets: []Target{}}, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
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

// List returns the current targets. Empty until #1 lands.
func (s *Store) List() ([]Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

// Add will persist a new URL.
//
// TODO(#1): validate URL, normalize, reject duplicates, assign id.
func (s *Store) Add(_ string) (Target, error) {
	return Target{}, ErrNotImplemented
}

// Update will change an existing target's URL.
//
// TODO(#1): reject unknown id and illegal / duplicate URLs.
func (s *Store) Update(_ string, _ string) (Target, error) {
	return Target{}, ErrNotImplemented
}

// Delete will remove a target so the probe loop no longer sees it.
//
// TODO(#1): persist the removal.
func (s *Store) Delete(_ string) error {
	return ErrNotImplemented
}
