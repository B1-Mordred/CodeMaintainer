package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/B1-Mordred/CodeMaintainer/internal/evidence"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) RecordEvidenceNode(ctx context.Context, node evidence.Node) (evidence.Node, error) {
	if node.CreatedAt.IsZero() {
		node.CreatedAt = s.now()
	}
	if err := node.Normalize(); err != nil {
		return evidence.Node{}, storage.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO evidence_nodes(
		id,job_id,project_id,kind,subject_id,subject_sha256,label,producer,metadata_json,metadata_sha256,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, node.ID, node.JobID, node.ProjectID, node.Kind, node.SubjectID,
		node.SubjectSHA256, node.Label, node.Producer, string(node.Metadata), node.MetadataSHA256,
		node.CreatedAt.Format(timestampFormat))
	if err != nil {
		return evidence.Node{}, err
	}
	return node, nil
}

func (s *Store) RecordEvidenceEdge(ctx context.Context, edge evidence.Edge) (evidence.Edge, error) {
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = s.now()
	}
	if err := edge.Normalize(); err != nil {
		return evidence.Edge{}, storage.ErrInvalid
	}
	if err := s.validateEvidenceEdgeEndpoints(ctx, s.db, edge); err != nil {
		return evidence.Edge{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO evidence_edges(
		id,job_id,project_id,from_node_id,to_node_id,relationship,reason,actor_id,metadata_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, edge.ID, edge.JobID, edge.ProjectID, edge.FromNodeID, edge.ToNodeID,
		edge.Relationship, edge.Reason, edge.ActorID, string(edge.Metadata), edge.CreatedAt.Format(timestampFormat))
	if err != nil {
		return evidence.Edge{}, err
	}
	return edge, nil
}

func (s *Store) ListJobEvidenceGraph(ctx context.Context, jobID string) (evidence.Graph, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return evidence.Graph{}, err
	}
	graph := evidence.Graph{JobID: job.ID, ProjectID: job.ProjectID, Nodes: []evidence.Node{}, Edges: []evidence.Edge{}}
	nodeRows, err := s.db.QueryContext(ctx, `SELECT id,job_id,project_id,kind,subject_id,subject_sha256,label,producer,metadata_json,metadata_sha256,created_at
		FROM evidence_nodes WHERE job_id=? ORDER BY created_at,id`, job.ID)
	if err != nil {
		return evidence.Graph{}, err
	}
	defer nodeRows.Close()
	for nodeRows.Next() {
		node, err := scanEvidenceNode(nodeRows)
		if err != nil {
			return evidence.Graph{}, err
		}
		graph.Nodes = append(graph.Nodes, node)
	}
	if err := nodeRows.Err(); err != nil {
		return evidence.Graph{}, err
	}
	edgeRows, err := s.db.QueryContext(ctx, `SELECT id,job_id,project_id,from_node_id,to_node_id,relationship,reason,actor_id,metadata_json,created_at
		FROM evidence_edges WHERE job_id=? ORDER BY created_at,id`, job.ID)
	if err != nil {
		return evidence.Graph{}, err
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		edge, err := scanEvidenceEdge(edgeRows)
		if err != nil {
			return evidence.Graph{}, err
		}
		graph.Edges = append(graph.Edges, edge)
	}
	return graph, edgeRows.Err()
}

func (s *Store) indexArtifactEvidenceTx(ctx context.Context, tx *sql.Tx, record storage.ArtifactRecord) error {
	job := evidence.JobNode(record.JobID, record.ProjectID, record.CreatedAt)
	if err := job.Normalize(); err != nil {
		return storage.ErrInvalid
	}
	artifact := evidence.ArtifactNode(evidence.ArtifactRef{
		ID: record.ID, JobID: record.JobID, ProjectID: record.ProjectID, ObjectSHA256: record.ObjectSHA256,
		Bytes: record.Bytes, Kind: record.Kind, MediaType: record.MediaType, Producer: record.Producer, CreatedAt: record.CreatedAt,
	})
	if err := artifact.Normalize(); err != nil {
		return storage.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_nodes(
		id,job_id,project_id,kind,subject_id,subject_sha256,label,producer,metadata_json,metadata_sha256,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, job.ID, job.JobID, job.ProjectID, job.Kind, job.SubjectID,
		job.SubjectSHA256, job.Label, job.Producer, string(job.Metadata), job.MetadataSHA256, job.CreatedAt.Format(timestampFormat)); err != nil {
		return fmt.Errorf("index job evidence node: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_nodes(
		id,job_id,project_id,kind,subject_id,subject_sha256,label,producer,metadata_json,metadata_sha256,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, artifact.ID, artifact.JobID, artifact.ProjectID,
		artifact.Kind, artifact.SubjectID, artifact.SubjectSHA256, artifact.Label, artifact.Producer,
		string(artifact.Metadata), artifact.MetadataSHA256, artifact.CreatedAt.Format(timestampFormat)); err != nil {
		return fmt.Errorf("index artifact evidence node: %w", err)
	}
	edge := evidence.ProducedEdge(job, artifact)
	if err := edge.Normalize(); err != nil {
		return storage.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_edges(
		id,job_id,project_id,from_node_id,to_node_id,relationship,reason,actor_id,metadata_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, edge.ID, edge.JobID, edge.ProjectID, edge.FromNodeID,
		edge.ToNodeID, edge.Relationship, edge.Reason, edge.ActorID, string(edge.Metadata), edge.CreatedAt.Format(timestampFormat)); err != nil {
		return fmt.Errorf("index artifact evidence edge: %w", err)
	}
	return nil
}

type evidenceQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) validateEvidenceEdgeEndpoints(ctx context.Context, querier evidenceQuerier, edge evidence.Edge) error {
	for _, nodeID := range []string{edge.FromNodeID, edge.ToNodeID} {
		var jobID, projectID string
		err := querier.QueryRowContext(ctx, "SELECT job_id, project_id FROM evidence_nodes WHERE id=?", nodeID).Scan(&jobID, &projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ErrNotFound
		}
		if err != nil {
			return err
		}
		if jobID != edge.JobID || projectID != edge.ProjectID {
			return storage.ErrInvalid
		}
	}
	return nil
}

func scanEvidenceNode(row scanner) (evidence.Node, error) {
	var node evidence.Node
	var metadata, created string
	err := row.Scan(&node.ID, &node.JobID, &node.ProjectID, &node.Kind, &node.SubjectID, &node.SubjectSHA256,
		&node.Label, &node.Producer, &metadata, &node.MetadataSHA256, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence.Node{}, storage.ErrNotFound
	}
	if err != nil {
		return evidence.Node{}, err
	}
	if !json.Valid([]byte(metadata)) {
		return evidence.Node{}, storage.ErrInvalid
	}
	node.Metadata = json.RawMessage(metadata)
	createdAt, err := parseTime(created)
	if err != nil {
		return evidence.Node{}, err
	}
	node.CreatedAt = createdAt
	return node, nil
}

func scanEvidenceEdge(row scanner) (evidence.Edge, error) {
	var edge evidence.Edge
	var metadata, created string
	err := row.Scan(&edge.ID, &edge.JobID, &edge.ProjectID, &edge.FromNodeID, &edge.ToNodeID,
		&edge.Relationship, &edge.Reason, &edge.ActorID, &metadata, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence.Edge{}, storage.ErrNotFound
	}
	if err != nil {
		return evidence.Edge{}, err
	}
	if !json.Valid([]byte(metadata)) {
		return evidence.Edge{}, storage.ErrInvalid
	}
	edge.Metadata = json.RawMessage(metadata)
	createdAt, err := parseTime(created)
	if err != nil {
		return evidence.Edge{}, err
	}
	edge.CreatedAt = createdAt
	return edge, nil
}
