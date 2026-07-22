package windowsworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedProtocolCarriesOnlyClosedImmutableRequests(t *testing.T) {
	profile := simulatorProfile()
	token := []byte("0123456789abcdef0123456789abcdef")
	handler, err := NewProtocolHandler(token, []Profile{profile}, &fixtureAdapter{now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := client.ProbeWindowsWorker(context.Background(), profile)
	if err != nil || !probe.Ready || probe.ProfileID != profile.ID {
		t.Fatalf("probe = %#v err=%v", probe, err)
	}
	request := validRun(profile.ID, JobDotNet)
	result, err := client.RunWindowsJob(context.Background(), profile, request)
	if err != nil || result.InputSHA256 != InputSHA(request) || len(result.Checks) != 3 {
		t.Fatalf("run = %#v err=%v", result, err)
	}

	unauthorized, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/run", strings.NewReader(`{}`))
	unauthorized.Header.Set("Authorization", "Bearer wrong-token-with-at-least-thirty-two")
	response, err := server.Client().Do(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}

	malicious := `{"profile_id":"windows-sim","project_id":"project-one","job_id":"job-one","job_type":"dotnet_restore_build_test","idempotency_key":"malicious","operator_gated":false,"input":{"repository_sha":"` + strings.Repeat("a", 40) + `","capability_pack_checksum":"` + strings.Repeat("b", 64) + `","toolchain_inventory_checksum":"` + strings.Repeat("c", 64) + `","source_artifact_id":"source-one"},"command":"powershell -EncodedCommand bad"}`
	requestHTTP, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/run", strings.NewReader(malicious))
	requestHTTP.Header.Set("Authorization", "Bearer "+string(token))
	requestHTTP.Header.Set("Content-Type", "application/json")
	response, err = server.Client().Do(requestHTTP)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown command field status = %d", response.StatusCode)
	}
}
