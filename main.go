// devrun — generic multi-step workflow runner.
//
// Define named workflows in devrun.yaml; each workflow is a sequence of
// steps that share accumulated state (env vars, captured outputs).
// Commands are building blocks used by workflows; workflows are what you run.
//
// Usage:
//
//	devrun <workflow>    run a named workflow
//	devrun list         list all workflows
//	devrun help         show this message
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	arg := os.Args[1]
	if arg == "help" || arg == "-h" || arg == "--help" {
		printHelp()
		return
	}

	configPath, err := findConfig()
	if err != nil {
		fatalf("error: %v", err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fatalf("error: %v", err)
	}

	absDir, _ := filepath.Abs(filepath.Dir(configPath))

	if arg == "list" {
		listWorkflows(cfg)
		return
	}

	// devrun clean                → run the workflow named "clean" if it exists
	// devrun clean <workflow>     → workflow-specific clean steps
	if arg == "clean" {
		state := buildState(absDir, cfg)
		runner := newRunner(absDir, state)

		if len(os.Args) >= 3 {
			// workflow-specific clean
			wfName := os.Args[2]
			if _, ok := cfg.Workflows[wfName]; !ok {
				fmt.Fprintf(os.Stderr, "error: workflow %q not found\n", wfName)
				os.Exit(1)
			}
			if err := runner.RunClean(wfName, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "\n✗  %v\n", err)
				os.Exit(1)
			}
		} else {
			// run the workflow named "clean"
			if _, ok := cfg.Workflows["clean"]; !ok {
				fmt.Fprintln(os.Stderr, "error: no workflow named \"clean\" defined")
				fmt.Fprintln(os.Stderr, "       use 'devrun clean <workflow>' for per-workflow clean steps")
				os.Exit(1)
			}
			if err := runner.RunWorkflow("clean", cfg); err != nil {
				fmt.Fprintf(os.Stderr, "\n✗  %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("\n✓  cleaned\n")
		return
	}

	runner := newRunner(absDir, buildState(absDir, cfg))

	// Workflows take priority; fall back to global commands.
	if _, ok := cfg.Workflows[arg]; ok {
		if err := runner.RunWorkflow(arg, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "\n✗  %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n✓  done\n")
		return
	}

	if cmd, ok := cfg.Commands[arg]; ok {
		if err := runner.RunCommand(cmd); err != nil {
			fmt.Fprintf(os.Stderr, "\n✗  %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n✓  done\n")
		return
	}

	fmt.Fprintf(os.Stderr, "error: %q not found\n\n", arg)
	listWorkflows(cfg)
	os.Exit(1)
}

func buildState(absDir string, cfg *Config) *State {
	state := newState(nil)
	state.Set("DEVRUN_ROOT", absDir)
	// Expand each var as it is set so later vars can reference earlier ones
	// (and all can reference $DEVRUN_ROOT which is already in state).
	for k, v := range cfg.Vars {
		state.Set(k, state.Expand(v))
	}
	for k, v := range cfg.Env {
		state.Set(k, state.Expand(v))
	}
	return state
}

// ── Config loading ────────────────────────────────────────────────────────────

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// findConfig walks up from cwd looking for devrun.yaml or .devrun.yaml.
func findConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		for _, name := range []string{"devrun.yaml", ".devrun.yaml"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("devrun.yaml not found (searched from %s upward)", dir)
}

// ── Output helpers ────────────────────────────────────────────────────────────

func listWorkflows(cfg *Config) {
	if len(cfg.Commands) > 0 {
		names := make([]string, 0, len(cfg.Commands))
		for n := range cfg.Commands {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Println("Commands:")
		for _, n := range names {
			desc := cfg.Commands[n].Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("  %-26s %s\n", n, desc)
		}
		fmt.Println()
	}

	if len(cfg.Workflows) > 0 {
		names := make([]string, 0, len(cfg.Workflows))
		for n := range cfg.Workflows {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Println("Workflows:")
		for _, n := range names {
			desc := cfg.Workflows[n].Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("  %-26s %s\n", n, desc)
		}
	}
}

func printHelp() {
	fmt.Print(`devrun — generic multi-step workflow runner

Commands are building blocks; workflows are what you run.

Usage:
  devrun <workflow>           run a named workflow
  devrun clean                run the workflow named "clean"
  devrun clean <workflow>     run the clean steps for one workflow
  devrun list                 list all available workflows
  devrun help                 show this message

Config file: devrun.yaml (or .devrun.yaml) in the current or any parent directory.

Step features:
  run          shell command to execute
  env_from     load KEY=VALUE from a file into shared state
  capture      store stdout of run into a state variable
  prompt       ask the user for input, store in state
  parallel     run sub-steps concurrently
  skip_if      skip the step if a condition is true
    exists     file or directory exists
    cmd        shell command exits 0
    env        environment variable is already set
  extends      run another workflow's steps first

Variables:
  $DEVRUN_ROOT    absolute path to the directory containing devrun.yaml
  ${MY_VAR}       any variable set via vars:, env:, env_from, capture, or prompt
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}