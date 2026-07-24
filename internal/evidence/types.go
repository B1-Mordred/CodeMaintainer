package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	NodeKindJob      = "job"
	NodeKindArtifact = "artifact"

	EdgeRelationProduced = "produced"
)

type Node struct {
	ID             string          `json:"id"`
	JobID          string          `json:"job_id"`
	ProjectID      string          `json:"project_id"`
	Kind           string          `json:"kind"`
	SubjectID      string          `json:"subject_id"`
	SubjectSHA256  string          `json:"subject_sha256"`
	Label          string          `json:"label"`
	Producer       string          `json:"producer"`
	Metadata       json.RawMessage `json:"metadata"`
	MetadataSHA256 string          `json:"metadata_sha256"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Edge struct {
	ID           string          `json:"id"`
	JobID        string          `json:"job_id"`
	ProjectID    string          `json:"project_id"`
	FromNodeID   string          `json:"from_node_id"`
	ToNodeID     string          `json:"to_node_id"`
	Relationship string          `json:"relationship"`
	Reason       string          `json:"reason"`
	ActorID      string          `json:"actor_id"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

type Graph struct {
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
}

type ArtifactRef struct {
	ID           string
	JobID        string
	ProjectID    string
	ObjectSHA256 string
	Bytes        int64
	Kind         string
	MediaType    string
	Producer     string
	CreatedAt    time.Time
}

type Store interface {
	RecordEvidenceNode(context.Context, Node) (Node, error)
	RecordEvidenceEdge(context.Context, Edge) (Edge, error)
	ListJobEvidenceGraph(context.Context, string) (Graph, error)
}

func JobNode(jobID, projectID string, createdAt time.Time) Node {
	return Node{
		ID:            "evidence_node_job_" + safeID(jobID),
		JobID:         jobID,
		ProjectID:     projectID,
		Kind:          NodeKindJob,
		SubjectID:     jobID,
		SubjectSHA256: sha256Text(jobID),
		Label:         "Job " + jobID,
		Producer:      "controller",
		Metadata:      json.RawMessage(`{"source":"job"}`),
		CreatedAt:     createdAt,
	}
}

func ArtifactNode(record ArtifactRef) Node {
	metadata, _ := json.Marshal(map[string]any{
		"media_type": record.MediaType,
		"bytes":      record.Bytes,
		"kind":       record.Kind,
	})
	return Node{
		ID:            "evidence_node_artifact_" + safeID(record.ID),
		JobID:         record.JobID,
		ProjectID:     record.ProjectID,
		Kind:          NodeKindArtifact,
		SubjectID:     record.ID,
		SubjectSHA256: record.ObjectSHA256,
		Label:         record.Kind + " artifact",
		Producer:      record.Producer,
		Metadata:      metadata,
		CreatedAt:     record.CreatedAt,
	}
}

func ProducedEdge(job Node, artifact Node) Edge {
	metadata, _ := json.Marshal(map[string]string{"source": "artifact_index"})
	return Edge{
		ID:           "evidence_edge_" + shortHash(job.ID+"\n"+artifact.ID+"\n"+EdgeRelationProduced),
		JobID:        artifact.JobID,
		ProjectID:    artifact.ProjectID,
		FromNodeID:   job.ID,
		ToNodeID:     artifact.ID,
		Relationship: EdgeRelationProduced,
		Reason:       "artifact retained for job evidence",
		ActorID:      artifact.Producer,
		Metadata:     metadata,
		CreatedAt:    artifact.CreatedAt,
	}
}

func (node *Node) Normalize() error {
	node.ID = strings.TrimSpace(node.ID)
	node.JobID = strings.TrimSpace(node.JobID)
	node.ProjectID = strings.TrimSpace(node.ProjectID)
	node.Kind = strings.TrimSpace(node.Kind)
	node.SubjectID = strings.TrimSpace(node.SubjectID)
	node.SubjectSHA256 = strings.TrimSpace(node.SubjectSHA256)
	node.Label = strings.TrimSpace(node.Label)
	node.Producer = strings.TrimSpace(node.Producer)
	if len(node.Metadata) == 0 {
		node.Metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(node.Metadata) {
		return errors.New("evidence node metadata must be valid JSON")
	}
	node.MetadataSHA256 = sha256Text(string(node.Metadata))
	if node.ID == "" || node.JobID == "" || node.ProjectID == "" || node.Kind == "" ||
		node.SubjectID == "" || node.SubjectSHA256 == "" || len(node.SubjectSHA256) != 64 ||
		node.Label == "" || node.Producer == "" {
		return errors.New("evidence node has missing required fields")
	}
	if len(node.ID) > 160 || len(node.Kind) > 64 || len(node.SubjectID) > 200 || len(node.Label) > 240 || len(node.Producer) > 120 {
		return errors.New("evidence node exceeds bounded field length")
	}
	return nil
}

func (edge *Edge) Normalize() error {
	edge.ID = strings.TrimSpace(edge.ID)
	edge.JobID = strings.TrimSpace(edge.JobID)
	edge.ProjectID = strings.TrimSpace(edge.ProjectID)
	edge.FromNodeID = strings.TrimSpace(edge.FromNodeID)
	edge.ToNodeID = strings.TrimSpace(edge.ToNodeID)
	edge.Relationship = strings.TrimSpace(edge.Relationship)
	edge.Reason = strings.TrimSpace(edge.Reason)
	edge.ActorID = strings.TrimSpace(edge.ActorID)
	if len(edge.Metadata) == 0 {
		edge.Metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(edge.Metadata) {
		return errors.New("evidence edge metadata must be valid JSON")
	}
	if edge.ID == "" || edge.JobID == "" || edge.ProjectID == "" || edge.FromNodeID == "" ||
		edge.ToNodeID == "" || edge.Relationship == "" || edge.Reason == "" || edge.ActorID == "" {
		return errors.New("evidence edge has missing required fields")
	}
	if edge.FromNodeID == edge.ToNodeID {
		return errors.New("evidence edge cannot self-reference")
	}
	if len(edge.ID) > 160 || len(edge.Relationship) > 80 || len(edge.Reason) > 500 || len(edge.ActorID) > 120 {
		return errors.New("evidence edge exceeds bounded field length")
	}
	return nil
}

func safeID(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func shortHash(value string) string {
	return sha256Text(value)[:32]
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
