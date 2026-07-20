package verification

import "fmt"

type Registry struct {
	commands map[Language]map[Class]Command
}

func NewRegistry() *Registry {
	registry := &Registry{commands: make(map[Language]map[Class]Command)}
	registry.add(LanguageGo,
		Command{Class: ClassFormat, Executable: "maintainer-worker", Arguments: []string{"verify", "go-format"}},
		Command{Class: ClassCompile, Executable: "go", Arguments: []string{"test", "-run=^$", "./..."}},
		Command{Class: ClassLint, Executable: "go", Arguments: []string{"vet", "./..."}},
		Command{Class: ClassTargetedTests, Executable: "go", Arguments: []string{"test", "./..."}},
		Command{Class: ClassFullTests, Executable: "go", Arguments: []string{"test", "./..."}},
		Command{Class: ClassFuzz, Executable: "go", Arguments: []string{"test", "-run=^$", "-fuzz=.", "-fuzztime=10s", "./..."}},
	)
	registry.add(LanguagePython,
		Command{Class: ClassCompile, Executable: "python3", Arguments: []string{"-m", "compileall", "-q", "."}},
		Command{Class: ClassLint, Executable: "python3", Arguments: []string{"-m", "flake8", "."}},
		Command{Class: ClassTargetedTests, Executable: "python3", Arguments: []string{"-m", "pytest", "-q"}},
		Command{Class: ClassFullTests, Executable: "python3", Arguments: []string{"-m", "pytest"}},
		Command{Class: ClassPropertyTests, Executable: "python3", Arguments: []string{"-m", "pytest", "-m", "property"}},
	)
	registry.add(LanguageNode,
		Command{Class: ClassCompile, Executable: "npm", Arguments: []string{"run", "build", "--if-present"}},
		Command{Class: ClassLint, Executable: "npm", Arguments: []string{"run", "lint", "--if-present"}},
		Command{Class: ClassTargetedTests, Executable: "npm", Arguments: []string{"test", "--", "--run"}},
		Command{Class: ClassFullTests, Executable: "npm", Arguments: []string{"test"}},
	)
	registry.add(LanguageRust,
		Command{Class: ClassFormat, Executable: "cargo", Arguments: []string{"fmt", "--all", "--", "--check"}},
		Command{Class: ClassCompile, Executable: "cargo", Arguments: []string{"check", "--all-targets", "--locked"}},
		Command{Class: ClassLint, Executable: "cargo", Arguments: []string{"clippy", "--all-targets", "--locked", "--", "-D", "warnings"}},
		Command{Class: ClassTargetedTests, Executable: "cargo", Arguments: []string{"test", "--locked"}},
		Command{Class: ClassFullTests, Executable: "cargo", Arguments: []string{"test", "--all-targets", "--locked"}},
	)
	registry.add(LanguageC,
		Command{Class: ClassCompile, Executable: "cmake", Arguments: []string{"--build", "build", "--parallel"}},
		Command{Class: ClassTargetedTests, Executable: "ctest", Arguments: []string{"--test-dir", "build", "--output-on-failure"}},
		Command{Class: ClassFullTests, Executable: "ctest", Arguments: []string{"--test-dir", "build", "--output-on-failure"}},
	)
	registry.add(LanguageCPP,
		Command{Class: ClassCompile, Executable: "cmake", Arguments: []string{"--build", "build", "--parallel"}},
		Command{Class: ClassTargetedTests, Executable: "ctest", Arguments: []string{"--test-dir", "build", "--output-on-failure"}},
		Command{Class: ClassFullTests, Executable: "ctest", Arguments: []string{"--test-dir", "build", "--output-on-failure"}},
	)
	registry.add(LanguageBase,
		Command{Class: ClassSecretScan, Executable: "maintainer-worker", Arguments: []string{"scan", "secrets"}},
		Command{Class: ClassDependencyScan, Executable: "maintainer-worker", Arguments: []string{"scan", "dependencies"}},
		Command{Class: ClassDiffPolicy, Executable: "maintainer-worker", Arguments: []string{"scan", "diff-policy"}},
	)
	registry.commands[LanguageFull] = mergeCommands(registry.commands)
	return registry
}

func (r *Registry) Resolve(language Language, class Class) (Command, error) {
	profile, exists := r.commands[language]
	if !exists {
		return Command{}, fmt.Errorf("%w: unknown language %q", ErrUnknownClass, language)
	}
	command, exists := profile[class]
	if !exists {
		return Command{}, fmt.Errorf("%w: %s/%s", ErrUnknownClass, language, class)
	}
	command.Arguments = append([]string(nil), command.Arguments...)
	return command, nil
}

func (r *Registry) add(language Language, commands ...Command) {
	profile := make(map[Class]Command, len(commands))
	for _, command := range commands {
		profile[command.Class] = command
	}
	r.commands[language] = profile
}

func mergeCommands(profiles map[Language]map[Class]Command) map[Class]Command {
	result := make(map[Class]Command)
	for _, language := range []Language{LanguageBase, LanguagePython, LanguageNode, LanguageC, LanguageCPP, LanguageRust, LanguageGo} {
		for class, command := range profiles[language] {
			if _, exists := result[class]; !exists {
				result[class] = command
			}
		}
	}
	return result
}
