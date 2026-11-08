package check

import (
	"context"
	"slices"
	"testing"

	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// sarEnv answers SubjectAccessReviews with allow(review) and records the last one.
func sarEnv(t *testing.T, allow func(authzv1.SubjectAccessReviewSpec) bool) (Env, *authzv1.SubjectAccessReviewSpec) {
	env := testEnv(t, nil)
	cs := kubefake.NewClientset()
	var last authzv1.SubjectAccessReviewSpec
	cs.PrependReactor("create", "subjectaccessreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		sar := a.(k8stesting.CreateAction).GetObject().(*authzv1.SubjectAccessReview).DeepCopy()
		last = sar.Spec
		sar.Status.Allowed = allow(sar.Spec)
		return true, sar, nil
	})
	env.Kube = cs
	return env, &last
}

func TestCanI_ServiceAccountMapping(t *testing.T) {
	env, last := sarEnv(t, func(s authzv1.SubjectAccessReviewSpec) bool {
		return s.User == "system:serviceaccount:web:deployer"
	})
	spec := Spec{Type: "canI", Description: "deployer can create deployments",
		Subject: &Subject{ServiceAccount: "web:deployer"}, Verb: "create", Resource: "deployments", Namespace: "web"}
	res := Run(context.Background(), env, []Spec{spec})
	if !res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
	if last.ResourceAttributes.Namespace != "web" || last.ResourceAttributes.Verb != "create" {
		t.Fatalf("attributes = %+v", last.ResourceAttributes)
	}
	if !slices.Contains(last.Groups, "system:serviceaccounts:web") {
		t.Fatalf("groups = %v", last.Groups)
	}
}

func TestCanI_ExpectDenied(t *testing.T) {
	env, last := sarEnv(t, func(authzv1.SubjectAccessReviewSpec) bool { return false })
	spec := Spec{Type: "canI", Description: "jane cannot delete pods",
		Subject: &Subject{User: "jane"}, Verb: "delete", Resource: "pods", Allowed: boolPtr(false)}
	res := Run(context.Background(), env, []Spec{spec})
	if !res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
	if last.ResourceAttributes.Namespace != "lab-test" {
		t.Fatalf("namespace should default to task ns, got %q", last.ResourceAttributes.Namespace)
	}
}

func TestCanI_AllowedMismatch(t *testing.T) {
	env, _ := sarEnv(t, func(authzv1.SubjectAccessReviewSpec) bool { return false })
	spec := Spec{Type: "canI", Description: "d", Subject: &Subject{Group: "devs"}, Verb: "get", Resource: "pods"}
	res := Run(context.Background(), env, []Spec{spec})
	if res[0].Pass || res[0].Actual != "denied" {
		t.Fatalf("got %+v", res[0])
	}
}

func TestCanI_ResourceWithGroup(t *testing.T) {
	env, last := sarEnv(t, func(authzv1.SubjectAccessReviewSpec) bool { return true })
	spec := Spec{Type: "canI", Description: "d", Subject: &Subject{User: "u"}, Verb: "create", Resource: "deployments.apps"}
	Run(context.Background(), env, []Spec{spec})
	if last.ResourceAttributes.Resource != "deployments" || last.ResourceAttributes.Group != "apps" {
		t.Fatalf("attributes = %+v", last.ResourceAttributes)
	}
}
