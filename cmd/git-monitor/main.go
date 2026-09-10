// Command git-monitor polls a git repository and, when it sees a new
// commit, fires ArgoCD's own webhook endpoint - acting as a fast, reliable
// fallback for a slow or flaky CI/CD webhook delivery.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/azurebrasil-cloud/git-monitor/internal/config"
	"github.com/azurebrasil-cloud/git-monitor/internal/discovery"
	"github.com/azurebrasil-cloud/git-monitor/internal/gitwatch"
	"github.com/azurebrasil-cloud/git-monitor/internal/health"
	"github.com/azurebrasil-cloud/git-monitor/internal/k8sclient"
	"github.com/azurebrasil-cloud/git-monitor/internal/state"
	"github.com/azurebrasil-cloud/git-monitor/internal/webhook"
	"k8s.io/client-go/kubernetes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(ctx); err != nil {
		slog.Error("git-monitor exited with an error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setLogLevel(cfg.LogLevel)

	k8s, err := k8sclient.New()
	if err != nil {
		return err
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = k8sclient.OwnNamespace()
	}

	healthErrs := make(chan error, 1)
	go health.Serve(ctx, ":8080", healthErrs)

	stateStore, err := state.New(ctx, k8s, namespace, cfg.StateSecretName)
	if err != nil {
		return err
	}
	lastCommit, err := stateStore.LastCommit(ctx)
	if err != nil {
		return err
	}

	webhookClient, err := buildWebhookClient(ctx, k8s, cfg)
	if err != nil {
		return err
	}

	slog.Info("git-monitor started",
		"repo", cfg.RepoURL,
		"revision", cfg.Revision,
		"pollInterval", cfg.PollInterval.String(),
		"webhookProvider", cfg.WebhookProvider,
		"lastKnownCommit", lastCommit,
	)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-healthErrs:
			return err
		case <-ticker.C:
			lastCommit = pollOnce(ctx, k8s, stateStore, webhookClient, cfg, lastCommit)
		}
	}
}

// buildWebhookClient resolves the ArgoCD base URL (explicit or
// auto-discovered) and builds the webhook client once up front.
func buildWebhookClient(ctx context.Context, k8s kubernetes.Interface, cfg *config.Config) (*webhook.Client, error) {
	baseURL := cfg.WebhookURL
	if baseURL == "" {
		discovered, err := discovery.ArgoCDServerBaseURL(ctx, k8s, cfg.ArgoCDNamespace)
		if err != nil {
			return nil, err
		}
		baseURL = discovered
		slog.Info("auto-discovered ArgoCD server", "url", baseURL)
	}
	return webhook.NewClient(
		webhook.Provider(cfg.WebhookProvider), baseURL, cfg.WebhookUsername, cfg.WebhookPassword, cfg.InsecureSkipVerify,
	)
}

// pollOnce checks the repository once and, if a new commit is found, fires
// the webhook and persists the new commit. It returns the commit that
// should be treated as "last known" going forward.
func pollOnce(
	ctx context.Context,
	k8s kubernetes.Interface,
	stateStore *state.Store,
	webhookClient *webhook.Client,
	cfg *config.Config,
	lastCommit string,
) string {
	creds, ok, err := gitwatch.FindCredentials(ctx, k8s, cfg.ArgoCDNamespace, cfg.RepoURL)
	if err != nil {
		slog.Error("looking up repository credentials", "error", err)
		return lastCommit
	}
	if !ok {
		slog.Debug("no matching ArgoCD repository secret found; trying without credentials", "repo", cfg.RepoURL)
	}

	ws, err := gitwatch.NewWorkspace(creds, cfg.ExtraKnownHosts)
	if err != nil {
		slog.Error("preparing git workspace", "error", err)
		return lastCommit
	}
	defer ws.Close()

	result, err := ws.LsRemote(ctx, cfg.RepoURL, cfg.Revision)
	if err != nil {
		slog.Error("git ls-remote failed", "error", err)
		return lastCommit
	}

	if result.SHA == lastCommit {
		return lastCommit
	}

	slog.Info("new commit detected", "repo", cfg.RepoURL, "ref", result.Ref, "sha", result.SHA, "previousSha", lastCommit)

	change := webhook.Change{
		RepoURL:          cfg.RepoURL,
		Ref:              result.Ref,
		DefaultBranchRef: result.DefaultBranchRef,
		NewSHA:           result.SHA,
		OldSHA:           lastCommit,
	}

	if cfg.DryRun {
		slog.Info("dry-run: would fire webhook now", "change", change)
	} else if err := webhookClient.Send(ctx, change); err != nil {
		slog.Error("firing webhook failed, will retry next poll", "error", err)
		return lastCommit
	}

	if err := stateStore.SetLastCommit(ctx, result.SHA); err != nil {
		slog.Error("persisting last commit failed; may re-fire this webhook after a restart", "error", err)
	}

	slog.Info("webhook fired successfully", "sha", result.SHA)
	return result.SHA
}

func setLogLevel(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	slog.SetLogLoggerLevel(l)
}
