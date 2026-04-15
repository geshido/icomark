package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TabState persists per-tab metadata across zellij restarts. Keyed by
// baseName (the part of the tab name without status/💬 prefixes), which
// survives zellij resurrect — tab_id does not.
//
// Status is stored by its config name (e.g. "wip"), not by index, so it
// survives config reordering. "none" / empty means no status.
type TabState struct {
	Comment string `json:"comment,omitempty"`
	Status  string `json:"status,omitempty"`
}

// State is the full sidecar payload, keyed by baseName.
type State map[string]TabState

// Comments is a convenience view over State{}.Comment — kept so callers that
// only care about comments don't have to destructure TabState.
type Comments map[string]string

func statePath() (string, error) {
	session := os.Getenv("ZELLIJ_SESSION_NAME")
	if session == "" {
		return "", fmt.Errorf("ZELLIJ_SESSION_NAME is not set")
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ztabs", session+".json"), nil
}

func loadState() (State, error) {
	path, err := statePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	// New format: map[baseName]TabState. TabState unmarshals from a JSON
	// object, so legacy string values produce a decode error — fall back in
	// that case.
	var s State
	if err := json.Unmarshal(data, &s); err == nil {
		return s, nil
	}
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return State{}, err
	}
	s = State{}
	for k, v := range legacy {
		if v == "" {
			continue
		}
		s[k] = TabState{Comment: v}
	}
	return s, nil
}

func saveState(s State) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadComments() (Comments, error) {
	s, err := loadState()
	if err != nil {
		return Comments{}, err
	}
	c := Comments{}
	for k, v := range s {
		if v.Comment != "" {
			c[k] = v.Comment
		}
	}
	return c, nil
}

// putEntry writes an entry, dropping the key entirely if both fields are empty
// so the sidecar stays clean.
func putEntry(s State, baseName string, entry TabState) {
	if entry.Comment == "" && entry.Status == "" {
		delete(s, baseName)
		return
	}
	s[baseName] = entry
}

func setComment(baseName, text string) error {
	s, err := loadState()
	if err != nil {
		return err
	}
	entry := s[baseName]
	entry.Comment = text
	putEntry(s, baseName, entry)
	return saveState(s)
}

// setStatus records the tab's current status by its config name. Pass "none"
// or "" to clear the stored status.
func setStatus(baseName, status string) error {
	if status == "none" {
		status = ""
	}
	s, err := loadState()
	if err != nil {
		return err
	}
	entry := s[baseName]
	entry.Status = status
	putEntry(s, baseName, entry)
	return saveState(s)
}

// migrateTabState renames the sidecar key when a tab's baseName changes.
// Called from both the TUI rename flow and from the background plugin that
// watches external renames.
func migrateTabState(oldBase, newBase string) error {
	if oldBase == newBase {
		return nil
	}
	s, err := loadState()
	if err != nil {
		return err
	}
	entry, ok := s[oldBase]
	if !ok {
		return nil
	}
	delete(s, oldBase)
	putEntry(s, newBase, entry)
	return saveState(s)
}
