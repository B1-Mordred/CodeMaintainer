package runners

import (
	"context"
	"errors"
	"testing"
)

func TestFakeAcceptsOnlyServerRecognizedKinds(t *testing.T) {
	runner := NewFake()
	if _, err := runner.Start(context.Background(), JobRequest{JobID: "job", ProjectID: "project", Kind: Kind("docker-run")}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown execution kind was accepted: %v", err)
	}
	id, err := runner.Start(context.Background(), JobRequest{JobID: "job", ProjectID: "project", Kind: KindImplementation})
	if err != nil {
		t.Fatal(err)
	}
	status, err := runner.Inspect(context.Background(), id)
	if err != nil || status.Kind != KindImplementation {
		t.Fatalf("inspect returned %#v, %v", status, err)
	}
}

func TestFakeLogsAreBounded(t *testing.T) {
	runner := NewFake()
	id, _ := runner.Start(context.Background(), JobRequest{JobID: "job", ProjectID: "project", Kind: KindQC})
	chunk, err := runner.Logs(context.Background(), id, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.Data) > 4 || !chunk.Truncated {
		t.Fatalf("unbounded log chunk: %#v", chunk)
	}
}
