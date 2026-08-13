package lab

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/events"
)

type visitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "lab-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO lab_orders (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}

// Broadcasts declares none: lab has no per-replica cache to invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
