// Package selftest proves each task is gradable: its checks fail on a fresh setup
// and pass after the reference solution.
package selftest

import (
	"context"
	"fmt"
	"slices"

	"cka-lab/internal/check"
	"cka-lab/internal/content"
)

type Deps struct {
	Prepare func(ctx context.Context, t *content.Task) error
	Check   func(ctx context.Context, t *content.Task) ([]check.Result, error)
	Exec    check.NodeExecutor
	Restore func(ctx context.Context) error
}

type Report struct {
	TaskID               string
	FailedBeforeSolution bool
	PassedAfterSolution  bool
	Err                  error
	Results              []check.Result // after the solution
}

func (r Report) OK() bool { return r.Err == nil && r.FailedBeforeSolution && r.PassedAfterSolution }

func RunTask(ctx context.Context, d Deps, t *content.Task) (r Report) {
	r.TaskID = t.ID
	if t.RequiresSnapshot {
		defer func() {
			if err := d.Restore(ctx); err != nil && r.Err == nil {
				r.Err = fmt.Errorf("restore snapshot: %w", err)
			}
		}()
	}
	if err := d.Prepare(ctx, t); err != nil {
		r.Err = fmt.Errorf("setup: %w", err)
		return r
	}
	// Unsolved checks are expected to fail, so do not wait for their retry windows.
	before, err := d.Check(ctx, withoutRetry(t))
	if err != nil {
		r.Err = fmt.Errorf("checks before solution: %w", err)
		return r
	}
	r.FailedBeforeSolution = !allPass(before)

	out, code, err := d.Exec.Run(ctx, "controlplane", t.SolutionScript)
	if err != nil {
		r.Err = fmt.Errorf("solutionScript: %w", err)
		return r
	}
	if code != 0 {
		r.Err = fmt.Errorf("solutionScript exited %d: %s", code, out)
		return r
	}
	r.Results, err = d.Check(ctx, t)
	if err != nil {
		r.Err = fmt.Errorf("checks after solution: %w", err)
		return r
	}
	r.PassedAfterSolution = allPass(r.Results)
	return r
}

func withoutRetry(t *content.Task) *content.Task {
	c := *t
	c.Checks = slices.Clone(t.Checks)
	for i := range c.Checks {
		c.Checks[i].Retry = 0
	}
	return &c
}

func allPass(rs []check.Result) bool {
	for _, r := range rs {
		if !r.Pass {
			return false
		}
	}
	return len(rs) > 0
}

// Order puts cluster-breaking tasks last so a slow restore does not sit between quick tasks.
func Order(tasks []*content.Task) []*content.Task {
	out := slices.Clone(tasks)
	slices.SortStableFunc(out, func(a, b *content.Task) int {
		switch {
		case a.RequiresSnapshot == b.RequiresSnapshot:
			return 0
		case a.RequiresSnapshot:
			return 1
		default:
			return -1
		}
	})
	return out
}
