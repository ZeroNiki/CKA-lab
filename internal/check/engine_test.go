package check

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// fakeExec records calls and replies from a function.
type fakeExec struct {
	calls []string // "node|script"
	reply func(node, script string) (string, int, error)
}

func (f *fakeExec) Run(_ context.Context, node, script string) (string, int, error) {
	f.calls = append(f.calls, node+"|"+script)
	return f.reply(node, script)
}

var (
	gvkDeployment = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkConfigMap  = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	gvkPod        = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
)

func testMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(gvkDeployment, meta.RESTScopeNamespace)
	m.Add(gvkConfigMap, meta.RESTScopeNamespace)
	m.Add(gvkPod, meta.RESTScopeNamespace)
	return m
}

func deployment(ns, name string, replicas int64, image string, labels map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name, "namespace": ns, "labels": labels},
		"spec": map[string]any{
			"replicas": replicas,
			"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": "app", "image": image}},
			}},
		},
	}}
}

// testEnv builds an Env over fake clients holding objs (dynamic) and kubeObjs (typed).
func testEnv(t *testing.T, objs []runtime.Object, kubeObjs ...runtime.Object) Env {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "pods"}:                       "PodList",
	}
	return Env{
		Dynamic:   dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objs...),
		Mapper:    testMapper(),
		Kube:      kubefake.NewClientset(kubeObjs...),
		Exec:      &fakeExec{reply: func(string, string) (string, int, error) { return "", 0, nil }},
		Namespace: "lab-test",
	}
}

func TestRun_ContinuesAfterFail(t *testing.T) {
	env := testEnv(t, []runtime.Object{deployment("lab-test", "api", 3, "nginx:1.27.3", nil)})
	specs := []Spec{
		{Type: "resource", Description: "exists", Target: &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}},
		{Type: "resource", Description: "missing", Target: &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "nope"}},
		{Type: "resource", Description: "replicas", Target: &Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"},
			Assert: []Assertion{{Path: "{.spec.replicas}", Op: "eq", Value: "3"}}},
	}
	res := Run(context.Background(), env, specs)
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3", len(res))
	}
	if !res[0].Pass || res[1].Pass || !res[2].Pass {
		t.Fatalf("results = %+v", res)
	}
}

func TestRun_RetryEventuallyPasses(t *testing.T) {
	retryInterval = 10 * time.Millisecond
	t.Cleanup(func() { retryInterval = 2 * time.Second })

	env := testEnv(t, nil)
	attempts := 0
	env.Exec = &fakeExec{reply: func(string, string) (string, int, error) {
		attempts++
		if attempts < 2 {
			return "", 1, nil
		}
		return "", 0, nil
	}}
	spec := Spec{Type: "command", Description: "eventually", Run: "true", Retry: Duration(5 * time.Second)}
	res := Run(context.Background(), env, []Spec{spec})
	if !res[0].Pass || attempts != 2 {
		t.Fatalf("pass=%v attempts=%d", res[0].Pass, attempts)
	}
}

func TestRunErr_Unavailable(t *testing.T) {
	env := testEnv(t, nil)
	env.Exec = &fakeExec{reply: func(string, string) (string, int, error) {
		return "", -1, errors.New("dial tcp 192.168.56.10:22: connect: connection refused")
	}}
	_, err := RunErr(context.Background(), env, []Spec{{Type: "command", Description: "d", Run: "true"}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err should keep the cause: %v", err)
	}
}
