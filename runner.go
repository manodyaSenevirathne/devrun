package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// Runner executes workflows and commands against a shared State.
type Runner struct {
	configDir string
	state     *State
	visited   map[string]bool // guards against circular workflow references
}

func newRunner(configDir string, state *State) *Runner {
	return &Runner{
		configDir: configDir,
		state:     state,
		visited:   make(map[string]bool),
	}
}

// RunWorkflow executes a named workflow, resolving extends first, then
// dispatching each WorkflowStep according to the 3-tier resolution order:
//  1. step.Command != "" -> look up localCmds first, then cfg.Commands[name], run inline via runCommand()
//  2. step.Workflow != "" && step.Spawn -> spawnInTerminal(name, configDir)
//  3. step.Workflow != "" && !step.Spawn -> RunWorkflow(name, cfg) recursively
//  4. else -> treat step fields as an inline Command and run via runCommand()
func (r *Runner) RunWorkflow(name string, cfg *Config) error {
	wf, ok := cfg.Workflows[name]
	if !ok {
		return fmt.Errorf("workflow %q not found", name)
	}
	if r.visited[name] {
		return fmt.Errorf("circular workflow reference detected at %q", name)
	}
	r.visited[name] = true

	label := wf.Description
	if label == "" {
		label = name
	}
	fmt.Printf("\n▶  %s\n%s\n", label, strings.Repeat("─", 52))

	localCmds := wf.Commands

	total := len(wf.Steps)
	for i, step := range wf.Steps {
		stepLabel := step.Name
		if step.Description != "" {
			stepLabel = step.Description
		}
		fmt.Printf("\n[%d/%d] %s\n", i+1, total, stepLabel)

		if err := r.dispatchStep(step, cfg, localCmds); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
	}
	return nil
}

// dispatchStep resolves and executes a single WorkflowStep.
// Command resolution order:
//  1. localCmds[step.Command] — workflow-local
//  2. cfg.Commands[step.Command] — global
//  3. error if neither found
func (r *Runner) dispatchStep(step WorkflowStep, cfg *Config, localCmds map[string]Command) error {
	// Tier 1: named command reference
	if step.Command != "" {
		if cmd, ok := localCmds[step.Command]; ok {
			return r.runCommand(cmd)
		}
		if cmd, ok := cfg.Commands[step.Command]; ok {
			return r.runCommand(cmd)
		}
		return fmt.Errorf("command %q not found", step.Command)
	}

	// Tier 2 & 3: named workflow reference
	if step.Workflow != "" {
		if step.Spawn {
			fmt.Printf("      ↗  spawning workflow %q in new terminal\n", step.Workflow)
			if err := spawnInTerminal(step.Workflow, r.configDir); err != nil {
				return fmt.Errorf("spawn %q: %w", step.Workflow, err)
			}
			return nil
		}
		return r.RunWorkflow(step.Workflow, cfg)
	}

	// Tier 4: inline command — treat step fields as a Command
	return r.runCommand(step.AsCommand())
}

// RunCommand is the public entry point for running a single named global command.
func (r *Runner) RunCommand(cmd Command) error {
	label := cmd.Description
	if label == "" {
		label = cmd.Run
	}
	fmt.Printf("\n▶  %s\n%s\n", label, strings.Repeat("─", 52))
	return r.runCommand(cmd)
}

// runCommand executes a Command: processes skip_if, env_from (persisted to
// state), prompt (persisted), run, capture (persisted), and env (local only).
func (r *Runner) runCommand(cmd Command) error {
	// ── skip_if ───────────────────────────────────────────────────────────────
	if cmd.SkipIf != nil {
		skip, reason, err := r.evalSkipIf(cmd.SkipIf)
		if err != nil {
			return fmt.Errorf("skip_if: %w", err)
		}
		if skip {
			fmt.Printf("      ↷  skipped (%s)\n", reason)
			return nil
		}
	}

	// ── env_from (persisted to state) ─────────────────────────────────────────
	for _, src := range cmd.EnvFrom {
		if err := r.applyEnvFrom(src); err != nil {
			return fmt.Errorf("env_from %s: %w", src.File, err)
		}
	}

	// ── prompt (persisted to state) ───────────────────────────────────────────
	if cmd.Prompt != nil {
		if err := r.runPrompt(cmd.Prompt); err != nil {
			return err
		}
	}

	// ── run ───────────────────────────────────────────────────────────────────
	if cmd.Run == "" {
		return nil
	}

	workdir := r.configDir
	if cmd.Workdir != "" {
		workdir = filepath.Join(r.configDir, cmd.Workdir)
	}

	// Local env: merged on top of state snapshot, NOT persisted back to state
	env := r.state.Environ()
	if len(cmd.Env) > 0 {
		keys := make([]string, 0, len(cmd.Env))
		for k, v := range cmd.Env {
			val := r.state.Expand(v)
			prefix := k + "="
			replaced := false
			for i, kv := range env {
				if strings.HasPrefix(kv, prefix) {
					env[i] = prefix + val
					replaced = true
					break
				}
			}
			if !replaced {
				env = append(env, prefix+val)
			}
			keys = append(keys, k+"="+val)
		}
		for _, kv := range keys {
			fmt.Printf("      env  %s\n", kv)
		}
	}

	expandedRun := r.state.Expand(cmd.Run)
	c := shellCmd(expandedRun)
	c.Dir = workdir
	c.Env = env

	if cmd.Workdir != "" {
		fmt.Printf("      dir  ./%s\n", cmd.Workdir)
	}
	// Always show the command so the user can see what's executing,
	// but use a dimmer prefix when a description already names the intent.
	if cmd.Description != "" {
		fmt.Printf("      $    %s\n", expandedRun)
	} else {
		fmt.Printf("      run  %s\n", expandedRun)
	}

	// ── capture (persisted to state) ──────────────────────────────────────────
	if cmd.Capture != nil {
		c.Stderr = os.Stderr
		out, err := c.Output()
		if err != nil {
			return err
		}
		val := string(out)
		if cmd.Capture.Trim {
			val = strings.TrimSpace(val)
		}
		r.state.Set(cmd.Capture.Var, val)
		fmt.Printf("      ✓  captured → $%s\n", cmd.Capture.Var)
		return nil
	}

	// Normal mode: full terminal I/O
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// RunClean iterates workflow.Clean []Command and calls runCommand for each.
func (r *Runner) RunClean(name string, cfg *Config) error {
	wf, ok := cfg.Workflows[name]
	if !ok {
		return fmt.Errorf("workflow %q not found", name)
	}
	if len(wf.Clean) == 0 {
		fmt.Printf("  (no clean steps defined for %q)\n", name)
		return nil
	}
	label := wf.Description
	if label == "" {
		label = name
	}
	fmt.Printf("\n✦  clean: %s\n%s\n", label, strings.Repeat("─", 52))
	total := len(wf.Clean)
	for i, cmd := range wf.Clean {
		cleanLabel := cmd.Description
		if cleanLabel == "" {
			cleanLabel = fmt.Sprintf("clean step %d", i+1)
		}
		fmt.Printf("\n[%d/%d] %s\n", i+1, total, cleanLabel)
		if err := r.runCommand(cmd); err != nil {
			return fmt.Errorf("clean step %d: %w", i+1, err)
		}
	}
	return nil
}

func (r *Runner) evalSkipIf(s *SkipIf) (skip bool, reason string, err error) {
	if s.Exists != "" {
		path := r.abs(s.Exists)
		if _, e := os.Stat(path); e == nil {
			return true, fmt.Sprintf("exists: %s", s.Exists), nil
		}
	}
	if s.Env != "" {
		if val := r.state.Get(s.Env); val != "" {
			return true, fmt.Sprintf("$%s is set", s.Env), nil
		}
	}
	if s.Cmd != "" {
		expanded := r.state.Expand(s.Cmd)
		cmd := shellCmd(expanded)
		cmd.Env = r.state.Environ()
		cmd.Dir = r.configDir
		if e := cmd.Run(); e == nil {
			return true, fmt.Sprintf("cmd passed: %s", s.Cmd), nil
		}
	}
	return false, "", nil
}

func (r *Runner) applyEnvFrom(src EnvSource) error {
	data, err := os.ReadFile(r.abs(src.File))
	if err != nil {
		return err
	}
	content := string(data)

	if src.Name != "" {
		val := content
		if src.Trim {
			val = strings.TrimSpace(val)
		}
		r.state.Set(src.Name, val)
		fmt.Printf("      ✓  $%s ← %s\n", src.Name, src.File)
		return nil
	}

	keys := r.state.LoadDotenv(content)
	if len(keys) > 0 {
		kvs := make([]string, len(keys))
		for i, k := range keys {
			kvs[i] = "$" + k
		}
		fmt.Printf("      ✓  %s ← %s\n", strings.Join(kvs, ", "), src.File)
	}
	return nil
}

func (r *Runner) runPrompt(p *Prompt) error {
	msg := r.state.Expand(p.Message)
	if msg == "" {
		msg = p.Var
	}
	def := r.state.Expand(p.Default)
	if def != "" {
		fmt.Printf("      ?  %s [%s]: ", msg, def)
	} else {
		fmt.Printf("      ?  %s: ", msg)
	}

	var val string
	if p.Secret {
		b, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("reading password: %w", err)
		}
		val = string(b)
	} else {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		val = scanner.Text()
	}
	if val == "" {
		val = def
	}
	r.state.Set(p.Var, val)
	fmt.Printf("      ✓  $%s set\n", p.Var)
	return nil
}

func (r *Runner) abs(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.configDir, path)
}

