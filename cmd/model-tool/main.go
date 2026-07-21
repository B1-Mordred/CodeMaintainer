package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/models"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "model-tool:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) < 3 {
		return errors.New("usage: model-tool <list|verify|import|download> MANIFEST_DIR MODEL_ROOT [PROFILE_ID] [SOURCE_PATH]")
	}
	command, manifestRoot, modelRoot := arguments[0], arguments[1], arguments[2]
	catalog, err := models.LoadCatalog(manifestRoot)
	if err != nil {
		return err
	}
	if command == "list" && len(arguments) == 3 {
		return json.NewEncoder(os.Stdout).Encode(catalog.Profiles())
	}
	if len(arguments) < 4 {
		return errors.New("profile ID is required")
	}
	manifest, ok := catalog.Manifest(arguments[3])
	if !ok {
		return errors.New("profile is not allow-listed")
	}
	switch command {
	case "verify":
		if len(arguments) != 4 {
			return errors.New("verify accepts no additional arguments")
		}
		path, err := models.VerifyInstalledModel(modelRoot, manifest)
		if err == nil {
			fmt.Println(path)
		}
		return err
	case "import":
		if len(arguments) != 5 {
			return errors.New("import requires one absolute source path")
		}
		path, err := models.ImportModel(arguments[4], modelRoot, manifest)
		if err == nil {
			fmt.Println(path)
		}
		return err
	case "download":
		if len(arguments) != 4 {
			return errors.New("download source is fixed by the manifest")
		}
		client := &http.Client{
			Timeout:   24 * time.Hour,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				parsed, err := url.Parse(request.URL.String())
				if err != nil || parsed.Scheme != "https" || parsed.User != nil || len(via) > 5 {
					return errors.New("unsafe model download redirect")
				}
				return nil
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()
		path, err := models.DownloadModel(ctx, client, modelRoot, manifest)
		if err == nil {
			fmt.Println(path)
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", strings.TrimSpace(command))
	}
}
