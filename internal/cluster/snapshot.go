package cluster

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SnapshotName is the base snapshot created by ./lab snapshot-base.
const SnapshotName = "clean"

// Snapshotter rolls the whole cluster back to the base snapshot.
type Snapshotter interface {
	Exists(ctx context.Context) (bool, error)
	Restore(ctx context.Context) error
}

// VagrantSnapshotter restores snapshot "clean" on all nodes at once: restoring only the
// control plane would leave etcd and the workers out of sync.
type VagrantSnapshotter struct {
	Dir    string
	Nodes  []string // default controlplane, node01, node02
	Runner func(ctx context.Context, dir string, args ...string) (string, error)
}

func (s VagrantSnapshotter) run(ctx context.Context, args ...string) (string, error) {
	if s.Runner != nil {
		return s.Runner(ctx, s.Dir, args...)
	}
	return runVagrant(ctx, s.Dir, args...)
}

func (s VagrantSnapshotter) Restore(ctx context.Context) error {
	_, err := s.run(ctx, "snapshot", "restore", "--no-provision", SnapshotName)
	return err
}

// Exists reports whether every node has the base snapshot.
func (s VagrantSnapshotter) Exists(ctx context.Context) (bool, error) {
	nodes := s.Nodes
	if len(nodes) == 0 {
		nodes = []string{"controlplane", "node01", "node02"}
	}
	for _, n := range nodes {
		out, err := s.run(ctx, "snapshot", "list", n)
		if err != nil {
			return false, err
		}
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == SnapshotName {
				found = true
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func runVagrant(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "vagrant", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("vagrant %v: %w: %s", args, err, out)
	}
	return string(out), nil
}
