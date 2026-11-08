package check

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
)

func runResource(ctx context.Context, env Env, s Spec) (Result, error) {
	objs, err := fetchObjects(ctx, env, s.Target)
	if err != nil {
		if apiError(err) != nil {
			return Result{}, apiError(err)
		}
		return Result{Actual: err.Error()}, nil
	}
	if len(s.Assert) == 0 {
		if len(objs) == 0 {
			return Result{Actual: "not found"}, nil
		}
		return Result{Pass: true, Actual: fmt.Sprintf("%d found", len(objs))}, nil
	}
	for _, a := range s.Assert {
		pass, actual, err := evalAssertion(objs, a)
		if err != nil {
			return Result{Actual: err.Error()}, nil
		}
		if !pass {
			return Result{Actual: actual}, nil
		}
	}
	return Result{Pass: true}, nil
}

func fetchObjects(ctx context.Context, env Env, t *Target) ([]unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(t.APIVersion)
	if err != nil {
		return nil, err
	}
	mapping, err := env.Mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: t.Kind}, gv.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("unknown kind %s %s", t.APIVersion, t.Kind)
		}
		return nil, err
	}
	ri := env.Dynamic.Resource(mapping.Resource)
	var client interface {
		Get(context.Context, string, metav1.GetOptions, ...string) (*unstructured.Unstructured, error)
		List(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error)
	} = ri
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := t.Namespace
		if ns == "" {
			ns = env.Namespace
		}
		client = ri.Namespace(ns)
	}
	if t.Name != "" {
		obj, err := client.Get(ctx, t.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []unstructured.Unstructured{*obj}, nil
	}
	list, err := client.List(ctx, metav1.ListOptions{LabelSelector: t.LabelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// evalAssertion checks a on every object; zero objects means "not found".
func evalAssertion(objs []unstructured.Unstructured, a Assertion) (bool, string, error) {
	if a.Op == "count" {
		want, err := strconv.Atoi(a.Value)
		if err != nil {
			return false, "", fmt.Errorf("count needs an integer, got %q", a.Value)
		}
		return len(objs) == want, strconv.Itoa(len(objs)), nil
	}
	if len(objs) == 0 {
		pass, err := Compare(a.Op, nil, false, a.Value)
		return pass, "not found", err
	}
	var all []string
	for _, o := range objs {
		values := []string{}
		found := true
		if a.Path != "" {
			var err error
			values, err = evalPath(o.Object, a.Path)
			if err != nil {
				return false, "", err
			}
			found = len(values) > 0
		}
		pass, err := Compare(a.Op, values, found, a.Value)
		if err != nil {
			return false, "", err
		}
		all = append(all, values...)
		if !pass {
			if !found {
				return false, "not set", nil
			}
			return false, strings.Join(values, ","), nil
		}
	}
	return true, strings.Join(all, ","), nil
}

func evalPath(obj map[string]any, path string) ([]string, error) {
	jp := jsonpath.New("check").AllowMissingKeys(true)
	if err := jp.Parse(path); err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	results, err := jp.FindResults(obj)
	if err != nil {
		return nil, nil // a missing intermediate field is "not set", not an error
	}
	var out []string
	for _, group := range results {
		for _, v := range group {
			if !v.IsValid() || (v.CanInterface() && v.Interface() == nil) {
				continue
			}
			out = append(out, fmt.Sprint(v.Interface()))
		}
	}
	return out, nil
}
