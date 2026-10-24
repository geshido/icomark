// Package zellij adapts Zellij tabs to the shared application backend.
package zellij

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/command"
)

// Backend resolves and renames Zellij tabs.
type Backend struct {
	Runner command.Runner
	Env    func(string) string
}

var _ app.Backend = (*Backend)(nil)

// New returns a backend connected to the current process environment.
func New() *Backend {
	return &Backend{Runner: command.ProcessRunner{}, Env: os.Getenv}
}

func (*Backend) Name() string { return "zellij" }

// Resolve selects either an exact tab ID or the current Zellij tab.
func (b *Backend) Resolve(ctx context.Context, options app.BackendOptions) (app.Target, error) {
	scope, err := b.scope(options)
	if err != nil {
		return app.Target{}, err
	}

	id := options.Target
	if id != "" {
		if !decimal(id) {
			return app.Target{}, fmt.Errorf("Zellij target %q must be a numeric tab id", id)
		}
	} else {
		currentArgs := []string{"action", "current-tab-info"}
		currentOutput, runErr := b.runner().Run(ctx, "zellij", currentArgs, nil)
		if runErr != nil {
			return app.Target{}, commandError("zellij", currentArgs, currentOutput, runErr)
		}
		id, err = parseCurrentTabID(currentOutput)
		if err != nil {
			return app.Target{}, err
		}
	}

	listArgs := []string{"action", "list-tabs"}
	output, err := b.runner().Run(ctx, "zellij", listArgs, nil)
	if err != nil {
		return app.Target{}, commandError("zellij", listArgs, output, err)
	}
	tabs, err := parseTabs(output)
	if err != nil {
		return app.Target{}, err
	}

	label, found := tabs[id]
	if !found {
		return app.Target{}, fmt.Errorf("Zellij tab id %s not found in list-tabs output", id)
	}
	return app.Target{ID: id, Label: label, Scope: scope}, nil
}

// Rename changes a tab label by its stable numeric ID.
func (b *Backend) Rename(ctx context.Context, target app.Target, newLabel string, _ app.BackendOptions) error {
	args := []string{"action", "rename-tab-by-id", target.ID, newLabel}
	output, err := b.runner().Run(ctx, "zellij", args, nil)
	if err != nil {
		return commandError("zellij", args, output, err)
	}
	return nil
}

func (b *Backend) runner() command.Runner {
	if b != nil && b.Runner != nil {
		return b.Runner
	}
	return command.ProcessRunner{}
}

func (b *Backend) getenv(key string) string {
	if b != nil && b.Env != nil {
		return b.Env(key)
	}
	return os.Getenv(key)
}

func (b *Backend) scope(options app.BackendOptions) (string, error) {
	if options.StateKey != "" {
		return options.StateKey, nil
	}
	if session := b.getenv("ZELLIJ_SESSION_NAME"); session != "" {
		return session, nil
	}
	return "", fmt.Errorf("Zellij state scope is required: use --state-key or set ZELLIJ_SESSION_NAME")
}

func parseTabs(output []byte) (map[string]string, error) {
	tabs := make(map[string]string)
	for lineNumber, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "TAB_ID") {
			continue
		}
		id, position, name, ok := splitTabRow(line)
		if !ok {
			return nil, fmt.Errorf("malformed list-tabs row %d: %q; want TAB_ID POSITION NAME", lineNumber+1, raw)
		}
		if !decimal(id) {
			return nil, fmt.Errorf("malformed tab id %q in list-tabs row %d", id, lineNumber+1)
		}
		if !decimal(position) {
			return nil, fmt.Errorf("malformed tab position %q in list-tabs row %d", position, lineNumber+1)
		}
		if _, exists := tabs[id]; exists {
			return nil, fmt.Errorf("duplicate tab id %s in list-tabs output", id)
		}
		tabs[id] = name
	}
	return tabs, nil
}

func splitTabRow(line string) (id, position, name string, ok bool) {
	id, rest, ok := takeField(line)
	if !ok {
		return "", "", "", false
	}
	position, rest, ok = takeField(rest)
	if !ok {
		return "", "", "", false
	}
	name = strings.TrimSpace(rest)
	if name == "" {
		return "", "", "", false
	}
	return id, position, name, true
}

func takeField(value string) (field, rest string, ok bool) {
	value = strings.TrimLeft(value, " \t\r")
	separator := strings.IndexAny(value, " \t\r")
	if separator < 0 {
		return "", "", false
	}
	field = value[:separator]
	rest = strings.TrimLeft(value[separator:], " \t\r")
	if field == "" || rest == "" {
		return "", "", false
	}
	return field, rest, true
}

func parseCurrentTabID(output []byte) (string, error) {
	var id string
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "id:") {
			continue
		}
		if id != "" {
			return "", fmt.Errorf("malformed current-tab-info: multiple id fields")
		}
		id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		if !decimal(id) {
			return "", fmt.Errorf("malformed current-tab-info id %q", id)
		}
	}
	if id == "" {
		return "", fmt.Errorf("malformed current-tab-info: missing id")
	}
	return id, nil
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func commandError(name string, args []string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s failed: %w; output: %s", name, strings.Join(args, " "), err, message)
}
