package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// shellCmd returns an exec.Cmd that runs script through the platform shell.
//   - Unix:    sh -c <script>
//   - Windows: cmd /C <script>
func shellCmd(script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", script)
	}
	return exec.Command("sh", "-c", script)
}

// spawnInTerminal opens a new terminal window/pane running "devrun <workflowName>".
// Detection order:
//
//	Windows: Windows Terminal (wt) → cmd start
//	Unix:    tmux → iTerm2 → Apple Terminal → manual fallback
func spawnInTerminal(workflowName, devrunRoot string) error {
	self, err := os.Executable()
	if err != nil {
		self = "devrun"
	}

	if runtime.GOOS == "windows" {
		return spawnWindows(workflowName, devrunRoot, self)
	}
	return spawnUnix(workflowName, devrunRoot, self)
}

func spawnWindows(workflowName, devrunRoot, self string) error {
	// cmd /K keeps the window open after the command finishes so the user
	// can read output — correct behaviour for a long-running dev server.
	run := fmt.Sprintf(`cd /d "%s" && "%s" %s`, devrunRoot, self, workflowName)

	// Prefer Windows Terminal (wt) when available.
	if wtPath, err := exec.LookPath("wt"); err == nil {
		return exec.Command(wtPath, "new-tab", "--title", workflowName,
			"--", "cmd", "/K", run).Run()
	}

	// Fall back to a plain cmd window.
	return exec.Command("cmd", "/C", "start", "cmd", "/K", run).Run()
}

func spawnUnix(workflowName, devrunRoot, self string) error {
	switch {
	case os.Getenv("TMUX") != "":
		cmd := fmt.Sprintf("%s %s", shellQuote(self), workflowName)
		return exec.Command("tmux", "new-window", "-c", devrunRoot,
			"-n", workflowName, cmd).Run()

	case os.Getenv("TERM_PROGRAM") == "iTerm.app":
		run := fmt.Sprintf("cd %s && %s %s", shellQuote(devrunRoot),
			shellQuote(self), workflowName)
		script := fmt.Sprintf(`tell application "iTerm2"
  tell current window
    create tab with default profile
    tell current session
      write text %s
    end tell
  end tell
end tell`, osascriptQuote(run))
		return exec.Command("osascript", "-e", script).Run()

	case os.Getenv("TERM_PROGRAM") == "Apple_Terminal":
		run := fmt.Sprintf("cd %s && %s %s", shellQuote(devrunRoot),
			shellQuote(self), workflowName)
		script := fmt.Sprintf(`tell application "Terminal"
  do script %s
  activate
end tell`, osascriptQuote(run))
		return exec.Command("osascript", "-e", script).Run()

	default:
		fmt.Printf("      ⚠  unknown terminal — run this in a new window:\n")
		fmt.Printf("         cd %s && %s %s\n", devrunRoot, self, workflowName)
		return nil
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func osascriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
