package webhook

import "testing"

func TestNewClient_UnsupportedProvider(t *testing.T) {
	_, err := NewClient(Provider("github"), "https://argocd.example.com", "", "", false)
	if err == nil {
		t.Fatal("expected an error for an unsupported provider")
	}
}

func TestNewClient_SupportedProvider(t *testing.T) {
	if _, err := NewClient(ProviderAzureDevOps, "https://argocd.example.com", "", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
