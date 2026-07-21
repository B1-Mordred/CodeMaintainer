package projects

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	repository = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)
)

type Project struct {
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	Repository      string    `json:"repository"`
	DefaultBranch   string    `json:"default_branch"`
	LocalRemoteName string    `json:"local_remote_name,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type UpsertRequest struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	Repository      string `json:"repository"`
	DefaultBranch   string `json:"default_branch"`
	LocalRemoteName string `json:"local_remote_name,omitempty"`
}

func (r UpsertRequest) Validate() error {
	if !identifier.MatchString(r.ID) || !repository.MatchString(r.Repository) ||
		!identifier.MatchString(r.DefaultBranch) || (r.Provider != "local" && r.Provider != "github" && r.Provider != "gitlab") {
		return errors.New("project identity, repository, branch, or provider is invalid")
	}
	if r.Provider == "local" {
		if !identifier.MatchString(r.LocalRemoteName) || !strings.HasSuffix(r.LocalRemoteName, ".git") {
			return errors.New("local projects require a safe .git remote name")
		}
	} else if r.LocalRemoteName != "" {
		return errors.New("hosted forge projects cannot select a local remote")
	}
	return nil
}

func ValidID(value string) bool { return identifier.MatchString(value) }
