package capabilities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

type Catalog struct {
	versions map[string]map[string]Manifest
}

func BuiltInCatalog() (*Catalog, error) {
	manifests := []Manifest{php83Intranet(), rStatistical(), sbomFMEA()}
	for index := range manifests {
		checksum, err := ComputeChecksum(manifests[index])
		if err != nil {
			return nil, err
		}
		manifests[index].ChecksumSHA256 = checksum
	}
	return NewCatalog(manifests)
}

func NewCatalog(manifests []Manifest) (*Catalog, error) {
	c := &Catalog{versions: map[string]map[string]Manifest{}}
	for _, manifest := range manifests {
		if report := ValidateManifest(manifest); !report.ChecksumValid || !report.AuthoritySafe || !report.Compatible {
			return nil, fmt.Errorf("invalid capability pack %s@%s: %s", manifest.ID, manifest.Version, strings.Join(report.Issues, "; "))
		}
		if c.versions[manifest.ID] == nil {
			c.versions[manifest.ID] = map[string]Manifest{}
		}
		if _, exists := c.versions[manifest.ID][manifest.Version]; exists {
			return nil, errors.New("duplicate capability pack version")
		}
		c.versions[manifest.ID][manifest.Version] = manifest
	}
	return c, nil
}

func (c *Catalog) List() []Manifest {
	items := make([]Manifest, 0)
	for _, versions := range c.versions {
		for _, manifest := range versions {
			items = append(items, manifest)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ID == items[j].ID {
			return items[i].Version > items[j].Version
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (c *Catalog) Get(id, version string) (Manifest, error) {
	manifest, ok := c.versions[id][version]
	if !ok {
		return Manifest{}, errors.New("trusted capability pack version not found")
	}
	return manifest, nil
}

func ComputeChecksum(manifest Manifest) (string, error) {
	manifest.ChecksumSHA256 = ""
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateManifest(manifest Manifest) TrustReport {
	report := TrustReport{PackID: manifest.ID, Version: manifest.Version, Expected: manifest.ChecksumSHA256, AuthoritySafe: true, Compatible: true}
	report.Computed, _ = ComputeChecksum(manifest)
	report.ChecksumValid = report.Expected != "" && report.Expected == report.Computed
	if manifest.SchemaVersion != SchemaVersion {
		report.Issues = append(report.Issues, "unsupported schema version")
		report.Compatible = false
	}
	if !safeID.MatchString(manifest.ID) || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(manifest.Version) {
		report.Issues = append(report.Issues, "unsafe pack identity")
	}
	if !report.ChecksumValid {
		report.Issues = append(report.Issues, "checksum mismatch")
	}
	for _, id := range append(append([]string{}, manifest.RunnerProfileIDs...), append(manifest.OperationClasses, manifest.ParserIDs...)...) {
		if !safeID.MatchString(id) {
			report.AuthoritySafe = false
			report.Issues = append(report.Issues, "unsafe trusted identifier")
		}
	}
	for _, rule := range manifest.DetectionRules {
		if !safeID.MatchString(rule.ID) || rule.Confidence < 1 || rule.Confidence > 100 {
			report.Issues = append(report.Issues, "invalid detection rule")
		}
		for _, candidate := range append(append([]string{}, rule.AnyPaths...), rule.AllPaths...) {
			if strings.HasPrefix(candidate, "/") || strings.Contains(candidate, "..") || strings.Contains(candidate, "\\") {
				report.AuthoritySafe = false
				report.Issues = append(report.Issues, "unsafe detection path")
			}
		}
	}
	return report
}

func matches(rule DetectionRule, paths map[string]struct{}) (bool, []string) {
	evidence := make([]string, 0)
	any := len(rule.AnyPaths) == 0
	for candidate := range paths {
		for _, pattern := range rule.AnyPaths {
			if ok, _ := path.Match(pattern, candidate); ok {
				any = true
				evidence = append(evidence, candidate)
			}
		}
	}
	if !any {
		return false, nil
	}
	for _, pattern := range rule.AllPaths {
		found := false
		for candidate := range paths {
			if ok, _ := path.Match(pattern, candidate); ok {
				found = true
				evidence = append(evidence, candidate)
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	sort.Strings(evidence)
	return true, compact(evidence)
}

func compact(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func booleanField(key, label, help string, defaultValue bool) UIField {
	raw, _ := json.Marshal(defaultValue)
	return UIField{Key: key, Label: label, Kind: "boolean", Default: raw, Help: help}
}
func enumField(key, label, help, fallback string, allowed ...string) UIField {
	raw, _ := json.Marshal(fallback)
	return UIField{Key: key, Label: label, Kind: "enum", Default: raw, Allowed: allowed, Help: help}
}

func php83Intranet() Manifest {
	return Manifest{SchemaVersion: 1, ID: "php83-intranet", Name: "PHP 8.3 intranet", Version: "1.0.0", Description: "Evidence-driven PHP 8.3 intranet verification profiles.", Languages: []string{"php", "javascript", "typescript", "twig"}, Compatibility: Compatibility{ControllerConstraint: ">=2.0.0", Platforms: []string{"linux/amd64", "linux/arm64"}}, Prerequisites: []Prerequisite{{ID: "php83-toolchain", Required: true, Help: "Pinned PHP 8.3 verifier profile must be installed."}, {ID: "mysql8-service", Help: "Required only when database rehearsals are selected."}, {ID: "redis-service", Help: "Required only when Redis integration is selected."}}, DetectionRules: []DetectionRule{{ID: "composer-project", AnyPaths: []string{"composer.json"}, Confidence: 95, Explanation: "Composer manifest identifies a PHP project."}, {ID: "phpunit-project", AnyPaths: []string{"phpunit.xml", "phpunit.xml.dist"}, Confidence: 90, Explanation: "PHPUnit configuration enables parsed tests."}, {ID: "web-assets", AnyPaths: []string{"vite.config.*", "tailwind.config.*"}, Confidence: 75, Explanation: "Frontend build configuration is present."}}, RunnerProfileIDs: []string{"php83-verify", "php83-browser"}, OperationClasses: []string{"composer-validate", "composer-audit", "phpunit", "phpstan", "psalm", "rector-dry-run", "infection-selected", "twig-validate", "apache-rehearsal", "mysql8-rehearsal", "redis-rehearsal", "vite-build", "playwright-test", "repository-portability-check"}, ParserIDs: []string{"junit-v1", "clover-v1", "phpstan-json-v1", "playwright-json-v1"}, PolicyFragments: []string{"php-lock-required", "rector-dry-run-only", "database-disposable-only"}, ContextSelectors: []string{"composer-autoload", "php-config", "templates", "migrations", "frontend-config"}, RiskRules: []string{"migration-high-risk", "auth-high-risk", "theme-contract-risk"}, Documentation: []string{"composer-command-reference", "migration-notes", "theme-plugin-contract"}, WorkflowChanges: []WorkflowChange{{Stage: "verify", OperationID: "composer-validate", Required: true, Description: "Validate Composer metadata and lock consistency."}, {Stage: "verify", OperationID: "phpunit", Description: "Run repository-selected PHPUnit profile."}, {Stage: "rehearsal", OperationID: "repository-portability-check", Required: true, Description: "Check CRLF, shebangs, permissions, encoding, and case collisions."}}, UISchema: []UIField{enumField("analysis.static", "Static analysis", "Select a repository-pinned analyzer.", "auto", "auto", "phpstan", "psalm", "disabled"), enumField("tests.coverage", "Coverage engine", "Select coverage only when available.", "auto", "auto", "pcov", "xdebug", "disabled"), booleanField("services.mysql8", "Disposable MySQL 8", "Enable schema and fixture rehearsal.", false), booleanField("services.redis", "Disposable Redis", "Enable Redis integration rehearsal.", false), booleanField("browser.playwright", "Playwright", "Enable browser and visual checks.", false)}, Rehearsals: []RehearsalDefinition{{ID: "php-portability", Kind: "repository", OperationID: "repository-portability-check", ArtifactKinds: []string{"portability-report"}, ComparisonClass: "structured", ApprovalPolicy: "review-required"}, {ID: "php-web-journey", Kind: "browser", OperationID: "playwright-test", ArtifactKinds: []string{"screenshot", "accessibility-snapshot", "journey-report"}, ComparisonClass: "masked-visual", ApprovalPolicy: "review-required"}, {ID: "php-database", Kind: "database", OperationID: "mysql8-rehearsal", ArtifactKinds: []string{"migration-report", "schema-diff"}, ComparisonClass: "structured", ApprovalPolicy: "review-required"}}}
}

func rStatistical() Manifest {
	return Manifest{SchemaVersion: 1, ID: "r-statistical-validation", Name: "R statistical validation", Version: "1.0.0", Description: "Reproducible R checks and numerical golden comparisons.", Languages: []string{"r"}, Compatibility: Compatibility{ControllerConstraint: ">=2.0.0", Platforms: []string{"linux/amd64"}}, Prerequisites: []Prerequisite{{ID: "r-toolchain", Required: true, Help: "Pinned R verifier profile must be installed."}}, DetectionRules: []DetectionRule{{ID: "r-package", AnyPaths: []string{"DESCRIPTION"}, Confidence: 95, Explanation: "R package DESCRIPTION is present."}, {ID: "renv-lock", AnyPaths: []string{"renv.lock"}, Confidence: 95, Explanation: "renv lockfile enables deterministic restore."}}, RunnerProfileIDs: []string{"r-validation"}, OperationClasses: []string{"renv-restore", "r-cmd-check", "testthat", "lintr", "roxygen-check", "pkgdown-build", "r-golden-compare"}, ParserIDs: []string{"r-check-v1", "testthat-v1", "r-golden-v1"}, PolicyFragments: []string{"renv-lock-preferred", "golden-update-review"}, ContextSelectors: []string{"r-package-metadata", "tests", "vignettes", "reference-data"}, RiskRules: []string{"numerical-output-risk", "seed-change-risk"}, Documentation: []string{"roxygen-source", "pkgdown-site"}, WorkflowChanges: []WorkflowChange{{Stage: "verify", OperationID: "r-cmd-check", Required: true, Description: "Run R CMD check."}, {Stage: "rehearsal", OperationID: "r-golden-compare", Description: "Compare selected statistical outputs."}}, UISchema: []UIField{enumField("missing_values.policy", "Missing values", "Choose comparison behavior.", "exact", "exact", "ignore-paired", "reject"), enumField("environment.locale", "Locale", "Use a controller-trusted locale.", "C", "C", "en_US.UTF-8"), enumField("environment.timezone", "Time zone", "Use a controller-trusted time zone.", "UTC", "UTC"), booleanField("documentation.pkgdown", "pkgdown", "Build repository documentation.", false)}, Rehearsals: []RehearsalDefinition{{ID: "r-reference-results", Kind: "statistical", OperationID: "r-golden-compare", ArtifactKinds: []string{"table-diff", "model-diff", "chart-diff", "reproducibility-metadata"}, ComparisonClass: "numeric-tolerance", ApprovalPolicy: "review-required"}}}
}

func sbomFMEA() Manifest {
	return Manifest{SchemaVersion: 1, ID: "sbom-fmea-security", Name: "SBOM, FMEA, and security", Version: "1.0.0", Description: "Selectable pinned security evidence and FMEA correlation.", Languages: []string{"mixed"}, Compatibility: Compatibility{ControllerConstraint: ">=2.0.0", Platforms: []string{"linux/amd64"}}, Prerequisites: []Prerequisite{{ID: "scanner-database", Help: "Required only by scanners selected for the project."}}, DetectionRules: []DetectionRule{{ID: "dependency-manifest", AnyPaths: []string{"go.mod", "package-lock.json", "composer.lock", "renv.lock", "*.csproj", "Cargo.lock"}, Confidence: 80, Explanation: "A dependency lock or manifest can produce an SBOM."}}, RunnerProfileIDs: []string{"security-scan"}, OperationClasses: []string{"syft-sbom", "grype-scan", "trivy-scan", "codeql-analyze", "sbom-diff", "fmea-correlate"}, ParserIDs: []string{"cyclonedx-v1", "sarif-v2.1", "fmea-v1"}, PolicyFragments: []string{"scanner-selective", "suppression-expiry-required", "release-risk-gate"}, ContextSelectors: []string{"dependency-manifests", "public-interfaces", "security-config"}, RiskRules: []string{"new-critical-vulnerability", "new-privilege", "new-exposed-interface", "license-change"}, Documentation: []string{"security-evidence", "sbom-release-note", "fmea-record"}, WorkflowChanges: []WorkflowChange{{Stage: "verify", OperationID: "syft-sbom", Description: "Generate a selected SBOM."}, {Stage: "qc", OperationID: "sbom-diff", Description: "Compare dependencies, vulnerabilities, licenses, interfaces, and privileges."}}, UISchema: []UIField{enumField("scanner.primary", "Primary scanner", "Run only explicitly selected scanners.", "syft", "syft", "grype", "trivy", "codeql", "disabled"), enumField("threshold.severity", "Release severity", "Select release gate severity.", "high", "medium", "high", "critical"), booleanField("fmea.enabled", "FMEA correlation", "Correlate evidence with operator-maintained failure modes.", true)}, Rehearsals: []RehearsalDefinition{{ID: "sbom-release-diff", Kind: "security", OperationID: "sbom-diff", ArtifactKinds: []string{"sbom", "sbom-diff", "risk-matrix"}, ComparisonClass: "structured", ApprovalPolicy: "release-review"}}}
}
