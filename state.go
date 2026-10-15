package main

import (
	"os"
	"strings"
)

// State holds the accumulated environment that grows as steps execute.
// It is the shared data bus between steps: env loaded or captured in
// one step is visible to every step that follows in the same workflow.
type State struct {
	env []string // KEY=VALUE strings, same format as os.Environ()
}

func newState(base map[string]string) *State {
	s := &State{env: os.Environ()}
	for k, v := range base {
		s.Set(k, v)
	}
	return s
}

// Set adds or replaces a key in the state.
func (s *State) Set(key, val string) {
	prefix := key + "="
	for i, kv := range s.env {
		if strings.HasPrefix(kv, prefix) {
			s.env[i] = prefix + val
			return
		}
	}
	s.env = append(s.env, prefix+val)
}

// Get returns the value for key, or "" if not set.
func (s *State) Get(key string) string {
	prefix := key + "="
	for _, kv := range s.env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

// Environ returns a copy of the env slice for use in exec.Cmd.Env.
func (s *State) Environ() []string {
	out := make([]string, len(s.env))
	copy(out, s.env)
	return out
}

// Expand replaces ${VAR} and $VAR tokens using the current state.
func (s *State) Expand(str string) string {
	return os.Expand(str, func(key string) string {
		v := s.Get(key)
		if v == "" {
			return os.Getenv(key) // fall back to process env
		}
		return v
	})
}

// LoadDotenv parses KEY=VALUE lines from src into the state.
// Blank lines and lines starting with # are ignored.
// Returns the list of keys that were loaded.
func (s *State) LoadDotenv(src string) []string {
	var loaded []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key, val := line[:idx], line[idx+1:]
		s.Set(key, val)
		loaded = append(loaded, key)
	}
	return loaded
}
