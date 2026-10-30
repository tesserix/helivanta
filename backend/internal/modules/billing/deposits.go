package billing

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Deposits (#199 slice 1): taken as receipts (cash.go), held for the patient,
// and applied against the account once a final bill is issued.

var (
	errDepositApplied = errors.New("this deposit has been applied against the bill and cannot be voided")
	errNothingToApply = errors.New("nothing to apply: no deposit is held or nothing is due")
	errNoFinalBill    = errors.New("deposits are applied once a final bill is issued")
)

// balanceDue is what is owed less payments and applied deposits.
func balanceDue(tx *gorm.DB, tenant string, admission uuid.UUID, currency string) (int64, error) {
	var v struct{ Due int64 }
	err := tx.Raw(`SELECT billing_owed(?,?,?) - billing_received(?,?,?,'payment') - billing_applied(?,?,?) AS due`,
		tenant, admission, currency, tenant, admission, currency, tenant, admission, currency).Scan(&v).Error
	return v.Due, err
}

// depositHeld is what deposits brought in, less what was applied.
func depositHeld(tx *gorm.DB, tenant string, admission uuid.UUID, currency string) (int64, error) {
	var v struct{ Held int64 }
	err := tx.Raw(`SELECT billing_received(?,?,?,'deposit') - billing_applied(?,?,?) AS held`,
		tenant, admission, currency, tenant, admission, currency).Scan(&v).Error
	return v.Held, err
}

type depositApplication struct {
	ID          uuid.UUID `json:"id"`
	AdmissionID uuid.UUID `json:"admission_id"`
	BillNumber  int       `json:"bill_number"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	AppliedBy   string    `json:"applied_by"`
	AppliedAt   time.Time `json:"applied_at"`
}

func (depositApplication) TableName() string                 { return "billing_deposit_applications" }
func (a depositApplication) PageKey() (time.Time, uuid.UUID) { return a.AppliedAt, a.ID }

// applyDeposits applies what is held against the account, up to the balance
// due, against the latest final bill. The amount is the server's.
func (h *handlers) applyDeposits(c *gin.Context) {
	admission, err := uuid.Parse(c.Param("admission"))
	id, ok := v7(c, "application")
	if err != nil || !ok {
		respond.NotFound(c, "account")
		return
	}
	var out depositApplication
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		var held []depositApplication
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Limit(1).Find(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			if held[0].AdmissionID != admission || held[0].AppliedBy != p.Subject {
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
		var final []int
		if err := tx.Raw(`SELECT number FROM billing_interim_bills WHERE tenant_id=? AND admission_id=? AND kind='final' ORDER BY number DESC LIMIT 1`, p.TenantID, admission).Scan(&final).Error; err != nil {
			return err
		}
		if len(final) == 0 {
			return errNoFinalBill
		}
		due, err := balanceDue(tx, p.TenantID, admission, shiftCurrency)
		if err != nil {
			return err
		}
		have, err := depositHeld(tx, p.TenantID, admission, shiftCurrency)
		if err != nil {
			return err
		}
		amount := min(due, have)
		if amount < 1 {
			return errNothingToApply
		}
		if err := tx.Exec(`INSERT INTO billing_deposit_applications (tenant_id,hospital_id,id,admission_id,bill_number,amount_minor,currency,applied_by) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, id, admission, final[0], amount, shiftCurrency, p.Subject).Error; err != nil {
			return err
		}
		created = true
		return tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error
	})
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errNoFinalBill), errors.As(err, &pg) && pg.ConstraintName == "billing_deposit_final_bill":
		respond.Error(c, http.StatusConflict, "no_final_bill", errNoFinalBill.Error())
	case errors.Is(err, errNothingToApply), errors.As(err, &pg) && pg.ConstraintName == "billing_deposit_application_bound":
		respond.Error(c, http.StatusConflict, "nothing_to_apply", errNothingToApply.Error())
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "billing_deposit_applications_pkey":
		respond.Conflict(c, errCashConflict.Error())
	case err != nil:
		failure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

func (h *handlers) depositApplications(c *gin.Context, params pagination.Params) ([]depositApplication, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []depositApplication
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		return tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "applied_at", Direction: pagination.NewestFirst}).
			Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
