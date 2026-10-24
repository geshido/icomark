package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/geshido/icomark/internal/core"
	"github.com/geshido/icomark/internal/state"
)

type fakeBackend struct {
	name       string
	target     Target
	resolveErr error
	renameErr  error
	resolved   []BackendOptions
	renames    []string
	events     []string
	onRename   func()
}

func (b *fakeBackend) Name() string { return b.name }

func (b *fakeBackend) Resolve(_ context.Context, options BackendOptions) (Target, error) {
	b.events = append(b.events, "resolve")
	b.resolved = append(b.resolved, options)
	return b.target, b.resolveErr
}

func (b *fakeBackend) Rename(_ context.Context, target Target, label string, options BackendOptions) error {
	b.events = append(b.events, "rename")
	b.renames = append(b.renames, label)
	if b.onRename != nil {
		b.onRename()
	}
	return b.renameErr
}

type aggregatingBackend struct {
	*fakeBackend
	refreshes    int
	allRefreshes int
	onRefresh    func()
}

func (b *aggregatingBackend) RefreshStatusAggregate(_ context.Context, _ Target, _ core.Config, _ BackendOptions) error {
	b.events = append(b.events, "refresh")
	b.refreshes++
	if b.onRefresh != nil {
		b.onRefresh()
	}
	return nil
}

func (b *aggregatingBackend) RefreshAllStatusAggregates(_ context.Context, _ core.Config, _ BackendOptions) error {
	b.events = append(b.events, "refresh-all")
	b.allRefreshes++
	if b.onRefresh != nil {
		b.onRefresh()
	}
	return nil
}

type appFixture struct {
	app       App
	backend   *fakeBackend
	root      string
	config    string
	stateHome string
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
}

func newAppFixture(t *testing.T, backendName string, target Target) *appFixture {
	t.Helper()
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "state")
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	backend := &fakeBackend{name: backendName, target: target}
	fixture := &appFixture{
		backend:   backend,
		root:      root,
		config:    filepath.Join(configHome, "icomark", "config.yml"),
		stateHome: stateHome,
		stdout:    out,
		stderr:    errOut,
	}
	fixture.app = App{
		CommandName: "ztabs",
		Backend:     backend,
		Env: func(key string) string {
			switch key {
			case "XDG_CONFIG_HOME":
				return configHome
			case "XDG_STATE_HOME":
				return stateHome
			default:
				return ""
			}
		},
		HomeDir: func() (string, error) { return filepath.Join(root, "home"), nil },
		Stdout:  out,
		Stderr:  errOut,
	}
	return fixture
}

func TestAppHelpAndUsageUseCommandName(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{})
	f.app.CommandName = "htabs"
	if code := f.app.Run(context.Background(), nil); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(f.stdout.String(), "Usage: htabs") || strings.Contains(f.stdout.String(), "zellij") {
		t.Fatalf("help was not command-branded: %q", f.stdout.String())
	}
	if len(f.backend.events) != 0 {
		t.Fatalf("help contacted backend: %v", f.backend.events)
	}

	f.stdout.Reset()
	if code := f.app.Run(context.Background(), []string{"next", "extra"}); code != 2 {
		t.Fatalf("usage error exit = %d", code)
	}
	if !strings.Contains(f.stderr.String(), "next takes no arguments") || !strings.Contains(f.stderr.String(), "Usage: htabs") {
		t.Fatalf("usage error output = %q", f.stderr.String())
	}
}

func TestAppInvalidHelpArgumentsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"help", "extra"},
		{"--help", "--unknown"},
		{"help", "--target"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := newAppFixture(t, "zellij", Target{})
			if code := f.app.Run(context.Background(), args); code != 2 {
				t.Fatalf("Run(%q) = %d, stdout %q, stderr %q", args, code, f.stdout.String(), f.stderr.String())
			}
			if len(f.backend.events) != 0 {
				t.Fatalf("invalid help contacted backend: %v", f.backend.events)
			}
		})
	}
}

func TestAppMutationRenamesBeforeCanonicalSaveAndPassesBackendOptions(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "7", Label: "tab", Scope: "session-a"})
	canonical := state.FilePath(f.stateHome, "zellij", "stable-key")
	f.backend.onRename = func() {
		if _, err := os.Stat(canonical); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical state existed before rename: %v", err)
		}
	}
	args := []string{"--target", "requested", "next", "--socket=/tmp/z.sock", "--state-key", "stable-key"}
	if code := f.app.Run(context.Background(), args); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if got := f.backend.renames; len(got) != 1 || got[0] != "🔨 tab" {
		t.Fatalf("renames = %q", got)
	}
	wantOptions := BackendOptions{Target: "requested", Socket: "/tmp/z.sock", StateKey: "stable-key"}
	if len(f.backend.resolved) != 1 || f.backend.resolved[0] != wantOptions {
		t.Fatalf("resolve options = %#v", f.backend.resolved)
	}
	loaded, err := state.Load(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Entries["7"]; got.Base != "tab" || got.Status != "wip" {
		t.Fatalf("saved entry = %#v", got)
	}
}

func TestAppMapsPrevAndSetActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "prev", args: []string{"prev"}, want: "🚫 tab"},
		{name: "set", args: []string{"set", "done"}, want: "✅ tab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
			if code := f.app.Run(context.Background(), tc.args); code != 0 {
				t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
			}
			if len(f.backend.renames) != 1 || f.backend.renames[0] != tc.want {
				t.Fatalf("renames = %q, want %q", f.backend.renames, tc.want)
			}
		})
	}
}

func TestAppRenameFailureDoesNotSave(t *testing.T) {
	f := newAppFixture(t, "agterm", Target{ID: "window-2", Label: "tab", Scope: "sock"})
	f.backend.renameErr = errors.New("rename failed")
	path := state.FilePath(f.stateHome, "agterm", "sock")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 1 {
		t.Fatalf("Run() = %d", code)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state written after rename failure: %v", err)
	}
}

func TestAppRefreshesStatusAggregateAfterSavingCanonicalState(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "tab-1", Label: "tab", Scope: "session"})
	backend := &aggregatingBackend{fakeBackend: f.backend}
	canonical := state.FilePath(f.stateHome, "herdr", "session")
	backend.onRefresh = func() {
		if _, err := os.Stat(canonical); err != nil {
			t.Fatalf("canonical state was not saved before aggregate refresh: %v", err)
		}
	}
	f.app.Backend = backend

	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if backend.refreshes != 1 {
		t.Fatalf("aggregate refreshes = %d, want 1", backend.refreshes)
	}
	if got, want := backend.events, []string{"resolve", "rename", "refresh"}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestAppSyncRefreshesAllStatusAggregates(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "tab-1", Label: "tab", Scope: "session"})
	backend := &aggregatingBackend{fakeBackend: f.backend}
	f.app.Backend = backend

	if code := f.app.Run(context.Background(), []string{"sync"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if backend.allRefreshes != 1 || backend.refreshes != 0 {
		t.Fatalf("aggregate refreshes: all=%d current=%d, want all=1 current=0", backend.allRefreshes, backend.refreshes)
	}
	if got, want := backend.events, []string{"resolve", "refresh-all"}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestAppMutationSavesAndRekeysWhenRenameIsUnnecessary(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "new-id", Label: "🔨 renamed", Scope: "session"})
	path := state.FilePath(f.stateHome, "zellij", "session")
	s := state.New()
	s.Entries["old-id"] = state.Entry{Base: "renamed", Status: "wip"}
	if err := state.Save(path, s); err != nil {
		t.Fatal(err)
	}
	if code := f.app.Run(context.Background(), []string{"sync"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if len(f.backend.renames) != 0 {
		t.Fatalf("unexpected rename: %v", f.backend.renames)
	}
	got, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Entries["old-id"]; ok {
		t.Fatalf("old ID remained: %#v", got)
	}
	if got.Entries["new-id"].Status != "wip" {
		t.Fatalf("new ID missing: %#v", got)
	}
}

func TestAppStatusUsesStoredStateAndCurrentManualBaseWithoutWriting(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "pane-1", Label: "🔨 manually renamed", Scope: "workspace"})
	path := state.FilePath(f.stateHome, "herdr", "workspace")
	s := state.New()
	s.Entries["pane-1"] = state.Entry{Base: "old base", Status: "review", Comment: "needs eyes"}
	if err := state.Save(path, s); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := f.app.Run(context.Background(), []string{"status"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if got, want := f.stdout.String(), "review manually renamed — needs eyes\n"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(f.backend.renames) != 0 {
		t.Fatal("status performed a write or rename")
	}
}

func TestAppCommentMappingSetAndClear(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "1", Label: "base", Scope: "work"})
	if code := f.app.Run(context.Background(), []string{"comment", "hello", "world"}); code != 0 {
		t.Fatalf("set comment = %d, %q", code, f.stderr.String())
	}
	f.backend.target.Label = "💬 base"
	if code := f.app.Run(context.Background(), []string{"comment"}); code != 0 {
		t.Fatalf("clear comment = %d, %q", code, f.stderr.String())
	}
	if got := f.backend.renames; len(got) != 2 || got[0] != "💬 base" || got[1] != "base" {
		t.Fatalf("comment renames = %q", got)
	}
}

func TestAppUsesHomeDirFallbackWhenXDGVariablesAreAbsent(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
	home := filepath.Join(f.root, "fallback-home")
	f.app.Env = func(string) string { return "" }
	f.app.HomeDir = func() (string, error) { return home, nil }
	writeAppFile(t, filepath.Join(home, ".config", "icomark", "config.yml"), "statuses:\n- {name: none, prefix: ''}\n- {name: home, prefix: 'H '}\n")

	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if len(f.backend.renames) != 1 || f.backend.renames[0] != "H tab" {
		t.Fatalf("fallback config not loaded: %q", f.backend.renames)
	}
	path := state.FilePath(filepath.Join(home, ".local", "state"), "herdr", "scope")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fallback state not written: %v", err)
	}
}

func TestAppFallsBackOnlyForMissingXDGHome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configXDG bool
		stateXDG  bool
	}{
		{name: "config XDG and state fallback", configXDG: true},
		{name: "config fallback and state XDG", stateXDG: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
			home := filepath.Join(f.root, "home")
			xdgConfig := filepath.Join(f.root, "xdg-config")
			xdgState := filepath.Join(f.root, "xdg-state")
			f.app.Env = func(key string) string {
				if key == "XDG_CONFIG_HOME" && tc.configXDG {
					return xdgConfig
				}
				if key == "XDG_STATE_HOME" && tc.stateXDG {
					return xdgState
				}
				return ""
			}
			f.app.HomeDir = func() (string, error) { return home, nil }
			configHome := filepath.Join(home, ".config")
			if tc.configXDG {
				configHome = xdgConfig
			}
			stateHome := filepath.Join(home, ".local", "state")
			if tc.stateXDG {
				stateHome = xdgState
			}
			writeAppFile(t, filepath.Join(configHome, "icomark", "config.yml"), "statuses:\n- {name: none, prefix: ''}\n- {name: mixed, prefix: 'M '}\n")

			if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
				t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
			}
			if len(f.backend.renames) != 1 || f.backend.renames[0] != "M tab" {
				t.Fatalf("selected wrong config home: %q", f.backend.renames)
			}
			if _, err := os.Stat(state.FilePath(stateHome, "herdr", "scope")); err != nil {
				t.Fatalf("selected wrong state home: %v", err)
			}
		})
	}
}

func TestAppAgtermLegacyConfigFallback(t *testing.T) {
	f := newAppFixture(t, "agterm", Target{ID: "1", Label: "tab", Scope: "scope"})
	configHome := filepath.Dir(filepath.Dir(f.config))
	writeAppFile(t, filepath.Join(configHome, "atabs", "config.yml"), "statuses:\n- {name: none, prefix: ''}\n- {name: legacy, prefix: 'A '}\n")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if len(f.backend.renames) != 1 || f.backend.renames[0] != "A tab" {
		t.Fatalf("agterm legacy config not loaded: %q", f.backend.renames)
	}
}

func TestAppConfigPrecedenceAndLegacyBackendFallback(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "1", Label: "tab", Scope: "session"})
	legacyConfig := filepath.Join(filepath.Dir(filepath.Dir(f.config)), "ztabs", "config.yml")
	writeAppFile(t, legacyConfig, "statuses:\n- {name: none, prefix: ''}\n- {name: legacy, prefix: 'L '}\n")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("legacy config run = %d, %q", code, f.stderr.String())
	}
	if got := f.backend.renames[0]; got != "L tab" {
		t.Fatalf("legacy config label = %q", got)
	}

	f.backend.target.Label = "tab"
	if err := os.Remove(state.FilePath(f.stateHome, "zellij", "session")); err != nil {
		t.Fatal(err)
	}
	writeAppFile(t, f.config, "statuses:\n- {name: none, prefix: ''}\n- {name: canonical, prefix: 'C '}\n")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("canonical config run = %d, %q", code, f.stderr.String())
	}
	if got := f.backend.renames[1]; got != "C tab" {
		t.Fatalf("canonical config label = %q", got)
	}
}

func TestAppExplicitMissingConfigErrors(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "1", Label: "tab", Scope: "session"})
	missing := filepath.Join(f.root, "missing.yml")
	if code := f.app.Run(context.Background(), []string{"next", "--config", missing}); code != 1 {
		t.Fatalf("Run() = %d", code)
	}
	if len(f.backend.events) != 1 || f.backend.events[0] != "resolve" {
		t.Fatalf("target was not resolved first: %v", f.backend.events)
	}
}

func TestAppLoadsLegacyStateOnlyWhenCanonicalMissingAndWritesCanonical(t *testing.T) {
	for _, tc := range []struct {
		backend string
		dir     string
		payload string
	}{
		{backend: "zellij", dir: "ztabs", payload: `{"tab":{"status":"review","comment":"old"}}`},
		{backend: "agterm", dir: "atabs", payload: `{"legacy-id":{"base":"tab","status":"review","comment":"old"}}`},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			f := newAppFixture(t, tc.backend, Target{ID: "legacy-id", Label: "tab", Scope: "scope-a"})
			legacy := filepath.Join(f.stateHome, tc.dir, "scope-a.json")
			writeAppFile(t, legacy, tc.payload)
			if code := f.app.Run(context.Background(), []string{"sync"}); code != 0 {
				t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
			}
			if got := f.backend.renames; len(got) != 1 || got[0] != "👀 💬 tab" {
				t.Fatalf("legacy rename = %q", got)
			}
			canonical := state.FilePath(f.stateHome, tc.backend, "scope-a")
			if _, err := os.Stat(canonical); err != nil {
				t.Fatalf("canonical state not written: %v", err)
			}
			if data, _ := os.ReadFile(legacy); string(data) != tc.payload {
				t.Fatal("legacy state was modified")
			}
		})
	}
}

func TestAppLegacyStatePathSanitizesScopeAndStateKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scope    string
		args     []string
		filename string
	}{
		{name: "scope", scope: "../../escape", args: []string{"sync"}, filename: ".._.._escape.json"},
		{name: "state key", scope: "safe", args: []string{"sync", "--state-key", "../../override"}, filename: ".._.._override.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t, "zellij", Target{ID: "1", Label: "tab", Scope: tc.scope})
			legacy := filepath.Join(f.stateHome, "ztabs", tc.filename)
			writeAppFile(t, legacy, `{"tab":{"status":"review","comment":"safe"}}`)
			if code := f.app.Run(context.Background(), tc.args); code != 0 {
				t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
			}
			if len(f.backend.renames) != 1 || f.backend.renames[0] != "👀 💬 tab" {
				t.Fatalf("sanitized legacy state not loaded: %q", f.backend.renames)
			}
		})
	}
}

func TestAppHerdrDoesNotLoadLegacyState(t *testing.T) {
	f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
	writeAppFile(t, filepath.Join(f.stateHome, "htabs", "scope.json"), "not-json")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if len(f.backend.renames) != 1 || f.backend.renames[0] != "🔨 tab" {
		t.Fatalf("Herdr legacy state affected result: %q", f.backend.renames)
	}
}

func TestAppCanonicalStateTakesPrecedenceOverLegacy(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "1", Label: "tab", Scope: "scope"})
	canonical := state.FilePath(f.stateHome, "zellij", "scope")
	s := state.New()
	s.Entries["1"] = state.Entry{Base: "tab", Status: "review", Comment: "canonical"}
	if err := state.Save(canonical, s); err != nil {
		t.Fatal(err)
	}
	writeAppFile(t, filepath.Join(f.stateHome, "ztabs", "scope.json"), "not-json")

	if code := f.app.Run(context.Background(), []string{"sync"}); code != 0 {
		t.Fatalf("Run() = %d, stderr %q", code, f.stderr.String())
	}
	if len(f.backend.renames) != 1 || f.backend.renames[0] != "👀 💬 tab" {
		t.Fatalf("canonical state did not win: %q", f.backend.renames)
	}
}

func TestAppFailuresReturnOne(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
		writeAppFile(t, f.config, "statuses: [")
		if code := f.app.Run(context.Background(), []string{"next"}); code != 1 {
			t.Fatalf("Run() = %d", code)
		}
	})

	t.Run("state", func(t *testing.T) {
		f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
		writeAppFile(t, state.FilePath(f.stateHome, "herdr", "scope"), "not-json")
		if code := f.app.Run(context.Background(), []string{"next"}); code != 1 {
			t.Fatalf("Run() = %d", code)
		}
	})

	t.Run("backend", func(t *testing.T) {
		f := newAppFixture(t, "herdr", Target{})
		f.backend.resolveErr = errors.New("backend unavailable")
		if code := f.app.Run(context.Background(), []string{"next"}); code != 1 {
			t.Fatalf("Run() = %d", code)
		}
	})

	t.Run("planner", func(t *testing.T) {
		f := newAppFixture(t, "herdr", Target{ID: "1", Label: "tab", Scope: "scope"})
		if code := f.app.Run(context.Background(), []string{"set", "unknown"}); code != 1 {
			t.Fatalf("Run() = %d", code)
		}
	})
}

func TestAppMalformedLegacyStateErrorsAfterResolve(t *testing.T) {
	f := newAppFixture(t, "zellij", Target{ID: "1", Label: "tab", Scope: "scope"})
	writeAppFile(t, filepath.Join(f.stateHome, "ztabs", "scope.json"), "not-json")
	if code := f.app.Run(context.Background(), []string{"next"}); code != 1 {
		t.Fatalf("Run() = %d", code)
	}
	if len(f.backend.events) != 1 || f.backend.events[0] != "resolve" {
		t.Fatalf("events = %v", f.backend.events)
	}
}

func writeAppFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
