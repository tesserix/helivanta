package pharmacy

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/hms/internal/modules/medicore/contract"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/events"
)

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "pharmacy-visit-intake",
		Subject: medicorecontract.SubjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.VisitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// The bus scoped this tx to evt.TenantID (Task 1), so this
			// RLS-forced insert lands under the visit's tenant.
			return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}

// Broadcasts declares none: pharmacy has no per-replica cache to
// invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
