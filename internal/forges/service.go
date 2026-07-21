package forges

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Store interface {
	GetForgeProfile(context.Context, string) (Profile, error)
	SaveForgeProfile(context.Context, SaveProfileRequest) (Profile, error)
	ListForgeProfiles(context.Context, int) ([]Profile, error)
	SaveForgeSync(context.Context, SyncPage, string, string) (SyncRun, error)
	ListForgeSyncRuns(context.Context, string, int) ([]SyncRun, error)
	ListForgeObjects(context.Context, string, string, int) ([]Object, error)
}

// ForgeProvider is the credential-isolated boundary implemented by the Git
// bridge. The controller sees normalized objects and status, never forge
// tokens or provider SDKs.
type ForgeProvider interface {
	ProbeForge(context.Context, string) (Probe, error)
	SyncForge(context.Context, SyncRequest) (SyncPage, error)
}
type Service struct {
	store    Store
	provider ForgeProvider
}

func NewService(store Store, provider ForgeProvider) (*Service, error) {
	if store == nil || provider == nil {
		return nil, errors.New("forge store and credential-isolated provider are required")
	}
	return &Service{store: store, provider: provider}, nil
}
func (s *Service) Profiles(ctx context.Context) ([]Profile, error) {
	return s.store.ListForgeProfiles(ctx, 100)
}
func (s *Service) Profile(ctx context.Context, projectID string) (Profile, error) {
	return s.store.GetForgeProfile(ctx, projectID)
}
func (s *Service) SaveProfile(ctx context.Context, request SaveProfileRequest) (Profile, error) {
	if err := validateProfile(request.Profile); err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		return Profile{}, errors.New("a bounded profile reason is required")
	}
	current, _ := s.store.GetForgeProfile(ctx, request.Profile.ProjectID)
	if current.CredentialReference != request.Profile.CredentialReference && !request.Reauthenticated {
		return Profile{}, errors.New("recent reauthentication is required for credential-reference changes")
	}
	return s.store.SaveForgeProfile(ctx, request)
}
func (s *Service) Probe(ctx context.Context, projectID string) (Probe, error) {
	profile, err := s.store.GetForgeProfile(ctx, projectID)
	if err != nil {
		return Probe{}, err
	}
	if !profile.Enabled {
		return Probe{}, errors.New("forge profile is disabled")
	}
	return s.provider.ProbeForge(ctx, projectID)
}
func (s *Service) Sync(ctx context.Context, projectID, cursor, idempotencyKey, actorID string) (SyncRun, error) {
	profile, err := s.store.GetForgeProfile(ctx, projectID)
	if err != nil {
		return SyncRun{}, err
	}
	if !profile.Enabled {
		return SyncRun{}, errors.New("forge profile is disabled")
	}
	if !safeID.MatchString(idempotencyKey) {
		return SyncRun{}, errors.New("a safe idempotency key is required")
	}
	page, err := s.provider.SyncForge(ctx, SyncRequest{ProjectID: projectID, Cursor: cursor, Limit: 500, IdempotencyKey: idempotencyKey})
	if err != nil {
		return SyncRun{}, err
	}
	if err := validatePage(profile, page); err != nil {
		return SyncRun{}, err
	}
	return s.store.SaveForgeSync(ctx, page, idempotencyKey, actorID)
}
func (s *Service) Runs(ctx context.Context, projectID string) ([]SyncRun, error) {
	return s.store.ListForgeSyncRuns(ctx, projectID, 100)
}
func (s *Service) Objects(ctx context.Context, projectID, kind string) ([]Object, error) {
	return s.store.ListForgeObjects(ctx, projectID, kind, 500)
}
func validateProfile(profile Profile) error {
	if !safeID.MatchString(profile.ProjectID) || (profile.Provider != "github" && profile.Provider != "gitlab" && profile.Provider != "local") || strings.TrimSpace(profile.Repository) == "" {
		return errors.New("invalid forge profile identity")
	}
	if profile.SyncDirection != "pull" && profile.SyncDirection != "bidirectional_draft" {
		return errors.New("invalid forge sync direction")
	}
	if profile.PollingMinutes < 1 || profile.PollingMinutes > 10080 {
		return errors.New("polling interval must be between 1 and 10080 minutes")
	}
	if len(profile.EndpointAllowlist) == 0 || len(profile.EndpointAllowlist) > 20 {
		return errors.New("a bounded endpoint allow-list is required")
	}
	allowed := false
	for _, candidate := range profile.EndpointAllowlist {
		if err := validateEndpoint(candidate, profile.Provider); err != nil {
			return err
		}
		if candidate == profile.Endpoint {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("forge endpoint is not in its exact allow-list")
	}
	if len(profile.LabelMapping) > 100 {
		return errors.New("label mapping is oversized")
	}
	for key, value := range profile.LabelMapping {
		if len(key) > 128 || len(value) > 128 {
			return errors.New("label mapping entry is oversized")
		}
	}
	return nil
}
func validateEndpoint(raw, provider string) error {
	if provider == "local" {
		if raw != "local://bare-git" {
			return errors.New("local forge endpoint must be local://bare-git")
		}
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid forge endpoint")
	}
	loopback := parsed.Hostname() == "localhost"
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return errors.New("hosted forge endpoint must use HTTPS except loopback test endpoints")
	}
	return nil
}
func validatePage(profile Profile, page SyncPage) error {
	if page.ProjectID != profile.ProjectID || page.Provider != profile.Provider || len(page.Objects) > 500 || page.RateLimitRemaining < 0 || page.RetryAfterSeconds < 0 {
		return errors.New("forge provider returned an invalid page")
	}
	for _, item := range page.Objects {
		if item.ProjectID != profile.ProjectID || item.Provider != profile.Provider || !validKind(item.Kind) || strings.TrimSpace(item.ExternalID) == "" || len(item.ProviderMetadata) > 64<<10 || !jsonValid(item.ProviderMetadata) {
			return fmt.Errorf("forge provider returned invalid %s object", item.Kind)
		}
	}
	return nil
}
func validKind(value string) bool {
	switch value {
	case "repository", "issue", "change_request", "discussion", "pipeline", "job", "artifact", "branch", "tag", "release", "submodule":
		return true
	}
	return false
}
func jsonValid(raw []byte) bool { return json.Valid(raw) }
