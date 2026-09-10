// Package webhook fires ArgoCD's native /api/webhook endpoint, simulating
// the Service Hook payload a real git host would have sent. This makes
// git-monitor a drop-in fallback for slow or unreliable webhook delivery,
// exercising the exact same ArgoCD code path a real webhook call would.
//
// ArgoCD's webhook endpoint accepts several payload shapes (GitHub, GitLab,
// Bitbucket, Azure DevOps, ...), each with its own JSON layout and its own
// way of signalling which one a given request is (see
// github.com/argoproj/argo-cd's util/webhook/scm.go). Client picks one via
// Provider; adding another provider is a self-contained addition here, not
// a redesign - see azuredevops.go for the reference implementation.
package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Change describes the commit update to report.
type Change struct {
	RepoURL string
	// Ref is the fully-qualified ref that moved (e.g. "refs/heads/main").
	Ref string
	// DefaultBranchRef, when non-empty and equal to Ref, tells ArgoCD this
	// update touched the repo's default branch - required for Applications
	// with targetRevision "HEAD" to be refreshed.
	DefaultBranchRef string
	NewSHA           string
	OldSHA           string
}

// Provider identifies which git host's webhook payload shape to send.
type Provider string

// payload is what a Provider builds for a given Change: the request body
// and any extra headers ArgoCD's parser for that provider needs to
// recognize the request (beyond Content-Type and basic auth, which Client
// always sets).
type payload struct {
	body    []byte
	headers map[string]string
}

// builder renders a Change into a provider-specific payload.
type builder func(Change) (payload, error)

// builders maps each supported Provider to its payload builder. Register
// new providers here.
var builders = map[Provider]builder{
	ProviderAzureDevOps: buildAzureDevOpsPayload,
}

// SupportedProviders lists the Provider values Client accepts, for error
// messages and documentation.
func SupportedProviders() []Provider {
	providers := make([]Provider, 0, len(builders))
	for p := range builders {
		providers = append(providers, p)
	}
	return providers
}

// Client fires synthetic webhook calls at an ArgoCD server.
type Client struct {
	baseURL    string
	username   string
	password   string
	build      builder
	httpClient *http.Client
}

// NewClient builds a Client targeting baseURL (e.g.
// "https://argocd.example.com") using provider's payload shape.
// username/password may be empty if the ArgoCD webhook endpoint doesn't
// require basic auth.
func NewClient(provider Provider, baseURL, username, password string, insecureSkipVerify bool) (*Client, error) {
	build, ok := builders[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported webhook provider %q (supported: %v)", provider, SupportedProviders())
	}
	return &Client{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		username: username,
		password: password,
		build:    build,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify}, //nolint:gosec // opt-in via config, for in-cluster self-signed certs
			},
		},
	}, nil
}

// Send posts a synthetic webhook event for change to ArgoCD's webhook
// endpoint, in this Client's configured provider's payload shape.
func (c *Client) Send(ctx context.Context, change Change) error {
	p, err := c.build(change)
	if err != nil {
		return fmt.Errorf("building webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/webhook", bytes.NewReader(p.body))
	if err != nil {
		return fmt.Errorf("building webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling ArgoCD webhook endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ArgoCD webhook endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}
