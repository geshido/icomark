package herdr

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/core"
)

type runnerCall struct {
	name string
	args []string
	env  map[string]string
}

type fakeRunner struct {
	output  []byte
	outputs [][]byte
	err     error
	errs    []error
	calls   []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, env map[string]string) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...), env: cloneMap(env)})
	callErr := r.err
	if len(r.errs) > 0 {
		callErr = r.errs[0]
		r.errs = r.errs[1:]
	}
	if len(r.outputs) > 0 {
		output := r.outputs[0]
		r.outputs = r.outputs[1:]
		return output, callErr
	}
	return r.output, callErr
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

const tabJSON = `{"id":"request-1","result":{"tab":{"tab_id":"tab-42","label":"Payments","workspace_id":"workspace-7","focused":true},"type":"tab_info"}}`

func TestName(t *testing.T) {
	if got := New().Name(); got != "herdr" {
		t.Fatalf("Name() = %q, want herdr", got)
	}
}

func TestResolveUsesExplicitTargetSocketAndStateKey(t *testing.T) {
	runner := &fakeRunner{output: []byte(tabJSON)}
	backend := &Backend{
		Runner: runner,
		Env: env(map[string]string{
			"HERDR_TAB_ID":      "environment-tab",
			"HERDR_SOCKET_PATH": "/tmp/environment.sock",
		}),
	}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{
		Target:   "tab-42",
		Socket:   "/tmp/explicit.sock",
		StateKey: "state-scope",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := app.Target{ID: "tab-42", Label: "Payments", Scope: "state-scope", GroupID: "workspace-7"}
	if target != want {
		t.Fatalf("Resolve() = %#v, want %#v", target, want)
	}
	assertSingleCall(t, runner, "herdr", []string{"tab", "get", "tab-42"}, map[string]string{"HERDR_SOCKET_PATH": "/tmp/explicit.sock"})
}

func TestResolveUsesEnvironmentTargetWithoutGuessing(t *testing.T) {
	runner := &fakeRunner{output: []byte(tabJSON)}
	backend := &Backend{Runner: runner, Env: env(map[string]string{"HERDR_TAB_ID": "tab-42"})}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.ID != "tab-42" || target.Label != "Payments" {
		t.Fatalf("Resolve() = %#v", target)
	}
	assertSingleCall(t, runner, "herdr", []string{"tab", "get", "tab-42"}, nil)
}

func TestResolveUsesActiveTargetFromCustomCommandEnvironment(t *testing.T) {
	runner := &fakeRunner{output: []byte(tabJSON)}
	backend := &Backend{Runner: runner, Env: env(map[string]string{"HERDR_ACTIVE_TAB_ID": "tab-42"})}

	target, err := backend.Resolve(context.Background(), app.BackendOptions{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.ID != "tab-42" || target.Label != "Payments" {
		t.Fatalf("Resolve() = %#v", target)
	}
	assertSingleCall(t, runner, "herdr", []string{"tab", "get", "tab-42"}, nil)
}

func TestResolveRequiresTarget(t *testing.T) {
	runner := &fakeRunner{output: []byte(tabJSON)}
	backend := &Backend{Runner: runner, Env: env(nil)}

	_, err := backend.Resolve(context.Background(), app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "target") || !strings.Contains(err.Error(), "HERDR_ACTIVE_TAB_ID") {
		t.Fatalf("Resolve() error = %v, want clear missing target error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestResolveScopePrecedenceAndSocketForwarding(t *testing.T) {
	tests := []struct {
		name    string
		options app.BackendOptions
		env     map[string]string
		want    string
		wantEnv map[string]string
	}{
		{name: "state key", options: app.BackendOptions{Target: "tab-42", StateKey: "state", Socket: "/tmp/option.sock"}, env: map[string]string{"HERDR_SOCKET_PATH": "/tmp/environment.sock"}, want: "state", wantEnv: map[string]string{"HERDR_SOCKET_PATH": "/tmp/option.sock"}},
		{name: "option socket", options: app.BackendOptions{Target: "tab-42", Socket: "/tmp/option.sock"}, env: map[string]string{"HERDR_SOCKET_PATH": "/tmp/environment.sock"}, want: "/tmp/option.sock", wantEnv: map[string]string{"HERDR_SOCKET_PATH": "/tmp/option.sock"}},
		{name: "environment socket", options: app.BackendOptions{Target: "tab-42"}, env: map[string]string{"HERDR_SOCKET_PATH": "/tmp/environment.sock"}, want: "/tmp/environment.sock"},
		{name: "default", options: app.BackendOptions{Target: "tab-42"}, want: "default"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{output: []byte(tabJSON)}
			backend := &Backend{Runner: runner, Env: env(test.env)}
			target, err := backend.Resolve(context.Background(), test.options)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if target.Scope != test.want {
				t.Fatalf("Scope = %q, want %q", target.Scope, test.want)
			}
			assertSingleCall(t, runner, "herdr", []string{"tab", "get", "tab-42"}, test.wantEnv)
		})
	}
}

func TestResolveRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantInError string
	}{
		{name: "invalid json", output: "not json", wantInError: "decode"},
		{name: "missing envelope id", output: `{"result":{"type":"tab_info","tab":{"tab_id":"tab-42","label":"Payments","workspace_id":"workspace-7"}}}`, wantInError: "missing id"},
		{name: "missing result", output: `{"id":"request-1"}`, wantInError: "missing result"},
		{name: "wrong result type", output: `{"id":"request-1","result":{"type":"other","tab":{"tab_id":"tab-42","label":"Payments","workspace_id":"workspace-7"}}}`, wantInError: "tab_info"},
		{name: "missing tab", output: `{"id":"request-1","result":{"type":"tab_info"}}`, wantInError: "missing result.tab"},
		{name: "missing tab id", output: `{"id":"request-1","result":{"type":"tab_info","tab":{"label":"Payments","workspace_id":"workspace-7"}}}`, wantInError: "tab_id"},
		{name: "missing label", output: `{"id":"request-1","result":{"type":"tab_info","tab":{"tab_id":"tab-42","workspace_id":"workspace-7"}}}`, wantInError: "label"},
		{name: "missing workspace", output: `{"id":"request-1","result":{"type":"tab_info","tab":{"tab_id":"tab-42","label":"Payments"}}}`, wantInError: "workspace_id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &Backend{Runner: &fakeRunner{output: []byte(test.output)}, Env: env(nil)}
			_, err := backend.Resolve(context.Background(), app.BackendOptions{Target: "tab-42"})
			if err == nil || !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.wantInError)
			}
		})
	}
}

func TestResolveReportsCommandOutput(t *testing.T) {
	runner := &fakeRunner{output: []byte("connection refused\n"), err: errors.New("exit status 2")}
	backend := &Backend{Runner: runner, Env: env(nil)}

	_, err := backend.Resolve(context.Background(), app.BackendOptions{Target: "tab-42"})
	if err == nil || !strings.Contains(err.Error(), "connection refused") || !strings.Contains(err.Error(), "exit status 2") || !strings.Contains(err.Error(), "herdr tab get") {
		t.Fatalf("Resolve() error = %v, want command, output, and cause", err)
	}
}

func TestRenameUsesExactArgumentsAndOptionalSocketEnvironment(t *testing.T) {
	runner := &fakeRunner{}
	backend := &Backend{Runner: runner, Env: env(nil)}
	target := app.Target{ID: "tab-42", Label: "Payments"}

	if err := backend.Rename(context.Background(), target, "✅ Payments", app.BackendOptions{Socket: "/tmp/herdr.sock"}); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	assertSingleCall(t, runner, "herdr", []string{"tab", "rename", "tab-42", "✅ Payments"}, map[string]string{"HERDR_SOCKET_PATH": "/tmp/herdr.sock"})

	runner.calls = nil
	if err := backend.Rename(context.Background(), target, "Payments", app.BackendOptions{}); err != nil {
		t.Fatalf("Rename() without socket error = %v", err)
	}
	assertSingleCall(t, runner, "herdr", []string{"tab", "rename", "tab-42", "Payments"}, nil)
}

func TestRefreshStatusAggregatePublishesMarkedTabsInTabOrder(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{
		[]byte(`{"id":"tabs","result":{"type":"tab_list","tabs":[{"tab_id":"tab-3","workspace_id":"workspace-7","number":3,"label":"✅ docs"},{"tab_id":"tab-1","workspace_id":"workspace-7","number":1,"label":"🔥 api"},{"tab_id":"tab-2","workspace_id":"workspace-7","number":2,"label":"plain"},{"tab_id":"tab-4","workspace_id":"workspace-7","number":4,"label":"👀 💬 review"}]}}`),
		[]byte(`{"id":"metadata","result":{"type":"workspace_info"}}`),
	}}
	backend := &Backend{Runner: runner, Env: env(nil)}
	cfg := core.Config{Statuses: []core.Status{
		{Name: "none", Prefix: ""},
		{Name: "wip", Prefix: "🔥 "},
		{Name: "review", Prefix: "👀 "},
		{Name: "done", Prefix: "✅ "},
	}}
	target := app.Target{ID: "tab-1", GroupID: "workspace-7"}

	if err := backend.RefreshStatusAggregate(context.Background(), target, cfg, app.BackendOptions{Socket: "/tmp/herdr.sock"}); err != nil {
		t.Fatalf("RefreshStatusAggregate() error = %v", err)
	}
	wantEnv := map[string]string{"HERDR_SOCKET_PATH": "/tmp/herdr.sock"}
	assertCalls(t, runner, []runnerCall{
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-7"}, env: wantEnv},
		{name: "herdr", args: []string{"workspace", "report-metadata", "workspace-7", "--source", "user:icomark", "--token", "status=🔥 ✅ 👀"}, env: wantEnv},
	})
}

func TestRefreshAllStatusAggregatesPublishesEveryWorkspace(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{
		[]byte(`{"id":"workspaces","result":{"type":"workspace_list","workspaces":[{"workspace_id":"workspace-8","number":2},{"workspace_id":"workspace-3","number":1}]}}`),
		[]byte(`{"id":"tabs-3","result":{"type":"tab_list","tabs":[{"number":1,"label":"🔥 api"}]}}`),
		[]byte(`{"id":"metadata-3","result":{"type":"workspace_info"}}`),
		[]byte(`{"id":"tabs-8","result":{"type":"tab_list","tabs":[{"number":1,"label":"plain"},{"number":2,"label":"✅ docs"}]}}`),
		[]byte(`{"id":"metadata-8","result":{"type":"workspace_info"}}`),
	}}
	backend := &Backend{Runner: runner, Env: env(nil)}
	cfg := core.Config{Statuses: []core.Status{
		{Name: "none", Prefix: ""},
		{Name: "wip", Prefix: "🔥 "},
		{Name: "done", Prefix: "✅ "},
	}}

	syncer, ok := any(backend).(interface {
		RefreshAllStatusAggregates(context.Context, core.Config, app.BackendOptions) error
	})
	if !ok {
		t.Fatal("Backend does not implement all-workspace status synchronization")
	}
	if err := syncer.RefreshAllStatusAggregates(context.Background(), cfg, app.BackendOptions{Socket: "/tmp/herdr.sock"}); err != nil {
		t.Fatalf("RefreshAllStatusAggregates() error = %v", err)
	}
	wantEnv := map[string]string{"HERDR_SOCKET_PATH": "/tmp/herdr.sock"}
	assertCalls(t, runner, []runnerCall{
		{name: "herdr", args: []string{"workspace", "list"}, env: wantEnv},
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-3"}, env: wantEnv},
		{name: "herdr", args: []string{"workspace", "report-metadata", "workspace-3", "--source", "user:icomark", "--token", "status=🔥"}, env: wantEnv},
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-8"}, env: wantEnv},
		{name: "herdr", args: []string{"workspace", "report-metadata", "workspace-8", "--source", "user:icomark", "--token", "status=✅"}, env: wantEnv},
	})
}

func TestRefreshAllStatusAggregatesContinuesAfterWorkspaceFailure(t *testing.T) {
	runner := &fakeRunner{
		outputs: [][]byte{
			[]byte(`{"id":"workspaces","result":{"type":"workspace_list","workspaces":[{"workspace_id":"workspace-3","number":1},{"workspace_id":"workspace-8","number":2}]}}`),
			[]byte("workspace disappeared"),
			[]byte(`{"id":"tabs-8","result":{"type":"tab_list","tabs":[{"number":1,"label":"✅ docs"}]}}`),
			[]byte(`{"id":"metadata-8","result":{"type":"workspace_info"}}`),
		},
		errs: []error{nil, errors.New("exit status 1"), nil, nil},
	}
	backend := &Backend{Runner: runner, Env: env(nil)}

	err := backend.RefreshAllStatusAggregates(context.Background(), core.DefaultConfig(), app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "workspace-3") {
		t.Fatalf("RefreshAllStatusAggregates() error = %v, want failed workspace", err)
	}
	assertCalls(t, runner, []runnerCall{
		{name: "herdr", args: []string{"workspace", "list"}},
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-3"}},
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-8"}},
		{name: "herdr", args: []string{"workspace", "report-metadata", "workspace-8", "--source", "user:icomark", "--token", "status=✅"}},
	})
}

func TestRefreshStatusAggregateClearsTokenWhenNoTabsAreMarked(t *testing.T) {
	runner := &fakeRunner{outputs: [][]byte{
		[]byte(`{"id":"tabs","result":{"type":"tab_list","tabs":[{"tab_id":"tab-1","workspace_id":"workspace-7","number":1,"label":"api"}]}}`),
		[]byte(`{"id":"metadata","result":{"type":"workspace_info"}}`),
	}}
	backend := &Backend{Runner: runner, Env: env(nil)}
	target := app.Target{ID: "tab-1", GroupID: "workspace-7"}

	if err := backend.RefreshStatusAggregate(context.Background(), target, core.DefaultConfig(), app.BackendOptions{}); err != nil {
		t.Fatalf("RefreshStatusAggregate() error = %v", err)
	}
	assertCalls(t, runner, []runnerCall{
		{name: "herdr", args: []string{"tab", "list", "--workspace", "workspace-7"}},
		{name: "herdr", args: []string{"workspace", "report-metadata", "workspace-7", "--source", "user:icomark", "--clear-token", "status"}},
	})
}

func TestRenameReportsCommandOutput(t *testing.T) {
	runner := &fakeRunner{output: []byte("rename failed\n"), err: errors.New("exit status 3")}
	backend := &Backend{Runner: runner, Env: env(nil)}

	err := backend.Rename(context.Background(), app.Target{ID: "tab-42"}, "New", app.BackendOptions{})
	if err == nil || !strings.Contains(err.Error(), "rename failed") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("Rename() error = %v, want output and cause", err)
	}
}

func assertCalls(t *testing.T, runner *fakeRunner, want []runnerCall) {
	t.Helper()
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, want)
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
