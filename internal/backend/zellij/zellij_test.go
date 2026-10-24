package zellij

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

type runnerResult struct {
	output []byte
	err    error
}

type fakeRunner struct {
	results []runnerResult
	calls   []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...), env: cloneMap(env)})
	if len(r.results) == 0 {
		return nil, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result.output, result.err
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

func testEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestName(t *testing.T) {
	if got := New().Name(); got != "zellij" {
		t.Fatalf("Name() = %q, want zellij", got)
	}
}

func TestResolveExplicitTargetUsesExactListCommand(t *testing.T) {
	runner := &fakeRunner{results: []runnerResult{{output: []byte("TAB_ID POSITION NAME\n1 0 First\n2 1 🚧 Project alpha 日本語\n")}}}
	backend := &Backend{Runner: runner, Env: testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "environment-session"})}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{Target: "2", StateKey: "explicit-scope"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := app.Target{ID: "2", Label: "🚧 Project alpha 日本語", Scope: "explicit-scope"}
	if target != want {
		t.Fatalf("Resolve() = %#v, want %#v", target, want)
	}
	assertCalls(t, runner.calls, []runnerCall{{name: "zellij", args: []string{"action", "list-tabs"}}})
}

func TestResolveCurrentTabFromCurrentInfo(t *testing.T) {
	runner := &fakeRunner{results: []runnerResult{
		{output: []byte("name: Design review notes ✨\nid: 7\nposition: 1\n")},
		{output: []byte("\n1 0 Alpha\n7 1 Design review notes ✨\n")},
	}}
	backend := &Backend{Runner: runner, Env: testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"})}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := app.Target{ID: "7", Label: "Design review notes ✨", Scope: "work"}
	if target != want {
		t.Fatalf("Resolve() = %#v, want %#v", target, want)
	}
	assertCalls(t, runner.calls, []runnerCall{
		{name: "zellij", args: []string{"action", "current-tab-info"}},
		{name: "zellij", args: []string{"action", "list-tabs"}},
	})
}

func TestResolveScopePrecedence(t *testing.T) {
	tests := []struct {
		name    string
		options app.BackendOptions
		env     map[string]string
		want    string
	}{
		{name: "state key", options: app.BackendOptions{Target: "1", StateKey: "chosen"}, env: map[string]string{"ZELLIJ_SESSION_NAME": "session"}, want: "chosen"},
		{name: "session environment", options: app.BackendOptions{Target: "1"}, env: map[string]string{"ZELLIJ_SESSION_NAME": "session"}, want: "session"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{results: []runnerResult{{output: []byte("1 0 Alpha\n")}}}
			backend := &Backend{Runner: runner, Env: testEnv(test.env)}
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

func TestResolveRequiresSessionScope(t *testing.T) {
	runner := &fakeRunner{}
	backend := &Backend{Runner: runner, Env: testEnv(nil)}

	_, err := backend.Resolve(context.Background(), app.BackendOptions{Target: "1"})
	if err == nil || !strings.Contains(err.Error(), "--state-key") || !strings.Contains(err.Error(), "ZELLIJ_SESSION_NAME") {
		t.Fatalf("Resolve() error = %v, want actionable missing scope error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestResolveRejectsMalformedListOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantInError string
	}{
		{name: "missing name", output: "1 0\n", wantInError: "malformed list-tabs row"},
		{name: "non-numeric id", output: "one 0 Alpha\n", wantInError: "malformed tab id"},
		{name: "non-numeric position", output: "1 first Alpha\n", wantInError: "malformed tab position"},
		{name: "duplicate id", output: "1 0 Alpha\n1 1 Beta\n", wantInError: "duplicate tab id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{
				Runner: &fakeRunner{results: []runnerResult{{output: []byte(test.output)}}},
				Env:    testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"}),
			}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{Target: "1"})
			if err == nil || !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.wantInError)
			}
		})
	}
}

func TestResolveRejectsInvalidOrMissingExplicitTarget(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		wantInError string
	}{
		{name: "not numeric", target: "Alpha", wantInError: "numeric tab id"},
		{name: "not found", target: "9", wantInError: "tab id 9 not found"},
		{name: "not exact", target: "02", wantInError: "tab id 02 not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{
				Runner: &fakeRunner{results: []runnerResult{{output: []byte("2 0 Alpha\n")}}},
				Env:    testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"}),
			}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{Target: test.target})
			if err == nil || !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.wantInError)
			}
		})
	}
}

func TestResolveRejectsMalformedCurrentTabInfo(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantInError string
	}{
		{name: "missing id", output: "name: Alpha\nposition: 0\n", wantInError: "missing id"},
		{name: "invalid id", output: "id: nope\n", wantInError: "malformed current-tab-info id"},
		{name: "duplicate id", output: "id: 1\nid: 2\n", wantInError: "multiple id fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{
				Runner: &fakeRunner{results: []runnerResult{
					{output: []byte(test.output)},
				}},
				Env: testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"}),
			}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{})
			if err == nil || !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.wantInError)
			}
		})
	}
}

func TestResolveRequiresCurrentIDToExistInList(t *testing.T) {
	backend := &Backend{
		Runner: &fakeRunner{results: []runnerResult{
			{output: []byte("id: 8\n")},
			{output: []byte("1 0 Alpha\n")},
		}},
		Env: testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"}),
	}
	_, err := backend.Resolve(context.Background(), app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "tab id 8 not found") {
		t.Fatalf("Resolve() error = %v, want missing tab id error", err)
	}
}

func TestResolveReportsCommandErrorsWithCombinedOutput(t *testing.T) {
	tests := []struct {
		name        string
		results     []runnerResult
		wantCommand string
	}{
		{
			name: "list tabs",
			results: []runnerResult{
				{output: []byte("id: 1\n")},
				{output: []byte("list unavailable\n"), err: errors.New("exit status 1")},
			},
			wantCommand: "zellij action list-tabs",
		},
		{
			name: "current tab info",
			results: []runnerResult{
				{output: []byte("session closed\n"), err: errors.New("exit status 2")},
			},
			wantCommand: "zellij action current-tab-info",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{Runner: &fakeRunner{results: test.results}, Env: testEnv(map[string]string{"ZELLIJ_SESSION_NAME": "work"})}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{})
			if err == nil || !strings.Contains(err.Error(), test.wantCommand) || !strings.Contains(err.Error(), "exit status") {
				t.Fatalf("Resolve() error = %v, want command and underlying error", err)
			}
			wantOutput := "list unavailable"
			if test.name == "current tab info" {
				wantOutput = "session closed"
			}
			if !strings.Contains(err.Error(), wantOutput) {
				t.Fatalf("Resolve() error = %v, want output %q", err, wantOutput)
			}
		})
	}
}

func TestRenameUsesExactArgumentsAndReportsOutput(t *testing.T) {
	runner := &fakeRunner{results: []runnerResult{{}}}
	backend := &Backend{Runner: runner, Env: testEnv(nil)}
	target := app.Target{ID: "7", Label: "Old"}

	if err := backend.Rename(context.Background(), target, "👀 Design review", app.BackendOptions{}); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	assertCalls(t, runner.calls, []runnerCall{{name: "zellij", args: []string{"action", "rename-tab-by-id", "7", "👀 Design review"}}})

	runner.calls = nil
	runner.results = []runnerResult{{output: []byte("permission denied\n"), err: errors.New("exit status 3")}}
	err := backend.Rename(context.Background(), target, "New", app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("Rename() error = %v, want command error and combined output", err)
	}
}

func assertCalls(t *testing.T, got, want []runnerCall) {
	t.Helper()
	for i := range want {
		if want[i].env == nil {
			want[i].env = nil
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runner calls = %#v, want %#v", got, want)
	}
}
