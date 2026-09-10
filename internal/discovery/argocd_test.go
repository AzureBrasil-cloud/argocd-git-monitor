package discovery

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestArgoCDServerBaseURL_ByLabel(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-server",
			Namespace: "argocd",
			Labels:    map[string]string{"app.kubernetes.io/name": "argocd-server"},
		},
	})

	url, err := ArgoCDServerBaseURL(context.Background(), client, "argocd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://argocd-server.argocd.svc.cluster.local"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestArgoCDServerBaseURL_FallbackByName(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-server",
			Namespace: "argocd",
			// deliberately no label, to exercise the name-based fallback
		},
	})

	url, err := ArgoCDServerBaseURL(context.Background(), client, "argocd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://argocd-server.argocd.svc.cluster.local"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestArgoCDServerBaseURL_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()

	if _, err := ArgoCDServerBaseURL(context.Background(), client, "argocd"); err == nil {
		t.Fatal("expected an error when no matching service exists")
	}
}
