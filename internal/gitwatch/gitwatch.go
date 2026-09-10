// Package gitwatch detects new commits on a remote git repository by
// shelling out to `git ls-remote`, reusing whatever SSH credentials ArgoCD
// itself already has configured for that repository.
package gitwatch

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

//go:embed known_hosts
var embeddedFS embed.FS

// repoSecretLabelSelector matches ArgoCD's own repository Secrets:
// https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/#repositories
const repoSecretLabelSelector = "argocd.argoproj.io/secret-type=repository"

// Credentials holds what's needed to authenticate git operations against a
// repository, extracted from an existing ArgoCD repository Secret.
type Credentials struct {
	SSHPrivateKey []byte
	Username      string
	Password      string
}

// FindCredentials looks through every ArgoCD repository Secret in
// namespace, and returns the credentials for the one whose "url" field
// matches repoURL. It returns ok=false if no matching Secret is found (the
// repo may be public, or credentials may need to be supplied another way).
func FindCredentials(ctx context.Context, client kubernetes.Interface, namespace, repoURL string) (creds Credentials, ok bool, err error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: repoSecretLabelSelector,
	})
	if err != nil {
		return Credentials{}, false, fmt.Errorf("listing ArgoCD repository secrets in %s: %w", namespace, err)
	}

	want := normalizeURL(repoURL)
	for _, secret := range secrets.Items {
		if normalizeURL(string(secret.Data["url"])) != want {
			continue
		}
		return credentialsFromSecret(secret), true, nil
	}
	return Credentials{}, false, nil
}

func credentialsFromSecret(secret corev1.Secret) Credentials {
	return Credentials{
		SSHPrivateKey: secret.Data["sshPrivateKey"],
		Username:      string(secret.Data["username"]),
		Password:      string(secret.Data["password"]),
	}
}

// normalizeURL makes repo URL comparisons tolerant of a trailing slash,
// trailing ".git", and surrounding whitespace, without needing a full git
// URL-normalization implementation.
func normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	return url
}

// Workspace holds the temporary files git needs to authenticate as a given
// identity, and must be cleaned up with Close when no longer needed.
type Workspace struct {
	dir       string
	sshCmd    string
	basicAuth *Credentials
}

// NewWorkspace writes out an SSH identity file (when creds carries one) and
// the known_hosts file (the embedded list, plus extraKnownHosts if given)
// into a private temp directory, returning a Workspace ready to run git
// commands.
func NewWorkspace(creds Credentials, extraKnownHosts string) (*Workspace, error) {
	dir, err := os.MkdirTemp("", "git-monitor-")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	ws := &Workspace{dir: dir}

	if len(creds.SSHPrivateKey) > 0 {
		keyPath := filepath.Join(dir, "id")
		if err := os.WriteFile(keyPath, normalizeSSHKey(creds.SSHPrivateKey), 0o600); err != nil {
			ws.Close()
			return nil, fmt.Errorf("writing ssh key: %w", err)
		}

		knownHosts, err := embeddedFS.ReadFile("known_hosts")
		if err != nil {
			ws.Close()
			return nil, fmt.Errorf("reading embedded known_hosts: %w", err)
		}
		if extraKnownHosts != "" {
			knownHosts = append(knownHosts, '\n')
			knownHosts = append(knownHosts, []byte(extraKnownHosts)...)
		}
		knownHostsPath := filepath.Join(dir, "known_hosts")
		if err := os.WriteFile(knownHostsPath, knownHosts, 0o600); err != nil {
			ws.Close()
			return nil, fmt.Errorf("writing known_hosts: %w", err)
		}

		ws.sshCmd = fmt.Sprintf("ssh -i %s -o UserKnownHostsFile=%s -o IdentitiesOnly=yes",
			shellQuote(keyPath), shellQuote(knownHostsPath))
	}

	if creds.Username != "" || creds.Password != "" {
		ws.basicAuth = &creds
		if err := writeAskpassScript(dir, creds.Username, creds.Password); err != nil {
			ws.Close()
			return nil, fmt.Errorf("writing askpass script: %w", err)
		}
	}

	return ws, nil
}

// writeAskpassScript writes a GIT_ASKPASS helper so `git ls-remote` can
// authenticate against HTTPS remotes non-interactively, without ever
// passing the credentials on the command line.
func writeAskpassScript(dir, username, password string) error {
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  *Username*) echo " + shellQuote(username) + " ;;\n" +
		"  *Password*) echo " + shellQuote(password) + " ;;\n" +
		"esac\n"
	path := filepath.Join(dir, "askpass.sh")
	return os.WriteFile(path, []byte(script), 0o700)
}

// Close removes the workspace's temporary files.
func (w *Workspace) Close() error {
	if w.dir == "" {
		return nil
	}
	return os.RemoveAll(w.dir)
}

// Result is what ls-remote found for the watched revision.
type Result struct {
	// SHA is the commit the revision currently points to.
	SHA string
	// Ref is the fully-qualified ref name (e.g. "refs/heads/main") that was
	// resolved. Empty if it couldn't be determined (unusual).
	Ref string
	// DefaultBranchRef is set to the same value as Ref only when Revision
	// was "HEAD", signalling that this ref update touched the remote's
	// default branch.
	DefaultBranchRef string
}

// LsRemote resolves revision on repoURL using this workspace's credentials.
func (w *Workspace) LsRemote(ctx context.Context, repoURL, revision string) (Result, error) {
	var args []string
	if revision == "HEAD" || revision == "" {
		args = []string{"ls-remote", "--symref", repoURL, "HEAD"}
	} else {
		args = []string{"ls-remote", repoURL, revision}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = w.gitEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	if revision == "HEAD" || revision == "" {
		return parseSymrefOutput(stdout.String())
	}
	return parseLsRemoteOutput(stdout.String())
}

func (w *Workspace) gitEnv() []string {
	env := os.Environ()
	if w.sshCmd != "" {
		env = append(env, "GIT_SSH_COMMAND="+w.sshCmd)
	}
	if w.basicAuth != nil {
		env = append(env,
			"GIT_ASKPASS="+filepath.Join(w.dir, "askpass.sh"),
			"GIT_TERMINAL_PROMPT=0",
		)
	}
	return env
}

// parseSymrefOutput parses the output of `git ls-remote --symref <repo> HEAD`,
// e.g.:
//
//	ref: refs/heads/main	HEAD
//	9daeafb9864cf43055ae93beb0afd6c243396788	HEAD
func parseSymrefOutput(out string) (Result, error) {
	var res Result
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, "ref: "); ok {
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				res.Ref = fields[0]
				res.DefaultBranchRef = fields[0]
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "HEAD" {
			res.SHA = fields[0]
		}
	}
	if res.SHA == "" {
		return Result{}, fmt.Errorf("could not determine HEAD sha from ls-remote output: %q", out)
	}
	return res, nil
}

// parseLsRemoteOutput parses the output of `git ls-remote <repo> <revision>`,
// e.g.:
//
//	9daeafb9864cf43055ae93beb0afd6c243396788	refs/heads/develop
func parseLsRemoteOutput(out string) (Result, error) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 {
			return Result{SHA: fields[0], Ref: fields[1]}, nil
		}
	}
	return Result{}, fmt.Errorf("revision not found in ls-remote output: %q", out)
}

// normalizeSSHKey ensures the key ends with exactly one trailing newline.
// Some sources (e.g. a value passed through Terraform's file()/chomp())
// strip the final newline from a PEM/OpenSSH key, which makes OpenSSH's
// parser fail with an opaque "error in libcrypto" - trust the key material,
// not its exact byte-for-byte formatting.
func normalizeSSHKey(key []byte) []byte {
	trimmed := bytes.TrimRight(key, "\n")
	return append(trimmed, '\n')
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
