package selftest

import (
	"context"
	"testing"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/content"
)

// world simulates a cluster where the task is solved once the solution script ran.
type world struct {
	solved      bool
	alwaysPass  bool
	scriptCode  int
	restores    int
	sawRetry    []check.Duration
	scriptsSeen []string
}

func (w *world) deps() Deps {
	return Deps{
		Prepare: func(context.Context, *content.Task) error { w.solved = false; return nil },
		Check: func(_ context.Context, t *content.Task) ([]check.Result, error) {
			w.sawRetry = append(w.sawRetry, t.Checks[0].Retry)
			return []check.Result{{Description: "d", Pass: w.solved || w.alwaysPass}}, nil
		},
		Exec: execFunc(func(_ context.Context, node, script string) (string, int, error) {
			w.scriptsSeen = append(w.scriptsSeen, node+"|"+script)
			if w.scriptCode == 0 {
				w.solved = true
			}
			return "out", w.scriptCode, nil
		}),
		Restore: func(context.Context) error { w.restores++; return nil },
	}
}

type execFunc func(context.Context, string, string) (string, int, error)

func (f execFunc) Run(ctx context.Context, node, script string) (string, int, error) {
	return f(ctx, node, script)
}

func task(snapshot bool) *content.Task {
	return &content.Task{
		ID: "t1", RequiresSnapshot: snapshot, SolutionScript: "kubectl create ns x",
		Checks: []check.Spec{{Type: "command", Description: "d", Run: "true", Retry: check.Duration(time.Minute)}},
	}
}

func TestRunTask_OK(t *testing.T) {
	w := &world{}
	r := RunTask(context.Background(), w.deps(), task(false))
	if !r.OK() || !r.FailedBeforeSolution || !r.PassedAfterSolution {
		t.Fatalf("report = %+v", r)
	}
	if w.scriptsSeen[0] != "controlplane|kubectl create ns x" {
		t.Fatalf("scripts = %v", w.scriptsSeen)
	}
	// Before the solution checks run without retry (they are expected to fail fast); after it with retry.
	if w.sawRetry[0] != 0 || w.sawRetry[1] != check.Duration(time.Minute) {
		t.Fatalf("retry = %v", w.sawRetry)
	}
}

func TestRunTask_ChecksPassBeforeSolution(t *testing.T) {
	w := &world{alwaysPass: true}
	r := RunTask(context.Background(), w.deps(), task(false))
	if r.OK() || r.FailedBeforeSolution {
		t.Fatalf("report = %+v", r)
	}
}

func TestRunTask_SolutionScriptFails(t *testing.T) {
	w := &world{scriptCode: 1}
	r := RunTask(context.Background(), w.deps(), task(false))
	if r.OK() || r.Err == nil {
		t.Fatalf("report = %+v", r)
	}
}

func TestRunTask_SnapshotRestored(t *testing.T) {
	w := &world{scriptCode: 1}
	RunTask(context.Background(), w.deps(), task(true))
	if w.restores != 1 {
		t.Fatalf("restores = %d (must restore even on failure)", w.restores)
	}
	w2 := &world{}
	RunTask(context.Background(), w2.deps(), task(false))
	if w2.restores != 0 {
		t.Fatal("non-snapshot task must not restore")
	}
}

func TestOrder_SnapshotLast(t *testing.T) {
	a, b, c := &content.Task{ID: "a", RequiresSnapshot: true}, &content.Task{ID: "b"}, &content.Task{ID: "c"}
	got := Order([]*content.Task{a, b, c})
	if got[0].ID != "b" || got[1].ID != "c" || got[2].ID != "a" {
		t.Fatalf("order = %s %s %s", got[0].ID, got[1].ID, got[2].ID)
	}
}
