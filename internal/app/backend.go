package app

import (
	"context"

	"github.com/geshido/icomark/internal/core"
)

// Target is the backend item resolved for the requested operation.
type Target struct {
	ID      string
	Label   string
	Scope   string
	GroupID string
}

// BackendOptions contains backend-specific selectors shared by Resolve and Rename.
type BackendOptions struct {
	Target   string
	Socket   string
	StateKey string
}

// Backend adapts one runtime to the shared headless application.
type Backend interface {
	Name() string
	Resolve(context.Context, BackendOptions) (Target, error)
	Rename(context.Context, Target, string, BackendOptions) error
}

// StatusAggregator refreshes a backend-specific summary after canonical state
// and the target's visible label have both been updated.
type StatusAggregator interface {
	RefreshStatusAggregate(context.Context, Target, core.Config, BackendOptions) error
}

// AllStatusAggregator refreshes every backend-specific summary during an
// explicit sync operation.
type AllStatusAggregator interface {
	RefreshAllStatusAggregates(context.Context, core.Config, BackendOptions) error
}
