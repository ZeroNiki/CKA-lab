package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"cka-lab/internal/cluster"
)

type fakeSnap struct{ restores int }

func (f *fakeSnap) Exists(context.Context) (bool, error) { return true, nil }
func (f *fakeSnap) Restore(context.Context) error      { f.restores++; return nil }

func TestRestoringSnapshotter_DropsClientsAndWaitsForAPI(t *testing.T) {
	lc := &lazyCluster{clients: &cluster.Clients{}}
	probes := 0
	r := restoringSnapshotter{
		Snapshotter: &fakeSnap{},
		lc:          lc,
		ready: func(context.Context) error {
			probes++
			if lc.clients != nil {
				t.Error("probe ran on pre-restore clients: their keep-alive connections are dead")
			}
			if probes < 3 {
				return errors.New("connection reset by peer")
			}
			return nil
		},
		interval: time.Millisecond,
	}
	if err := r.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if probes != 3 {
		t.Errorf("probes = %d, want 3 (wait until the API answers)", probes)
	}
}

func TestRestoringSnapshotter_GivesUpWhenContextEnds(t *testing.T) {
	r := restoringSnapshotter{
		Snapshotter: &fakeSnap{},
		lc:          &lazyCluster{},
		ready:       func(context.Context) error { return errors.New("down") },
		interval:    time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Restore(ctx); err == nil {
		t.Fatal("Restore returned nil although the API never came up")
	}
}
