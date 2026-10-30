package medicore

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

// Discharge orders (#96 slice 1): the doctor's decision that an admitted
// patient is fit for discharge. The release stays the departure.

var (
	errAlreadyOrdered = errors.New("discharge is already ordered for this admission; withdraw it first")
	errNotInBed       = errors.New("the patient is no longer admitted")
	errNoOrder        = errors.New("discharge order not found")
)

var dispositions = map[string]bool{"home": true, "transfer_out": true}

// dischargeOrder is an order as the ward and the doctor see it.
type dischargeOrder struct {
	ID               uuid.UUID  `json:"id"`
	AdmissionID      uuid.UUID  `json:"admission_id"`
	Disposition      string     `json:"disposition"`
	Note             *string    `json:"note"`
	OrderedBy        string     `json:"ordered_by"`
	OrderedAt        time.Time  `json:"ordered_at"`
	WithdrawalID     *uuid.UUID `json:"-"`
	WithdrawalReason *string    `json:"withdrawal_reason"`
	WithdrawnBy      *string    `json:"withdrawn_by"`
	WithdrawnAt      *time.Time `json:"withdrawn_at"`
}

func (dischargeOrder) TableName() string { return "medicore_discharge_orders" }

// trimmed bounds an optional or required text to 1-500 characters.
func trimmed(s *string) (*string, bool) {
	if s == nil {
		return nil, true
	}
	t := strings.TrimSpace(*s)
	return &t, t != "" && len([]rune(t)) <= 500
}

func dischargeFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "medicore_discharge_active":
		respond.Error(c, http.StatusConflict, "already_ordered", errAlreadyOrdered.Error())
	case errors.Is(err, errNotInBed), errors.As(err, &pg) && pg.ConstraintName == "medicore_discharge_admitted":
		respond.Error(c, http.StatusConflict, "not_admitted", errNotInBed.Error())
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "medicore_discharge_withdrawal_id":
		// The withdrawal id was used for another withdrawal.
		respond.Conflict(c, errIntentConflict.Error())
	case errors.Is(err, errNoOrder):
		respond.NotFound(c, "discharge order")
	default:
		bedFailure(c, err)
	}
}

// lockAdmitted locks the admission, the same lock a transfer and release
// take, so an order and a release are one after the other.
func lockAdmitted(tx *gorm.DB, tenant string, hospital, id uuid.UUID, a *admission) error {
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errNoAdmission
		}
		return err
	}
	return nil
}

func loadOrder(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (dischargeOrder, error) {
	var rows []dischargeOrder
	if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Limit(1).Find(&rows).Error; err != nil {
		return dischargeOrder{}, err
	}
	if len(rows) == 0 {
		return dischargeOrder{}, errNoOrder
	}
	return rows[0], nil
}

// orderDischarge records the doctor's order: 201, or 200 for the same order
// again.
func (h *encounterHandlers) orderDischarge(c *gin.Context) {
	admissionID, ok1 := v7Param(c, "admission")
	orderID, ok2 := v7Param(c, "order")
	if !ok1 || !ok2 {
		respond.NotFound(c, "admission")
		return
	}
	var in struct {
		Disposition string  `json:"disposition" binding:"required"`
		Note        *string `json:"note"`
	}
	err := c.ShouldBindJSON(&in)
	note, noteOK := trimmed(in.Note)
	if err != nil || !dispositions[in.Disposition] || !noteOK {
		respond.BadRequest(c, errors.New("disposition is home or transfer_out; a note is 1 to 500 characters"))
		return
	}
	var out dischargeOrder
	created := false
	err = h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var a admission
		if err := lockAdmitted(tx, p.TenantID, hospital, admissionID, &a); err != nil {
			return err
		}
		// A replay is answered from the record, whatever has happened since.
		existing, err := loadOrder(tx, p.TenantID, hospital, orderID)
		if err == nil {
			if existing.AdmissionID != admissionID || existing.Disposition != in.Disposition || !sameNote(existing.Note, note) || existing.OrderedBy != p.Subject {
				return errIntentConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, errNoOrder) {
			return err
		}
		if a.Status != "admitted" {
			return errNotInBed
		}
		// A second active order is refused by medicore_discharge_active.
		at, err := now(tx)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_discharge_orders (tenant_id,hospital_id,id,admission_id,disposition,note,ordered_by,ordered_at) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, orderID, admissionID, in.Disposition, note, p.Subject, at).Error; err != nil {
			return err
		}
		if err := publish(tx, p.TenantID, medicorecontract.SubjectDischargeOrdered, "DischargeOrdered", medicorecontract.DischargeOrderedData{
			OrderID: orderID.String(), AdmissionID: admissionID.String(), PatientID: a.PatientID.String(), HospitalID: hospital.String(),
			Disposition: in.Disposition, OrderedAt: at.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		created = true
		out, err = loadOrder(tx, p.TenantID, hospital, orderID)
		return err
	})
	switch {
	case err != nil:
		dischargeFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

// withdrawDischarge withdraws an active order while the patient is admitted.
// The withdrawal has its own command id, so a retry is answered from the
// record and a second, different withdrawal is refused.
func (h *encounterHandlers) withdrawDischarge(c *gin.Context) {
	admissionID, ok1 := v7Param(c, "admission")
	orderID, ok2 := v7Param(c, "order")
	withdrawalID, ok3 := v7Param(c, "withdrawal")
	if !ok1 || !ok2 || !ok3 {
		respond.NotFound(c, "discharge order")
		return
	}
	var in struct {
		Reason *string `json:"reason" binding:"required"`
	}
	err := c.ShouldBindJSON(&in)
	reason, reasonOK := trimmed(in.Reason)
	if err != nil || reason == nil || !reasonOK {
		respond.BadRequest(c, errors.New("a reason of 1 to 500 characters is required"))
		return
	}
	var out dischargeOrder
	err = h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var a admission
		if err := lockAdmitted(tx, p.TenantID, hospital, admissionID, &a); err != nil {
			return err
		}
		o, err := loadOrder(tx, p.TenantID, hospital, orderID)
		if err != nil {
			return err
		}
		if o.AdmissionID != admissionID {
			return errNoOrder
		}
		if o.WithdrawnAt != nil {
			if *o.WithdrawalID != withdrawalID || *o.WithdrawalReason != *reason || *o.WithdrawnBy != p.Subject {
				return errIntentConflict
			}
			out = o
			return nil
		}
		if a.Status != "admitted" {
			return errNotInBed
		}
		at, err := now(tx)
		if err != nil {
			return err
		}
		if err := exactlyOne(tx.Exec(`UPDATE medicore_discharge_orders SET withdrawal_id=?,withdrawal_reason=?,withdrawn_by=?,withdrawn_at=? WHERE tenant_id=? AND id=? AND withdrawn_at IS NULL`,
			withdrawalID, reason, p.Subject, at, p.TenantID, orderID)); err != nil {
			return err
		}
		if err := publish(tx, p.TenantID, medicorecontract.SubjectDischargeWithdrawn, "DischargeWithdrawn", medicorecontract.DischargeWithdrawnData{
			OrderID: orderID.String(), AdmissionID: admissionID.String(), HospitalID: hospital.String(), WithdrawnAt: at.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		out, err = loadOrder(tx, p.TenantID, hospital, orderID)
		return err
	})
	if err != nil {
		dischargeFailure(c, err)
		return
	}
	respond.OK(c, out)
}
