package gitbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const maxWebhookBytes = 1 << 20

type WebhookValidationRequest struct {
	Provider     string `json:"provider,omitempty"`
	DeliveryID   string `json:"delivery_id"`
	Event        string `json:"event"`
	Signature256 string `json:"signature_256"`
	Token        string `json:"token,omitempty"`
	Payload      string `json:"payload_base64"`
}

type PullRequestEvent struct {
	Provider      string `json:"provider"`
	DeliveryID    string `json:"delivery_id"`
	Repository    string `json:"repository"`
	Number        int    `json:"number"`
	Action        string `json:"action"`
	Outcome       string `json:"outcome"`
	Branch        string `json:"branch"`
	BaseBranch    string `json:"base_branch"`
	HeadSHA       string `json:"head_sha"`
	MergedCommit  string `json:"merged_commit,omitempty"`
	PayloadSHA256 string `json:"payload_sha256"`
}

type WebhookValidator struct{ secret []byte }

func NewWebhookValidator(secret []byte) (*WebhookValidator, error) {
	if len(secret) < 32 {
		return nil, ErrInvalid
	}
	return &WebhookValidator{secret: append([]byte(nil), secret...)}, nil
}

func (v *WebhookValidator) Validate(_ context.Context, request WebhookValidationRequest) (PullRequestEvent, error) {
	if (request.Provider != "" && request.Provider != "github") || !safeID.MatchString(request.DeliveryID) || request.Event != "pull_request" || !strings.HasPrefix(request.Signature256, "sha256=") {
		return PullRequestEvent{}, ErrInvalid
	}
	payload, err := base64.StdEncoding.DecodeString(request.Payload)
	if err != nil || len(payload) == 0 || len(payload) > maxWebhookBytes {
		return PullRequestEvent{}, ErrInvalid
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(request.Signature256, "sha256="))
	if err != nil || len(provided) != sha256.Size {
		return PullRequestEvent{}, ErrInvalid
	}
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	if subtle.ConstantTimeCompare(provided, expected) != 1 {
		return PullRequestEvent{}, ErrInvalid
	}
	var envelope struct {
		Action     string `json:"action"`
		Number     int    `json:"number"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			State          string  `json:"state"`
			Merged         bool    `json:"merged"`
			MergeCommitSHA *string `json:"merge_commit_sha"`
			Head           struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Action != "closed" || envelope.Number <= 0 ||
		!validRepository(envelope.Repository.FullName) || envelope.PullRequest.State != "closed" ||
		!validGitRef(envelope.PullRequest.Head.Ref) || !validGitRef(envelope.PullRequest.Base.Ref) ||
		!commit.MatchString(envelope.PullRequest.Head.SHA) {
		return PullRequestEvent{}, ErrInvalid
	}
	outcome := "rejected"
	mergedCommit := ""
	if envelope.PullRequest.Merged {
		if envelope.PullRequest.MergeCommitSHA == nil || !commit.MatchString(*envelope.PullRequest.MergeCommitSHA) {
			return PullRequestEvent{}, ErrInvalid
		}
		outcome, mergedCommit = "merged", *envelope.PullRequest.MergeCommitSHA
	}
	digest := sha256.Sum256(payload)
	return PullRequestEvent{
		Provider: "github", DeliveryID: request.DeliveryID, Repository: envelope.Repository.FullName, Number: envelope.Number,
		Action: envelope.Action, Outcome: outcome, Branch: envelope.PullRequest.Head.Ref,
		BaseBranch: envelope.PullRequest.Base.Ref, HeadSHA: envelope.PullRequest.Head.SHA,
		MergedCommit: mergedCommit, PayloadSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

type GitLabWebhookValidator struct{ secret []byte }

func NewGitLabWebhookValidator(secret []byte) (*GitLabWebhookValidator, error) {
	if len(secret) < 32 {
		return nil, ErrInvalid
	}
	return &GitLabWebhookValidator{secret: append([]byte(nil), secret...)}, nil
}

func (v *GitLabWebhookValidator) Validate(_ context.Context, request WebhookValidationRequest) (PullRequestEvent, error) {
	if request.Provider != "gitlab" || !safeID.MatchString(request.DeliveryID) || request.Event != "Merge Request Hook" || len(request.Token) != len(v.secret) || subtle.ConstantTimeCompare([]byte(request.Token), v.secret) != 1 {
		return PullRequestEvent{}, ErrInvalid
	}
	payload, err := base64.StdEncoding.DecodeString(request.Payload)
	if err != nil || len(payload) == 0 || len(payload) > maxWebhookBytes {
		return PullRequestEvent{}, ErrInvalid
	}
	var envelope struct {
		ObjectKind string `json:"object_kind"`
		Project    struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		ObjectAttributes struct {
			IID            int    `json:"iid"`
			Action         string `json:"action"`
			State          string `json:"state"`
			SourceBranch   string `json:"source_branch"`
			TargetBranch   string `json:"target_branch"`
			MergeCommitSHA string `json:"merge_commit_sha"`
			LastCommit     struct {
				ID string `json:"id"`
			} `json:"last_commit"`
		} `json:"object_attributes"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.ObjectKind != "merge_request" || envelope.ObjectAttributes.IID <= 0 ||
		!validRepository(envelope.Project.PathWithNamespace) || !validGitRef(envelope.ObjectAttributes.SourceBranch) || !validGitRef(envelope.ObjectAttributes.TargetBranch) ||
		!commit.MatchString(envelope.ObjectAttributes.LastCommit.ID) {
		return PullRequestEvent{}, ErrInvalid
	}
	outcome, action, mergedCommit := "", envelope.ObjectAttributes.Action, ""
	switch {
	case action == "merge" && envelope.ObjectAttributes.State == "merged":
		outcome, action, mergedCommit = "merged", "closed", envelope.ObjectAttributes.MergeCommitSHA
		if mergedCommit == "" {
			mergedCommit = envelope.ObjectAttributes.LastCommit.ID
		}
		if !commit.MatchString(mergedCommit) {
			return PullRequestEvent{}, ErrInvalid
		}
	case action == "close" && envelope.ObjectAttributes.State == "closed":
		outcome, action = "rejected", "closed"
	default:
		return PullRequestEvent{}, ErrInvalid
	}
	digest := sha256.Sum256(payload)
	return PullRequestEvent{
		Provider: "gitlab", DeliveryID: request.DeliveryID, Repository: envelope.Project.PathWithNamespace,
		Number: envelope.ObjectAttributes.IID, Action: action, Outcome: outcome,
		Branch: envelope.ObjectAttributes.SourceBranch, BaseBranch: envelope.ObjectAttributes.TargetBranch,
		HeadSHA: envelope.ObjectAttributes.LastCommit.ID, MergedCommit: mergedCommit, PayloadSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

type WebhookValidators struct {
	GitHub *WebhookValidator
	GitLab *GitLabWebhookValidator
}

func (v WebhookValidators) Validate(ctx context.Context, request WebhookValidationRequest) (PullRequestEvent, error) {
	if request.Provider == "gitlab" {
		if v.GitLab == nil {
			return PullRequestEvent{}, ErrInvalid
		}
		return v.GitLab.Validate(ctx, request)
	}
	if v.GitHub == nil {
		return PullRequestEvent{}, ErrInvalid
	}
	return v.GitHub.Validate(ctx, request)
}

func validRepository(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && safeID.MatchString(parts[0]) && safeID.MatchString(parts[1])
}
