package reference

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	referencecontract "github.com/tesserix/helivanta/internal/modules/reference/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
)

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "reference-receipts",
		Subject: referencecontract.SubjectPinged,
		Handle: func(ctx_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d referencecontract.PingedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// tenant_id is NOT NULL; the bus already scoped this tx to
			// evt.TenantID (Task 1), but the column still needs an
			// explicit value on insert.
			return tx.Exec(`INSERT INTO reference_ping_receipts (event_id, ping_id, tenant_id) VALUES (?, ?, ?)
				ON CONFLICT DO NOTHING`, evt.ID, d.PingID, evt.TenantID).Error
		},
	}}
}

// Broadcasts declares none: reference has no per-replica cache to
// invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
