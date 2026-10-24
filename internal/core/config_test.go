package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	want := Config{Statuses: []Status{
		{Name: "none", Prefix: ""},
		{Name: "wip", Prefix: "🔨 "},
		{Name: "review", Prefix: "👀 "},
		{Name: "test", Prefix: "🧪 "},
		{Name: "done", Prefix: "✅ "},
		{Name: "blocked", Prefix: "🚫 "},
	}}
	if got := DefaultConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultConfig() = %#v, want %#v", got, want)
	}
}

func TestLoadConfigPrecedenceAndFallback(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "canonical.yml")
	legacy1 := filepath.Join(dir, "legacy-one.yml")
	legacy2 := filepath.Join(dir, "legacy-two.yml")
	writeConfig(t, legacy2, "statuses:\n  - name: none\n    prefix: ''\n  - name: legacy\n    prefix: 'L '\n")

	got, err := LoadConfig(canonical, []string{legacy1, legacy2}, "")
	if err != nil || len(got.Statuses) != 2 || got.Statuses[1].Name != "legacy" {
		t.Fatalf("legacy load = %#v, %v", got, err)
	}

	writeConfig(t, canonical, "statuses:\n  - name: none\n    prefix: ''\n  - name: canonical\n    prefix: 'C '\n")
	got, err = LoadConfig(canonical, []string{legacy2}, "")
	if err != nil || got.Statuses[1].Name != "canonical" {
		t.Fatalf("canonical load = %#v, %v", got, err)
	}
}

func TestLoadConfigNoFilesUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadConfig(filepath.Join(dir, "missing"), []string{filepath.Join(dir, "also-missing")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, DefaultConfig()) {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadConfigOverrideIsAuthoritative(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "canonical.yml")
	override := filepath.Join(dir, "override.yml")
	writeConfig(t, canonical, "statuses:\n  - name: none\n    prefix: ''\n")
	if _, err := LoadConfig(canonical, nil, override); err == nil {
		t.Fatal("missing explicit override should error")
	}
	writeConfig(t, override, "statuses: [")
	if _, err := LoadConfig(canonical, nil, override); err == nil {
		t.Fatal("malformed explicit override should error")
	}
}

func TestLoadConfigFoundEmptyStatusesErrors(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "config.yml")
	writeConfig(t, canonical, "statuses: []\n")
	if _, err := LoadConfig(canonical, nil, ""); err == nil {
		t.Fatal("empty statuses should be actionable")
	}
}

func TestLoadConfigRejectsInvalidStatusDefinitions(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"duplicate names":    "statuses:\n- {name: none, prefix: ''}\n- {name: none, prefix: 'x'}\n",
		"duplicate prefixes": "statuses:\n- {name: none, prefix: ''}\n- {name: x, prefix: ''}\n",
		"missing none first": "statuses:\n- {name: wip, prefix: 'W '}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yml")
			writeConfig(t, path, body)
			if _, err := LoadConfig(path, nil, ""); err == nil {
				t.Fatal("invalid status definitions should error")
			}
		})
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
