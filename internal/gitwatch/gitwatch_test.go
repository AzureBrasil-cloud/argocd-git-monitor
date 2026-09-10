package gitwatch

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestParseSymrefOutput(t *testing.T) {
	out := "ref: refs/heads/main\tHEAD\n" +
		"9daeafb9864cf43055ae93beb0afd6c243396788\tHEAD\n"

	res, err := parseSymrefOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SHA != "9daeafb9864cf43055ae93beb0afd6c243396788" {
		t.Errorf("unexpected sha: %q", res.SHA)
	}
	if res.Ref != "refs/heads/main" {
		t.Errorf("unexpected ref: %q", res.Ref)
	}
	if res.DefaultBranchRef != "refs/heads/main" {
		t.Errorf("unexpected default branch ref: %q", res.DefaultBranchRef)
	}
}

func TestParseSymrefOutput_MissingSHA(t *testing.T) {
	if _, err := parseSymrefOutput("ref: refs/heads/main\tHEAD\n"); err == nil {
		t.Fatal("expected error when sha line is missing")
	}
}

func TestParseLsRemoteOutput(t *testing.T) {
	out := "abc123\trefs/heads/develop\n"
	res, err := parseLsRemoteOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SHA != "abc123" || res.Ref != "refs/heads/develop" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestParseLsRemoteOutput_NotFound(t *testing.T) {
	if _, err := parseLsRemoteOutput(""); err == nil {
		t.Fatal("expected error for empty output")
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"git@ssh.dev.azure.com:v3/org/proj/repo":     "git@ssh.dev.azure.com:v3/org/proj/repo",
		"git@ssh.dev.azure.com:v3/org/proj/repo/":    "git@ssh.dev.azure.com:v3/org/proj/repo",
		"git@ssh.dev.azure.com:v3/org/proj/repo.git": "git@ssh.dev.azure.com:v3/org/proj/repo",
		"  https://example.com/repo.git  ":           "https://example.com/repo",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindCredentials(t *testing.T) {
	repoURL := "git@ssh.dev.azure.com:v3/bdevs/azbr/azbr-continuous-deployment"

	client := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "repo-other",
				Namespace: "argocd",
				Labels:    map[string]string{"argocd.argoproj.io/secret-type": "repository"},
			},
			Data: map[string][]byte{
				"url": []byte("https://charts.jetstack.io"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "repo-target",
				Namespace: "argocd",
				Labels:    map[string]string{"argocd.argoproj.io/secret-type": "repository"},
			},
			Data: map[string][]byte{
				"url":           []byte(repoURL + "/"),
				"sshPrivateKey": []byte("PRIVATE-KEY-MATERIAL"),
			},
		},
	)

	creds, ok, err := FindCredentials(context.Background(), client, "argocd", repoURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected to find matching credentials")
	}
	if string(creds.SSHPrivateKey) != "PRIVATE-KEY-MATERIAL" {
		t.Errorf("unexpected ssh key: %q", creds.SSHPrivateKey)
	}
}

func TestNormalizeSSHKey(t *testing.T) {
	cases := map[string]string{
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----":       "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n":     "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n\n\n": "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
	}
	for in, want := range cases {
		if got := string(normalizeSSHKey([]byte(in))); got != want {
			t.Errorf("normalizeSSHKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindCredentials_NoMatch(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "repo-other",
			Namespace: "argocd",
			Labels:    map[string]string{"argocd.argoproj.io/secret-type": "repository"},
		},
		Data: map[string][]byte{"url": []byte("https://charts.jetstack.io")},
	})

	_, ok, err := FindCredentials(context.Background(), client, "argocd", "git@example.com:org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no match")
	}
}
