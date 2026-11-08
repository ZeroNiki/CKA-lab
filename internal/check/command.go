package check

import (
	"context"
	"fmt"
	"strings"
)

const maxStdoutShown = 200

func runCommand(ctx context.Context, env Env, s Spec) (Result, error) {
	node := s.Node
	if node == "" {
		node = defaultNode
	}
	stdout, code, err := env.Exec.Run(ctx, node, s.Run)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrUnavailable, node, err)
	}
	out := strings.TrimSpace(stdout)
	shown := out
	if len(shown) > maxStdoutShown {
		shown = shown[:maxStdoutShown] + "…"
	}
	actual := fmt.Sprintf("exit %d, stdout: %q", code, shown)

	wantCode := 0
	if s.ExitCode != nil {
		wantCode = *s.ExitCode
	}
	if code != wantCode {
		return Result{Actual: actual}, nil
	}
	if s.Stdout != nil {
		pass, err := Compare(s.Stdout.Op, []string{out}, out != "", s.Stdout.Value)
		if err != nil || !pass {
			return Result{Actual: actual}, nil
		}
	}
	return Result{Pass: true, Actual: actual}, nil
}
