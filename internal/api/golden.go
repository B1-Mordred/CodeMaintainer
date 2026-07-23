package api

import (
	"encoding/json"
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/golden"
)

func (s *Server) listJobGoldenReports(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	if _, err := s.store.GetJob(r.Context(), jobID); err != nil {
		s.storageError(w, r, err)
		return
	}
	reports, err := s.store.ListGoldenReports(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	approvals, err := s.store.ListGoldenApprovals(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "approvals": approvals})
}

type goldenApprovalRequest struct {
	Reason   string `json:"reason"`
	Approved bool   `json:"approved"`
}

func (s *Server) approveGoldenUpdate(w http.ResponseWriter, r *http.Request) {
	var request goldenApprovalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_golden_approval", "approval request must be valid JSON")
		return
	}
	reauthenticated := s.auth == nil || recentlyReauthenticated(r)
	if !reauthenticated {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "golden update approval requires recent reauthentication")
		return
	}
	approval, err := s.store.ApproveGoldenUpdate(r.Context(), golden.ApprovalRequest{
		ReportID: r.PathValue("reportID"), ComparisonID: r.PathValue("comparisonID"),
		ActorID: actorID(r), ActorRole: actorRole(r), Reason: request.Reason,
		Approved: request.Approved, Reauthenticated: reauthenticated,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, approval)
}
