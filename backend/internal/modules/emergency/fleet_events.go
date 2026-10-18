package emergency

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func fleetChanged(tx *gorm.DB, p authn.Principal, b, id uuid.UUID, operation string) error {
	if err := audit(tx, p, b, id, "fleet."+operation, nil); err != nil {
		return err
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	data, err := json.Marshal(emergencycontract.FleetChangedData{BaseID: b.String(), ResourceID: id.String(), Operation: operation})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, emergencycontract.SubjectFleetChanged, events.Event{ID: eventID.String(), Type: "FleetChanged", Version: 1, TenantID: p.TenantID, OccurredAt: time.Now().UTC(), Data: data})
}
