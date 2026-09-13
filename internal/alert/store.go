package alert

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const stateFilename = "alerts.json"

type incident struct {
	TargetID string    `json:"target_id"`
	Kind     Kind      `json:"kind"`
	SentAt   time.Time `json:"sent_at"`
}

type statePayload struct {
	Incidents []incident `json:"incidents"`
}

// stateStore remembers the last delivered event per target so cooldown
// survives process restarts. An empty path keeps state in memory only.
type stateStore struct {
	path     string
	mu       sync.Mutex
	byTarget map[string]incident
}

func openState(dataDir string) (*stateStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, stateFilename)
	st := &stateStore{path: path, byTarget: map[string]incident{}}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := st.saveLocked(); err != nil {
			return nil, fmt.Errorf("init alerts file: %w", err)
		}
		return st, nil
	} else if err != nil {
		return nil, err
	}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

func newMemState() *stateStore {
	return &stateStore{byTarget: map[string]incident{}}
}

func (s *stateStore) last(targetID string) (incident, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.byTarget[targetID]
	return got, ok, nil
}

func (s *stateStore) record(targetID string, kind Kind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byTarget[targetID] = incident{
		TargetID: targetID,
		Kind:     kind,
		SentAt:   at.UTC(),
	}
	return s.saveLocked()
}

func (s *stateStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read alerts: %w", err)
	}
	var payload statePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("parse alerts: %w", err)
	}
	s.byTarget = make(map[string]incident, len(payload.Incidents))
	for _, inc := range payload.Incidents {
		if inc.TargetID == "" {
			continue
		}
		inc.SentAt = inc.SentAt.UTC()
		s.byTarget[inc.TargetID] = inc
	}
	return nil
}

func (s *stateStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	list := make([]incident, 0, len(s.byTarget))
	for _, inc := range s.byTarget {
		list = append(list, inc)
	}
	payload, err := json.MarshalIndent(statePayload{Incidents: list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
