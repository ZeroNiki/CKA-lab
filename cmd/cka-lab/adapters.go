package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
		l.mu.Lock()
		l.clients, l.exec = nil, nil
		l.mu.Unlock()
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
