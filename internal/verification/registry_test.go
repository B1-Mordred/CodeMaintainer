package verification

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRegistryNeverAcceptsCallerCommands(t *testing.T) {
	registry := NewRegistry()
	command, err := registry.Resolve(LanguageGo, ClassFullTests)
	if err != nil {
		t.Fatal(err)
	}
	if command.Executable != "go" || strings.Join(command.Arguments, " ") != "test ./..." {
		t.Fatalf("unexpected fixed command: %#v", command)
	}
	if _, err := registry.Resolve(Language("docker"), Class("sh -c attacker")); err == nil {
		t.Fatal("caller-defined language/class was accepted")
	}
	command.Arguments[0] = "attacker"
	again, _ := registry.Resolve(LanguageGo, ClassFullTests)
	if again.Arguments[0] != "test" {
		t.Fatal("caller mutated registry arguments")
	}
}

func TestLocalExecutorBoundsAndRedactsOutput(t *testing.T) {
	result := (LocalExecutor{}).Run(context.Background(), t.TempDir(), Command{
		Class: ClassFullTests, Executable: "printf",
		Arguments: []string{"ghp_1234567890abcdefghijklmnop extra-output"},
	}, time.Second, 32)
	if result.ExitCode != 0 || !result.Truncated || strings.Contains(result.Output, "ghp_") {
		t.Fatalf("unsafe execution result: %#v", result)
	}
}
