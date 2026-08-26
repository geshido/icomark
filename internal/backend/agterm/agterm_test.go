package agterm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geshido/icomark/internal/app"
)

type runnerCall struct {
	name string
	args []string
	env  map[string]string
}

type fakeRunner struct {
	output []byte
	err    error
	calls  []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...), env: cloneMap(env)})
	return r.output, r.err
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

const treeJSON = `{
  "ok": true,
  "result": {
    "tree": {
      "window_id": "tree-window",
      "workspaces": [
        {"id":"workspace-1","name":"First","sessions":[
          {"id":"session-alpha","name":"Alpha","active":false},
          {"id":"session-alpine","name":"Alpine","active":false}
        ]},
        {"id":"workspace-2","name":"Second","sessions":[
          {"id":"session-beta","name":"Beta","active":true}
        ]}
      ]
    }
  }
}`

func TestName(t *testing.T) {
	if got := New().Name(); got != "agterm" {
		t.Fatalf("Name() = %q, want agterm", got)
	}
}

func TestResolveUsesExplicitTargetAndSocket(t *testing.T) {
	runner := &fakeRunner{output: []byte(treeJSON)}
	backend := &Backend{
		Runner: runner,
		Env: env(map[string]string{
			"AGTERM_SESSION_ID": "session-beta",
			"AGTERM_WINDOW_ID":  "env-window",
		}),
	}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{
		Target:   "session-alpha",
		Socket:   "/tmp/agterm.sock",
		StateKey: "chosen-scope",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := app.Target{ID: "session-alpha", Label: "Alpha", Scope: "chosen-scope"}
	if target != want {
		t.Fatalf("Resolve() = %#v, want %#v", target, want)
	}
	assertSingleCall(t, runner, "agtermctl", []string{"tree", "--json", "--socket", "/tmp/agterm.sock"}, nil)
}

func TestResolveTargetSelection(t *testing.T) {
	tests := []struct {
		name    string
		options app.BackendOptions
		env     map[string]string
		want    app.Target
	}{
		{
			name: "environment session before active",
			env:  map[string]string{"AGTERM_SESSION_ID": "session-alpha"},
			want: app.Target{ID: "session-alpha", Label: "Alpha", Scope: "tree-window"},
		},
		{
			name:    "unique case insensitive prefix",
			options: app.BackendOptions{Target: "SESSION-B"},
			want:    app.Target{ID: "session-beta", Label: "Beta", Scope: "tree-window"},
		},
		{
			name: "active session fallback",
			want: app.Target{ID: "session-beta", Label: "Beta", Scope: "tree-window"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{output: []byte(treeJSON)}
			backend := &Backend{Runner: runner, Env: env(test.env)}
			got, err := backend.Resolve(context.Background(), test.options)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Resolve() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestResolveRejectsMissingAndAmbiguousTargets(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		options     app.BackendOptions
		wantInError string
	}{
		{name: "unknown", json: treeJSON, options: app.BackendOptions{Target: "missing"}, wantInError: "no session matches"},
		{name: "ambiguous prefix", json: treeJSON, options: app.BackendOptions{Target: "session-al"}, wantInError: "ambiguous"},
		{
			name:        "no active",
			json:        `{"ok":true,"result":{"tree":{"workspaces":[{"id":"w","name":"W","sessions":[{"id":"s","name":"S","active":false}]}]}}}`,
			wantInError: "no active session",
		},
		{
			name:        "multiple active",
			json:        `{"ok":true,"result":{"tree":{"workspaces":[{"id":"w","name":"W","sessions":[{"id":"s1","name":"S1","active":true},{"id":"s2","name":"S2","active":true}]}]}}}`,
			wantInError: "multiple active sessions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{Runner: &fakeRunner{output: []byte(test.json)}, Env: env(nil)}
			_, err := backend.Resolve(context.Background(), test.options)
			if err == nil || !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.wantInError)
			}
		})
	}
}

func TestResolveScopePrecedence(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		options app.BackendOptions
		env     map[string]string
		want    string
	}{
		{name: "state key", json: treeJSON, options: app.BackendOptions{StateKey: "state"}, env: map[string]string{"AGTERM_WINDOW_ID": "environment"}, want: "state"},
		{name: "environment window", json: treeJSON, env: map[string]string{"AGTERM_WINDOW_ID": "environment"}, want: "environment"},
		{name: "tree window", json: treeJSON, want: "tree-window"},
		{name: "default", json: `{"ok":true,"result":{"tree":{"workspaces":[{"id":"w","name":"W","sessions":[{"id":"s","name":"S","active":true}]}]}}}`, want: "default"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{Runner: &fakeRunner{output: []byte(test.json)}, Env: env(test.env)}
			target, err := backend.Resolve(context.Background(), test.options)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if target.Scope != test.want {
				t.Fatalf("Scope = %q, want %q", target.Scope, test.want)
			}
		})
	}
}

func TestResolveReportsCommandAndJSONErrors(t *testing.T) {
	tests := []struct {
		name        string
		runner      *fakeRunner
		wantInError []string
	}{
		{name: "command", runner: &fakeRunner{output: []byte("daemon unavailable\n"), err: errors.New("exit status 1")}, wantInError: []string{"agtermctl tree", "daemon unavailable", "exit status 1"}},
		{name: "invalid json", runner: &fakeRunner{output: []byte("not json")}, wantInError: []string{"decode", "not json"}},
		{name: "api error", runner: &fakeRunner{output: []byte(`{"ok":false,"error":"socket refused"}`)}, wantInError: []string{"socket refused"}},
		{name: "missing tree", runner: &fakeRunner{output: []byte(`{"ok":true,"result":{}}`)}, wantInError: []string{"missing result.tree"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{Runner: test.runner, Env: env(nil)}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{})
			if err == nil {
				t.Fatal("Resolve() error = nil")
			}
			for _, want := range test.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Resolve() error = %q, want containing %q", err, want)
				}
			}
		})
	}
}

func TestRenameUsesExactArgumentsAndReportsOutput(t *testing.T) {
	runner := &fakeRunner{}
	backend := &Backend{Runner: runner, Env: env(nil)}
	target := app.Target{ID: "session-alpha", Label: "Alpha"}

	if err := backend.Rename(context.Background(), target, "👀 Alpha", app.BackendOptions{Socket: "/tmp/agterm.sock"}); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	assertSingleCall(t, runner, "agtermctl", []string{"session", "rename", "👀 Alpha", "--target", "session-alpha", "--socket", "/tmp/agterm.sock"}, nil)

	runner.err = errors.New("exit status 7")
	runner.output = []byte("rename denied\n")
	err := backend.Rename(context.Background(), target, "New", app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "rename denied") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("Rename() error = %v, want command error and output", err)
	}
}

func assertSingleCall(t *testing.T, runner *fakeRunner, name string, args []string, env map[string]string) {
	t.Helper()
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.name != name || !reflect.DeepEqual(call.args, args) || !reflect.DeepEqual(call.env, env) {
		t.Fatalf("runner call = %#v, want name=%q args=%#v env=%#v", call, name, args, env)
	}
}
