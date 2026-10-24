package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/geshido/icomark/internal/core"
	"github.com/geshido/icomark/internal/state"
)

// App is the shared headless CLI application. Its environment and I/O are
// injectable so callers and tests can isolate it from the host process.
type App struct {
	CommandName string
	// Name is accepted as a shorter alias for CommandName.
	Name    string
	Backend Backend
	Env     func(string) string
	HomeDir func() (string, error)
	Stdout  io.Writer
	Stderr  io.Writer
}

// New returns an application with process-backed environment and I/O.
func New(commandName string, backend Backend) App {
	return App{
		CommandName: commandName,
		Backend:     backend,
		Env:         os.Getenv,
		HomeDir:     os.UserHomeDir,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
}

// Run executes one headless command and returns a process exit code.
func (a App) Run(ctx context.Context, args []string) int {
	options, err := ParseOptions(args)
	if err != nil {
		fmt.Fprintf(a.stderr(), "%s: %v\n\n", a.commandName(), err)
		a.printUsage(a.stderr())
		return 2
	}
	if options.Help {
		a.printUsage(a.stdout())
		return 0
	}
	if a.Backend == nil {
		return a.fail(errors.New("backend is not configured"))
	}

	backendOptions := BackendOptions{
		Target:   options.Target,
		Socket:   options.Socket,
		StateKey: options.StateKey,
	}
	target, err := a.Backend.Resolve(ctx, backendOptions)
	if err != nil {
		return a.fail(fmt.Errorf("resolve target: %w", err))
	}

	configHome, stateHome, err := a.homes()
	if err != nil {
		return a.fail(err)
	}
	cfg, err := core.LoadConfig(
		filepath.Join(configHome, "icomark", "config.yml"),
		a.legacyConfigPaths(configHome),
		options.Config,
	)
	if err != nil {
		return a.fail(err)
	}

	parsed := core.ParseLabel(target.Label, cfg)
	scope := target.Scope
	if options.StateKey != "" {
		scope = options.StateKey
	}
	canonical := state.FilePath(stateHome, a.Backend.Name(), scope)
	storedState, err := a.loadState(canonical, stateHome, scope)
	if err != nil {
		return a.fail(err)
	}
	stored, found := storedState.Lookup(target.ID, parsed.Base)
	var storedEntry *core.Entry
	if found {
		storedEntry = &stored
	}

	if options.Command == CommandStatus {
		result, err := core.Plan(core.Update{
			Config: cfg,
			Label:  target.Label,
			Stored: storedEntry,
			Action: core.Sync,
		})
		if err != nil {
			return a.fail(err)
		}
		statusName := result.Entry.Status
		if statusName == "" {
			statusName = "none"
		}
		fmt.Fprintf(a.stdout(), "%s %s", statusName, result.Entry.Base)
		if result.Entry.Comment != "" {
			fmt.Fprintf(a.stdout(), " — %s", result.Entry.Comment)
		}
		fmt.Fprintln(a.stdout())
		return 0
	}

	update := core.Update{
		Config: cfg,
		Label:  target.Label,
		Stored: storedEntry,
	}
	switch options.Command {
	case CommandNext:
		update.Action = core.Next
	case CommandPrev:
		update.Action = core.Prev
	case CommandSet:
		update.Action = core.SetStatus
		update.Status = options.Status
	case CommandComment:
		if options.Comment == "" {
			update.Action = core.ClearComment
		} else {
			update.Action = core.SetComment
			update.Comment = options.Comment
		}
	case CommandSync:
		update.Action = core.Sync
	default:
		return a.fail(fmt.Errorf("unsupported command %q", options.Command))
	}

	result, err := core.Plan(update)
	if err != nil {
		return a.fail(err)
	}
	if result.Label != target.Label {
		if err := a.Backend.Rename(ctx, target, result.Label, backendOptions); err != nil {
			return a.fail(fmt.Errorf("rename target: %w", err))
		}
	}
	storedState.Put(target.ID, result.Entry)
	if err := state.Save(canonical, storedState); err != nil {
		return a.fail(err)
	}
	if options.Command == CommandSync {
		if aggregator, ok := a.Backend.(AllStatusAggregator); ok {
			if err := aggregator.RefreshAllStatusAggregates(ctx, cfg, backendOptions); err != nil {
				return a.fail(fmt.Errorf("refresh all status aggregates: %w", err))
			}
			return 0
		}
	}
	if aggregator, ok := a.Backend.(StatusAggregator); ok {
		if err := aggregator.RefreshStatusAggregate(ctx, target, cfg, backendOptions); err != nil {
			return a.fail(fmt.Errorf("refresh status aggregate: %w", err))
		}
	}
	return 0
}

func (a App) loadState(canonical, stateHome, scope string) (state.State, error) {
	_, err := os.Stat(canonical)
	if err == nil {
		return state.Load(canonical)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return state.State{}, fmt.Errorf("inspect state %q: %w", canonical, err)
	}

	var legacyTool string
	var migrate func([]byte) (state.State, error)
	switch a.Backend.Name() {
	case "zellij":
		legacyTool = "ztabs"
		migrate = state.MigrateZtabs
	case "agterm":
		legacyTool = "atabs"
		migrate = state.MigrateAtabs
	default:
		return state.New(), nil
	}
	legacyPath := state.LegacyFilePath(stateHome, legacyTool, scope)
	data, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return state.New(), nil
	}
	if err != nil {
		return state.State{}, fmt.Errorf("read legacy state %q: %w", legacyPath, err)
	}
	migrated, err := migrate(data)
	if err != nil {
		return state.State{}, fmt.Errorf("migrate legacy state %q: %w", legacyPath, err)
	}
	return migrated, nil
}

func (a App) legacyConfigPaths(configHome string) []string {
	switch a.Backend.Name() {
	case "zellij":
		return []string{filepath.Join(configHome, "ztabs", "config.yml")}
	case "agterm":
		return []string{filepath.Join(configHome, "atabs", "config.yml")}
	default:
		return nil
	}
}

func (a App) homes() (configHome, stateHome string, err error) {
	env := a.Env
	if env == nil {
		env = os.Getenv
	}
	configHome = env("XDG_CONFIG_HOME")
	stateHome = env("XDG_STATE_HOME")
	if configHome != "" && stateHome != "" {
		return configHome, stateHome, nil
	}
	homeDir := a.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	return configHome, stateHome, nil
}

func (a App) commandName() string {
	if a.CommandName != "" {
		return a.CommandName
	}
	if a.Name != "" {
		return a.Name
	}
	return "icomark"
}

func (a App) stdout() io.Writer {
	if a.Stdout != nil {
		return a.Stdout
	}
	return io.Discard
}

func (a App) stderr() io.Writer {
	if a.Stderr != nil {
		return a.Stderr
	}
	return io.Discard
}

func (a App) fail(err error) int {
	fmt.Fprintf(a.stderr(), "%s: %v\n", a.commandName(), err)
	return 1
}

func (a App) printUsage(writer io.Writer) {
	name := a.commandName()
	fmt.Fprintf(writer, "Usage: %s [flags] <command> [arguments]\n\n", name)
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  next")
	fmt.Fprintln(writer, "  prev")
	fmt.Fprintln(writer, "  set <status>")
	fmt.Fprintln(writer, "  comment [text...]")
	fmt.Fprintln(writer, "  sync")
	fmt.Fprintln(writer, "  status")
	fmt.Fprintln(writer, "\nFlags:")
	fmt.Fprintln(writer, "  --target ID")
	fmt.Fprintln(writer, "  --socket PATH")
	fmt.Fprintln(writer, "  --state-key KEY")
	fmt.Fprintln(writer, "  --config PATH")
}
