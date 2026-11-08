package progress

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen_MissingFileEmptyState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.Coins != 0 || st.TheoryPassed == nil || st.LevelSolved == nil || st.SolvedCount == nil ||
		st.Revealed == nil || st.LevelBonusPaid == nil {
		t.Fatalf("state = %+v", st)
	}
}

func TestOpen_CorruptFileErrorsAndKeepsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	const junk = "{not json"
	os.WriteFile(path, []byte(junk), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("want error for corrupt file")
	}
	b, _ := os.ReadFile(path)
	if string(b) != junk {
		t.Fatalf("file was modified: %q", b)
	}
}

func TestUpdate_PersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "progress.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	err = s.Update(func(st *State) error {
		st.Coins = 42
		st.SolvedCount["t1"] = 2
		st.BreakUntil = &until
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st := s2.Get()
	if st.Coins != 42 || st.SolvedCount["t1"] != 2 || !st.BreakUntil.Equal(until) {
		t.Fatalf("reloaded = %+v", st)
	}
}

func TestUpdate_ErrorRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	s, _ := Open(path)
	s.Update(func(st *State) error { st.Coins = 10; return nil })
	boom := errors.New("boom")
	err := s.Update(func(st *State) error {
		st.Coins = 999
		st.Revealed["x"] = true
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if st := s.Get(); st.Coins != 10 || st.Revealed["x"] {
		t.Fatalf("in-memory state changed: %+v", st)
	}
	s2, _ := Open(path)
	if s2.Get().Coins != 10 {
		t.Fatal("file changed")
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "progress.json"))
	st := s.Get()
	st.LevelSolved["t1"] = true
	if s.Get().LevelSolved["t1"] {
		t.Fatal("Get must return a deep copy")
	}
}
