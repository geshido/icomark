package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFilePathSanitizesBackendAndScope(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name    string
		backend string
		scope   string
		want    string
	}{
		{name: "separators", backend: "../../escape/backend", scope: "team/foo bar", want: filepath.Join(home, "icomark", ".._.._escape_backend", "team_foo_bar.json")},
		{name: "exact parent component", backend: "..", scope: "scope", want: filepath.Join(home, "icomark", "__", "scope.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := FilePath(home, tc.backend, tc.scope)
			if got != tc.want {
				t.Fatalf("FilePath() = %q, want %q", got, tc.want)
			}
			within, err := filepath.Rel(filepath.Join(home, "icomark"), got)
			if err != nil || within == ".." || filepath.IsAbs(within) || len(within) >= 3 && within[:3] == ".."+string(filepath.Separator) {
				t.Fatalf("FilePath() escaped state root: relative path %q, error %v", within, err)
			}
		})
	}
}

func TestLegacyFilePathSanitizesToolAndScope(t *testing.T) {
	home := t.TempDir()
	if got, want := LegacyFilePath(home, "ztabs", "session"), filepath.Join(home, "ztabs", "session.json"); got != want {
		t.Fatalf("normal LegacyFilePath() = %q, want %q", got, want)
	}
	got := LegacyFilePath(home, "../../tool", "../../scope")
	want := filepath.Join(home, ".._.._tool", ".._.._scope.json")
	if got != want {
		t.Fatalf("traversal LegacyFilePath() = %q, want %q", got, want)
	}
	within, err := filepath.Rel(home, got)
	if err != nil || within == ".." || filepath.IsAbs(within) || len(within) >= 3 && within[:3] == ".."+string(filepath.Separator) {
		t.Fatalf("LegacyFilePath() escaped state home: relative path %q, error %v", within, err)
	}
}

func TestSaveLoadCanonicalJSONAndAtomicCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	s := New()
	s.Entries["42"] = Entry{Base: "tab", Status: "wip", Comment: "note"}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw["version"] != float64(1) || raw["entries"] == nil {
		t.Fatalf("unexpected payload: %s", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".state.json-*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("state permissions = %04o, want %04o", got, want)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("Load() = %#v, %v, want %#v", got, err, s)
	}
}

func TestLoadMissingReturnsEmptyAndRejectsInvalidCanonicalPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, New()) {
		t.Fatalf("missing Load() = %#v, %v", got, err)
	}
	for name, payload := range map[string]string{
		"wrong version": `{"version":2,"entries":{}}`,
		"trailing data": `{"version":1,"entries":{}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid canonical payload should error")
			}
		})
	}
}

func TestLookupExactThenUniqueBaseFallbackRekeys(t *testing.T) {
	s := New()
	s.Entries["old"] = Entry{Base: "base", Status: "wip"}
	s.Entries["current"] = Entry{Base: "other", Comment: "exact"}
	got, ok := s.Lookup("current", "base")
	if !ok || got.Comment != "exact" || len(s.Entries) != 2 {
		t.Fatalf("exact lookup = %#v, %v, state %#v", got, ok, s)
	}
	got, ok = s.Lookup("new", "base")
	if !ok || got.Status != "wip" {
		t.Fatalf("fallback = %#v, %v", got, ok)
	}
	if _, exists := s.Entries["old"]; exists || s.Entries["new"].Base != "base" {
		t.Fatalf("fallback did not rekey: %#v", s)
	}
}

func TestLookupAmbiguousBaseDoesNotMatch(t *testing.T) {
	s := New()
	s.Entries["1"] = Entry{Base: "same", Status: "wip"}
	s.Entries["2"] = Entry{Base: "same", Comment: "note"}
	if _, ok := s.Lookup("3", "same"); ok {
		t.Fatal("ambiguous base should not match")
	}
	if len(s.Entries) != 2 {
		t.Fatalf("ambiguous lookup mutated state: %#v", s)
	}
}

func TestPutDeletesEmptyEntry(t *testing.T) {
	s := New()
	s.Put("1", Entry{Base: "base", Status: "wip"})
	s.Put("1", Entry{Base: "base"})
	if len(s.Entries) != 0 {
		t.Fatalf("empty entry retained: %#v", s)
	}
}

func TestMigrateZtabsBaseKeysCannotCollideWithRuntimeIDs(t *testing.T) {
	migrated, err := MigrateZtabs([]byte(`{"old-base":{"status":"review","comment":"note"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := migrated.Lookup("old-base", "different-base"); ok {
		t.Fatalf("runtime ID collided with migrated base key: %#v", entry)
	}
	entry, ok := migrated.Lookup("current-runtime-id", "old-base")
	if !ok || entry.Base != "old-base" || entry.Status != "review" || entry.Comment != "note" {
		t.Fatalf("base fallback = %#v, %v", entry, ok)
	}
}

func TestReadOnlyMigrationHelpers(t *testing.T) {
	atabs, err := MigrateAtabs([]byte(`{"7":{"base":"tab","status":"review","comment":"note"}}`))
	if err != nil {
		t.Fatal(err)
	}
	wantAtabs := New()
	wantAtabs.Entries["7"] = Entry{Base: "tab", Status: "review", Comment: "note"}
	if !reflect.DeepEqual(atabs, wantAtabs) {
		t.Fatalf("atabs = %#v", atabs)
	}

	ztabs, err := MigrateZtabs([]byte(`{"tab":{"status":"done","comment":"ok"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := ztabs.Lookup("runtime", "tab"); !ok || entry != (Entry{Base: "tab", Status: "done", Comment: "ok"}) {
		t.Fatalf("ztabs fallback = %#v, %v", entry, ok)
	}

	legacyComments, err := MigrateZtabs([]byte(`{"tab":"remember this"}`))
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := legacyComments.Lookup("runtime", "tab"); !ok || entry != (Entry{Base: "tab", Comment: "remember this"}) {
		t.Fatalf("legacy string comments fallback = %#v, %v", entry, ok)
	}

	empty, err := MigrateZtabs([]byte(`{"unused":{"status":"","comment":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Entries) != 0 {
		t.Fatalf("empty migrated entry retained: %#v", empty)
	}
}
