package events

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
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
//
// Handle runs in the SAME tx as the idempotency claim — do not open your
// own transaction; rollback of the claim implies rollback of handler
// effects.
type Consumer struct {
	Name    string
	Subject string
	Handle  func(ctx context.Context, tx *gorm.DB, evt Event) error
}
