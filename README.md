# git-monitor

A tiny, generic fallback for slow or unreliable git webhook delivery into
[ArgoCD](https://argo-cd.readthedocs.io/).

## The problem

ArgoCD Applications with `syncPolicy.automated` sync themselves as soon as
they notice their source repo has changed - but *noticing* usually depends
on a webhook call from your git host. When that webhook is slow, flaky, or
just misconfigured, your cluster only catches up on ArgoCD's own polling
interval (`timeout.reconciliation`, 3 minutes by default).

git-monitor closes that gap: it polls the repository itself (`git
ls-remote`, no full clone) every few seconds, and the moment it sees a new
commit, it calls ArgoCD's own `/api/webhook` endpoint - the exact same
endpoint and payload shape your git host would have called. ArgoCD does the
rest, exactly as if the real webhook had fired.

## How it works

1. **Poll**: `git ls-remote <repo> <revision>` on an interval. No clone, no
   working copy.
2. **Reuse ArgoCD's own credentials**: git-monitor looks through ArgoCD's
   repository Secrets (`argocd.argoproj.io/secret-type=repository`) for one
   whose `url` matches the repo you're watching, and uses its SSH key (or
   username/password, for HTTPS repos). If your repo is already registered
   in ArgoCD, there's nothing extra to configure.
3. **Fire the webhook**: on a new commit, it POSTs a payload to ArgoCD's
   `/api/webhook` endpoint in the shape of whichever `webhookProvider` you
   configured (optionally with HTTP basic auth, if your ArgoCD install
   requires it). This is the same parser ArgoCD already ships
   ([go-playground/webhooks](https://github.com/go-playground/webhooks)),
   so it works for any Application whose source matches that repo -
   there's no need to list Applications by name. Note that `webhookProvider`
   describes *which JSON shape ArgoCD's webhook code expects*, not
   necessarily where your repo is hosted - see [Webhook providers](#webhook-providers).
4. **Remember**: the last commit it already reported is kept in a
   Kubernetes Secret (not a database), so a pod restart doesn't re-fire a
   webhook for a commit already handled.

git-monitor doesn't know or care about Azure DevOps, Terraform, or any
particular ArgoCD install - it only needs a repo URL and network access to
your cluster's Kubernetes API and ArgoCD server.

## Install

```bash
helm repo add git-monitor https://azurebrasil-cloud.github.io/argocd-git-monitor/
helm repo update

helm install git-monitor git-monitor/git-monitor \
  --namespace git-monitor --create-namespace \
  --set repoURL=git@github.com:your-org/your-gitops-repo \
  --set argocd.namespace=argocd
```

(swap the chart reference for `./deploy/helm/git-monitor` to install straight
from a checkout instead of the published repo.)

That's the minimum: `repoURL` and, if ArgoCD isn't in the `argocd`
namespace, `argocd.namespace`. Everything else has a sensible default:

- `argocd.webhookURL` is left empty by default, which makes git-monitor
  auto-discover the in-cluster `argocd-server` Service. That Service
  usually serves a self-signed certificate, so you'll likely also want
  `argocd.insecureSkipVerify=true` unless you point `webhookURL` at a
  properly-certificated public endpoint instead.
- `argocd.webhookUsername`/`webhookPassword` are **only required if your
  ArgoCD install itself was configured with webhook credentials** (e.g.
  `webhook.azuredevops.username`/`.password` in the argo-helm chart values
  - `argocd-cm`/`argocd-secret` is where to check). If it wasn't, leave
  these empty; ArgoCD's webhook parser skips auth entirely when no
  username/password is configured on its side, and git-monitor mirrors
  that - it only sends an `Authorization` header when you set one of these.
  If your ArgoCD *does* require it and these are missing or wrong, the
  webhook call fails with `401 Unauthorized` (git-monitor logs it and
  retries next poll, but the sync stays stuck until fixed). Use
  `webhookPassword` for a quick test, `webhookPasswordSecretName` (points
  at an existing Secret) for anything beyond that.

**First install on a new repo/cluster?** Set `--set dryRun=true` first, watch
the logs to confirm commits are detected and the payload looks right, then
`helm upgrade ... --set dryRun=false` once you're happy.

```bash
kubectl -n git-monitor logs deploy/git-monitor -f
```

## Configuration reference

All settings are plain environment variables (the Helm chart just wires
`values.yaml` to them - see [`values.yaml`](deploy/helm/git-monitor/values.yaml)
for the Helm-side names).

| Env var | Default | Description |
|---|---|---|
| `GIT_MONITOR_REPO_URL` | *(required)* | Repository to watch. Must match the `url` field of an ArgoCD repository Secret for credentials to be picked up automatically. |
| `GIT_MONITOR_REVISION` | `HEAD` | Branch/tag to watch. `HEAD` follows the remote's default branch. |
| `GIT_MONITOR_POLL_INTERVAL` | `10s` | Interval between `git ls-remote` checks. |
| `GIT_MONITOR_ARGOCD_NAMESPACE` | `argocd` | Namespace holding ArgoCD's repository Secrets and (for auto-discovery) its Service. |
| `GIT_MONITOR_WEBHOOK_PROVIDER` | `azuredevops` | Payload shape sent to ArgoCD's webhook endpoint. See [Webhook providers](#webhook-providers). |
| `GIT_MONITOR_EXTRA_KNOWN_HOSTS` | *(empty)* | Extra `known_hosts` lines, for self-hosted git servers beyond the built-in GitHub/GitLab/Bitbucket/Azure DevOps keys. |
| `GIT_MONITOR_STATE_SECRET_NAME` | `git-monitor-state` | Secret (in git-monitor's own namespace) used to persist the last commit reported. |
| `GIT_MONITOR_NAMESPACE` | *(auto)* | git-monitor's own namespace. Auto-detected from the pod's service account when unset. |
| `GIT_MONITOR_WEBHOOK_URL` | *(auto-discovered)* | ArgoCD base URL, e.g. `https://argocd.example.com`. Auto-discovered from the `argocd-server` Service when unset. |
| `GIT_MONITOR_WEBHOOK_USERNAME` | *(empty)* | Basic-auth username. **Required only if your ArgoCD's webhook endpoint was configured with one** - otherwise leave unset. Wrong/missing value when required → `401 Unauthorized`. |
| `GIT_MONITOR_WEBHOOK_PASSWORD` | *(empty)* | Basic-auth password. Same conditional requirement as `GIT_MONITOR_WEBHOOK_USERNAME` above. |
| `GIT_MONITOR_ARGOCD_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification when calling the webhook endpoint. Usually needed with an auto-discovered in-cluster URL. |
| `GIT_MONITOR_DRY_RUN` | `false` | Log what would be sent instead of calling the webhook. |
| `GIT_MONITOR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

## Webhook providers

ArgoCD's `/api/webhook` endpoint accepts several JSON payload shapes - one
per git host - and tells them apart by inspecting the request (a header, a
field, ...), not by anything you configure on the ArgoCD side per repo. So
`argocd.webhookProvider` only needs to match **whichever shape ArgoCD's
webhook parser expects for how you've set it up**, not necessarily where
your repo is actually hosted.

| Provider | `webhookProvider` value | Auth |
|---|---|---|
| Azure DevOps | `azuredevops` (default) | HTTP Basic Auth (`webhookUsername`/`webhookPassword`, base64-encoded on the wire per RFC 7617), *if* your ArgoCD's `webhook.azuredevops.username`/`.password` chart config is set. Optional otherwise - see the config reference table above. |

Only `azuredevops` is implemented today. Adding another provider (GitHub,
GitLab, Bitbucket, ...) is a self-contained addition: implement a `builder`
function returning that provider's JSON body and any headers ArgoCD's
parser for it needs (see `internal/webhook/azuredevops.go` for the
reference shape, and `argoproj/argo-cd`'s `util/webhook/scm.go` for how
each parser recognizes its own requests), then register it in the
`builders` map in `internal/webhook/webhook.go` - no changes needed
anywhere else, including the Helm chart (`webhookProvider` already just
passes the string through).

## Required RBAC

The Helm chart creates two Roles, scoped as tightly as Kubernetes RBAC
allows:

- In `argocd.namespace`: `get`/`list`/`watch` on Secrets (needed to find
  the repository credentials by label - RBAC can't filter by label, so this
  technically covers every Secret in that namespace) and `get`/`list` on
  Services (for webhook URL auto-discovery).
- In git-monitor's own namespace: `get`/`update` on exactly one named
  Secret (the state Secret, pre-created by the chart), nothing else.

## Design notes / limitations

- **Single replica.** There's no leader election; running more than one
  replica just means occasional duplicate (harmless) webhook calls.
- **Credentials are read once at startup.** If the underlying ArgoCD
  repository Secret's key rotates, restart the pod.
- Only one webhook provider is implemented today (`azuredevops`) - see
  [Webhook providers](#webhook-providers) for what that means and how to
  add another.

## Project layout

```
cmd/git-monitor/       entrypoint and poll loop
internal/config/       environment variable parsing
internal/gitwatch/     git ls-remote + ArgoCD repo Secret credential lookup
internal/state/        last-commit persistence (Kubernetes Secret)
internal/discovery/    argocd-server Service auto-discovery
internal/webhook/      builds and sends the ArgoCD webhook payload
internal/health/       /healthz for liveness/readiness probes
deploy/helm/git-monitor/  Helm chart
```

## Contributing

Contributions are welcome - open an issue or a PR.

```bash
go build ./... && go vet ./... && go test ./...
docker build -t argocd-git-monitor:dev .
helm lint deploy/helm/git-monitor --set repoURL=git@example.com:org/repo
```

Releasing (maintainers only):

- **Image** - [`.github/workflows/docker-publish.yml`](.github/workflows/docker-publish.yml)
  publishes to Docker Hub on every push to `main` (tag `edge`) and on
  `vX.Y.Z` tags (tags `X.Y.Z`, `X.Y`, `latest`). Needs repo secrets
  `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
- **Helm chart** - [`.github/workflows/chart-release.yml`](.github/workflows/chart-release.yml)
  publishes `deploy/helm/git-monitor` to a Helm repo on GitHub Pages
  whenever its `version` in `Chart.yaml` changes on `main` (a no-op if that
  version was already published). One-time setup: *Settings → Actions →
  General → Workflow permissions* → "Read and write permissions", then
  after the first run, *Settings → Pages* → source = `gh-pages` branch,
  `/ (root)`.

## License

MIT - see [LICENSE](LICENSE).
