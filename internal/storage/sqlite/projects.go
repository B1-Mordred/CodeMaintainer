package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Store) UpsertProject(ctx context.Context, request projects.UpsertRequest, actorID string) (projects.Project, error) {
	if err := request.Validate(); err != nil {
		return projects.Project{}, fmt.Errorf("%w: %v", storage.ErrInvalid, err)
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return projects.Project{}, err
	}
	defer tx.Rollback()
	var existingID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE provider = ? AND repository = ?", request.Provider, request.Repository).Scan(&existingID)
	if err == nil && existingID != request.ID {
		return projects.Project{}, storage.ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return projects.Project{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(
		id, provider, repository, default_branch, local_remote_name, enabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, repository=excluded.repository,
		default_branch=excluded.default_branch, local_remote_name=excluded.local_remote_name,
		enabled=1, updated_at=excluded.updated_at`, request.ID, request.Provider, request.Repository,
		request.DefaultBranch, request.LocalRemoteName, now.Format(timestampFormat), now.Format(timestampFormat))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return projects.Project{}, storage.ErrConflict
		}
		return projects.Project{}, err
	}
	details, _ := json.Marshal(map[string]string{"provider": request.Provider, "repository": request.Repository})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: required(actorID, "system"), ActorRole: "administrator", Action: "project.upsert",
		TargetType: "project", TargetID: request.ID, Details: details,
	}); err != nil {
		return projects.Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return projects.Project{}, err
	}
	return s.GetProject(ctx, request.ID)
}

const projectSelect = `SELECT id, provider, repository, default_branch, local_remote_name,
	enabled, created_at, updated_at FROM projects`

func (s *Store) GetProject(ctx context.Context, id string) (projects.Project, error) {
	if !projects.ValidID(id) {
		return projects.Project{}, storage.ErrInvalid
	}
	return scanProject(s.db.QueryRowContext(ctx, projectSelect+" WHERE id = ?", id))
}

func (s *Store) ListProjects(ctx context.Context, limit int) ([]projects.Project, error) {
	limit = boundedLimit(limit, 100, 500)
	rows, err := s.db.QueryContext(ctx, projectSelect+" ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projects.Project, 0)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanProject(row scanner) (projects.Project, error) {
	var item projects.Project
	var enabled int
	var created, updated string
	if err := row.Scan(&item.ID, &item.Provider, &item.Repository, &item.DefaultBranch,
		&item.LocalRemoteName, &enabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projects.Project{}, storage.ErrNotFound
		}
		return projects.Project{}, err
	}
	item.Enabled = enabled == 1
	var err error
	item.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return projects.Project{}, err
	}
	item.UpdatedAt, err = time.Parse(timestampFormat, updated)
	return item, err
}
