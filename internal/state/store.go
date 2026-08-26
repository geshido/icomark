package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/geshido/icomark/internal/core"
)

const currentVersion = 1

// Entry is the canonical persisted state for one runtime item.
type Entry = core.Entry

// State is the versioned canonical state payload.
type State struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

// New returns an initialized empty canonical state.
func New() State {
	return State{Version: currentVersion, Entries: make(map[string]Entry)}
}

// FilePath returns the canonical path for a backend and scope.
func FilePath(stateHome, backend, scope string) string {
	return filepath.Join(stateHome, "icomark", sanitizePathComponent(backend), sanitizePathComponent(scope)+".json")
}

// LegacyFilePath returns a traversal-safe legacy path while preserving the
// filenames produced for ordinary tool and scope names.
func LegacyFilePath(stateHome, tool, scope string) string {
	return filepath.Join(stateHome, sanitizePathComponent(tool), sanitizePathComponent(scope)+".json")
}

func sanitizePathComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	sanitized := b.String()
	if sanitized == "." || sanitized == ".." {
		return strings.Repeat("_", len(sanitized))
	}
	return sanitized
}

// Load reads canonical state. A missing file produces an initialized empty state.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state %q: %w", path, err)
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode state %q: %w", path, err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return State{}, fmt.Errorf("decode state %q: %w", path, err)
	}
	if state.Version != currentVersion {
		return State{}, fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Entries == nil {
		state.Entries = make(map[string]Entry)
	}
	return state, nil
}

// Save atomically writes canonical state using a temporary file and rename.
func Save(path string, state State) error {
	state.Version = currentVersion
	if state.Entries == nil {
		state.Entries = make(map[string]Entry)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary state permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace state %q: %w", path, err)
	}
	return nil
}

// Lookup checks runtime ID first. A unique base fallback is rekeyed to the
// current runtime ID; an ambiguous base is not matched.
func (s *State) Lookup(runtimeID, base string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	if entry, ok := s.Entries[runtimeID]; ok {
		return entry, true
	}
	var matchedID string
	var matched Entry
	matches := 0
	for id, entry := range s.Entries {
		if entry.Base == base {
			matchedID, matched = id, entry
			matches++
		}
	}
	if matches != 1 {
		return Entry{}, false
	}
	if s.Entries == nil {
		s.Entries = make(map[string]Entry)
	}
	delete(s.Entries, matchedID)
	s.Entries[runtimeID] = matched
	return matched, true
}

// Put inserts an entry, or deletes it when it has neither status nor comment.
func (s *State) Put(runtimeID string, entry Entry) {
	if s.Entries == nil {
		s.Entries = make(map[string]Entry)
	}
	if entry.Status == "" && entry.Comment == "" {
		delete(s.Entries, runtimeID)
		return
	}
	s.Entries[runtimeID] = entry
}

// MigrateAtabs decodes the old atabs map keyed by runtime ID.
func MigrateAtabs(data []byte) (State, error) {
	var old map[string]Entry
	if err := json.Unmarshal(data, &old); err != nil {
		return State{}, fmt.Errorf("decode atabs state: %w", err)
	}
	state := New()
	for id, entry := range old {
		state.Put(id, entry)
	}
	return state, nil
}

// MigrateZtabs decodes old ztabs maps keyed by base label. It supports both
// object entries and the earlier comments-only string entries. Synthetic IDs
// prevent a base label from being mistaken for an exact runtime ID.
func MigrateZtabs(data []byte) (State, error) {
	var old map[string]struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(data, &old); err == nil {
		state := New()
		for base, entry := range old {
			state.Put(legacyZtabsID(base), Entry{Base: base, Status: entry.Status, Comment: entry.Comment})
		}
		return state, nil
	}

	var comments map[string]string
	if err := json.Unmarshal(data, &comments); err != nil {
		return State{}, fmt.Errorf("decode ztabs state: %w", err)
	}
	state := New()
	for base, comment := range comments {
		state.Put(legacyZtabsID(base), Entry{Base: base, Comment: comment})
	}
	return state, nil
}

func legacyZtabsID(base string) string {
	return "\x00ztabs:" + base
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
