// Package discovery finds the ArgoCD server's base URL by looking at the
// Kubernetes API, for installs that don't configure GIT_MONITOR_WEBHOOK_URL
// explicitly.
package discovery

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// argoCDServerLabelSelector matches the Service created by every official
// ArgoCD installation method (argo-helm chart and the upstream manifests).
const argoCDServerLabelSelector = "app.kubernetes.io/name=argocd-server"

// fallbackServiceName is tried when no Service carries the standard label,
// e.g. on older or heavily customized installs.
const fallbackServiceName = "argocd-server"

// ArgoCDServerBaseURL finds the argocd-server Service in namespace and
// returns its in-cluster base URL (e.g.
// "https://argocd-server.argocd.svc.cluster.local").
func ArgoCDServerBaseURL(ctx context.Context, client kubernetes.Interface, namespace string) (string, error) {
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: argoCDServerLabelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("listing services in %s: %w", namespace, err)
	}

	if len(services.Items) > 0 {
		return serviceBaseURL(services.Items[0].Name, namespace), nil
	}

	if _, err := client.CoreV1().Services(namespace).Get(ctx, fallbackServiceName, metav1.GetOptions{}); err == nil {
		return serviceBaseURL(fallbackServiceName, namespace), nil
	} else if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("getting service %s/%s: %w", namespace, fallbackServiceName, err)
	}

	return "", fmt.Errorf(
		"could not find the argocd-server Service in namespace %q (looked for label %q and name %q); "+
			"set GIT_MONITOR_WEBHOOK_URL explicitly",
		namespace, argoCDServerLabelSelector, fallbackServiceName)
}

func serviceBaseURL(serviceName, namespace string) string {
	return fmt.Sprintf("https://%s.%s.svc.cluster.local", serviceName, namespace)
}
