package app

import (
	"reflect"
	"testing"
)

func TestParseOptionsCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Options
	}{
		{name: "next", args: []string{"next"}, want: Options{Command: CommandNext}},
		{name: "previous", args: []string{"prev"}, want: Options{Command: CommandPrev}},
		{name: "set", args: []string{"set", "review"}, want: Options{Command: CommandSet, Status: "review"}},
		{name: "comment joins text", args: []string{"comment", "waiting", "for", "CI"}, want: Options{Command: CommandComment, Comment: "waiting for CI"}},
		{name: "help is valid comment text", args: []string{"comment", "help"}, want: Options{Command: CommandComment, Comment: "help"}},
		{name: "comment clears", args: []string{"comment"}, want: Options{Command: CommandComment}},
		{name: "sync", args: []string{"sync"}, want: Options{Command: CommandSync}},
		{name: "status", args: []string{"status"}, want: Options{Command: CommandStatus}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseOptions(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseOptions() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseOptionsSharedFlagsAnywhereAndEqualsForm(t *testing.T) {
	got, err := ParseOptions([]string{
		"--target", "tab-7", "comment", "waiting", "--socket=/tmp/backend.sock",
		"for", "CI", "--state-key", "stable", "--config=/tmp/config.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Options{
		Command:  CommandComment,
		Comment:  "waiting for CI",
		Target:   "tab-7",
		Socket:   "/tmp/backend.sock",
		StateKey: "stable",
		Config:   "/tmp/config.yml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseOptions() = %#v, want %#v", got, want)
	}
}

func TestParseOptionsEndOfOptionsKeepsLiteralPositionals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want Options
	}{
		{
			name: "comment-like flags are literal",
			args: []string{"comment", "--", "--target", "-h", "--help"},
			want: Options{Command: CommandComment, Comment: "--target -h --help"},
		},
		{
			name: "custom status is literal",
			args: []string{"--target", "tab-1", "set", "--", "--custom"},
			want: Options{Command: CommandSet, Status: "--custom", Target: "tab-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseOptions(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseOptions() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestParseOptionsHelpForms(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}, {"next", "--help"}} {
		got, err := ParseOptions(args)
		if err != nil || !got.Help {
			t.Fatalf("ParseOptions(%q) = %#v, %v; want help", args, got, err)
		}
	}
}

func TestParseOptionsHelpDoesNotBypassValidation(t *testing.T) {
	for _, args := range [][]string{
		{"help", "extra"},
		{"--help", "--unknown"},
		{"help", "--target"},
		{"--target", "-h"},
		{"--help", "--config="},
	} {
		if _, err := ParseOptions(args); err == nil {
			t.Errorf("ParseOptions(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParseOptionsRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"wat"},
		{"set"},
		{"set", "wip", "extra"},
		{"next", "extra"},
		{"--target"},
		{"next", "--socket"},
		{"next", "--unknown", "x"},
	} {
		if _, err := ParseOptions(args); err == nil {
			t.Errorf("ParseOptions(%q) unexpectedly succeeded", args)
		}
	}
}
