package check

import (
	"context"
	"testing"
)

func intPtr(i int) *int { return &i }

func TestCommand_ExitCodeAndStdout(t *testing.T) {
	env := testEnv(t, nil)
	env.Exec = &fakeExec{reply: func(_, _ string) (string, int, error) { return "  v1.37.0\n", 0, nil }}
	res := Run(context.Background(), env, []Spec{
		{Type: "command", Description: "version", Run: "kubeadm version -o short",
			Stdout: &Assertion{Op: "eq", Value: "v1.37.0"}},
		{Type: "command", Description: "wrong version", Run: "kubeadm version -o short",
			Stdout: &Assertion{Op: "eq", Value: "v1.36.0"}},
		{Type: "command", Description: "expects exit 1", Run: "false", ExitCode: intPtr(1)},
	})
	if !res[0].Pass || res[1].Pass || res[2].Pass {
		t.Fatalf("got %+v", res)
	}
}

func TestCommand_DefaultNodeControlplane(t *testing.T) {
	env := testEnv(t, nil)
	fe := &fakeExec{reply: func(_, _ string) (string, int, error) { return "", 0, nil }}
	env.Exec = fe
	Run(context.Background(), env, []Spec{
		{Type: "command", Description: "a", Run: "true"},
		{Type: "command", Description: "b", Run: "true", Node: "node01"},
	})
	if fe.calls[0] != "controlplane|true" || fe.calls[1] != "node01|true" {
		t.Fatalf("calls = %v", fe.calls)
	}
}
