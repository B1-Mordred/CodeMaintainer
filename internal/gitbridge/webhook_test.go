package gitbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestWebhookValidatorAuthenticatesAndNormalizesMerge(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	validator, err := NewWebhookValidator(secret)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"action":"closed","number":23,"repository":{"full_name":"owner/repo"},"pull_request":{"state":"closed","merged":true,"merge_commit_sha":"abcdef0123456789abcdef0123456789abcdef01","head":{"ref":"maintainer/job_23","sha":"0123456789abcdef0123456789abcdef01234567"},"base":{"ref":"main"}}}`)
	request := WebhookValidationRequest{
		DeliveryID: "delivery-23", Event: "pull_request", Signature256: webhookSignature(secret, payload),
		Payload: base64.StdEncoding.EncodeToString(payload),
	}
	event, err := validator.Validate(context.Background(), request)
	if err != nil || event.Outcome != "merged" || event.Repository != "owner/repo" || event.Number != 23 || event.MergedCommit == "" {
		t.Fatalf("event = %#v, %v", event, err)
	}
	request.Signature256 = webhookSignature(secret, append(payload, ' '))
	if _, err := validator.Validate(context.Background(), request); err == nil {
		t.Fatal("tampered webhook was accepted")
	}
}

func TestWebhookValidatorNormalizesClosedUnmergedAsRejected(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	validator, err := NewWebhookValidator(secret)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"action":"closed","number":23,"repository":{"full_name":"owner/repo"},"pull_request":{"state":"closed","merged":false,"merge_commit_sha":null,"head":{"ref":"maintainer/job_23","sha":"0123456789abcdef0123456789abcdef01234567"},"base":{"ref":"main"}}}`)
	event, err := validator.Validate(context.Background(), WebhookValidationRequest{
		DeliveryID: "delivery-24", Event: "pull_request", Signature256: webhookSignature(secret, payload),
		Payload: base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil || event.Outcome != "rejected" || event.MergedCommit != "" {
		t.Fatalf("event = %#v, %v", event, err)
	}
}

func TestWebhookValidationStaysBehindAuthenticatedBridgeClient(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	validator, err := NewWebhookValidator(secret)
	if err != nil {
		t.Fatal(err)
	}
	token := []byte("abcdef0123456789abcdef0123456789")
	handler, err := NewServiceWithWebhook(&fixtureBackend{}, token, validator, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"action":"closed","number":23,"repository":{"full_name":"owner/repo"},"pull_request":{"state":"closed","merged":false,"merge_commit_sha":null,"head":{"ref":"maintainer/job_23","sha":"0123456789abcdef0123456789abcdef01234567"},"base":{"ref":"main"}}}`)
	event, err := client.ValidateWebhook(context.Background(), WebhookValidationRequest{
		DeliveryID: "delivery-client", Event: "pull_request", Signature256: webhookSignature(secret, payload),
		Payload: base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil || event.Outcome != "rejected" {
		t.Fatalf("validated event = %#v, %v", event, err)
	}
}

func TestGitLabWebhookValidatorAuthenticatesAndNormalizesMergeRequest(t *testing.T) {
	secret := []byte("gitlab-webhook-secret-0123456789ab")
	validator, err := NewGitLabWebhookValidator(secret)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"object_kind":"merge_request","project":{"path_with_namespace":"owner/repo"},"object_attributes":{"iid":41,"action":"merge","state":"merged","source_branch":"maintainer/job_41","target_branch":"main","merge_commit_sha":"abcdef0123456789abcdef0123456789abcdef01","last_commit":{"id":"0123456789abcdef0123456789abcdef01234567"}}}`)
	request := WebhookValidationRequest{Provider: "gitlab", DeliveryID: "gitlab-delivery-41", Event: "Merge Request Hook", Token: string(secret), Payload: base64.StdEncoding.EncodeToString(payload)}
	event, err := validator.Validate(context.Background(), request)
	if err != nil || event.Provider != "gitlab" || event.Outcome != "merged" || event.Number != 41 || event.Repository != "owner/repo" {
		t.Fatalf("event %#v error %v", event, err)
	}
	request.Token = "wrong-token-with-at-least-thirty-two"
	if _, err := validator.Validate(context.Background(), request); err == nil {
		t.Fatal("invalid GitLab secret token accepted")
	}
	request.Token = string(secret)
	request.Payload = base64.StdEncoding.EncodeToString(append(payload, ' '))
	if tampered, err := validator.Validate(context.Background(), request); err != nil || tampered.PayloadSHA256 == event.PayloadSHA256 {
		t.Fatalf("payload identity was not exact: %#v %v", tampered, err)
	}
}

func TestWebhookMultiplexerKeepsProviderValidatorsSeparate(t *testing.T) {
	githubSecret := []byte("0123456789abcdef0123456789abcdef")
	gitlabSecret := []byte("gitlab-webhook-secret-0123456789ab")
	github, _ := NewWebhookValidator(githubSecret)
	gitlab, _ := NewGitLabWebhookValidator(gitlabSecret)
	validators := WebhookValidators{GitHub: github, GitLab: gitlab}
	payload := []byte(`{"object_kind":"merge_request","project":{"path_with_namespace":"owner/repo"},"object_attributes":{"iid":41,"action":"close","state":"closed","source_branch":"maintainer/job_41","target_branch":"main","merge_commit_sha":"","last_commit":{"id":"0123456789abcdef0123456789abcdef01234567"}}}`)
	event, err := validators.Validate(context.Background(), WebhookValidationRequest{Provider: "gitlab", DeliveryID: "gitlab-close-41", Event: "Merge Request Hook", Token: string(gitlabSecret), Payload: base64.StdEncoding.EncodeToString(payload)})
	if err != nil || event.Outcome != "rejected" {
		t.Fatalf("event %#v error %v", event, err)
	}
	bridgeToken := []byte("bridge-token-0123456789abcdef0123")
	handler, err := NewServiceWithWebhooks(&fixtureBackend{}, bridgeToken, validators, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, bridgeToken)
	if err != nil {
		t.Fatal(err)
	}
	event, err = client.ValidateWebhook(context.Background(), WebhookValidationRequest{Provider: "gitlab", DeliveryID: "gitlab-client-close-41", Event: "Merge Request Hook", Token: string(gitlabSecret), Payload: base64.StdEncoding.EncodeToString(payload)})
	if err != nil || event.Provider != "gitlab" || event.Outcome != "rejected" {
		t.Fatalf("client event %#v error %v", event, err)
	}
}

func FuzzWebhookValidatorsRejectMalformedPayloads(f *testing.F) {
	githubSecret := []byte("0123456789abcdef0123456789abcdef")
	gitlabSecret := []byte("gitlab-webhook-secret-0123456789ab")
	github, _ := NewWebhookValidator(githubSecret)
	gitlab, _ := NewGitLabWebhookValidator(gitlabSecret)
	validators := WebhookValidators{GitHub: github, GitLab: gitlab}
	validPayload := []byte(`{"action":"closed","number":23,"repository":{"full_name":"owner/repo"},"pull_request":{"state":"closed","merged":false,"merge_commit_sha":null,"head":{"ref":"maintainer/job_23","sha":"0123456789abcdef0123456789abcdef01234567"},"base":{"ref":"main"}}}`)
	f.Add("github", "delivery-23", "pull_request", string(validPayload))
	f.Add("gitlab", "gitlab-delivery-41", "Merge Request Hook", `{"object_kind":"merge_request","project":{"path_with_namespace":"owner/repo"},"object_attributes":{"iid":41,"action":"close","state":"closed","source_branch":"maintainer/job_41","target_branch":"main","merge_commit_sha":"","last_commit":{"id":"0123456789abcdef0123456789abcdef01234567"}}}`)
	f.Add("github", "../delivery", "pull_request", `{"action":"closed"}`)
	f.Fuzz(func(t *testing.T, provider, delivery, event, payload string) {
		signature := webhookSignature(githubSecret, []byte(payload))
		token := ""
		if provider == "gitlab" {
			signature = ""
			token = string(gitlabSecret)
		}
		result, err := validators.Validate(context.Background(), WebhookValidationRequest{
			Provider: provider, DeliveryID: delivery, Event: event, Signature256: signature, Token: token,
			Payload: base64.StdEncoding.EncodeToString([]byte(payload)),
		})
		if err == nil {
			if result.Provider != provider || result.PayloadSHA256 == "" || result.Repository == "" || result.Number <= 0 ||
				(result.Outcome != "merged" && result.Outcome != "rejected") {
				t.Fatalf("accepted malformed webhook as %#v", result)
			}
		}
	})
}

func webhookSignature(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
