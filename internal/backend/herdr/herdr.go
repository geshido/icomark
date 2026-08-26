// Package herdr adapts Herdr tabs to the shared application backend.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/command"
	"github.com/geshido/icomark/internal/core"
)

// Backend resolves and renames Herdr tabs.
type Backend struct {
	Runner command.Runner
	Env    func(string) string
}

// New returns a backend connected to the current process environment.
func New() *Backend {
	return &Backend{Runner: command.ProcessRunner{}, Env: os.Getenv}
}

func (*Backend) Name() string { return "herdr" }

func (b *Backend) Resolve(ctx context.Context, options app.BackendOptions) (app.Target, error) {
	id := options.Target
	if id == "" {
		id = b.getenv("HERDR_TAB_ID")
	}
	if id == "" {
		id = b.getenv("HERDR_ACTIVE_TAB_ID")
	}
	if id == "" {
		return app.Target{}, fmt.Errorf("Herdr target is required: use --target or set HERDR_TAB_ID/HERDR_ACTIVE_TAB_ID")
	}

	args := []string{"tab", "get", id}
	output, err := b.runner().Run(ctx, "herdr", args, socketEnv(options.Socket))
	if err != nil {
		return app.Target{}, commandError("herdr", args, output, err)
	}
	tab, err := decodeTab(output)
	if err != nil {
		return app.Target{}, err
	}
	return app.Target{
		ID:      *tab.ID,
		Label:   *tab.Label,
		Scope:   b.scope(options),
		GroupID: *tab.WorkspaceID,
	}, nil
}

func (b *Backend) Rename(ctx context.Context, target app.Target, newLabel string, options app.BackendOptions) error {
	args := []string{"tab", "rename", target.ID, newLabel}
	output, err := b.runner().Run(ctx, "herdr", args, socketEnv(options.Socket))
	if err != nil {
		return commandError("herdr", args, output, err)
	}
	return nil
}

// RefreshStatusAggregate publishes the visible statuses of all marked tabs in
// their tab order as the workspace's display-only $status metadata token.
func (b *Backend) RefreshStatusAggregate(
	ctx context.Context,
	target app.Target,
	cfg core.Config,
	options app.BackendOptions,
) error {
	if target.GroupID == "" {
		return fmt.Errorf("Herdr workspace is missing for tab %q", target.ID)
	}
	return b.refreshWorkspaceStatusAggregate(ctx, target.GroupID, cfg, options)
}

// RefreshAllStatusAggregates republishes the display-only $status metadata for
// every live Herdr workspace.
func (b *Backend) RefreshAllStatusAggregates(
	ctx context.Context,
	cfg core.Config,
	options app.BackendOptions,
) error {
	env := socketEnv(options.Socket)
	listArgs := []string{"workspace", "list"}
	output, err := b.runner().Run(ctx, "herdr", listArgs, env)
	if err != nil {
		return commandError("herdr", listArgs, output, err)
	}
	workspaces, err := decodeWorkspaceList(output)
	if err != nil {
		return err
	}
	sort.SliceStable(workspaces, func(i, j int) bool { return workspaces[i].Number < workspaces[j].Number })
	var refreshErrors []error
	for _, workspace := range workspaces {
		if err := b.refreshWorkspaceStatusAggregate(ctx, workspace.ID, cfg, options); err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("refresh workspace %q: %w", workspace.ID, err))
		}
	}
	return errors.Join(refreshErrors...)
}

func (b *Backend) refreshWorkspaceStatusAggregate(
	ctx context.Context,
	workspaceID string,
	cfg core.Config,
	options app.BackendOptions,
) error {
	env := socketEnv(options.Socket)
	listArgs := []string{"tab", "list", "--workspace", workspaceID}
	output, err := b.runner().Run(ctx, "herdr", listArgs, env)
	if err != nil {
		return commandError("herdr", listArgs, output, err)
	}
	tabs, err := decodeTabList(output)
	if err != nil {
		return err
	}
	sort.SliceStable(tabs, func(i, j int) bool { return tabs[i].Number < tabs[j].Number })

	markers := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		parsed := core.ParseLabel(tab.Label, cfg)
		if parsed.StatusIndex == 0 {
			continue
		}
		marker := strings.TrimSpace(cfg.Statuses[parsed.StatusIndex].Prefix)
		if marker != "" {
			markers = append(markers, marker)
		}
	}

	reportArgs := []string{
		"workspace", "report-metadata", workspaceID,
		"--source", "user:icomark",
	}
	if len(markers) == 0 {
		reportArgs = append(reportArgs, "--clear-token", "status")
	} else {
		reportArgs = append(reportArgs, "--token", "status="+strings.Join(markers, " "))
	}
	output, err = b.runner().Run(ctx, "herdr", reportArgs, env)
	if err != nil {
		return commandError("herdr", reportArgs, output, err)
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

func (b *Backend) scope(options app.BackendOptions) string {
	if options.StateKey != "" {
		return options.StateKey
	}
	if options.Socket != "" {
		return options.Socket
	}
	if socket := b.getenv("HERDR_SOCKET_PATH"); socket != "" {
		return socket
	}
	return "default"
}

func socketEnv(socket string) map[string]string {
	if socket == "" {
		return nil
	}
	return map[string]string{"HERDR_SOCKET_PATH": socket}
}

type tabEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result *tabResult      `json:"result"`
}

type tabResult struct {
	Tab  *tabInfo `json:"tab"`
	Type string   `json:"type"`
}

type tabInfo struct {
	ID          *string `json:"tab_id"`
	Label       *string `json:"label"`
	WorkspaceID *string `json:"workspace_id"`
}

type tabListEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result *tabListResult  `json:"result"`
}

type tabListResult struct {
	Tabs []listedTab `json:"tabs"`
	Type string      `json:"type"`
}

type listedTab struct {
	Label  string `json:"label"`
	Number int    `json:"number"`
}

type workspaceListEnvelope struct {
	ID     json.RawMessage      `json:"id"`
	Result *workspaceListResult `json:"result"`
}

type workspaceListResult struct {
	Workspaces []listedWorkspace `json:"workspaces"`
	Type       string            `json:"type"`
}

type listedWorkspace struct {
	ID     string `json:"workspace_id"`
	Number int    `json:"number"`
}

func decodeTab(output []byte) (*tabInfo, error) {
	var envelope tabEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode herdr tab response %q: %w", strings.TrimSpace(string(output)), err)
	}
	if len(envelope.ID) == 0 || bytes.Equal(bytes.TrimSpace(envelope.ID), []byte("null")) {
		return nil, fmt.Errorf("malformed herdr tab response: missing id")
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("malformed herdr tab response: missing result")
	}
	if envelope.Result.Type != "tab_info" {
		return nil, fmt.Errorf("malformed herdr tab response: result.type is %q, want %q", envelope.Result.Type, "tab_info")
	}
	if envelope.Result.Tab == nil {
		return nil, fmt.Errorf("malformed herdr tab response: missing result.tab")
	}
	tab := envelope.Result.Tab
	if tab.ID == nil || *tab.ID == "" {
		return nil, fmt.Errorf("malformed herdr tab response: missing tab_id")
	}
	if tab.Label == nil {
		return nil, fmt.Errorf("malformed herdr tab response: missing label")
	}
	if tab.WorkspaceID == nil || *tab.WorkspaceID == "" {
		return nil, fmt.Errorf("malformed herdr tab response: missing workspace_id")
	}
	return tab, nil
}

func decodeTabList(output []byte) ([]listedTab, error) {
	var envelope tabListEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode herdr tab list response %q: %w", strings.TrimSpace(string(output)), err)
	}
	if len(envelope.ID) == 0 || bytes.Equal(bytes.TrimSpace(envelope.ID), []byte("null")) {
		return nil, fmt.Errorf("malformed herdr tab list response: missing id")
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("malformed herdr tab list response: missing result")
	}
	if envelope.Result.Type != "tab_list" {
		return nil, fmt.Errorf(
			"malformed herdr tab list response: result.type is %q, want %q",
			envelope.Result.Type,
			"tab_list",
		)
	}
	return envelope.Result.Tabs, nil
}

func decodeWorkspaceList(output []byte) ([]listedWorkspace, error) {
	var envelope workspaceListEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode herdr workspace list response %q: %w", strings.TrimSpace(string(output)), err)
	}
	if len(envelope.ID) == 0 || bytes.Equal(bytes.TrimSpace(envelope.ID), []byte("null")) {
		return nil, fmt.Errorf("malformed herdr workspace list response: missing id")
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("malformed herdr workspace list response: missing result")
	}
	if envelope.Result.Type != "workspace_list" {
		return nil, fmt.Errorf(
			"malformed herdr workspace list response: result.type is %q, want %q",
			envelope.Result.Type,
			"workspace_list",
		)
	}
	for _, workspace := range envelope.Result.Workspaces {
		if workspace.ID == "" {
			return nil, fmt.Errorf("malformed herdr workspace list response: missing workspace_id")
		}
	}
	return envelope.Result.Workspaces, nil
}

func commandError(name string, args []string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s failed: %w; output: %s", name, strings.Join(args, " "), err, message)
}
