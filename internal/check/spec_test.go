package check

import (
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

func boolPtr(b bool) *bool { return &b }

func TestValidate(t *testing.T) {
	deploy := &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}
	tests := []struct {
		name    string
		spec    Spec
		wantErr string
	}{
		{"empty description", Spec{Type: "resource", Target: deploy}, "description"},
		{"unknown type", Spec{Type: "magic", Description: "d"}, "unknown check type"},
		{"resource without kind", Spec{Type: "resource", Description: "d", Target: &Target{APIVersion: "v1", Name: "x"}}, "kind"},
		{"resource without target", Spec{Type: "resource", Description: "d"}, "target"},
		{"resource name and selector", Spec{Type: "resource", Description: "d",
			Target: &Target{APIVersion: "v1", Kind: "Pod", Name: "x", LabelSelector: "app=x"}}, "name"},
		{"count with path", Spec{Type: "resource", Description: "d", Target: deploy,
			Assert: []Assertion{{Path: "{.spec.replicas}", Op: "count", Value: "1"}}}, "count"},
		{"unknown op", Spec{Type: "resource", Description: "d", Target: deploy,
			Assert: []Assertion{{Path: "{.spec.replicas}", Op: "approx", Value: "1"}}}, "op"},
		{"bad regex", Spec{Type: "resource", Description: "d", Target: deploy,
			Assert: []Assertion{{Path: "{.metadata.name}", Op: "matches", Value: "("}}}, "regex"},
		{"canI without verb", Spec{Type: "canI", Description: "d", Resource: "pods",
			Subject: &Subject{User: "jane"}}, "verb"},
		{"canI without subject", Spec{Type: "canI", Description: "d", Verb: "get", Resource: "pods"}, "subject"},
		{"canI bad serviceAccount", Spec{Type: "canI", Description: "d", Verb: "get", Resource: "pods",
			Subject: &Subject{ServiceAccount: "deployer"}}, "serviceAccount"},
		{"command without run", Spec{Type: "command", Description: "d"}, "run"},
		{"podsReady without selector", Spec{Type: "podsReady", Description: "d"}, "selector"},

		{"valid resource", Spec{Type: "resource", Description: "d", Target: deploy,
			Assert: []Assertion{{Path: "{.spec.replicas}", Op: "eq", Value: "3"}}}, ""},
		{"valid count", Spec{Type: "resource", Description: "d",
			Target: &Target{APIVersion: "v1", Kind: "Pod", LabelSelector: "app=x"},
			Assert: []Assertion{{Op: "count", Value: "2"}}}, ""},
		{"valid podsReady", Spec{Type: "podsReady", Description: "d", Selector: "app=api"}, ""},
		{"valid canI", Spec{Type: "canI", Description: "d", Verb: "get", Resource: "pods",
			Subject: &Subject{ServiceAccount: "web:deployer"}, Allowed: boolPtr(false)}, ""},
		{"valid command", Spec{Type: "command", Description: "d", Run: "test -f /opt/backup.db"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSpecYAML(t *testing.T) {
	src := `
type: resource
description: Deployment api uses image nginx:1.27.3
target: {apiVersion: apps/v1, kind: Deployment, name: api}
assert:
  - {path: "{.spec.template.spec.containers[0].image}", op: eq, value: "nginx:1.27.3"}
retry: 60s
`
	var s Spec
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.Target.Kind != "Deployment" || s.Assert[0].Value != "nginx:1.27.3" {
		t.Fatalf("decoded wrong: %+v", s)
	}
	if time.Duration(s.Retry) != 60*time.Second {
		t.Fatalf("retry = %v", time.Duration(s.Retry))
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
