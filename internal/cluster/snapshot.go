package cluster

import (
	"context"
	"fmt"
	"os/exec"
)

// Snapshotter rolls the whole cluster back to the base snapshot.
type Snapshotter interface {
	Restore(ctx context.Context) error
}

// VagrantSnapshotter restores snapshot "clean" on all nodes at once: restoring only the
// control plane would leave etcd and the workers out of sync.
type VagrantSnapshotter struct {
	Dir    string
	Runner func(ctx context.Context, dir string, args ...string) error
}

func (s VagrantSnapshotter) Restore(ctx context.Context) error {
	run := s.Runner
	if run == nil {
		run = runVagrant
	}
	return run(ctx, s.Dir, "snapshot", "restore", "--no-provision", "clean")
}

func runVagrant(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "vagrant", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("vagrant %v: %w: %s", args, err, out)
	}
	return nil
}
