// Package cluster connects the trainer to the lab cluster: API clients, SSH to nodes,
// task setup and snapshot restore.
package cluster

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type Clients struct {
	Dynamic dynamic.Interface
	Mapper  meta.RESTMapper
	Kube    kubernetes.Interface
}

// NewClients builds clients from the kubeconfig written by `./lab up`.
func NewClients(kubeconfigPath string) (*Clients, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %s: %w", kubeconfigPath, err)
	}
	// Fail fast when the VMs are stopped instead of hanging the UI.
	cfg.Timeout = 5 * time.Second
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	// Deferred + memory cache: discovery happens on first use and CRDs created by tasks
	// are picked up after a cache reset.
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disc))
	return &Clients{Dynamic: dyn, Mapper: mapper, Kube: kube}, nil
}
