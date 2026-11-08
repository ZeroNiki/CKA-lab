package check

import (
	"context"
	"strings"

	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runCanI(ctx context.Context, env Env, s Spec) (Result, error) {
	ns := s.Namespace
	if ns == "" {
		ns = env.Namespace
	}
	// "deployments.apps" means resource deployments in API group apps, as in kubectl auth can-i.
	resource, group, _ := strings.Cut(s.Resource, ".")
	spec := authzv1.SubjectAccessReviewSpec{
		ResourceAttributes: &authzv1.ResourceAttributes{
			Namespace:   ns,
			Verb:        s.Verb,
			Group:       group,
			Resource:    resource,
			Subresource: s.Subresource,
		},
	}
	switch {
	case s.Subject.ServiceAccount != "":
		saNS, name, _ := strings.Cut(s.Subject.ServiceAccount, ":")
		spec.User = "system:serviceaccount:" + saNS + ":" + name
		// The API server adds these groups to every ServiceAccount token; bindings to them must count.
		spec.Groups = []string{"system:serviceaccounts", "system:serviceaccounts:" + saNS, "system:authenticated"}
	case s.Subject.User != "":
		spec.User = s.Subject.User
		if s.Subject.Group != "" {
			spec.Groups = []string{s.Subject.Group}
		}
	default:
		spec.Groups = []string{s.Subject.Group}
	}
	sar, err := env.Kube.AuthorizationV1().SubjectAccessReviews().Create(ctx,
		&authzv1.SubjectAccessReview{Spec: spec}, metav1.CreateOptions{})
	if err != nil {
		if e := apiError(err); e != nil {
			return Result{}, e
		}
		return Result{Actual: err.Error()}, nil
	}
	want := s.Allowed == nil || *s.Allowed
	actual := "denied"
	if sar.Status.Allowed {
		actual = "allowed"
	}
	return Result{Pass: sar.Status.Allowed == want, Actual: actual}, nil
}
