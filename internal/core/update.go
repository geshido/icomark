package core

import "fmt"

// Entry is the backend-independent persisted workflow state for one item.
type Entry struct {
	Base    string `json:"base"`
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

// Action identifies a pure workflow transition.
type Action string

const (
	Next         Action = "next"
	Prev         Action = "prev"
	SetStatus    Action = "set-status"
	SetComment   Action = "set-comment"
	ClearComment Action = "clear-comment"
	Sync         Action = "sync"
)

// Update is all input needed to plan a workflow transition.
type Update struct {
	Config  Config
	Label   string
	Stored  *Entry
	Action  Action
	Status  string
	Comment string
}

// Result contains the desired visible label and persisted entry.
type Result struct {
	Label string
	Entry Entry
}

// Plan computes a workflow transition without filesystem or backend effects.
func Plan(update Update) (Result, error) {
	if err := validateConfig(update.Config); err != nil {
		return Result{}, fmt.Errorf("invalid planner config: %w", err)
	}
	parsed := ParseLabel(update.Label, update.Config)
	status := parsed.StatusIndex
	comment := ""
	if update.Stored != nil {
		var err error
		status, err = statusIndex(update.Config, update.Stored.Status)
		if err != nil {
			return Result{}, fmt.Errorf("stored status: %w", err)
		}
		comment = update.Stored.Comment
	}

	switch update.Action {
	case Next:
		status = (status + 1) % len(update.Config.Statuses)
	case Prev:
		status = (status - 1 + len(update.Config.Statuses)) % len(update.Config.Statuses)
	case SetStatus:
		var err error
		status, err = requestedStatusIndex(update.Config, update.Status)
		if err != nil {
			return Result{}, err
		}
	case SetComment:
		comment = update.Comment
	case ClearComment:
		comment = ""
	case Sync:
		// The resolved stored/visible state is already the desired state.
	default:
		return Result{}, fmt.Errorf("unknown action %q", update.Action)
	}

	entry := Entry{Base: parsed.Base, Comment: comment}
	if status != 0 {
		entry.Status = update.Config.Statuses[status].Name
	}
	return Result{
		Label: BuildLabel(entry.Base, status, entry.Comment != "", update.Config),
		Entry: entry,
	}, nil
}

func statusIndex(cfg Config, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	return requestedStatusIndex(cfg, name)
}

func requestedStatusIndex(cfg Config, name string) (int, error) {
	for i, status := range cfg.Statuses {
		if status.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown status %q", name)
}
