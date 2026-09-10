// Package config reads git-monitor's runtime configuration from environment
// variables. Every setting is generic: nothing here knows about Azure DevOps,
// Terraform, or any specific ArgoCD installation.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the fully resolved runtime configuration.
type Config struct {
	// RepoURL is the git repository to poll. It must match the "url" field
	// of an ArgoCD repository Secret so git-monitor can reuse its credentials.
	RepoURL string
	// Revision is the branch/tag to watch. "HEAD" resolves to the remote's
	// default branch.
	Revision string
	// PollInterval is how often to run `git ls-remote`.
	PollInterval time.Duration

	// ArgoCDNamespace is where ArgoCD's repository Secrets (and, if
	// auto-discovery is used, its Service) live.
	ArgoCDNamespace string

	// ExtraKnownHosts is appended to the embedded known_hosts file,
	// letting self-hosted git servers (not in the built-in list of
	// GitHub/GitLab/Bitbucket/Azure DevOps) be trusted for SSH access.
	ExtraKnownHosts string

	// StateSecretName is the Secret (in git-monitor's own namespace) used to
	// persist the last commit that was already reported, so a pod restart
	// doesn't re-fire a webhook for a commit already handled.
	StateSecretName string
	// Namespace is git-monitor's own namespace, used to read/write the state
	// Secret. Auto-detected from the in-cluster service account when empty.
	Namespace string

	// WebhookProvider selects the payload shape sent to ArgoCD's webhook
	// endpoint (e.g. "azuredevops") - see internal/webhook for the
	// supported values and how each one authenticates.
	WebhookProvider string
	// WebhookURL is the ArgoCD base URL (e.g. https://argocd.example.com).
	// When empty, it is auto-discovered from the argocd-server Service.
	WebhookURL string
	// WebhookUsername/WebhookPassword are optional HTTP basic-auth
	// credentials for ArgoCD's webhook endpoint.
	WebhookUsername string
	WebhookPassword string
	// InsecureSkipVerify disables TLS certificate verification when calling
	// the webhook endpoint. Commonly needed when WebhookURL is
	// auto-discovered and points at an in-cluster Service with a
	// self-signed certificate.
	InsecureSkipVerify bool

	LogLevel string

	// DryRun logs what would be sent to the webhook instead of sending it.
	// Useful to validate polling/discovery/credentials on a real cluster
	// before letting git-monitor trigger real ArgoCD syncs.
	DryRun bool
}

const (
	defaultRevision        = "HEAD"
	defaultPollInterval    = 10 * time.Second
	defaultArgoCDNamespace = "argocd"
	defaultStateSecretName = "git-monitor-state"
	defaultLogLevel        = "info"
	defaultWebhookProvider = "azuredevops"
)

// Load reads configuration from environment variables and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		RepoURL:         os.Getenv("GIT_MONITOR_REPO_URL"),
		Revision:        getEnvDefault("GIT_MONITOR_REVISION", defaultRevision),
		ArgoCDNamespace: getEnvDefault("GIT_MONITOR_ARGOCD_NAMESPACE", defaultArgoCDNamespace),
		ExtraKnownHosts: os.Getenv("GIT_MONITOR_EXTRA_KNOWN_HOSTS"),
		StateSecretName: getEnvDefault("GIT_MONITOR_STATE_SECRET_NAME", defaultStateSecretName),
		Namespace:       os.Getenv("GIT_MONITOR_NAMESPACE"),
		WebhookProvider: getEnvDefault("GIT_MONITOR_WEBHOOK_PROVIDER", defaultWebhookProvider),
		WebhookURL:      os.Getenv("GIT_MONITOR_WEBHOOK_URL"),
		WebhookUsername: os.Getenv("GIT_MONITOR_WEBHOOK_USERNAME"),
		WebhookPassword: os.Getenv("GIT_MONITOR_WEBHOOK_PASSWORD"),
		LogLevel:        getEnvDefault("GIT_MONITOR_LOG_LEVEL", defaultLogLevel),
	}

	interval, err := parseDurationDefault("GIT_MONITOR_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		return nil, err
	}
	cfg.PollInterval = interval

	insecure, err := parseBoolDefault("GIT_MONITOR_ARGOCD_INSECURE_SKIP_VERIFY", false)
	if err != nil {
		return nil, err
	}
	cfg.InsecureSkipVerify = insecure

	dryRun, err := parseBoolDefault("GIT_MONITOR_DRY_RUN", false)
	if err != nil {
		return nil, err
	}
	cfg.DryRun = dryRun

	if cfg.RepoURL == "" {
		return nil, fmt.Errorf("GIT_MONITOR_REPO_URL is required")
	}

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDurationDefault(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}

func parseBoolDefault(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return b, nil
}
