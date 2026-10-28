package patientmaster

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// replayedRegistration is one patient whose registration was announced
// again: its id only, the same reference registered.v1 carries.
type replayedRegistration struct {
	ID        uuid.UUID `json:"patient_id"`
	CreatedAt time.Time `json:"-"`
}

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (r replayedRegistration) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// replayRegistrations re-publishes registered.v1 for one page of a
// hospital's existing patients (#1377), so a consumer that started after
// they registered (MediCore's encounter projection, #80) learns of them.
// Bounded by the page limit and resumable by cursor; each page is one
// transaction, audited as action 'replay' with the ids announced.
// Consumers already ignore duplicates, so repeating a page is harmless.
// It needs a patient-registry grant at the hospital as well as the
// admin-only permission, like every other patient read.
func (h *patientHandlers) replayRegistrations(c *gin.Context, params pagination.Params) ([]replayedRegistration, error) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		patientError(c, gorm.ErrRecordNotFound)
		return nil, nil
	}
	var rows []replayedRegistration
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx.Table("patients").Select("id, created_at"), params).
			Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		// The probe row past the limit is the next page's first; it is
		// announced there, not twice.
		page := rows
		if len(page) > params.Limit {
			page = page[:params.Limit]
		}
		audited := make([]patient, len(page))
		for i, row := range page {
			data, err := json.Marshal(patientmastercontract.PatientRegisteredData{PatientID: row.ID.String(), HospitalID: hospital.String()})
			if err != nil {
				return fmt.Errorf("marshaling patient.registered payload: %w", err)
			}
			if err := h.bus.Publish(tx, patientmastercontract.SubjectPatientRegistered, events.Event{
				Type: "PatientRegistered", Version: 1, TenantID: p.TenantID, Data: data,
			}); err != nil {
				return fmt.Errorf("publishing patient.registered: %w", err)
			}
			audited[i] = patient{ID: row.ID}
		}
		return patientAudit(tx, p, hospital, "replay", audited)
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}
