// Package progress keeps the player's state in a JSON file.
package progress

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type State struct {
	Coins          int             `json:"coins"`
	TheoryPassed   map[string]bool `json:"theoryPassed"`   // levelID
	LevelSolved    map[string]bool `json:"levelSolved"`    // taskID, solved in "levels" mode only
	SolvedCount    map[string]int  `json:"solvedCount"`    // taskID, solves in any mode
	Revealed       map[string]bool `json:"revealed"`       // taskID, solution shown before solving
	LevelBonusPaid map[string]bool `json:"levelBonusPaid"` // levelID
	BreakUntil     *time.Time      `json:"breakUntil,omitempty"`
}

func (s *State) init() {
	if s.TheoryPassed == nil {
		s.TheoryPassed = map[string]bool{}
	}
	if s.LevelSolved == nil {
		s.LevelSolved = map[string]bool{}
	}
	if s.SolvedCount == nil {
		s.SolvedCount = map[string]int{}
	}
	if s.Revealed == nil {
		s.Revealed = map[string]bool{}
	}
	if s.LevelBonusPaid == nil {
		s.LevelBonusPaid = map[string]bool{}
	}
}

func (s State) clone() State {
	// A JSON round trip is the simplest correct deep copy for this small struct.
	b, _ := json.Marshal(s)
	var c State
	_ = json.Unmarshal(b, &c)
	c.init()
	return c
}

type Store struct {
	path  string
	mu    sync.Mutex
	state State
}

// Open loads path. A missing file is an empty state; a corrupt one is an error and is left untouched,
// so a bug never wipes the player's coins.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.state); err != nil {
			return nil, fmt.Errorf("%s is corrupt (fix or move it away): %w", path, err)
		}
	}
	s.state.init()
	return s, nil
}

// Get returns a deep copy of the current state.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.clone()
}

// Update applies fn to a copy and persists it; if fn or the write fails, nothing changes.
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state.clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *Store) write(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".progress-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
