package events

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Event is the platform envelope (issue #2).
type Event struct {
	ID         string    `json:"event_id"`
	Type       string    `json:"type"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	TenantID   string    `json:"tenant_id"`
	// DestinationTenantID, when set, is the tenant whose data this event
	// creates. TenantID stays the ORIGIN — who published, and therefore
	// who is accountable for the disclosure — so a cross-organisation
	// handoff is auditable from the envelope alone (design D1).
	//
	// omitempty is load-bearing, not cosmetic: without it every event
	// ever published would gain a key, changing the bytes stored in
	// outbox_events for payloads that have not otherwise changed.
	DestinationTenantID string          `json:"destination_tenant_id,omitempty"`
	Data                json.RawMessage `json:"data"`
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
