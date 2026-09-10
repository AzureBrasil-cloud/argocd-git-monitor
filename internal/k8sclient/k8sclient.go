// Package k8sclient builds a Kubernetes clientset that works both when
// git-monitor runs inside a cluster (the normal case) and when it's pointed
// at a cluster via KUBECONFIG for local development.
package k8sclient

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// New builds a Kubernetes clientset, preferring in-cluster config and
// falling back to KUBECONFIG (or ~/.kube/config) for local runs.
func New() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		if kubeconfig != "" {
			loadingRules.ExplicitPath = kubeconfig
		}
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("building kubernetes client config: %w", err)
		}
	}
	return kubernetes.NewForConfig(cfg)
}

// namespaceFile is where the service account controller mounts the pod's
// own namespace.
const namespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// OwnNamespace returns the namespace git-monitor itself is running in,
// detected from the mounted service account, falling back to "default"
// outside a cluster.
func OwnNamespace() string {
	if b, err := os.ReadFile(namespaceFile); err == nil {
		if ns := strings.TrimSpace(string(b)); ns != "" {
			return ns
		}
	}
	return "default"
}
