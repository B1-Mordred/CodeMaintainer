package api

import (
	"net/http"
	"time"
)

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup service is unavailable")
		return
	}
	items, err := s.backups.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok || !principal.RecentlyReauthenticated(time.Now().UTC()) {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "backup creation requires reauthentication within five minutes")
		return
	}
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup service is unavailable")
		return
	}
	record, err := s.backups.Create(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok || !principal.RecentlyReauthenticated(time.Now().UTC()) {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "restore validation requires reauthentication within five minutes")
		return
	}
	var request struct {
		DryRun bool `json:"dry_run"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup service is unavailable")
		return
	}
	if !request.DryRun {
		result, err := s.backups.StageRestore(r.Context(), r.PathValue("backupID"))
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "backup_invalid", "backup compatibility or checksums failed validation")
			return
		}
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	report, err := s.backups.Validate(r.Context(), r.PathValue("backupID"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "backup_invalid", "backup compatibility or checksums failed validation")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) updatePreflight(w http.ResponseWriter, r *http.Request) {
	checks := []map[string]any{}
	ready := true
	if _, err := s.store.CurrentConfig(r.Context()); err != nil {
		checks = append(checks, map[string]any{"name": "configuration", "status": "failed", "detail": "active configuration is unavailable"})
		ready = false
	} else {
		checks = append(checks, map[string]any{"name": "configuration", "status": "passed", "detail": "active revision is readable"})
	}
	if _, err := s.store.ListJobs(r.Context(), 1, 0); err != nil {
		checks = append(checks, map[string]any{"name": "database", "status": "failed", "detail": "durable database is unavailable"})
		ready = false
	} else {
		checks = append(checks, map[string]any{"name": "database", "status": "passed", "detail": "durable database is readable"})
	}
	backupCount := 0
	if s.backups == nil {
		checks = append(checks, map[string]any{"name": "backup", "status": "failed", "detail": "backup service is unavailable"})
		ready = false
	} else if items, err := s.backups.List(r.Context()); err != nil {
		checks = append(checks, map[string]any{"name": "backup", "status": "failed", "detail": "backup inventory is unavailable"})
		ready = false
	} else {
		backupCount = len(items)
		status := "warning"
		detail := "create and validate a fresh backup before applying an update"
		if backupCount > 0 {
			status = "passed"
			detail = "at least one checksum-bound backup is retained"
		}
		checks = append(checks, map[string]any{"name": "backup", "status": status, "detail": detail})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": ready, "version": s.version, "profile": s.profile, "backup_count": backupCount, "checks": checks})
}
