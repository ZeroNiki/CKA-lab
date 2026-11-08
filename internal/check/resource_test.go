package check

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func deployTarget(name string) *Target {
	return &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: name}
}

func TestResource_ByNamePass(t *testing.T) {
	env := testEnv(t, []runtime.Object{deployment("lab-test", "api", 3, "nginx:1.27.3", nil)})
	res := Run(context.Background(), env, []Spec{{
		Type: "resource", Description: "image", Target: deployTarget("api"),
		Assert: []Assertion{
			{Path: "{.spec.template.spec.containers[0].image}", Op: "eq", Value: "nginx:1.27.3"},
			{Path: "{.spec.replicas}", Op: "gte", Value: "2"},
		},
	}})
	if !res[0].Pass {
		t.Fatalf("want pass, got %+v", res[0])
	}
}

func TestResource_FieldMismatchActual(t *testing.T) {
	env := testEnv(t, []runtime.Object{deployment("lab-test", "api", 2, "nginx:1.27.3", nil)})
	res := Run(context.Background(), env, []Spec{{
		Type: "resource", Description: "replicas", Target: deployTarget("api"),
		Assert: []Assertion{{Path: "{.spec.replicas}", Op: "eq", Value: "3"}},
	}})
	if res[0].Pass {
		t.Fatal("want fail")
	}
	if res[0].Actual != "2" {
		t.Fatalf("actual = %q, want %q", res[0].Actual, "2")
	}
}

func TestResource_SelectorAllMustMatch(t *testing.T) {
	labels := map[string]any{"tier": "web"}
	env := testEnv(t, []runtime.Object{
		deployment("lab-test", "a", 1, "nginx:1.27", labels),
		deployment("lab-test", "b", 1, "httpd:2.4", labels),
		deployment("lab-test", "c", 1, "httpd:2.4", map[string]any{"tier": "db"}),
	})
	spec := Spec{
		Type: "resource", Description: "web uses nginx",
		Target: &Target{APIVersion: "apps/v1", Kind: "Deployment", LabelSelector: "tier=web"},
		Assert: []Assertion{{Path: "{.spec.template.spec.containers[0].image}", Op: "contains", Value: "nginx"}},
	}
	if res := Run(context.Background(), env, []Spec{spec}); res[0].Pass {
		t.Fatalf("want fail: one web deployment is httpd, got %+v", res[0])
	}
}

func TestResource_NotFoundIsFail(t *testing.T) {
	env := testEnv(t, nil)
	res := Run(context.Background(), env, []Spec{{Type: "resource", Description: "d", Target: deployTarget("api")}})
	if res[0].Pass || res[0].Actual != "not found" {
		t.Fatalf("got %+v", res[0])
	}
}

func TestResource_NotExistsPassWhenAbsent(t *testing.T) {
	env := testEnv(t, nil)
	res := Run(context.Background(), env, []Spec{{
		Type: "resource", Description: "gone", Target: deployTarget("old"),
		Assert: []Assertion{{Op: "notExists"}},
	}})
	if !res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
}

func TestResource_Count(t *testing.T) {
	labels := map[string]any{"app": "x"}
	env := testEnv(t, []runtime.Object{
		deployment("lab-test", "a", 1, "i", labels),
		deployment("lab-test", "b", 1, "i", labels),
	})
	target := &Target{APIVersion: "apps/v1", Kind: "Deployment", LabelSelector: "app=x"}
	res := Run(context.Background(), env, []Spec{
		{Type: "resource", Description: "two", Target: target, Assert: []Assertion{{Op: "count", Value: "2"}}},
		{Type: "resource", Description: "three", Target: target, Assert: []Assertion{{Op: "count", Value: "3"}}},
	})
	if !res[0].Pass || res[1].Pass {
		t.Fatalf("got %+v", res)
	}
}

func TestResource_ExplicitNamespace(t *testing.T) {
	env := testEnv(t, []runtime.Object{deployment("other", "api", 1, "i", nil)})
	target := &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "other"}
	if res := Run(context.Background(), env, []Spec{{Type: "resource", Description: "d", Target: target}}); !res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
}
