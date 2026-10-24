package core

import "testing"

func TestParseLabelUsesLongestConfiguredPrefix(t *testing.T) {
	cfg := Config{Statuses: []Status{
		{Name: "none", Prefix: ""},
		{Name: "short", Prefix: "!"},
		{Name: "long", Prefix: "!!"},
	}}
	got := ParseLabel("!!💬 renamed", cfg)
	if got.StatusIndex != 2 || !got.HasComment || got.Base != "renamed" {
		t.Fatalf("ParseLabel() = %#v", got)
	}
}

func TestParseLabelNoPrefixHasStatusZero(t *testing.T) {
	got := ParseLabel("plain", DefaultConfig())
	if got != (Label{Base: "plain", StatusIndex: 0}) {
		t.Fatalf("ParseLabel() = %#v", got)
	}
}

func TestBuildLabelHandlesInvalidStatusSafely(t *testing.T) {
	cfg := DefaultConfig()
	if got := BuildLabel("name", -1, true, cfg); got != "💬 name" {
		t.Fatalf("negative status: %q", got)
	}
	if got := BuildLabel("name", len(cfg.Statuses), false, cfg); got != "name" {
		t.Fatalf("large status: %q", got)
	}
	if got := BuildLabel("name", 1, true, cfg); got != "🔨 💬 name" {
		t.Fatalf("normal build: %q", got)
	}
}
