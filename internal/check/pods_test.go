package check

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func pod(name string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "lab-test", Labels: map[string]string{"app": "api"}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
	}
}

func podsSpec(min int) Spec {
	return Spec{Type: "podsReady", Description: "api ready", Selector: "app=api", Min: min}
}

func TestPodsReady_AllReady(t *testing.T) {
	env := testEnv(t, nil, pod("a", true), pod("b", true))
	if res := Run(context.Background(), env, []Spec{podsSpec(2)}); !res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
}

func TestPodsReady_OneNotReady(t *testing.T) {
	env := testEnv(t, nil, pod("a", true), pod("b", false))
	res := Run(context.Background(), env, []Spec{podsSpec(0)})
	if res[0].Pass || res[0].Actual != "1/2 Ready" {
		t.Fatalf("got %+v", res[0])
	}
}

func TestPodsReady_BelowMin(t *testing.T) {
	env := testEnv(t, nil, pod("a", true))
	if res := Run(context.Background(), env, []Spec{podsSpec(3)}); res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
}

func TestPodsReady_NoPodsFails(t *testing.T) {
	env := testEnv(t, []runtime.Object{})
	if res := Run(context.Background(), env, []Spec{podsSpec(0)}); res[0].Pass {
		t.Fatalf("got %+v", res[0])
	}
}
