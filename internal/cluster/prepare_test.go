package cluster

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"cka-lab/internal/content"
)

type fakeExec struct {
	calls []string
	code  int
	out   string
}

func (f *fakeExec) Run(_ context.Context, node, script string) (string, int, error) {
	f.calls = append(f.calls, node+"|"+script)
	return f.out, f.code, nil
}

var (
	gvrDeploy = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvrCRole  = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
	listKinds = map[schema.GroupVersionResource]string{gvrDeploy: "DeploymentList", gvrCRole: "ClusterRoleList"}
)

func testClients(kubeObjs ...runtime.Object) *Clients {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, meta.RESTScopeRoot)
	return &Clients{
		Dynamic: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds),
		Mapper:  m,
		Kube:    kubefake.NewClientset(kubeObjs...),
	}
}

func testTask() *content.Task {
	return &content.Task{
		ID: "t1", NS: "lab-web",
		Setup: content.Setup{
			Manifests: []string{`apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  replicas: 1
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: lab-reader
rules: []`},
			Commands: []content.SetupCommand{{Node: "node01", Run: "echo hi"}},
		},
	}
}

func TestPrepareTask_CreatesNamespaceAndApplies(t *testing.T) {
	c := testClients()
	ex := &fakeExec{}
	if err := PrepareTask(context.Background(), c, ex, testTask()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Kube.CoreV1().Namespaces().Get(context.Background(), "lab-web", metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace: %v", err)
	}
	if _, err := c.Dynamic.Resource(gvrDeploy).Namespace("lab-web").Get(context.Background(), "api", metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment should be created in the task namespace: %v", err)
	}
	if _, err := c.Dynamic.Resource(gvrCRole).Get(context.Background(), "lab-reader", metav1.GetOptions{}); err != nil {
		t.Fatalf("cluster-scoped object: %v", err)
	}
	if len(ex.calls) != 1 || ex.calls[0] != "node01|echo hi" {
		t.Fatalf("calls = %v", ex.calls)
	}
}

func TestPrepareTask_RecreatesExistingNamespace(t *testing.T) {
	c := testClients(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "lab-web"}})
	cs := c.Kube.(*kubefake.Clientset)
	if err := PrepareTask(context.Background(), c, &fakeExec{}, testTask()); err != nil {
		t.Fatal(err)
	}
	var verbs []string
	for _, a := range cs.Actions() {
		if a.GetResource().Resource == "namespaces" && (a.GetVerb() == "delete" || a.GetVerb() == "create") {
			verbs = append(verbs, a.GetVerb())
		}
	}
	if strings.Join(verbs, ",") != "delete,create" {
		t.Fatalf("namespace actions = %v, want delete then create", verbs)
	}
}

func TestPrepareTask_ExistingClusterObjectIsUpdated(t *testing.T) {
	c := testClients()
	task := testTask()
	if err := PrepareTask(context.Background(), c, &fakeExec{}, task); err != nil {
		t.Fatal(err)
	}
	// Second run: the ClusterRole survives namespace recreation and must not fail with AlreadyExists.
	if err := PrepareTask(context.Background(), c, &fakeExec{}, task); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareTask_CommandFailure(t *testing.T) {
	c := testClients()
	err := PrepareTask(context.Background(), c, &fakeExec{code: 1, out: "boom"}, testTask())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestVagrantSnapshotter_Args(t *testing.T) {
	var gotDir string
	var gotArgs []string
	s := VagrantSnapshotter{Dir: "/lab", Runner: func(_ context.Context, dir string, args ...string) error {
		gotDir, gotArgs = dir, args
		return nil
	}}
	if err := s.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotDir != "/lab" || strings.Join(gotArgs, " ") != "snapshot restore --no-provision clean" {
		t.Fatalf("dir=%s args=%v", gotDir, gotArgs)
	}
}

func init() { nsPollInterval = time.Millisecond }
