package api

import (
	"encoding/json"
	"net/http"

	"github.com/B1-Mordred/CodeMaintainer/internal/golden"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
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
	profiles, err := s.store.ListGoldenComparisonProfiles(r.Context(), jobID, queryInt(r, "limit", 100))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "approvals": approvals, "profiles": profiles})
}

type goldenApprovalRequest struct {
	Reason   string `json:"reason"`
	Approved bool   `json:"approved"`
}

type goldenProfileRequest struct {
	Reason             string   `json:"reason"`
	ToleranceAbsolute  float64  `json:"tolerance_absolute"`
	ToleranceRelative  float64  `json:"tolerance_relative"`
	MaskDynamicRegions bool     `json:"mask_dynamic_regions"`
	MaskSelectors      []string `json:"mask_selectors"`
}

func (s *Server) configureGoldenComparisonProfile(w http.ResponseWriter, r *http.Request) {
	var request goldenProfileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_golden_profile", "golden profile request must be valid JSON")
		return
	}
	reauthenticated := s.auth == nil || recentlyReauthenticated(r)
	if !reauthenticated {
		writeError(w, http.StatusForbidden, "recent_reauthentication_required", "golden profile changes require recent reauthentication")
		return
	}
	role := actorRole(r)
	if role == "operator" {
		role = "reviewer"
	}
	profile, err := s.store.SaveGoldenComparisonProfile(r.Context(), golden.ProfileRequest{
		ReportID: r.PathValue("reportID"), ComparisonID: r.PathValue("comparisonID"),
		ActorID: actorID(r), ActorRole: role, Reason: request.Reason,
		Reauthenticated: reauthenticated,
		Tolerance: golden.ToleranceProfile{
			Mode: "typed-review-profile-v1", Absolute: request.ToleranceAbsolute, Relative: request.ToleranceRelative,
		},
		Mask: golden.MaskProfile{
			Mode: "typed-review-profile-v1", DynamicRegions: request.MaskDynamicRegions,
			Selectors: request.MaskSelectors,
		},
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": profile})
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
	report, err := s.store.GetGoldenReport(r.Context(), approval.ReportID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	approvals, err := s.store.ListGoldenApprovals(r.Context(), report.JobID, 500)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	resolution := golden.ResolveApprovals(report, approvals)
	var refreshed *jobs.Job
	if resolution.Pending == 0 {
		job, jobErr := s.store.GetJob(r.Context(), report.JobID)
		if jobErr != nil {
			s.storageError(w, r, jobErr)
			return
		}
		if job.State == jobs.StateAwaitingGoldenApproval {
			details, _ := json.Marshal(map[string]any{
				"report_id": report.ID, "pending_approvals": resolution.Pending,
			})
			advanced, transitionErr := s.store.TransitionJob(r.Context(), job.ID, jobs.TransitionRequest{
				To: jobs.StateLoadingDocumentationModel, ActorID: actorID(r),
				Reason:          "all required golden or rehearsal updates have explicit approvals",
				ExpectedVersion: job.Version, Details: details,
			})
			if transitionErr != nil {
				s.storageError(w, r, transitionErr)
				return
			}
			refreshed = &advanced
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"approval": approval, "pending_approvals": resolution.Pending,
		"rejected_approvals": resolution.Rejected, "job": refreshed,
	})
}
