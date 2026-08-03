package events

import (
	"context"
	"encoding/json"
	"time"
)

// Event is the platform envelope (issue #2).
type Event struct {
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	TenantID   string          `json:"tenant_id"`
	Data       json.RawMessage `json:"data"`
}

// Consumer is a durable, idempotent subscription owned by a module.
type Consumer struct {
	Name    string
	Subject string
	Handle  func(ctx context.Context, evt Event) error
}

// Bus placeholder until Task 6 implements bus.go.
type Bus struct{}
