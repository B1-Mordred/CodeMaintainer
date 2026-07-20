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
	DeliveryID   string `json:"delivery_id"`
	Event        string `json:"event"`
	Signature256 string `json:"signature_256"`
	Payload      string `json:"payload_base64"`
}

type PullRequestEvent struct {
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
	if !safeID.MatchString(request.DeliveryID) || request.Event != "pull_request" || !strings.HasPrefix(request.Signature256, "sha256=") {
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
		DeliveryID: request.DeliveryID, Repository: envelope.Repository.FullName, Number: envelope.Number,
		Action: envelope.Action, Outcome: outcome, Branch: envelope.PullRequest.Head.Ref,
		BaseBranch: envelope.PullRequest.Base.Ref, HeadSHA: envelope.PullRequest.Head.SHA,
		MergedCommit: mergedCommit, PayloadSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func validRepository(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && safeID.MatchString(parts[0]) && safeID.MatchString(parts[1])
}
