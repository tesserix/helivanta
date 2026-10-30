package billing

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Refunds of unused deposits (#199 slice 2): a receipt with purpose refund
// (cash.go), after a final bill, within what is held and the drawer, and
// above the hospital's threshold against someone else's approval.

// defaultRefundThreshold is ₹10,000 in paise, used until a hospital sets one.
const defaultRefundThreshold = 1_000_000

var (
	errOverHeld         = errors.New("the refund is more than the deposits held beyond what the bill still needs")
	errDrawerShort      = errors.New("the drawer does not hold enough cash for this refund")
	errApprovalRequired = errors.New("a refund above the threshold needs a supervisor's approval for exactly this amount")
	errOwnApproval      = errors.New("a refund is approved by someone other than the cashier who pays it")
)

func sameID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// refundable says why a refund cannot be paid; the database checks the same.
func refundable(tx *gorm.DB, tenant string, hospital, admission, shift uuid.UUID, cashier string, amount int64, approval *uuid.UUID) error {
	var v struct {
		Final               bool
		Held, Drawer, Limit int64
	}
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM billing_interim_bills WHERE tenant_id=? AND admission_id=? AND kind='final') AS final,
 billing_refundable(?,?,?) AS held, billing_shift_expected(?,?) AS drawer, billing_refund_threshold(?,?) AS "limit"`,
		tenant, admission, tenant, admission, shiftCurrency, tenant, shift, tenant, hospital).Scan(&v).Error; err != nil {
		return err
	}
	switch {
	case !v.Final:
		return errNoFinalBill
	case amount > v.Held:
		return errOverHeld
	case amount > v.Drawer:
		return errDrawerShort
	case amount <= v.Limit && approval == nil:
		return nil
	case approval == nil:
		return errApprovalRequired
	}
	var a []refundApproval
	if err := tx.Where("tenant_id=? AND id=?", tenant, *approval).Limit(1).Find(&a).Error; err != nil {
		return err
	}
	if len(a) == 0 || a[0].AdmissionID != admission || a[0].AmountMinor != amount {
		return errApprovalRequired
	}
	if a[0].ApprovedBy == cashier {
		return errOwnApproval
	}
	var used int64
	if err := tx.Table("billing_cash_receipts").Where("tenant_id=? AND approval_id=?", tenant, *approval).Count(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return errApprovalRequired
	}
	return nil
}

type refundPolicy struct {
	HospitalID     uuid.UUID  `json:"hospital_id"`
	ThresholdMinor int64      `json:"threshold_minor"`
	Currency       string     `json:"currency"`
	CreatedBy      *string    `json:"created_by"`
	CreatedAt      *time.Time `json:"created_at"`
}

// refundPolicyOf is the latest policy, or the default with no author.
func refundPolicyOf(tx *gorm.DB, tenant string, hospital uuid.UUID) (refundPolicy, error) {
	var rows []refundPolicy
	if err := tx.Raw(`SELECT hospital_id,threshold_minor,currency,created_by,created_at FROM billing_refund_policies WHERE tenant_id=? AND hospital_id=? ORDER BY created_at DESC,id DESC LIMIT 1`,
		tenant, hospital).Scan(&rows).Error; err != nil {
		return refundPolicy{}, err
	}
	if len(rows) == 0 {
		return refundPolicy{HospitalID: hospital, ThresholdMinor: defaultRefundThreshold, Currency: shiftCurrency}, nil
	}
	return rows[0], nil
}

func (h *handlers) refundPolicy(c *gin.Context) {
	var out refundPolicy
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = refundPolicyOf(tx, p.TenantID, hospital)
		return err
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}

// setRefundPolicy records a new threshold version; the same id again replays.
func (h *handlers) setRefundPolicy(c *gin.Context) {
	id, ok := v7(c, "policy")
	var in struct {
		ThresholdMinor *int64 `json:"threshold_minor"`
	}
	if err := c.ShouldBindJSON(&in); !ok || err != nil || in.ThresholdMinor == nil || *in.ThresholdMinor < 0 || *in.ThresholdMinor > 1_000_000_000 {
		respond.BadRequest(c, errors.New("a threshold of 0 to 1000000000 minor units is required"))
		return
	}
	var out refundPolicy
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var held []refundPolicy
		if err := tx.Raw(`SELECT hospital_id,threshold_minor,currency,created_by,created_at FROM billing_refund_policies WHERE tenant_id=? AND id=?`, p.TenantID, id).Scan(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			if held[0].HospitalID != hospital || held[0].ThresholdMinor != *in.ThresholdMinor || held[0].CreatedBy == nil || *held[0].CreatedBy != p.Subject {
				return errCashConflict
			}
			out = held[0]
			return nil
		}
		if err := tx.Exec(`INSERT INTO billing_refund_policies (tenant_id,hospital_id,id,threshold_minor,currency,created_by) VALUES (?,?,?,?,?,?)`,
			p.TenantID, hospital, id, *in.ThresholdMinor, shiftCurrency, p.Subject).Error; err != nil {
			return err
		}
		created = true
		var err error
		out, err = refundPolicyOf(tx, p.TenantID, hospital)
		return err
	})
	switch {
	case err != nil:
		cashFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

type refundApproval struct {
	ID          uuid.UUID `json:"id"`
	AdmissionID uuid.UUID `json:"admission_id"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	Reason      string    `json:"reason"`
	ApprovedBy  string    `json:"approved_by"`
	ApprovedAt  time.Time `json:"approved_at"`
	// The refund receipt that used it, once paid.
	UsedBy *uuid.UUID `json:"used_by" gorm:"->"`
}

func (refundApproval) TableName() string                 { return "billing_refund_approvals" }
func (a refundApproval) PageKey() (time.Time, uuid.UUID) { return a.ApprovedAt, a.ID }

// approveRefund approves one amount for one admission, within what is held.
func (h *handlers) approveRefund(c *gin.Context) {
	admission, err := uuid.Parse(c.Param("admission"))
	id, ok := v7(c, "approval")
	var in struct {
		AmountMinor int64   `json:"amount_minor"`
		Reason      *string `json:"reason"`
	}
	bindErr := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Reason)
	if err != nil || !ok || bindErr != nil || !whyOK || in.AmountMinor < 1 || in.AmountMinor > 1_000_000_000 {
		respond.BadRequest(c, errors.New("an amount of 1 to 1000000000 minor units and a reason of 1 to 500 characters are required"))
		return
	}
	var out refundApproval
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		var held []refundApproval
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Limit(1).Find(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			if held[0].AdmissionID != admission || held[0].AmountMinor != in.AmountMinor || held[0].Reason != why || held[0].ApprovedBy != p.Subject {
				return errCashConflict
			}
			out = held[0]
			return nil
		}
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		// An approval is for the surplus over the bill, as the refund is.
		var surplus struct{ V int64 }
		if err := tx.Raw(`SELECT billing_refundable(?,?,?) AS v`, p.TenantID, admission, shiftCurrency).Scan(&surplus).Error; err != nil {
			return err
		}
		if in.AmountMinor > surplus.V {
			return errOverHeld
		}
		if err := tx.Exec(`INSERT INTO billing_refund_approvals (tenant_id,hospital_id,id,admission_id,amount_minor,currency,reason,approved_by) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, id, admission, in.AmountMinor, shiftCurrency, why, p.Subject).Error; err != nil {
			return err
		}
		created = true
		return tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error
	})
	switch {
	case err != nil:
		cashFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

func (h *handlers) refundApprovals(c *gin.Context, params pagination.Params) ([]refundApproval, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []refundApproval
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		q := tx.Table(`(SELECT a.*,(SELECT r.id FROM billing_cash_receipts r WHERE r.tenant_id=a.tenant_id AND r.approval_id=a.id) AS used_by
 FROM billing_refund_approvals a WHERE a.tenant_id=? AND a.hospital_id=? AND a.admission_id=?) AS approvals`, p.TenantID, hospital, admission)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "approved_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
