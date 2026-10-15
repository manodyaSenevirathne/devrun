package main

// Config is the root of a devrun.yaml file.
type Config struct {
	// Version is the schema version of this config file.
	Version int `yaml:"version"`

	// Vars holds static variables that are expanded (via ${VAR}) in all string
	// fields throughout the config before execution begins.
	Vars map[string]string `yaml:"vars"`

	// Env holds process-level environment variables added to every step's
	// environment, below vars expansion.
	Env map[string]string `yaml:"env"`

	// Commands defines named atomic operations available as top-level devrun
	// subcommands (e.g. devrun build). Each command is a self-contained unit
	// of work and is listed separately from workflows in devrun list.
	Commands map[string]Command `yaml:"commands"`

	// Workflows defines named sequences of WorkflowSteps.
	// Run with: devrun <name>
	Workflows map[string]Workflow `yaml:"workflows"`
}

// Command is a named atomic operation: a single shell invocation with optional
// working directory, environment, env-file loading, input capture, a prompt,
// and a skip condition. Commands are referenced by name from WorkflowStep.command
// or invoked directly as devrun <name>.
//
// Exactly one of Run or EnvFrom should be the primary action; the other fields
// are modifiers applied around it.
//
// Fields are evaluated in this order:
//  1. skip_if  — skip the command entirely if the condition is met
//  2. env_from — load env files into the shared environment state
//  3. prompt   — ask the user for input and store it as an env var
//  4. run      — execute the shell command
//  5. capture  — store stdout of run into an env var for subsequent steps
type Command struct {
	// Description is a short human-readable summary shown in devrun list.
	Description string `yaml:"description"`

	// SkipIf defines a condition that, when true, causes this command to be
	// skipped entirely without error.
	SkipIf *SkipIf `yaml:"skip_if"`

	// EnvFrom loads env vars from one or more files into the shared state
	// before Run executes, making them available to all subsequent steps.
	EnvFrom []EnvSource `yaml:"env_from"`

	// Prompt asks the user for input; the answer is stored as an env var and
	// persists to subsequent steps.
	Prompt *Prompt `yaml:"prompt"`

	// Run is the shell command to execute.
	Run string `yaml:"run"`

	// Workdir is the working directory for Run, relative to the config file.
	Workdir string `yaml:"workdir"`

	// Env holds step-local environment variables merged on top of the
	// accumulated env just for this Run. Supports ${VAR} expansion.
	// These values do NOT persist to subsequent steps.
	Env map[string]string `yaml:"env"`

	// Capture stores the stdout of Run as an env var that persists to all
	// subsequent steps.
	Capture *Capture `yaml:"capture"`
}

// Workflow is a named, ordered sequence of WorkflowSteps.
// Run with: devrun <name>
type Workflow struct {
	// Description is a short human-readable summary shown in devrun list.
	Description string `yaml:"description"`

	// Commands defines workflow-local named commands. These are available only within
	// this workflow and shadow any global command of the same name.
	Commands map[string]Command `yaml:"commands"`

	// Steps is the ordered list of steps that make up this workflow.
	Steps []WorkflowStep `yaml:"steps"`

	// Clean defines the commands run when `devrun clean <workflow>` is called.
	// Typically removes generated files, databases, and caches so the next
	// `devrun <workflow>` starts completely fresh.
	Clean []Command `yaml:"clean"`
}

// WorkflowStep is one unit of work within a workflow. Exactly one of the
// following must be set to specify what this step does:
//
//   - command — the name of a Command defined under config.commands
//   - workflow — the name of a Workflow defined under config.workflows
//   - run (with optional workdir, env, env_from, prompt, skip_if, capture) —
//     an inline shell command, equivalent to an anonymous Command
//
// When workflow is set and spawn is true, the referenced workflow is opened in
// a new terminal window/pane instead of running inline. This is the primary
// mechanism for composing multiple long-running services that each need their
// own terminal.
type WorkflowStep struct {
	// Name is an optional human-readable label for this step, shown during execution.
	Name string `yaml:"name"`

	// Description provides additional context shown in verbose output.
	Description string `yaml:"description"`

	// Command is the name of a top-level Command to execute. Mutually exclusive
	// with workflow and the inline run fields.
	Command string `yaml:"command"`

	// Workflow is the name of a top-level Workflow to execute as a sub-workflow.
	// When spawn is also true, the workflow is opened in a new terminal instead
	// of running inline. Mutually exclusive with command and the inline run fields.
	Workflow string `yaml:"workflow"`

	// Spawn, when true and workflow is set, opens the referenced workflow in a
	// new terminal window or pane (behaviour is platform-dependent) rather than
	// running it in the current process. Has no effect when command is set or
	// when the inline run fields are used.
	Spawn bool `yaml:"spawn"`

	// --- Inline run fields (used when neither command nor workflow is set) ---

	// SkipIf defines a condition that, when true, causes this step to be
	// skipped entirely without error.
	SkipIf *SkipIf `yaml:"skip_if"`

	// EnvFrom loads env vars from one or more files into the shared state
	// before Run executes, making them available to all subsequent steps.
	EnvFrom []EnvSource `yaml:"env_from"`

	// Prompt asks the user for input; the answer is stored as an env var and
	// persists to subsequent steps.
	Prompt *Prompt `yaml:"prompt"`

	// Run is the inline shell command to execute. Ignored when command or
	// workflow is set.
	Run string `yaml:"run"`

	// Workdir is the working directory for Run, relative to the config file.
	// Ignored when command or workflow is set.
	Workdir string `yaml:"workdir"`

	// Env holds step-local environment variables merged on top of the
	// accumulated env just for this Run. Supports ${VAR} expansion.
	// These values do NOT persist to subsequent steps.
	// Ignored when command or workflow is set.
	Env map[string]string `yaml:"env"`

	// Capture stores the stdout of Run as an env var that persists to all
	// subsequent steps. Ignored when command or workflow is set.
	Capture *Capture `yaml:"capture"`
}

// AsCommand converts an inline WorkflowStep into a Command so the same
// runCommand path handles both named commands and anonymous inline steps.
func (s WorkflowStep) AsCommand() Command {
	return Command{
		Description: s.Description,
		Run:         s.Run,
		Workdir:     s.Workdir,
		Env:         s.Env,
		EnvFrom:     s.EnvFrom,
		Capture:     s.Capture,
		Prompt:      s.Prompt,
		SkipIf:      s.SkipIf,
	}
}

// SkipIf defines conditions under which a command or step is skipped.
// At most one of Exists, Cmd, or Env should be set.
type SkipIf struct {
	// Exists: skip if this file or directory exists (relative to config dir).
	Exists string `yaml:"exists"`
	// Cmd: skip if this shell command exits with code 0.
	Cmd string `yaml:"cmd"`
	// Env: skip if this environment variable is already set and non-empty.
	Env string `yaml:"env"`
}

// EnvSource loads env vars from a file into the shared state,
// making them available to all subsequent steps.
type EnvSource struct {
	// File is the path to the env file, relative to the config dir.
	File string `yaml:"file"`
	// Name: when set, the entire file content becomes this one env var.
	// When empty, the file is parsed as KEY=VALUE (dotenv) lines.
	Name string `yaml:"name"`
	// Trim controls whether leading/trailing whitespace is stripped from
	// the loaded value (only meaningful when Name is set).
	Trim bool `yaml:"trim"`
}

// Prompt asks the user for input at runtime and stores the result as an env var.
type Prompt struct {
	// Var is the env var name where the user's answer is stored.
	Var string `yaml:"var"`
	// Message is the text shown to the user before they type.
	Message string `yaml:"message"`
	// Secret: if true, input is hidden (password-style entry).
	Secret bool `yaml:"secret"`
	// Default is the value used if the user presses Enter without typing.
	Default string `yaml:"default"`
}

// Capture stores a command's stdout as an env var accessible to all subsequent
// steps.
type Capture struct {
	// Var is the env var name where the captured output is stored.
	Var string `yaml:"var"`
	// Trim controls whether leading/trailing whitespace is stripped from the
	// captured output before storing.
	Trim bool `yaml:"trim"`
}