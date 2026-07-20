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
	if !request.DryRun {
		writeError(w, http.StatusUnprocessableEntity, "offline_restore_required", "state replacement is an offline maintainctl operation; run a dry run before stopping the appliance")
		return
	}
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "backup_unavailable", "backup service is unavailable")
		return
	}
	report, err := s.backups.Validate(r.Context(), r.PathValue("backupID"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "backup_invalid", "backup compatibility or checksums failed validation")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
