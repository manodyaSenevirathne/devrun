# devrun

A generic multi-step workflow runner for development environments. Define your project's setup and start commands in `devrun.yaml`, then run everything with a single command — regardless of how many terminals, steps, or env files are involved.

---

## Install

```bash
go install github.com/wso2/devrun@latest
```

Or build from source:

```bash
git clone https://github.com/wso2/devrun
cd devrun
go build -o devrun .
```

---

## How it works

Three tiers, each building on the last:

```
commands:    named atomic operations  (run, env_from, capture, prompt, skip_if)
workflows:   sequences of commands    (reference commands by name, or inline)
             workflows can reference other workflows — with spawn: true to open in a new terminal
```

Everything is driven by a `devrun.yaml` (or `.devrun.yaml`) placed in your project root. Run `devrun` from anywhere inside the project — it walks up to find the config.

---

## Quick start

```bash
# 1. Copy the sample config and edit it
cp sample.devrun.yaml devrun.yaml

# 2. See what's available
devrun list

# 3. Run a workflow
devrun setup
devrun dev
```

---

## Config reference

### Top level

```yaml
version: 1

vars:                        # expanded as ${VAR} everywhere in this file
  API_URL: "http://localhost:3000"
  ROOT: "${DEVRUN_ROOT}"     # DEVRUN_ROOT is always set to the config file's directory

env:                         # added to the process env for every step
  NODE_ENV: "development"

commands:                    # named atomic operations
  ...

workflows:                   # named sequences
  ...
```

### Commands

```yaml
commands:
  my-command:
    description: "What this does"

    # Shell command to execute
    run: go build ./...
    workdir: backend          # relative to devrun.yaml location

    # Step-local env — NOT shared with subsequent steps
    env:
      CGO_ENABLED: "0"

    # Load env from files into SHARED state (persists to all subsequent steps)
    env_from:
      - file: .env.local            # dotenv format: KEY=VALUE lines
      - name: DB_PASSWORD           # single variable from file content
        file: secrets/db.password
        trim: true

    # Capture stdout into shared state
    capture:
      var: GIT_SHA
      trim: true

    # Ask user for input, store in shared state
    prompt:
      var: DEPLOY_ENV
      message: "Deploy to (staging/prod)"
      default: staging
      secret: false           # set true to hide input (passwords)

    # Skip this command if condition is true
    skip_if:
      exists: certs/server.crt         # file or directory exists
      cmd: ./scripts/check-done.sh     # shell command exits 0
      env: ALREADY_SETUP               # env var is set and non-empty
```

### Workflows

```yaml
workflows:
  my-workflow:
    description: "What this workflow does"

    # Workflow-local commands — only visible inside this workflow,
    # shadow any global command with the same name
    commands:
      local-helper:
        run: ./scripts/helper.sh

    steps:
      # Reference a named command (local first, then global)
      - command: my-command

      # Reference another workflow — runs inline
      - workflow: another-workflow

      # Reference another workflow — opens in a NEW terminal
      - workflow: another-workflow
        spawn: true

      # Inline anonymous command (same fields as a named command)
      - name: optional-label
        description: "Shown during execution"
        run: npm run dev
        workdir: frontend
        env:
          PORT: "5173"
        skip_if:
          exists: frontend/node_modules

    # Steps run when: devrun clean <workflow>
    clean:
      - description: "Remove build output"
        run: rm -rf dist
      - command: my-cleanup-command    # can reference named commands too
```

### Variables

`${VAR}` tokens are expanded in every string field — `run`, `workdir`, `env` values, `env_from` file paths, `prompt` messages, `skip_if` cmd, and `capture` var names.

| Variable                       | Set by                                                              |
| ------------------------------ | ------------------------------------------------------------------- |
| `$DEVRUN_ROOT`               | Always — absolute path to the directory containing `devrun.yaml` |
| `$MY_VAR`                    | `vars:` block, expanded at startup                                |
| Any key from `env:`          | Startup                                                             |
| Any key loaded by `env_from` | When that command runs                                              |
| Any key set by `capture`     | When that command runs                                              |
| Any key set by `prompt`      | When that command runs                                              |

State flows forward: env loaded or captured in one step is visible to every step that follows in the same workflow run.

---

## CLI reference

```
devrun <workflow>           run a named workflow
devrun <command>            run a named global command
devrun clean                run the workflow named "clean"
devrun clean <workflow>     run the clean steps for one workflow
devrun list                 list all commands and workflows
devrun help                 show help
```

---

## Terminal spawning

`spawn: true` on a workflow reference opens that workflow in a new terminal. Detection order:

**Windows:** Windows Terminal (`wt`) → `cmd start`

**macOS / Linux:** tmux → iTerm2 → Apple Terminal → manual fallback (prints the command)

```yaml
workflows:
  dev:
    steps:
      - workflow: backend
        spawn: true     # new terminal
      - workflow: frontend
        spawn: true     # another new terminal
```

---

## Platform support

| Platform | Shell      | Terminal spawn               |
| -------- | ---------- | ---------------------------- |
| macOS    | `sh -c`  | tmux, iTerm2, Apple Terminal |
| Linux    | `sh -c`  | tmux, fallback               |
| Windows  | `cmd /C` | Windows Terminal, cmd        |

---

## Example: full-stack project

See [`sample.devrun.yaml`](sample.devrun.yaml) for a complete example covering all features: commands, workflows, env loading, secret prompts, capture, skip conditions, spawn, and clean.

---

## License

Apache 2.0
