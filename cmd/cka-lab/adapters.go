package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/cluster"
	"cka-lab/internal/content"
)

// lazyCluster connects on first use, so the server starts even while the VMs are stopped,
// and reconnects after an unavailable error (e.g. a fresh kubeconfig from ./lab up).
type lazyCluster struct {
	kubeconfig, sshConfig string

	mu      sync.Mutex
	clients *cluster.Clients
	exec    *cluster.SSHExecutor
}

func (l *lazyCluster) get() (*cluster.Clients, *cluster.SSHExecutor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients == nil {
		c, err := cluster.NewClients(l.kubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", check.ErrUnavailable, err)
		}
		e, err := cluster.NewSSHExecutor(l.sshConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", check.ErrUnavailable, err)
		}
		l.clients, l.exec = c, e
	}
	return l.clients, l.exec, nil
}

func (l *lazyCluster) resetOn(err error) {
	if errors.Is(err, check.ErrUnavailable) {
		l.drop()
	}
}

func (l *lazyCluster) env(t *content.Task) (check.Env, error) {
	c, e, err := l.get()
	if err != nil {
		return check.Env{}, err
	}
	return check.Env{Dynamic: c.Dynamic, Mapper: c.Mapper, Kube: c.Kube, Exec: e, Namespace: t.NS}, nil
}

// Check implements game.Checker.
func (l *lazyCluster) Check(ctx context.Context, t *content.Task) ([]check.Result, error) {
	env, err := l.env(t)
	if err != nil {
		return nil, err
	}
	res, err := check.RunErr(ctx, env, t.Checks)
	l.resetOn(err)
	return res, err
}

// Prepare implements game.Preparer.
func (l *lazyCluster) Prepare(ctx context.Context, t *content.Task) error {
	c, e, err := l.get()
	if err != nil {
		return err
	}
	err = cluster.PrepareTask(ctx, c, e, t)
	l.resetOn(err)
	return err
}

// Run implements check.NodeExecutor (used by selftest for solution scripts).
func (l *lazyCluster) Run(ctx context.Context, node, script string) (string, int, error) {
	_, e, err := l.get()
	if err != nil {
		return "", -1, err
	}
	out, code, err := e.Run(ctx, node, script)
	l.resetOn(err)
	return out, code, err
}

// restoringSnapshotter waits for the API after a rollback. The VMs resume from a memory
// snapshot, so pooled keep-alive connections are reset by the guest and the API needs a
// moment; without the wait the next task's setup fails with "connection reset by peer".
type restoringSnapshotter struct {
	cluster.Snapshotter
	lc       *lazyCluster
	ready    func(ctx context.Context) error // nil: GET /readyz through a fresh client
	interval time.Duration
}

const restoreReadyTimeout = 5 * time.Minute

func (r restoringSnapshotter) Restore(ctx context.Context) error {
	if err := r.Snapshotter.Restore(ctx); err != nil {
		return err
	}
	ready, interval := r.ready, r.interval
	if ready == nil {
		ready = r.lc.apiReady
	}
	if interval == 0 {
		interval = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, restoreReadyTimeout)
	defer cancel()
	for {
		r.lc.drop()
		err := ready(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("API not ready after restore: %w", err)
		case <-time.After(interval):
		}
	}
}

func (l *lazyCluster) drop() {
	l.mu.Lock()
	l.clients, l.exec = nil, nil
	l.mu.Unlock()
}

func (l *lazyCluster) apiReady(ctx context.Context) error {
	c, _, err := l.get()
	if err != nil {
		return err
	}
	return c.Kube.Discovery().RESTClient().Get().AbsPath("/readyz").Do(ctx).Error()
}
