package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"cka-lab/internal/check"
	"cka-lab/internal/content"
)

const nsDeleteTimeout = 120 * time.Second

// nsPollInterval is a variable so tests can shorten it.
var nsPollInterval = time.Second

// PrepareTask gives the task a fresh namespace, applies its manifests and runs its setup commands.
func PrepareTask(ctx context.Context, c *Clients, exec check.NodeExecutor, t *content.Task) error {
	if err := recreateNamespace(ctx, c, t.NS); err != nil {
		return err
	}
	for i, m := range t.Setup.Manifests {
		for _, doc := range content.SplitDocuments(m) {
			if err := applyDocument(ctx, c, t.NS, doc); err != nil {
				return fmt.Errorf("setup.manifests[%d]: %w", i, err)
			}
		}
	}
	for i, cmd := range t.Setup.Commands {
		out, code, err := exec.Run(ctx, cmd.Node, cmd.Run)
		if err != nil {
			return fmt.Errorf("setup.commands[%d] on %s: %w", i, cmd.Node, err)
		}
		if code != 0 {
			return fmt.Errorf("setup.commands[%d] on %s exited %d: %s", i, cmd.Node, code, out)
		}
	}
	return nil
}

func recreateNamespace(ctx context.Context, c *Clients, ns string) error {
	nsClient := c.Kube.CoreV1().Namespaces()
	err := nsClient.Delete(ctx, ns, metav1.DeleteOptions{})
	// Conflict means the namespace is already terminating: wait for it like after our own delete.
	if apierrors.IsConflict(err) {
		err = nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return wrapAPI(err)
	}
	if err == nil {
		deadline := time.Now().Add(nsDeleteTimeout)
		for {
			_, err := nsClient.Get(ctx, ns, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil {
				return wrapAPI(err)
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("namespace %s is still terminating after %s", ns, nsDeleteTimeout)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(nsPollInterval):
			}
		}
	}
	_, err = nsClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	return wrapAPI(err)
}

// applyDocument creates the object, or updates it if it already exists (cluster-scoped objects
// survive namespace recreation).
func applyDocument(ctx context.Context, c *Clients, ns, doc string) error {
	var obj unstructured.Unstructured
	if err := yaml.Unmarshal([]byte(doc), &obj.Object); err != nil {
		return err
	}
	gvk := obj.GroupVersionKind()
	mapping, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("%s: %w", gvk, err)
	}
	var ri dynamic.ResourceInterface = c.Dynamic.Resource(mapping.Resource)
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(ns)
		}
		ri = c.Dynamic.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	}
	_, err = ri.Create(ctx, &obj, metav1.CreateOptions{FieldManager: "cka-lab"})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if getErr != nil {
			return wrapAPI(getErr)
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		_, err = ri.Update(ctx, &obj, metav1.UpdateOptions{FieldManager: "cka-lab"})
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", gvk.Kind, obj.GetName(), wrapAPI(err))
	}
	return nil
}

// wrapAPI marks transport errors as check.ErrUnavailable so the UI can say "is the lab running?".
func wrapAPI(err error) error {
	if err == nil {
		return nil
	}
	var status apierrors.APIStatus
	if apierrors.IsNotFound(err) || errors.As(err, &status) {
		return err
	}
	return fmt.Errorf("%w: %v", check.ErrUnavailable, err)
}
