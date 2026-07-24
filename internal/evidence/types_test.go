package evidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArtifactEvidenceBuildsTypedNodeAndEdge(t *testing.T) {
	created := time.Unix(1, 0).UTC()
	job := JobNode("job-one", "project-one", created)
	artifact := ArtifactNode(ArtifactRef{
		ID: "artifact-one", JobID: "job-one", ProjectID: "project-one", ObjectSHA256: strings.Repeat("a", 64),
		Bytes: 128, Kind: "verification_report", MediaType: "application/json", Producer: "verifier", CreatedAt: created,
	})
	if err := job.Normalize(); err != nil {
		t.Fatal(err)
	}
	if err := artifact.Normalize(); err != nil {
		t.Fatal(err)
	}
	if artifact.Kind != NodeKindArtifact || artifact.SubjectSHA256 != strings.Repeat("a", 64) || artifact.MetadataSHA256 == "" {
		t.Fatalf("unexpected artifact node %#v", artifact)
	}
	var metadata map[string]any
	if err := json.Unmarshal(artifact.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["media_type"] != "application/json" || metadata["kind"] != "verification_report" {
		t.Fatalf("artifact metadata omitted typed provenance: %#v", metadata)
	}
	edge := ProducedEdge(job, artifact)
	if err := edge.Normalize(); err != nil {
		t.Fatal(err)
	}
	if edge.FromNodeID != job.ID || edge.ToNodeID != artifact.ID || edge.Relationship != EdgeRelationProduced {
		t.Fatalf("unexpected evidence edge %#v", edge)
	}
}

func TestEvidenceValidationRejectsMalformedOrSelfReferentialEvidence(t *testing.T) {
	node := Node{ID: "node", JobID: "job", ProjectID: "project", Kind: NodeKindArtifact, SubjectID: "artifact", SubjectSHA256: "not-a-sha", Label: "Artifact", Producer: "test", Metadata: json.RawMessage(`{}`)}
	if err := node.Normalize(); err == nil {
		t.Fatal("malformed node hash was accepted")
	}
	edge := Edge{ID: "edge", JobID: "job", ProjectID: "project", FromNodeID: "same", ToNodeID: "same", Relationship: EdgeRelationProduced, Reason: "bad", ActorID: "test", Metadata: json.RawMessage(`{}`)}
	if err := edge.Normalize(); err == nil {
		t.Fatal("self-referential edge was accepted")
	}
}
