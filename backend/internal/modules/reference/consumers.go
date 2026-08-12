package reference

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/events"
)

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "reference-receipts",
		Subject: SubjectPinged,
		Handle: func(ctx_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d pingedData
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
