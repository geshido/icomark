package rex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/core"
)

func sessionJSON(t *testing.T, label string, windows ...string) string {
	t.Helper()
	tabs := make([]map[string]string, 0, len(windows))
	for _, label := range windows {
		tabs = append(tabs, map[string]string{"label": label})
	}
	data, err := json.Marshal(map[string]any{
		"session_id": "session:one", "label": label, "windows": tabs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRefreshStatusAggregate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		label   string
		windows []string
		want    string
	}{
		{name: "deduplicate in first occurrence order", label: "Project", windows: []string{"✅ docs", "plain", "🔨 api", "💬 note", "👀 💬 review", "🔨 worker"}, want: "✅🔨👀 Project"},
		{name: "migrate old suffix after config changed", label: "Project [icomark: 🔥]", windows: []string{"👀 review"}, want: "👀 Project"},
		{name: "clear last status", label: "Project [icomark: 👀]", windows: []string{"plain", "💬 note"}, want: "Project"},
		{name: "empty session", label: "Project [icomark: 👀]", want: "Project"},
		{name: "already up to date", label: "👀 Project", windows: []string{"👀 review", "👀 another"}, want: "👀 Project"},
		{name: "replace existing prefix", label: "👀🔨 Project", windows: []string{"🔨 api", "👀 review"}, want: "🔨👀 Project"},
		{name: "clear leading marks", label: "👀🔨 Project", windows: []string{"plain"}, want: "Project"},
		{name: "remove duplicate leading marks", label: "🔨🔨 Project", windows: []string{"🔨 api", "🔨 worker"}, want: "🔨 Project"},
		{name: "unmarked session", label: "Project", windows: []string{"plain"}, want: "Project"},
		{name: "preserve ordinary brackets", label: "Project [work]", windows: []string{"👀 review"}, want: "👀 Project [work]"},
		{name: "preserve unknown emoji in base name", label: "🤖 Project", windows: []string{"👀 review"}, want: "👀 🤖 Project"},
		{name: "only complete leading marks are stripped", label: "👀Project", windows: []string{"👀 review"}, want: "👀 👀Project"},
		{name: "manual session rename", label: "New name", windows: []string{"✅ docs"}, want: "✅ New name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{output: sessionJSON(t, tc.label, tc.windows...)}
			backend := &Backend{Runner: runner, Env: env(nil)}
			options := app.BackendOptions{Socket: "unix:///tmp/rex.sock", Target: "not-the-session", StateKey: "not-the-session"}
			err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, core.DefaultConfig(), options)
			if err != nil {
				t.Fatal(err)
			}
			overrides := map[string]string{"REX_SERVER": options.Socket}
			want := []runnerCall{{name: "rex", args: []string{"session", "inspect", "--json", "--", "session:one"}, env: overrides}}
			if tc.want != tc.label {
				want = append(want, runnerCall{name: "rex", args: []string{"session", "rename", "--", "session:one", tc.want}, env: overrides})
			}
			if !reflect.DeepEqual(runner.calls, want) {
				t.Fatalf("calls = %#v, want %#v", runner.calls, want)
			}
		})
	}
}

func TestRefreshStatusAggregateCustomPrefixesAndUnicodeLimit(t *testing.T) {
	cfg := core.Config{Statuses: []core.Status{
		{Name: "none"}, {Name: "review", Prefix: "Review: "}, {Name: "hidden", Prefix: "  "},
	}}
	runner := &fakeRunner{output: sessionJSON(t, "Project", "Review: api", "  hidden")}
	backend := &Backend{Runner: runner, Env: env(nil)}
	if err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, cfg, app.BackendOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := runner.calls[1].args[4]; got != "Review: Project" {
		t.Fatalf("summary = %q", got)
	}

	for _, count := range []int{126, 127} {
		runner := &fakeRunner{output: sessionJSON(t, strings.Repeat("я", count), "👀 review")}
		backend := &Backend{Runner: runner, Env: env(nil)}
		err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, core.DefaultConfig(), app.BackendOptions{})
		// One status emoji and the separating space add two Unicode code points.
		if count == 126 {
			if err != nil || len(runner.calls) != 2 {
				t.Fatalf("128-character label rejected: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "128") || len(runner.calls) != 1 {
			t.Fatalf("129-character label not safely rejected: %v, calls %v", err, runner.calls)
		}
	}
}

func TestRefreshStatusAggregatePrefixRoundTrip(t *testing.T) {
	for _, cfg := range []core.Config{
		{Statuses: []core.Status{{Name: "none"}, {Name: "short", Prefix: "👩 "}, {Name: "long", Prefix: "👩‍💻 "}}},
		{Statuses: []core.Status{{Name: "none"}, {Name: "short", Prefix: "In "}, {Name: "long", Prefix: "In Progress "}}},
		{Statuses: []core.Status{{Name: "none"}, {Name: "regex", Prefix: "[R]+? "}, {Name: "words", Prefix: "In Progress "}}},
	} {
		first, second := cfg.Statuses[1].Prefix, cfg.Statuses[2].Prefix
		label := "Project"
		windows := []string{second + "a", first + "b", second + "c"}
		want := strings.TrimSpace(second) + strings.TrimSpace(first) + " Project"
		for i := 0; i < 3; i++ {
			runner := &fakeRunner{output: sessionJSON(t, label, windows...)}
			backend := &Backend{Runner: runner, Env: env(nil)}
			if err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, cfg, app.BackendOptions{}); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				label = runner.calls[1].args[4]
				if label != want {
					t.Fatalf("summary = %q, want %q", label, want)
				}
			} else if len(runner.calls) != 1 {
				t.Fatalf("unchanged summary was renamed again: %#v", runner.calls)
			}
		}
		runner := &fakeRunner{output: sessionJSON(t, label, "plain")}
		backend := &Backend{Runner: runner, Env: env(nil)}
		if err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, cfg, app.BackendOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := runner.calls[1].args[4]; got != "Project" {
			t.Fatalf("cleared name = %q", got)
		}
	}
}

func TestRefreshStatusAggregateRejectsMissingSessionAndMalformedResponses(t *testing.T) {
	runner := &fakeRunner{}
	backend := &Backend{Runner: runner, Env: env(nil)}
	if err := backend.RefreshStatusAggregate(context.Background(), app.Target{}, core.DefaultConfig(), app.BackendOptions{}); err == nil || len(runner.calls) != 0 {
		t.Fatalf("missing session: err=%v calls=%v", err, runner.calls)
	}
	for _, output := range []string{
		"not json", "null", `{}`, `{"session_id":"other","label":"Project"}`,
		`{"session_id":"session:one"}`, `{"session_id":"session:one","label":""}`,
		`{"session_id":"session:one","label":"Project [icomark: 👀]"}`,
		`{"session_id":"session:one","label":"Project [icomark: 👀]","windows":null}`,
		`{"session_id":"session:one","label":"Project","windows":[{}]}`,
		`{"session_id":"session:one","label":"Project","windows":[{"label":null}]}`,
		`{"session_id":"session:one","label":" [icomark: 👀]","windows":[]}`,
		`{"session_id":"session:one","label":"👀 ","windows":[]}`,
	} {
		t.Run(output, func(t *testing.T) {
			runner := &fakeRunner{output: output}
			backend := &Backend{Runner: runner, Env: env(nil)}
			err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, core.DefaultConfig(), app.BackendOptions{})
			if err == nil || len(runner.calls) != 1 {
				t.Fatalf("malformed response: err=%v calls=%v", err, runner.calls)
			}
		})
	}
}

func TestRefreshStatusAggregateRejectsReservedPrefixWithoutChangingSessionName(t *testing.T) {
	cfg := core.Config{Statuses: []core.Status{
		{Name: "none"}, {Name: "custom", Prefix: "x [icomark: y "},
	}}
	runner := &fakeRunner{output: sessionJSON(t, "Project", "x [icomark: y api")}
	backend := &Backend{Runner: runner, Env: env(nil)}
	for i := 0; i < 2; i++ {
		err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, cfg, app.BackendOptions{})
		if err == nil || !strings.Contains(err.Error(), "reserved summary marker") {
			t.Fatalf("unsafe prefix accepted: %v", err)
		}
	}
	runner.output = sessionJSON(t, "Project", "plain")
	if err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, cfg, app.BackendOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		if call.args[1] != "inspect" {
			t.Fatalf("session name changed: %#v", call)
		}
	}
}

func TestRefreshStatusAggregateCommandErrors(t *testing.T) {
	failure := errors.New("exit status 1")
	for _, rename := range []bool{false, true} {
		runner := &fakeRunner{output: "permission denied", err: failure}
		if rename {
			runner.output, runner.err = "", nil
			runner.outputs = []string{sessionJSON(t, "Project", "👀 review"), "permission denied"}
			runner.errs = []error{nil, failure}
		}
		backend := &Backend{Runner: runner, Env: env(nil)}
		err := backend.RefreshStatusAggregate(context.Background(), app.Target{GroupID: "session:one"}, core.DefaultConfig(), app.BackendOptions{})
		if !errors.Is(err, failure) || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("command failure not propagated: %v", err)
		}
	}
}
