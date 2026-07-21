package capabilities

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxScanFiles = 5000
	maxScanBytes = 64 << 20
	maxFileBytes = 2 << 20
)

type Store interface {
	GetCapabilityInstallation(context.Context, string) (Installation, error)
	ListCapabilityInstallations(context.Context, int) ([]Installation, error)
	TransitionCapability(context.Context, TransitionRequest) (Installation, LifecycleEvent, error)
	ListCapabilityEvents(context.Context, string, int) ([]LifecycleEvent, error)
	ListCapabilityAssignments(context.Context, string, int) ([]Assignment, error)
	SaveRepoDoctorScan(context.Context, Scan, string) (Scan, error)
	GetRepoDoctorScan(context.Context, string, string) (Scan, error)
	ListRepoDoctorScans(context.Context, string, int) ([]Scan, error)
	ReviewRepoDoctorProposal(context.Context, ReviewRequest) (Proposal, *Assignment, error)
}

type TransitionRequest struct {
	PackID           string
	Action           string
	TargetVersion    string
	Checksum         string
	ExpectedRevision int64
	ActorID          string
	Reason           string
}

type Service struct {
	store   Store
	catalog *Catalog
}

func NewService(store Store, catalog *Catalog) (*Service, error) {
	if store == nil {
		return nil, errors.New("capability store is required")
	}
	if catalog == nil {
		var err error
		catalog, err = BuiltInCatalog()
		if err != nil {
			return nil, err
		}
	}
	return &Service{store: store, catalog: catalog}, nil
}

func (s *Service) Catalog() []Manifest { return s.catalog.List() }
func (s *Service) Manifest(id, version string) (Manifest, TrustReport, error) {
	manifest, err := s.catalog.Get(id, version)
	if err != nil {
		return Manifest{}, TrustReport{}, err
	}
	return manifest, ValidateManifest(manifest), nil
}
func (s *Service) Installations(ctx context.Context) ([]Installation, error) {
	return s.store.ListCapabilityInstallations(ctx, 100)
}
func (s *Service) Assignments(ctx context.Context, projectID string) ([]Assignment, error) {
	return s.store.ListCapabilityAssignments(ctx, projectID, 100)
}
func (s *Service) Events(ctx context.Context, packID string) ([]LifecycleEvent, error) {
	return s.store.ListCapabilityEvents(ctx, packID, 100)
}

func (s *Service) PreviewTransition(ctx context.Context, packID, action, targetVersion string) (map[string]any, error) {
	if action != "install" && action != "upgrade" && action != "rollback" {
		return nil, errors.New("invalid capability preview action")
	}
	manifest, report, err := s.Manifest(packID, targetVersion)
	if err != nil {
		return nil, err
	}
	current, _ := s.store.GetCapabilityInstallation(ctx, packID)
	if action == "upgrade" && current.PackVersion != "" && compareVersions(targetVersion, current.PackVersion) <= 0 {
		return nil, errors.New("upgrade target must be newer than the installed version")
	}
	if action == "rollback" && current.PackVersion != "" && compareVersions(targetVersion, current.PackVersion) >= 0 {
		return nil, errors.New("rollback target must be older than the installed version")
	}
	changes := manifest.WorkflowChanges
	return map[string]any{"action": action, "current": current, "target": manifest, "trust": report, "workflow_changes": changes, "prerequisites": manifest.Prerequisites}, nil
}

func (s *Service) Transition(ctx context.Context, request TransitionRequest, reauthenticated bool) (Installation, LifecycleEvent, error) {
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		return Installation{}, LifecycleEvent{}, errors.New("a bounded reason is required")
	}
	if !reauthenticated {
		return Installation{}, LifecycleEvent{}, errors.New("recent reauthentication is required")
	}
	if request.Action == "enable" || request.Action == "disable" || request.Action == "pin" || request.Action == "unpin" {
		current, err := s.store.GetCapabilityInstallation(ctx, request.PackID)
		if err != nil {
			return Installation{}, LifecycleEvent{}, err
		}
		request.TargetVersion, request.Checksum = current.PackVersion, current.Checksum
	} else {
		manifest, report, err := s.Manifest(request.PackID, request.TargetVersion)
		if err != nil {
			return Installation{}, LifecycleEvent{}, err
		}
		if !report.ChecksumValid || !report.AuthoritySafe || !report.Compatible {
			return Installation{}, LifecycleEvent{}, errors.New("capability pack trust verification failed")
		}
		request.Checksum = manifest.ChecksumSHA256
		current, currentErr := s.store.GetCapabilityInstallation(ctx, request.PackID)
		if request.Action == "upgrade" && currentErr == nil && compareVersions(request.TargetVersion, current.PackVersion) <= 0 {
			return Installation{}, LifecycleEvent{}, errors.New("upgrade target must be newer than the installed version")
		}
		if request.Action == "rollback" && currentErr == nil && compareVersions(request.TargetVersion, current.PackVersion) >= 0 {
			return Installation{}, LifecycleEvent{}, errors.New("rollback target must be older than the installed version")
		}
	}
	return s.store.TransitionCapability(ctx, request)
}

func (s *Service) Scans(ctx context.Context, projectID string) ([]Scan, error) {
	return s.store.ListRepoDoctorScans(ctx, projectID, 50)
}
func (s *Service) Scan(ctx context.Context, input ScanInput, actorID string) (Scan, error) {
	if !safeID.MatchString(input.ProjectID) || strings.TrimSpace(input.Repository) == "" || strings.TrimSpace(input.Revision) == "" {
		return Scan{}, errors.New("project, repository, and revision are required")
	}
	if len(input.Files) == 0 || len(input.Files) > maxScanFiles {
		return Scan{}, errors.New("bounded trusted repository files are required")
	}
	paths := map[string]struct{}{}
	evidenceByPath := map[string]Evidence{}
	bytes := 0
	for _, file := range input.Files {
		clean := filepath.ToSlash(filepath.Clean(file.Path))
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || len(file.Content) > maxFileBytes {
			return Scan{}, errors.New("repository snapshot contains an unsafe or oversized file")
		}
		bytes += len(file.Content)
		if bytes > maxScanBytes {
			return Scan{}, errors.New("repository snapshot exceeds scan budget")
		}
		paths[clean] = struct{}{}
		digest := sha256.Sum256(file.Content)
		evidenceByPath[clean] = Evidence{Path: clean, Observation: "trusted snapshot path and content hash", SHA256: hex.EncodeToString(digest[:])}
	}
	findings := detectRepository(paths, evidenceByPath, input.Files)
	proposals := s.propose(input.ProjectID, paths, evidenceByPath, findings)
	previous, _ := s.store.ListRepoDoctorScans(ctx, input.ProjectID, 1)
	drift := compareFindings(previous, findings)
	scan := Scan{ID: newID("scan"), ProjectID: input.ProjectID, Repository: input.Repository, Revision: input.Revision, State: "complete", Findings: findings, Proposals: proposals, Drift: drift, FilesObserved: len(input.Files), ExcludedFiles: input.ExcludedFiles}
	for index := range scan.Proposals {
		scan.Proposals[index].ID = newID("proposal")
		scan.Proposals[index].ScanID = scan.ID
	}
	return s.store.SaveRepoDoctorScan(ctx, scan, actorID)
}

func (s *Service) GetScan(ctx context.Context, projectID, scanID string) (Scan, error) {
	return s.store.GetRepoDoctorScan(ctx, projectID, scanID)
}
func (s *Service) Review(ctx context.Context, request ReviewRequest) (Proposal, *Assignment, error) {
	if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 1000 {
		return Proposal{}, nil, errors.New("a bounded review reason is required")
	}
	if len(request.Config) == 0 {
		request.Config = json.RawMessage(`{}`)
	}
	if !json.Valid(request.Config) || len(request.Config) > 64<<10 {
		return Proposal{}, nil, errors.New("configuration must be bounded valid JSON")
	}
	return s.store.ReviewRepoDoctorProposal(ctx, request)
}

func (s *Service) DryRun(ctx context.Context, projectID, scanID, proposalID string, config json.RawMessage) (map[string]any, error) {
	scan, err := s.GetScan(ctx, projectID, scanID)
	if err != nil {
		return nil, err
	}
	for _, proposal := range scan.Proposals {
		if proposal.ID != proposalID {
			continue
		}
		if proposal.State != "pending" {
			return nil, errors.New("only pending proposals can be previewed")
		}
		result := map[string]any{"proposal": proposal, "will_modify_repository": false, "requires_explicit_acceptance": true, "valid": true, "issues": []string{}}
		if proposal.Kind == "capability_pack" {
			var selected struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(proposal.Value, &selected); err != nil {
				return nil, err
			}
			manifest, report, err := s.Manifest(proposal.Key, selected.Version)
			if err != nil {
				return nil, err
			}
			result["manifest"], result["trust"], result["workflow_changes"] = manifest, report, manifest.WorkflowChanges
			if len(config) > 0 && !json.Valid(config) {
				return nil, errors.New("configuration is invalid JSON")
			}
		}
		return result, nil
	}
	return nil, errors.New("proposal not found")
}

func (s *Service) propose(projectID string, paths map[string]struct{}, evidence map[string]Evidence, findings []Finding) []Proposal {
	items := make([]Proposal, 0)
	for _, manifest := range s.catalog.List() {
		best := 0
		matched := []Evidence{}
		for _, rule := range manifest.DetectionRules {
			if ok, observed := matches(rule, paths); ok && rule.Confidence > best {
				best = rule.Confidence
				matched = nil
				for _, p := range observed {
					matched = append(matched, evidence[p])
				}
			}
		}
		if best > 0 {
			value, _ := json.Marshal(map[string]any{"pack_id": manifest.ID, "version": manifest.Version, "enabled": false})
			items = append(items, Proposal{ProjectID: projectID, Kind: "capability_pack", Key: manifest.ID, Value: value, Confidence: best, Evidence: matched, State: "pending", Version: 1})
		}
	}
	for _, finding := range findings {
		if finding.Category == "ci" || finding.Category == "tool" {
			value, _ := json.Marshal(map[string]any{"operation_id": finding.Value, "enabled": false})
			items = append(items, Proposal{ProjectID: projectID, Kind: "command_allowlist", Key: finding.Value, Value: value, Confidence: finding.Confidence, Evidence: finding.Evidence, State: "pending", Version: 1})
		}
		if finding.Category == "risk" {
			value, _ := json.Marshal(map[string]string{"path": finding.Value})
			items = append(items, Proposal{ProjectID: projectID, Kind: "protected_path", Key: finding.Value, Value: value, Confidence: finding.Confidence, Evidence: finding.Evidence, State: "pending", Version: 1})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].Key < items[j].Key
		}
		return items[i].Kind < items[j].Kind
	})
	return items
}

func detectRepository(paths map[string]struct{}, evidence map[string]Evidence, files []SourceFile) []Finding {
	type rule struct {
		category, value string
		confidence      int
		patterns        []string
	}
	rules := []rule{{"language", "php", 100, []string{"*.php"}}, {"language", "r", 100, []string{"*.R", "*.r"}}, {"language", "dotnet", 95, []string{"*.csproj", "*.sln"}}, {"framework", "composer", 95, []string{"composer.json"}}, {"package_manager", "npm", 95, []string{"package-lock.json"}}, {"package_manager", "renv", 95, []string{"renv.lock"}}, {"tool", "phpunit", 90, []string{"phpunit.xml", "phpunit.xml.dist"}}, {"tool", "phpstan", 90, []string{"phpstan.neon", "phpstan.neon.dist"}}, {"tool", "testthat", 85, []string{"tests/testthat.R"}}, {"ci", "github-actions", 95, []string{".github/workflows/*.yml", ".github/workflows/*.yaml"}}, {"ci", "gitlab-ci", 95, []string{".gitlab-ci.yml"}}, {"guidance", "agents", 100, []string{"AGENTS.md"}}, {"ownership", "codeowners", 100, []string{"CODEOWNERS", ".github/CODEOWNERS"}}, {"risk", "migrations", 80, []string{"migrations/*", "database/migrations/*"}}, {"repository", "submodules", 100, []string{".gitmodules"}}, {"repository", "git-lfs", 90, []string{".lfsconfig"}}}
	findings := make([]Finding, 0)
	for _, item := range rules {
		matched := []Evidence{}
		for candidate := range paths {
			for _, pattern := range item.patterns {
				if ok, _ := filepath.Match(pattern, candidate); ok {
					matched = append(matched, evidence[candidate])
					break
				}
			}
		}
		if len(matched) > 0 {
			sort.Slice(matched, func(i, j int) bool { return matched[i].Path < matched[j].Path })
			findings = append(findings, Finding{Category: item.category, Value: item.value, Confidence: item.confidence, Evidence: matched})
		}
	}
	for _, file := range files {
		if len(file.Content) > 0 && !utf8.Valid(file.Content) {
			findings = append(findings, Finding{Category: "encoding", Value: "non-utf8", Confidence: 90, Evidence: []Evidence{evidence[filepath.ToSlash(filepath.Clean(file.Path))]}})
		}
		if strings.Contains(string(file.Content), "\r\n") {
			findings = append(findings, Finding{Category: "line_endings", Value: "crlf", Confidence: 100, Evidence: []Evidence{evidence[filepath.ToSlash(filepath.Clean(file.Path))]}})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Category == findings[j].Category {
			return findings[i].Value < findings[j].Value
		}
		return findings[i].Category < findings[j].Category
	})
	return findings
}

func compareFindings(previous []Scan, current []Finding) []string {
	if len(previous) == 0 {
		return []string{}
	}
	old := map[string]struct{}{}
	now := map[string]struct{}{}
	for _, f := range previous[0].Findings {
		old[f.Category+":"+f.Value] = struct{}{}
	}
	for _, f := range current {
		now[f.Category+":"+f.Value] = struct{}{}
	}
	drift := []string{}
	for key := range now {
		if _, ok := old[key]; !ok {
			drift = append(drift, "added "+key)
		}
	}
	for key := range old {
		if _, ok := now[key]; !ok {
			drift = append(drift, "removed "+key)
		}
	}
	sort.Strings(drift)
	return drift
}
func newID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("random identifier: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func compareVersions(left, right string) int {
	var la, lb, lc, ra, rb, rc int
	_, _ = fmt.Sscanf(left, "%d.%d.%d", &la, &lb, &lc)
	_, _ = fmt.Sscanf(right, "%d.%d.%d", &ra, &rb, &rc)
	if la != ra {
		if la < ra {
			return -1
		}
		return 1
	}
	if lb != rb {
		if lb < rb {
			return -1
		}
		return 1
	}
	if lc < rc {
		return -1
	}
	if lc > rc {
		return 1
	}
	return 0
}
