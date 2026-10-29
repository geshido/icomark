package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/state"
)

type runnerCall struct {
	name string
	args []string
	env  map[string]string
}

type fakeRunner struct {
	output  string
	outputs []string
	err     error
	errs    []error
	calls   []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name, append([]string(nil), args...), env})
	output, err := r.output, r.err
	if len(r.outputs) != 0 {
		output, r.outputs = r.outputs[0], r.outputs[1:]
	}
	if len(r.errs) != 0 {
		err, r.errs = r.errs[0], r.errs[1:]
	}
	return []byte(output), err
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

const windowJSON = `{"session_id":"session:one","window_id":"window:two","label":"Payments","layers":[]}`

func TestName(t *testing.T) {
	if got := New().Name(); got != "rex" {
		t.Fatalf("Name() = %q, want rex", got)
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options app.BackendOptions
		env     map[string]string
		binary  string
		args    []string
		wantEnv map[string]string
		scope   string
	}{
		{
			name:   "delegate current block selection to Rex",
			env:    map[string]string{"REX_SESSION": "session:one", "REX_BLOCK": "block:own"},
			binary: "rex", args: []string{"window", "inspect", "--json"}, scope: "session:one",
		},
		{
			name:    "explicit selector and server with independent state override",
			options: app.BackendOptions{Target: "Review", Socket: "unix:///tmp/explicit.sock", StateKey: "stable"},
			env:     map[string]string{"REX_SERVER": "unix:///tmp/other.sock", "REX_BLOCK": "block:own"},
			binary:  "rex", args: []string{"window", "inspect", "--json", "--", "Review"},
			wantEnv: map[string]string{"REX_SERVER": "unix:///tmp/explicit.sock"}, scope: "stable",
		},
		{
			name:   "matching binary outside PATH",
			env:    map[string]string{"REX_BIN_DIR": "/Applications/Rex Server.app/Contents/MacOS"},
			binary: "/Applications/Rex Server.app/Contents/MacOS/rex", args: []string{"window", "inspect", "--json"}, scope: "session:one",
		},
		{
			name:    "literal target starting with dash",
			options: app.BackendOptions{Target: "--review"},
			binary:  "rex", args: []string{"window", "inspect", "--json", "--", "--review"}, scope: "session:one",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{output: windowJSON}
			backend := &Backend{Runner: runner, Env: env(tc.env)}
			got, err := backend.Resolve(context.Background(), tc.options)
			if err != nil {
				t.Fatal(err)
			}
			want := app.Target{ID: "window:two", Label: "Payments", Scope: tc.scope, GroupID: "session:one"}
			if got != want {
				t.Fatalf("Resolve() = %#v, want %#v", got, want)
			}
			assertCall(t, runner, runnerCall{name: tc.binary, args: tc.args, env: tc.wantEnv})
		})
	}
}

func TestResolveRejectsMalformedResponse(t *testing.T) {
	for _, output := range []string{
		"not json", `null`, `{}`, `{"session_id":"s","window_id":"w"}`,
		`{"session_id":"s","label":"L"}`, `{"window_id":"w","label":"L"}`,
		`{"session_id":"s","window_id":"w","label":null}`,
		`{"session_id":"s","window_id":"w","label":42}`,
	} {
		t.Run(output, func(t *testing.T) {
			backend := &Backend{Runner: &fakeRunner{output: output}, Env: env(nil)}
			if _, err := backend.Resolve(context.Background(), app.BackendOptions{}); err == nil {
				t.Fatal("Resolve() accepted malformed response")
			}
		})
	}
}

func TestRenameUsesResolvedSessionAndLiteralLabel(t *testing.T) {
	runner := &fakeRunner{}
	backend := &Backend{Runner: runner, Env: env(map[string]string{"REX_SESSION": "session:wrong"})}
	target := app.Target{ID: "window:two", GroupID: "session:one", Scope: "not-a-session"}
	err := backend.Rename(context.Background(), target, "--label 'with quotes' 👀", app.BackendOptions{Socket: "server:1234"})
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, runner, runnerCall{name: "rex", args: []string{
		"window", "rename", "-s", "session:one", "--", "window:two", "--label 'with quotes' 👀",
	}, env: map[string]string{"REX_SERVER": "server:1234"}})
}

func TestRenameRequiresResolvedIDs(t *testing.T) {
	for _, target := range []app.Target{{}, {ID: "window:two"}, {GroupID: "session:one"}} {
		runner := &fakeRunner{}
		backend := &Backend{Runner: runner, Env: env(nil)}
		if err := backend.Rename(context.Background(), target, "Review", app.BackendOptions{}); err == nil {
			t.Fatal("Rename() accepted missing IDs")
		}
		if len(runner.calls) != 0 {
			t.Fatal("Rename() ran a command with missing IDs")
		}
	}
}

func TestCommandErrors(t *testing.T) {
	for _, rename := range []bool{false, true} {
		runner := &fakeRunner{output: "target ambiguous\n", err: errors.New("exit status 3")}
		backend := &Backend{Runner: runner, Env: env(nil)}
		var err error
		if rename {
			err = backend.Rename(context.Background(), app.Target{ID: "w", GroupID: "s"}, "L", app.BackendOptions{})
		} else {
			_, err = backend.Resolve(context.Background(), app.BackendOptions{})
		}
		if err == nil || !errors.Is(err, runner.err) || !strings.Contains(err.Error(), "target ambiguous") {
			t.Fatalf("command error = %v, want cause and output", err)
		}
	}
}

func TestAppPersistsRexStateOnlyAfterSuccessfulRename(t *testing.T) {
	for _, renameFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rename failure"}[renameFails], func(t *testing.T) {
			home := t.TempDir()
			var stderr bytes.Buffer
			runner := &appRunner{failRename: renameFails}
			application := app.New("rtabs", &Backend{Runner: runner, Env: env(nil)})
			application.Env = env(map[string]string{"XDG_CONFIG_HOME": home, "XDG_STATE_HOME": home})
			application.Stderr = &stderr
			code := application.Run(context.Background(), []string{"set", "review"})
			path := state.FilePath(home, "rex", "session:one")
			stored, err := state.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if renameFails {
				if code != 1 || len(stored.Entries) != 0 {
					t.Fatalf("failed rename exit=%d state=%#v stderr=%s", code, stored, &stderr)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("state file exists after failed rename: %v", err)
				}
				return
			}
			if code != 0 || stored.Entries["window:two"].Status != "review" || runner.label != "👀 Payments" || runner.sessionLabel != "👀 Work" {
				t.Fatalf("exit=%d state=%#v label=%q stderr=%s", code, stored, runner.label, &stderr)
			}
		})
	}
}

func TestAppSummaryFailureKeepsSavedTabState(t *testing.T) {
	home := t.TempDir()
	var stderr bytes.Buffer
	runner := &appRunner{failSummary: true}
	application := app.New("rtabs", &Backend{Runner: runner, Env: env(nil)})
	application.Env = env(map[string]string{"XDG_CONFIG_HOME": home, "XDG_STATE_HOME": home})
	application.Stderr = &stderr
	code := application.Run(context.Background(), []string{"set", "review"})
	stored, err := state.Load(state.FilePath(home, "rex", "session:one"))
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 || runner.label != "👀 Payments" || stored.Entries["window:two"].Status != "review" || runner.sessionLabel != "" {
		t.Fatalf("exit=%d state=%#v runner=%#v", code, stored, runner)
	}
	if !strings.Contains(stderr.String(), "refresh status aggregate") {
		t.Fatalf("summary failure not reported: %s", &stderr)
	}
}

type appRunner struct {
	failRename   bool
	failSummary  bool
	label        string
	sessionLabel string
}

func (r *appRunner) Run(_ context.Context, _ string, args []string, _ map[string]string) ([]byte, error) {
	if args[0] == "session" {
		if args[1] == "inspect" {
			return json.Marshal(map[string]any{
				"session_id": "session:one", "label": "Work",
				"windows": []map[string]string{{"label": r.label}},
			})
		}
		if r.failSummary {
			return []byte("session rename denied"), errors.New("exit status 1")
		}
		r.sessionLabel = args[len(args)-1]
		return nil, nil
	}
	if args[1] == "inspect" {
		return []byte(windowJSON), nil
	}
	if r.failRename {
		return []byte("label too long"), errors.New("exit status 1")
	}
	r.label = args[len(args)-1]
	return nil, nil
}

func assertCall(t *testing.T, runner *fakeRunner, want runnerCall) {
	t.Helper()
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}
