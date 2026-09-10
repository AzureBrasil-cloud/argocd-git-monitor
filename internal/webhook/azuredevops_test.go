package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Send(t *testing.T) {
	var gotAuthUser, gotAuthPass string
	var gotAuthOK bool
	var gotBody gitPushEvent
	var gotPath string
	var gotActivityID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotActivityID = r.Header.Get("X-Vss-ActivityId")
		gotAuthUser, gotAuthPass, gotAuthOK = r.BasicAuth()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := NewClient(ProviderAzureDevOps, srv.URL, "argocd-webhook", "s3cret", false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.Send(context.Background(), Change{
		RepoURL:          "git@ssh.dev.azure.com:v3/bdevs/azbr/azbr-continuous-deployment",
		Ref:              "refs/heads/main",
		DefaultBranchRef: "refs/heads/main",
		NewSHA:           "newsha123",
		OldSHA:           "oldsha456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/api/webhook" {
		t.Errorf("unexpected path: %q", gotPath)
	}
	if gotActivityID == "" {
		t.Error("expected X-Vss-ActivityId header to be set (ArgoCD's Azure DevOps parser requires it to be present)")
	}
	if !gotAuthOK || gotAuthUser != "argocd-webhook" || gotAuthPass != "s3cret" {
		t.Errorf("unexpected basic auth: user=%q pass=%q ok=%v", gotAuthUser, gotAuthPass, gotAuthOK)
	}
	if gotBody.EventType != gitPushEventType {
		t.Errorf("unexpected eventType: %q", gotBody.EventType)
	}
	if gotBody.Resource.Repository.RemoteURL != "git@ssh.dev.azure.com:v3/bdevs/azbr/azbr-continuous-deployment" {
		t.Errorf("unexpected remoteUrl: %q", gotBody.Resource.Repository.RemoteURL)
	}
	if gotBody.Resource.Repository.DefaultBranch != "refs/heads/main" {
		t.Errorf("unexpected defaultBranch: %q", gotBody.Resource.Repository.DefaultBranch)
	}
	if len(gotBody.Resource.RefUpdates) != 1 || gotBody.Resource.RefUpdates[0].NewObjectID != "newsha123" {
		t.Errorf("unexpected refUpdates: %+v", gotBody.Resource.RefUpdates)
	}
}

func TestClient_Send_NoAuthWhenNotConfigured(t *testing.T) {
	var gotAuthOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, gotAuthOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := NewClient(ProviderAzureDevOps, srv.URL, "", "", false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Send(context.Background(), Change{RepoURL: "x", Ref: "refs/heads/main", NewSHA: "sha"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuthOK {
		t.Error("expected no Authorization header to be sent")
	}
}

func TestClient_Send_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client, err := NewClient(ProviderAzureDevOps, srv.URL, "user", "wrong", false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.Send(context.Background(), Change{RepoURL: "x", Ref: "refs/heads/main", NewSHA: "sha"})
	if err == nil {
		t.Fatal("expected an error for non-2xx response")
	}
}
