package check

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// NodeExecutor runs a script on a cluster node.
type NodeExecutor interface {
	// Run executes script via `bash -s` (script on stdin) on node as user vagrant.
	// err is only for connection-level failures; a failing script is a non-zero exitCode.
	Run(ctx context.Context, node, script string) (stdout string, exitCode int, err error)
}

// Env is what checks need to look at the cluster.
type Env struct {
	Dynamic   dynamic.Interface
	Mapper    meta.RESTMapper
	Kube      kubernetes.Interface
	Exec      NodeExecutor
	Namespace string // task namespace, used when a check does not name one
}

// ErrUnavailable means the cluster could not be reached, so results say nothing about the task.
var ErrUnavailable = errors.New("cluster unavailable")

const (
	defaultTimeout = 10 * time.Second
	defaultNode    = "controlplane"
)

// retryInterval is a variable so tests can shorten it.
var retryInterval = 2 * time.Second

type checkFunc func(ctx context.Context, env Env, s Spec) (Result, error)

var checkers = map[string]checkFunc{
	"resource":  runResource,
	"podsReady": runPodsReady,
	"canI":      runCanI,
	"command":   runCommand,
}

// Run executes all specs and returns one result per spec, ignoring availability errors.
func Run(ctx context.Context, env Env, specs []Spec) []Result {
	res, _ := RunErr(ctx, env, specs)
	return res
}

// RunErr executes every spec (it does not stop at the first failure).
// The error wraps ErrUnavailable if any check could not reach the cluster.
func RunErr(ctx context.Context, env Env, specs []Spec) ([]Result, error) {
	results := make([]Result, 0, len(specs))
	var firstErr error
	for _, s := range specs {
		r, err := runWithRetry(ctx, env, s)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			r = Result{Description: s.Description, Actual: err.Error()}
		}
		results = append(results, r)
	}
	return results, firstErr
}

func runWithRetry(ctx context.Context, env Env, s Spec) (Result, error) {
	fn, ok := checkers[s.Type]
	if !ok {
		return Result{}, fmt.Errorf("unknown check type %q", s.Type)
	}
	timeout := time.Duration(s.Timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	deadline := time.Now().Add(time.Duration(s.Retry))
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		r, err := fn(attemptCtx, env, s)
		cancel()
		r.Description = s.Description
		if err != nil || r.Pass || time.Now().Add(retryInterval).After(deadline) {
			return r, err
		}
		select {
		case <-ctx.Done():
			return r, nil
		case <-time.After(retryInterval):
		}
	}
}

// apiError separates "the API answered no" (a check result) from "no answer" (unavailable).
func apiError(err error) error {
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
