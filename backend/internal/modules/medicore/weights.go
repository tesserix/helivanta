package medicore

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

var errNoWeight = errors.New("weight not found")

type weight struct {
	ID             uuid.UUID  `gorm:"column:id" json:"id"`
	WeightKg       string     `gorm:"column:weight_kg" json:"weight_kg"`
	MeasuredAt     time.Time  `gorm:"column:measured_at" json:"measured_at"`
	Method         string     `gorm:"column:method" json:"method"`
	EncounterID    *uuid.UUID `gorm:"column:encounter_id" json:"encounter_id"`
	RecordedBy     string     `gorm:"column:recorded_by" json:"recorded_by"`
	RecordedAt     time.Time  `gorm:"column:recorded_at" json:"recorded_at"`
	EnteredInError bool       `gorm:"column:entered_in_error" json:"entered_in_error"`
	ErrorReason    *string    `gorm:"column:error_reason" json:"error_reason"`
}

// PageKey orders weights by when they were measured, newest first.
func (w weight) PageKey() (time.Time, uuid.UUID) { return w.MeasuredAt, w.ID }

const weightQuery = `SELECT w.id, w.weight_kg::text AS weight_kg, w.measured_at, w.method, w.encounter_id, w.recorded_by, w.recorded_at,
	(e.observation_id IS NOT NULL) AS entered_in_error, e.reason AS error_reason
	FROM medicore_weight_observations w
	LEFT JOIN medicore_weight_errors e ON e.tenant_id = w.tenant_id AND e.observation_id = w.id`

type recordWeightRequest struct {
	Value       string  `json:"value" binding:"required"`
	Unit        string  `json:"unit" binding:"required,oneof=kg g"`
	MeasuredAt  string  `json:"measured_at" binding:"required"`
	Method      string  `json:"method" binding:"required,oneof=measured estimated reported"`
	EncounterID *string `json:"encounter_id"`
}

var weightValueRe = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,3})?$`)

// kilograms converts an exact decimal in kg or g to kg with three
// decimals, refusing anything that would round (#1373 A7).
func kilograms(value, unit string) (string, error) {
	if !weightValueRe.MatchString(value) {
		return "", errors.New("value must be a decimal with at most three decimal places")
	}
	r, _ := new(big.Rat).SetString(value)
	if unit == "g" {
		r.Quo(r, big.NewRat(1000, 1))
	}
	// Exactly representable in grams: three decimals of a kilogram.
	if !new(big.Rat).Mul(r, big.NewRat(1000, 1)).IsInt() {
		return "", errors.New("value is finer than a gram")
	}
	if r.Cmp(big.NewRat(1, 5)) < 0 || r.Cmp(big.NewRat(500, 1)) > 0 {
		return "", errors.New("weight must be between 0.2 and 500 kg")
	}
	return r.FloatString(3), nil
}

func (h *clinicalHandlers) recordWeight(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		respond.NotFound(c, "weight")
		return
	}
	var req recordWeightRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	kg, err := kilograms(strings.TrimSpace(req.Value), req.Unit)
	if err != nil {
		respond.BadRequest(c, err)
		return
	}
	measured, err := time.Parse(time.RFC3339, req.MeasuredAt)
	// Up to a minute ahead is clock skew between devices, not a future
	// measurement; the table refuses more than five, so this is stricter.
	if err != nil || measured.After(time.Now().Add(time.Minute)) || measured.Year() < 1900 {
		respond.BadRequest(c, errors.New("measured_at must be an RFC 3339 time, not in the future"))
		return
	}
	encounter, err := optionalUUID(req.EncounterID)
	if err != nil {
		respond.BadRequest(c, err)
		return
	}
	var row weight
	created := false
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		intent := digest(hospital.String(), patient.String(), kg, measured.UTC().Format(time.RFC3339Nano), req.Method, deref(req.EncounterID))
		var existing []struct {
			IntentSHA256, RecordedBy string
		}
		if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM medicore_weight_observations WHERE tenant_id = ? AND id = ?`, p.TenantID, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != intent || existing[0].RecordedBy != p.Subject {
				return errIntentConflict
			}
		} else {
			insert := tx.Exec(`INSERT INTO medicore_weight_observations (tenant_id, id, hospital_id, patient_id, weight_kg, measured_at, method, encounter_id, intent_sha256, recorded_by)
				VALUES (?, ?, ?, ?, ?::numeric, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, p.TenantID, id, hospital, patient, kg, measured, req.Method, encounter, intent, p.Subject)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				var won []struct{ IntentSHA256, RecordedBy string }
				if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM medicore_weight_observations WHERE tenant_id = ? AND id = ?`, p.TenantID, id).Scan(&won).Error; err != nil {
					return err
				}
				if len(won) != 1 || won[0].IntentSHA256 != intent || won[0].RecordedBy != p.Subject {
					return errIntentConflict
				}
				return tx.Raw(weightQuery+` WHERE w.tenant_id = ? AND w.hospital_id = ? AND w.patient_id = ? AND w.id = ?`, p.TenantID, hospital, patient, id).Scan(&row).Error
			}
			created = true
			if err := publish(tx, p.TenantID, medicorecontract.SubjectWeightRecorded, "WeightRecorded", medicorecontract.WeightRecordedData{
				PatientID: patient.String(), HospitalID: hospital.String(), ObservationID: id.String(),
			}); err != nil {
				return err
			}
		}
		return tx.Raw(weightQuery+` WHERE w.tenant_id = ? AND w.hospital_id = ? AND w.patient_id = ? AND w.id = ?`, p.TenantID, hospital, patient, id).Scan(&row).Error
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	if row.ID == uuid.Nil {
		// The id was used for another patient or hospital.
		respond.Conflict(c, errIntentConflict.Error())
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

type weightErrorRequest struct {
	Reason string `json:"reason" binding:"required,max=500"`
}

// weightEnteredInError marks a weight wrong; the observation stays.
func (h *clinicalHandlers) weightEnteredInError(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "weight")
		return
	}
	var req weightErrorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		respond.BadRequest(c, errReasonRequired)
		return
	}
	var row weight
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		var rows []weight
		if err := tx.Raw(weightQuery+` WHERE w.tenant_id = ? AND w.hospital_id = ? AND w.patient_id = ? AND w.id = ? FOR UPDATE OF w`,
			p.TenantID, hospital, patient, id).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoWeight
		}
		if rows[0].EnteredInError {
			return errIllegalMove
		}
		// The lock serialises two markings, but the second still read the row
		// before the first committed, so the insert decides who won.
		insert := tx.Exec(`INSERT INTO medicore_weight_errors (tenant_id, observation_id, reason, recorded_by) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			p.TenantID, id, reason, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errIllegalMove
		}
		if err := publish(tx, p.TenantID, medicorecontract.SubjectWeightRecorded, "WeightRecorded", medicorecontract.WeightRecordedData{
			PatientID: patient.String(), HospitalID: hospital.String(), ObservationID: id.String(), EnteredInError: true,
		}); err != nil {
			return err
		}
		return tx.Raw(weightQuery+` WHERE w.tenant_id = ? AND w.id = ?`, p.TenantID, id).Scan(&row).Error
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	respond.OK(c, row)
}

// weights lists a patient's weights, newest measurement first, including
// those entered in error (marked), so the history is complete.
func (h *clinicalHandlers) weights(c *gin.Context, params pagination.Params) ([]weight, error) {
	var rows []weight
	err := h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		q, args := weightQuery+` WHERE w.tenant_id = ? AND w.hospital_id = ? AND w.patient_id = ?`, []any{p.TenantID, hospital, patient}
		if params.After != nil {
			q, args = q+` AND (w.measured_at, w.id) < (?, ?)`, append(args, params.After.CreatedAt, params.After.ID)
		}
		if err := tx.Raw(q+` ORDER BY w.measured_at DESC, w.id DESC LIMIT ?`, append(args, params.Limit+1)...).Scan(&rows).Error; err != nil {
			return err
		}
		return readAudit(tx, p, hospital, patient, "weights")
	})
	if err != nil {
		clinicalFailure(c, err)
		return nil, nil
	}
	return rows, nil
}

type latestWeight struct {
	weight
	// AgeDays is whole days since the measurement, so a check can require
	// a recent weight; how recent is a clinical rule (#87), not this one.
	AgeDays int `json:"age_days"`
}

func (h *clinicalHandlers) latestWeight(c *gin.Context) {
	var rows []weight
	err := h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		if err := tx.Raw(weightQuery+` WHERE w.tenant_id = ? AND w.hospital_id = ? AND w.patient_id = ? AND e.observation_id IS NULL
			ORDER BY w.measured_at DESC, w.id DESC LIMIT 1`, p.TenantID, hospital, patient).Scan(&rows).Error; err != nil {
			return err
		}
		return readAudit(tx, p, hospital, patient, "weights")
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	if len(rows) == 0 {
		respond.NotFound(c, "weight")
		return
	}
	respond.OK(c, latestWeight{weight: rows[0], AgeDays: int(time.Since(rows[0].MeasuredAt).Hours() / 24)})
}
