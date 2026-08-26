package core

import "testing"

func TestPlanStoredStatusWinsAndManualRenameUpdatesBase(t *testing.T) {
	stored := Entry{Base: "old", Status: "review", Comment: "remember"}
	got, err := Plan(Update{Config: DefaultConfig(), Label: "🔨 new name", Stored: &stored, Action: Sync})
	if err != nil {
		t.Fatal(err)
	}
	wantEntry := Entry{Base: "new name", Status: "review", Comment: "remember"}
	if got.Entry != wantEntry || got.Label != "👀 💬 new name" {
		t.Fatalf("Plan() = %#v", got)
	}
}

func TestPlanNextPrevAndNonePersistence(t *testing.T) {
	cfg := Config{Statuses: []Status{{Name: "none"}, {Name: "one", Prefix: "1 "}, {Name: "two", Prefix: "2 "}}}
	cases := []struct {
		name   string
		label  string
		action Action
		status string
		want   Result
	}{
		{name: "next", label: "1 base", action: Next, want: Result{Label: "2 base", Entry: Entry{Base: "base", Status: "two"}}},
		{name: "next wraps to none", label: "2 base", action: Next, want: Result{Label: "base", Entry: Entry{Base: "base"}}},
		{name: "prev wraps", label: "base", action: Prev, want: Result{Label: "2 base", Entry: Entry{Base: "base", Status: "two"}}},
		{name: "set", label: "base", action: SetStatus, status: "one", want: Result{Label: "1 base", Entry: Entry{Base: "base", Status: "one"}}},
		{name: "set none", label: "2 base", action: SetStatus, status: "none", want: Result{Label: "base", Entry: Entry{Base: "base"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Plan(Update{Config: cfg, Label: tc.label, Action: tc.action, Status: tc.status})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Plan() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestPlanCommentSetAndClearControlMarker(t *testing.T) {
	stored := Entry{Base: "base", Status: "wip", Comment: "old"}
	set, err := Plan(Update{Config: DefaultConfig(), Label: "🔨 💬 base", Stored: &stored, Action: SetComment, Comment: "new"})
	if err != nil || set.Entry.Comment != "new" || set.Label != "🔨 💬 base" {
		t.Fatalf("set = %#v, %v", set, err)
	}
	clear, err := Plan(Update{Config: DefaultConfig(), Label: "🔨 💬 base", Stored: &stored, Action: ClearComment})
	if err != nil || clear.Entry.Comment != "" || clear.Label != "🔨 base" {
		t.Fatalf("clear = %#v, %v", clear, err)
	}
}

func TestPlanIgnoresUnbackedVisibleCommentMarker(t *testing.T) {
	got, err := Plan(Update{Config: DefaultConfig(), Label: "💬 base", Action: Sync})
	if err != nil || got.Label != "base" || got.Entry.Comment != "" {
		t.Fatalf("Plan() = %#v, %v", got, err)
	}
}

func TestPlanRejectsUnknownRequestedOrStoredStatus(t *testing.T) {
	if _, err := Plan(Update{Config: DefaultConfig(), Label: "base", Action: SetStatus, Status: "unknown"}); err == nil {
		t.Fatal("unknown requested status should error")
	}
	stored := Entry{Status: "removed"}
	if _, err := Plan(Update{Config: DefaultConfig(), Label: "base", Stored: &stored, Action: Sync}); err == nil {
		t.Fatal("unknown stored status should error")
	}
}
