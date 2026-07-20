package verification

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	artifactfiles "github.com/local-code-maintainer/appliance/internal/artifacts"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

var verificationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ArtifactWriter interface {
	Put(context.Context, artifactfiles.PutRequest) (storage.ArtifactRecord, error)
}

type Verifier struct {
	registry  *Registry
	scanner   *Scanner
	executor  Executor
	artifacts ArtifactWriter
	now       func() time.Time
}

func NewVerifier(registry *Registry, scanner *Scanner, executor Executor, artifacts ArtifactWriter) (*Verifier, error) {
	if registry == nil || scanner == nil || executor == nil || artifacts == nil {
		return nil, ErrInvalidRequest
	}
	return &Verifier{registry: registry, scanner: scanner, executor: executor, artifacts: artifacts, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (v *Verifier) Verify(ctx context.Context, request Request) (Report, error) {
	if !verificationID.MatchString(request.JobID) || !verificationID.MatchString(request.ProjectID) ||
		request.WorktreePath == "" || !exactCommit.MatchString(request.BaseSHA) || len(request.Classes) == 0 || len(request.Classes) > 16 {
		return Report{}, ErrInvalidRequest
	}
	commands := make([]Command, 0, len(request.Classes))
	seen := make(map[Class]struct{}, len(request.Classes))
	for _, class := range request.Classes {
		if _, exists := seen[class]; exists {
			return Report{}, fmt.Errorf("%w: duplicate class %s", ErrInvalidRequest, class)
		}
		seen[class] = struct{}{}
		command, err := v.registry.Resolve(request.Language, class)
		if err != nil {
			return Report{}, err
		}
		commands = append(commands, command)
	}
	started := v.now()
	scan, err := v.scanner.Scan(ctx, request.WorktreePath, request.BaseSHA, request.Policy)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		JobID: request.JobID, ProjectID: request.ProjectID, BaseSHA: request.BaseSHA,
		HeadSHA: scan.HeadSHA, PatchSHA256: scan.PatchSHA256, Findings: scan.Findings,
		Commands: make([]ExecutionResult, 0, len(commands)), ArtifactIDs: make([]string, 0), StartedAt: started,
	}
	passed := true
	for _, finding := range scan.Findings {
		if finding.Severity == "blocker" || finding.Severity == "must_fix" {
			passed = false
		}
	}
	for _, command := range commands {
		result := v.executor.Run(ctx, request.WorktreePath, command, request.Timeout, request.MaxLogBytes)
		report.Commands = append(report.Commands, result)
		if result.ExitCode != 0 || result.TimedOut {
			passed = false
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return Report{}, marshalErr
		}
		artifact, putErr := v.artifacts.Put(ctx, artifactfiles.PutRequest{
			JobID: request.JobID, ProjectID: request.ProjectID, Kind: "command_result",
			MediaType: "application/json", Producer: "verifier", Reader: strings.NewReader(string(payload)),
			Metadata: json.RawMessage(fmt.Sprintf(`{"class":%q,"head_sha":%q}`, command.Class, scan.HeadSHA)),
		})
		if putErr != nil {
			return Report{}, putErr
		}
		report.ArtifactIDs = append(report.ArtifactIDs, artifact.ID)
	}
	report.Passed = passed
	report.CompletedAt = v.now()

	standardArtifacts := []struct {
		kind, mediaType string
		payload         []byte
	}{
		{"junit", "application/xml", junitArtifact(report)},
		{"sarif", "application/sarif+json", sarifArtifact(report)},
		{"coverage", "application/json", coverageArtifact(report)},
	}
	for _, standard := range standardArtifacts {
		artifact, putErr := v.artifacts.Put(ctx, artifactfiles.PutRequest{
			JobID: request.JobID, ProjectID: request.ProjectID, Kind: standard.kind,
			MediaType: standard.mediaType, Producer: "verifier", Reader: strings.NewReader(string(standard.payload)),
			Metadata: json.RawMessage(fmt.Sprintf(`{"head_sha":%q,"patch_sha256":%q}`, report.HeadSHA, report.PatchSHA256)),
		})
		if putErr != nil {
			return Report{}, putErr
		}
		report.ArtifactIDs = append(report.ArtifactIDs, artifact.ID)
	}
	reportPayload, err := json.Marshal(report)
	if err != nil {
		return Report{}, err
	}
	summary, err := v.artifacts.Put(ctx, artifactfiles.PutRequest{
		JobID: request.JobID, ProjectID: request.ProjectID, Kind: "verification_report",
		MediaType: "application/json", Producer: "verifier", Reader: strings.NewReader(string(reportPayload)),
		Metadata: json.RawMessage(fmt.Sprintf(`{"head_sha":%q,"passed":%t}`, report.HeadSHA, report.Passed)),
	})
	if err != nil {
		return Report{}, err
	}
	report.ArtifactIDs = append(report.ArtifactIDs, summary.ID)
	return report, nil
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func junitArtifact(report Report) []byte {
	suite := junitSuite{Name: "local-code-maintainer", Tests: len(report.Commands), Cases: make([]junitCase, 0, len(report.Commands))}
	for _, command := range report.Commands {
		item := junitCase{Name: string(command.Class), Time: strconv.FormatFloat(command.Duration.Seconds(), 'f', 6, 64)}
		if command.ExitCode != 0 || command.TimedOut {
			suite.Failures++
			evidence := command.Output
			if len(evidence) > 8192 {
				evidence = evidence[:8192] + "\n[JUnit evidence truncated]"
			}
			item.Failure = &junitFailure{Message: fmt.Sprintf("exit=%d timeout=%t", command.ExitCode, command.TimedOut), Body: evidence}
		}
		suite.Cases = append(suite.Cases, item)
	}
	payload, _ := xml.MarshalIndent(suite, "", "  ")
	return append([]byte(xml.Header), payload...)
}

func sarifArtifact(report Report) []byte {
	results := make([]map[string]any, 0, len(report.Findings))
	for _, finding := range report.Findings {
		item := map[string]any{
			"ruleId": finding.Rule, "level": sarifLevel(finding.Severity),
			"message": map[string]string{"text": finding.Evidence + "; verification: " + finding.Verification},
		}
		if finding.Path != "" {
			item["locations"] = []any{map[string]any{"physicalLocation": map[string]any{"artifactLocation": map[string]string{"uri": finding.Path}}}}
		}
		results = append(results, item)
	}
	payload, _ := json.MarshalIndent(map[string]any{
		"version": "2.1.0", "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"runs": []any{map[string]any{
			"tool":    map[string]any{"driver": map[string]string{"name": "Local Code Maintainer verifier", "version": "1"}},
			"results": results,
		}},
	}, "", "  ")
	return payload
}

func coverageArtifact(report Report) []byte {
	payload, _ := json.MarshalIndent(map[string]any{
		"schema_version": 1, "available": false, "head_sha": report.HeadSHA,
		"reason": "selected verifier command classes did not emit normalized coverage",
	}, "", "  ")
	return payload
}

func sarifLevel(severity string) string {
	if severity == "blocker" || severity == "must_fix" {
		return "error"
	}
	if severity == "should_fix" {
		return "warning"
	}
	return "note"
}
