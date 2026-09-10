package webhook

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
)

// ProviderAzureDevOps sends an Azure DevOps "git.push" Service Hook
// payload, the shape ArgoCD's own webhook handler already parses (see
// github.com/go-playground/webhooks/v6/azuredevops). Its HTTP Basic Auth
// credentials (Client.username/password, base64-encoded on the wire per
// RFC 7617 by net/http's SetBasicAuth) are what Azure DevOps Service Hooks
// authenticate with, matching ArgoCD's own webhook.azuredevops config.
const ProviderAzureDevOps Provider = "azuredevops"

// gitPushEvent mirrors the fields azuredevops.GitPushEvent actually reads;
// see github.com/go-playground/webhooks/v6/azuredevops/payload.go. Every
// other field on the real struct is optional/unused by ArgoCD's handler.
type gitPushEvent struct {
	EventType string   `json:"eventType"`
	Resource  resource `json:"resource"`
}

type resource struct {
	RefUpdates []refUpdate `json:"refUpdates"`
	Repository repository  `json:"repository"`
}

type repository struct {
	RemoteURL     string `json:"remoteUrl"`
	DefaultBranch string `json:"defaultBranch"`
}

type refUpdate struct {
	Name        string `json:"name"`
	NewObjectID string `json:"newObjectId"`
	OldObjectID string `json:"oldObjectId"`
}

const gitPushEventType = "git.push"

func buildAzureDevOpsPayload(change Change) (payload, error) {
	event := gitPushEvent{
		EventType: gitPushEventType,
		Resource: resource{
			RefUpdates: []refUpdate{{
				Name:        change.Ref,
				NewObjectID: change.NewSHA,
				OldObjectID: change.OldSHA,
			}},
			Repository: repository{
				RemoteURL:     change.RepoURL,
				DefaultBranch: change.DefaultBranchRef,
			},
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return payload{}, fmt.Errorf("marshaling azuredevops payload: %w", err)
	}

	return payload{
		body: body,
		headers: map[string]string{
			// ArgoCD's webhook router picks the Azure DevOps parser purely
			// by the presence of this header (see argoproj/argo-cd's
			// util/webhook/scm.go azureDevOpsParser.CanHandle) - without it
			// every parser declines the request and ArgoCD responds 400
			// "Unknown webhook event". Its value isn't validated, only its
			// presence.
			"X-Vss-ActivityId": newActivityID(),
		},
	}, nil
}

// newActivityID returns a random UUID-v4-shaped string. Only its presence
// (not its value) matters to ArgoCD's Azure DevOps webhook parser.
func newActivityID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
