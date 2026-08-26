// Package agterm adapts agterm sessions to the shared application backend.
package agterm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/command"
)

// Backend resolves and renames agterm sessions.
type Backend struct {
	Runner command.Runner
	Env    func(string) string
}

// New returns a backend connected to the current process environment.
func New() *Backend {
	return &Backend{Runner: command.ProcessRunner{}, Env: os.Getenv}
}

func (*Backend) Name() string { return "agterm" }

func (b *Backend) Resolve(ctx context.Context, options app.BackendOptions) (app.Target, error) {
	args := []string{"tree", "--json"}
	if options.Socket != "" {
		args = append(args, "--socket", options.Socket)
	}
	output, err := b.runner().Run(ctx, "agtermctl", args, nil)
	if err != nil {
		return app.Target{}, commandError("agtermctl", args, output, err)
	}

	tree, err := decodeTree(output)
	if err != nil {
		return app.Target{}, err
	}
	candidates, err := tree.sessions()
	if err != nil {
		return app.Target{}, err
	}

	selector := options.Target
	if selector == "" {
		selector = b.getenv("AGTERM_SESSION_ID")
	}
	var selected session
	if selector != "" {
		selected, err = selectSession(candidates, selector)
	} else {
		selected, err = selectActive(candidates)
	}
	if err != nil {
		return app.Target{}, err
	}

	return app.Target{
		ID:    selected.ID,
		Label: selected.Name,
		Scope: b.scope(options, tree.windowID()),
	}, nil
}

func (b *Backend) Rename(ctx context.Context, target app.Target, newLabel string, options app.BackendOptions) error {
	args := []string{"session", "rename", newLabel, "--target", target.ID}
	if options.Socket != "" {
		args = append(args, "--socket", options.Socket)
	}
	output, err := b.runner().Run(ctx, "agtermctl", args, nil)
	if err != nil {
		return commandError("agtermctl", args, output, err)
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

func (b *Backend) scope(options app.BackendOptions, treeWindowID string) string {
	if options.StateKey != "" {
		return options.StateKey
	}
	if windowID := b.getenv("AGTERM_WINDOW_ID"); windowID != "" {
		return windowID
	}
	if treeWindowID != "" {
		return treeWindowID
	}
	return "default"
}

type treeEnvelope struct {
	OK     bool            `json:"ok"`
	Result *treeResult     `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type treeResult struct {
	Tree *tree `json:"tree"`
}

type tree struct {
	WindowID   string      `json:"window_id"`
	Workspaces []workspace `json:"workspaces"`
}

type workspace struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Sessions []treeSession `json:"sessions"`
}

type treeSession struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type workspaceRef struct {
	ID   string
	Name string
}

type session struct {
	ID        string
	Name      string
	Active    bool
	Workspace workspaceRef
}

func decodeTree(output []byte) (*tree, error) {
	var envelope treeEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode agtermctl tree response %q: %w", strings.TrimSpace(string(output)), err)
	}
	if !envelope.OK {
		message := jsonErrorMessage(envelope.Error)
		if message == "" {
			message = "agtermctl reported ok=false"
		}
		return nil, fmt.Errorf("agtermctl tree error: %s", message)
	}
	if envelope.Result == nil || envelope.Result.Tree == nil {
		return nil, fmt.Errorf("malformed agtermctl tree response: missing result.tree")
	}
	return envelope.Result.Tree, nil
}

func jsonErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return message
	}
	var object struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Message != "" {
		return object.Message
	}
	return string(raw)
}

func (t *tree) sessions() ([]session, error) {
	var sessions []session
	for _, workspace := range t.Workspaces {
		owner := workspaceRef{ID: workspace.ID, Name: workspace.Name}
		for _, item := range workspace.Sessions {
			if item.ID == "" {
				return nil, fmt.Errorf("malformed agtermctl tree response: session missing id")
			}
			if item.Name == "" {
				return nil, fmt.Errorf("malformed agtermctl tree response: session %q missing name", item.ID)
			}
			sessions = append(sessions, session{
				ID:        item.ID,
				Name:      item.Name,
				Active:    item.Active,
				Workspace: owner,
			})
		}
	}
	return sessions, nil
}

func (t *tree) windowID() string {
	return t.WindowID
}

func selectSession(sessions []session, selector string) (session, error) {
	for _, candidate := range sessions {
		if candidate.ID == selector {
			return candidate, nil
		}
	}
	var matches []session
	for _, candidate := range sessions {
		if strings.HasPrefix(strings.ToLower(candidate.ID), strings.ToLower(selector)) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return session{}, fmt.Errorf("no session matches target %q", selector)
	case 1:
		return matches[0], nil
	default:
		details := make([]string, 0, len(matches))
		for _, match := range matches {
			workspace := match.Workspace.ID
			if match.Workspace.Name != "" {
				workspace += "/" + match.Workspace.Name
			}
			details = append(details, fmt.Sprintf("%s (workspace %s)", match.ID, workspace))
		}
		return session{}, fmt.Errorf("target %q is ambiguous: %s", selector, strings.Join(details, ", "))
	}
}

func selectActive(sessions []session) (session, error) {
	var active []session
	for _, candidate := range sessions {
		if candidate.Active {
			active = append(active, candidate)
		}
	}
	switch len(active) {
	case 0:
		return session{}, fmt.Errorf("no active session found; specify --target or AGTERM_SESSION_ID")
	case 1:
		return active[0], nil
	default:
		return session{}, fmt.Errorf("multiple active sessions found; specify --target or AGTERM_SESSION_ID")
	}
}

func commandError(name string, args []string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s failed: %w; output: %s", name, strings.Join(args, " "), err, message)
}
