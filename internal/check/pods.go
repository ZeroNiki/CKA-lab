package check

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runPodsReady(ctx context.Context, env Env, s Spec) (Result, error) {
	ns := s.Namespace
	if ns == "" {
		ns = env.Namespace
	}
	pods, err := env.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: s.Selector})
	if err != nil {
		if e := apiError(err); e != nil {
			return Result{}, e
		}
		return Result{Actual: err.Error()}, nil
	}
	ready := 0
	for _, p := range pods.Items {
		if isReady(p) {
			ready++
		}
	}
	total := len(pods.Items)
	want := max(s.Min, 1)
	return Result{
		Pass:   total >= want && ready == total,
		Actual: fmt.Sprintf("%d/%d Ready", ready, total),
	}, nil
}

func isReady(p corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
