package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/intelligence"
)

const maxRequestBytes = 4 << 20

func main() {
	listen := strings.TrimSpace(os.Getenv("CODE_INTELLIGENCE_LISTEN"))
	if listen == "" {
		listen = "0.0.0.0:8090"
	}
	analyzer := intelligence.SyntaxAnalyzer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		var request intelligence.RemoteAnalysisRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes+1))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || request.Path == "" || len(request.Path) > 1024 || path.Clean(request.Path) != request.Path || strings.HasPrefix(request.Path, "../") || strings.HasPrefix(request.Path, "/") || strings.ContainsRune(request.Path, 0) || intelligence.ValidateIdentity(request.ParserID) != nil || len(request.Content) > 2<<20 {
			http.Error(w, "invalid bounded analysis request", http.StatusBadRequest)
			return
		}
		digest := sha256.Sum256(request.Content)
		if request.ContentSHA256 != hex.EncodeToString(digest[:]) {
			http.Error(w, "content identity mismatch", http.StatusUnprocessableEntity)
			return
		}
		analysis := analyzer.Analyze(r.Context(), request.Path, request.ParserID, request.Content)
		analysis.BlobSHA256 = request.ContentSHA256
		analysis.Bytes = int64(len(request.Content))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(analysis)
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 25 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	log.Printf("code-intelligence listening on %s", listen)
	log.Fatal(server.ListenAndServe())
}
