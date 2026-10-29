// Package rex adapts Rex windows (tabs) to the shared application backend.
package rex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/command"
	"github.com/geshido/icomark/internal/core"
)

// Backend resolves and renames Rex windows.
type Backend struct {
	Runner command.Runner
	Env    func(string) string
}

var _ app.Backend = (*Backend)(nil)
var _ app.StatusAggregator = (*Backend)(nil)

// New returns a backend connected to the current process environment.
func New() *Backend {
	return &Backend{Runner: command.ProcessRunner{}, Env: os.Getenv}
}

func (*Backend) Name() string { return "rex" }

func (b *Backend) Resolve(ctx context.Context, options app.BackendOptions) (app.Target, error) {
	// Rex resolves the window containing REX_BLOCK, even if another tab has focus.
	args := []string{"window", "inspect", "--json"}
	if options.Target != "" {
		args = append(args, "--", options.Target)
	}
	output, err := b.run(ctx, args, options)
	if err != nil {
		return app.Target{}, err
	}
	var window struct {
		SessionID string  `json:"session_id"`
		WindowID  string  `json:"window_id"`
		Label     *string `json:"label"`
	}
	if err := json.Unmarshal(output, &window); err != nil {
		return app.Target{}, fmt.Errorf("decode rex window response %q: %w", strings.TrimSpace(string(output)), err)
	}
	if window.SessionID == "" || window.WindowID == "" || window.Label == nil {
		return app.Target{}, fmt.Errorf("malformed rex window response: session_id, window_id, and label are required")
	}
	scope := window.SessionID
	if options.StateKey != "" {
		scope = options.StateKey
	}
	return app.Target{
		ID: window.WindowID, Label: *window.Label, Scope: scope, GroupID: window.SessionID,
	}, nil
}

func (b *Backend) Rename(ctx context.Context, target app.Target, label string, options app.BackendOptions) error {
	if target.GroupID == "" || target.ID == "" {
		return fmt.Errorf("Rex session and window IDs are required for rename")
	}
	_, err := b.run(ctx, []string{"window", "rename", "-s", target.GroupID, "--", target.ID, label}, options)
	return err
}

// RefreshStatusAggregate prefixes the session label with unique tab status icons.
func (b *Backend) RefreshStatusAggregate(ctx context.Context, target app.Target, cfg core.Config, options app.BackendOptions) error {
	if target.GroupID == "" {
		return fmt.Errorf("Rex session ID is required for status aggregation")
	}
	output, err := b.run(ctx, []string{"session", "inspect", "--json", "--", target.GroupID}, options)
	if err != nil {
		return err
	}
	var session struct {
		ID      string  `json:"session_id"`
		Label   *string `json:"label"`
		Windows *[]struct {
			Label *string `json:"label"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(output, &session); err != nil {
		return fmt.Errorf("decode rex session response: %w", err)
	}
	if session.ID != target.GroupID || session.Label == nil || *session.Label == "" || session.Windows == nil {
		return fmt.Errorf("malformed rex session response: matching session_id, nonempty label, and windows array are required")
	}

	var markers []string
	seen := make(map[string]bool)
	for _, window := range *session.Windows {
		if window.Label == nil {
			return fmt.Errorf("malformed rex session response: window label is required")
		}
		parsed := core.ParseLabel(*window.Label, cfg)
		if parsed.StatusIndex != 0 {
			if icon := strings.TrimSpace(cfg.Statuses[parsed.StatusIndex].Prefix); icon != "" {
				if strings.Contains(icon, "[icomark:") {
					return fmt.Errorf("Rex status prefixes must not contain the reserved summary marker [icomark:")
				}
				if !seen[icon] {
					markers = append(markers, icon)
					seen[icon] = true
				}
			}
		}
	}
	label := *session.Label
	// Migrate summaries written by the previous suffix format.
	const marker = " [icomark: "
	if start := strings.LastIndex(label, marker); start >= 0 && strings.HasSuffix(label, "]") {
		label = label[:start]
	}
	var prefixes []string
	for i, status := range cfg.Statuses {
		if icon := strings.TrimSpace(status.Prefix); i != 0 && icon != "" {
			prefixes = append(prefixes, regexp.QuoteMeta(icon))
		}
	}
	if len(prefixes) != 0 {
		pattern, err := regexp.Compile("^(?:" + strings.Join(prefixes, "|") + ")+ ")
		if err != nil {
			return fmt.Errorf("compile Rex session status prefixes: %w", err)
		}
		// Custom prefixes may overlap, such as "In" and "In Progress".
		pattern.Longest()
		label = pattern.ReplaceAllString(label, "")
	}
	if label == "" {
		return fmt.Errorf("Rex session name must not be empty after removing status marks")
	}
	if len(markers) != 0 {
		label = strings.Join(markers, "") + " " + label
	}
	if label == *session.Label {
		return nil
	}
	if label == "" || utf8.RuneCountInString(label) > 128 {
		return fmt.Errorf("Rex session summary must fit within 128 characters without removing the session name")
	}
	_, err = b.run(ctx, []string{"session", "rename", "--", target.GroupID, label}, options)
	return err
}

func (b *Backend) run(ctx context.Context, args []string, options app.BackendOptions) ([]byte, error) {
	env := b.Env
	if env == nil {
		env = os.Getenv
	}
	binary := "rex"
	if dir := env("REX_BIN_DIR"); dir != "" {
		binary = filepath.Join(dir, "rex")
	}
	var overrides map[string]string
	if options.Socket != "" {
		// -S discards Rex's inherited session/block context; an env override keeps it.
		overrides = map[string]string{"REX_SERVER": options.Socket}
	}
	runner := b.Runner
	if runner == nil {
		runner = command.ProcessRunner{}
	}
	output, err := runner.Run(ctx, binary, args, overrides)
	if err != nil {
		return nil, fmt.Errorf("rex %s failed: %w; output: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
