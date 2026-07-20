package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type increment2Coverage struct {
	SchemaVersion   int      `json:"schema_version"`
	Contract        string   `json:"contract"`
	BaseContract    string   `json:"base_contract"`
	AllowedStatuses []string `json:"allowed_statuses"`
	Requirements    []struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Milestone      int      `json:"milestone"`
		Status         string   `json:"status"`
		Implementation []string `json:"implementation"`
		Tests          []string `json:"tests"`
		UI             []string `json:"ui"`
		Docs           []string `json:"docs"`
	} `json:"requirements"`
}

func TestIncrement2CoverageInventoryIsWellFormed(t *testing.T) {
	root := filepath.Join("..", "..")
	payload, err := os.ReadFile(filepath.Join(root, "config", "increment-2-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory increment2Coverage
	if err := json.Unmarshal(payload, &inventory); err != nil {
		t.Fatalf("decode Increment 2 coverage inventory: %v", err)
	}
	if inventory.SchemaVersion != 1 || inventory.Contract != "LOCAL_CODE_MAINTAINER_INCREMENT_2_GOAL.md" || inventory.BaseContract != "project.md" {
		t.Fatalf("unexpected inventory identity: version=%d contract=%q base=%q", inventory.SchemaVersion, inventory.Contract, inventory.BaseContract)
	}
	allowed := make(map[string]bool, len(inventory.AllowedStatuses))
	for _, status := range inventory.AllowedStatuses {
		allowed[status] = true
	}
	wantStatuses := []string{"missing", "in_progress", "implemented", "operator_only"}
	for _, status := range wantStatuses {
		if !allowed[status] {
			t.Errorf("allowed_statuses omits %q", status)
		}
	}

	seen := make(map[string]bool, len(inventory.Requirements))
	for index, requirement := range inventory.Requirements {
		if strings.TrimSpace(requirement.ID) == "" || strings.TrimSpace(requirement.Title) == "" {
			t.Errorf("requirement %d has an empty id or title", index)
		}
		if seen[requirement.ID] {
			t.Errorf("duplicate requirement id %q", requirement.ID)
		}
		seen[requirement.ID] = true
		if requirement.Milestone < 1 || requirement.Milestone > 7 {
			t.Errorf("%s has invalid milestone %d", requirement.ID, requirement.Milestone)
		}
		if !allowed[requirement.Status] {
			t.Errorf("%s has invalid status %q", requirement.ID, requirement.Status)
		}
		if requirement.Status == "implemented" {
			if len(requirement.Implementation) == 0 || len(requirement.Tests) == 0 || len(requirement.UI) == 0 || len(requirement.Docs) == 0 {
				t.Errorf("%s claims implementation without implementation, test, UI, and documentation evidence", requirement.ID)
			}
		}
	}

	requiredIDs := []string{
		"I2-05.1", "I2-05.2", "I2-05.3", "I2-05.4", "I2-05.5",
		"I2-06", "I2-07", "I2-08", "I2-09", "I2-10", "I2-11", "I2-12",
		"I2-13", "I2-14", "I2-15", "I2-16", "I2-17", "I2-18", "I2-19",
		"I2-20", "I2-21", "I2-22.1", "I2-22.2", "I2-22.3", "I2-22.4",
		"I2-22.5", "I2-22.6", "I2-22.7", "I2-23", "I2-24", "I2-25",
		"I2-26", "I2-27", "I2-28", "I2-31.1", "I2-31.2", "I2-31.3",
		"I2-31.4", "I2-31.5", "I2-32",
	}
	for index := 1; index <= 27; index++ {
		requiredIDs = append(requiredIDs, fmt.Sprintf("I2-DOD-%02d", index))
	}
	for _, id := range requiredIDs {
		if !seen[id] {
			t.Errorf("coverage inventory omits required row %s", id)
		}
	}
}
