package state

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestStore_CreatesSecretAndPersists(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	store, err := New(ctx, client, "git-monitor", "git-monitor-state")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	last, err := store.LastCommit(ctx)
	if err != nil {
		t.Fatalf("LastCommit: %v", err)
	}
	if last != "" {
		t.Errorf("expected empty last commit on a fresh secret, got %q", last)
	}

	if err := store.SetLastCommit(ctx, "abc123"); err != nil {
		t.Fatalf("SetLastCommit: %v", err)
	}

	last, err = store.LastCommit(ctx)
	if err != nil {
		t.Fatalf("LastCommit: %v", err)
	}
	if last != "abc123" {
		t.Errorf("expected persisted commit abc123, got %q", last)
	}
}

func TestStore_ReusesExistingSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	first, err := New(ctx, client, "git-monitor", "git-monitor-state")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := first.SetLastCommit(ctx, "existing-sha"); err != nil {
		t.Fatalf("SetLastCommit: %v", err)
	}

	second, err := New(ctx, client, "git-monitor", "git-monitor-state")
	if err != nil {
		t.Fatalf("New (second): %v", err)
	}
	last, err := second.LastCommit(ctx)
	if err != nil {
		t.Fatalf("LastCommit: %v", err)
	}
	if last != "existing-sha" {
		t.Errorf("expected New to reuse the existing secret's data, got %q", last)
	}
}
