package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type SmokeResult struct {
	Status     string        `json:"status"`
	Namespace  string        `json:"namespace"`
	RecordID   string        `json:"record_id"`
	SearchSeen bool          `json:"search_seen"`
	Duration   time.Duration `json:"duration_ns"`
}

func SmokeIndex(ctx context.Context, index Index, scope ProjectScope) (SmokeResult, error) {
	if index == nil || !scope.Valid() {
		return SmokeResult{}, ErrInvalid
	}
	started := time.Now()
	if err := index.Health(ctx); err != nil {
		return SmokeResult{}, err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return SmokeResult{}, err
	}
	id := "memory_" + hex.EncodeToString(random)
	content := "bounded OpenViking smoke marker " + hex.EncodeToString(random)
	digest := sha256.Sum256([]byte(content))
	record := Record{
		ID: id, Scope: scope, Namespace: scope.Namespace(), Content: content,
		ContentHash: hex.EncodeToString(digest[:]), Kind: "pattern", Status: StatusCanonical,
		SecretScanPass: true, SourceURI: "system://openviking/smoke", Version: 1,
	}
	if err := index.Upsert(ctx, record); err != nil {
		return SmokeResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = index.Forget(cleanup, scope, id)
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		matches, err := index.Find(ctx, scope, content, 5)
		if err == nil {
			for _, match := range matches {
				if match.RecordID == id {
					return SmokeResult{Status: "passed", Namespace: scope.Namespace(), RecordID: id, SearchSeen: true, Duration: time.Since(started)}, nil
				}
			}
		} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return SmokeResult{}, err
		}
		select {
		case <-ctx.Done():
			return SmokeResult{}, ctx.Err()
		case <-deadline.C:
			return SmokeResult{}, errors.New("OpenViking smoke record was not searchable within 10 seconds")
		case <-ticker.C:
		}
	}
}
