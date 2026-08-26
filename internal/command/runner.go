// Package command contains the small process boundary used by runtime adapters.
package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Runner executes a command and returns its combined standard output and
// standard error. Env contains child-only overrides; nil retains the process
// environment unchanged.
type Runner interface {
	Run(ctx context.Context, name string, args []string, env map[string]string) ([]byte, error)
}

// ProcessRunner runs commands through os/exec.
type ProcessRunner struct{}

func (ProcessRunner) Run(ctx context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) != 0 {
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if _, overridden := env[key]; !overridden {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			cmd.Env = append(cmd.Env, key+"="+env[key])
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return output, fmt.Errorf("run %q with arguments %q: %w (%v)", name, args, ctx.Err(), err)
		}
		return output, fmt.Errorf("run %q with arguments %q: %w", name, args, err)
	}
	return output, nil
}
