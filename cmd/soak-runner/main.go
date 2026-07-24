package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/soak"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("soak-runner", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	output := flags.String("output", "", "write retained JSON report to this path")
	iterations := flags.Int("iterations", soak.DefaultInput().Iterations, "bounded local-fake iterations")
	generatedAt := flags.String("generated-at", "", "RFC3339 timestamp for deterministic report generation")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	input := soak.Input{Iterations: *iterations}
	if *generatedAt != "" {
		parsed, err := time.Parse(time.RFC3339, *generatedAt)
		if err != nil {
			return fmt.Errorf("parse --generated-at: %w", err)
		}
		input.GeneratedAt = parsed.UTC()
	}
	report, err := soak.Run(context.Background(), input)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(*output, encoded, 0o640); err != nil {
			return err
		}
	}
	_, err = stdout.Write(encoded)
	return err
}
