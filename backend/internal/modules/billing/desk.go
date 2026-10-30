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

// The billing desk (#197–#199): a hospital's admission accounts with their
// INR position, so the desk can settle a patient after they leave the ward
// board. Reads billing's own records only.

type deskAccount struct {
	ID               uuid.UUID  `json:"admission_id"`
	HospitalID       uuid.UUID  `json:"hospital_id"`
	PatientID        uuid.UUID  `json:"patient_id"`
	Status           string     `json:"status"`
	DischargeOrdered bool       `json:"discharge_ordered"`
	OpenedAt         *time.Time `json:"opened_at"`
	ClosedAt         *time.Time `json:"closed_at"`
	CreatedAt        time.Time  `json:"created_at"`
	// INR: what is left to pay (below zero is credit), what is held as
	// deposits, and the part of it that can be refunded now.
	BalanceMinor    int64 `json:"balance_minor"`
	HeldMinor       int64 `json:"deposit_held_minor"`
	RefundableMinor int64 `json:"refundable_minor"`
	HasFinalBill    bool  `json:"has_final_bill"`
}

func (a deskAccount) PageKey() (time.Time, uuid.UUID) { return a.CreatedAt, a.ID }

// deskViews narrow the list to who the desk needs to see.
var deskViews = map[string]string{
	"":                  "true",
	"open":              "status='open' AND NOT discharge_ordered",
	"discharge_ordered": "status='open' AND discharge_ordered",
	"to_collect":        "balance_minor > 0",
	"to_refund":         "refundable_minor > 0",
	"released":          "status='closed'",
}

var errUnknownView = errors.New("view is one of open, discharge_ordered, to_collect, to_refund, released")

func (h *handlers) deskAccounts(c *gin.Context, params pagination.Params) ([]deskAccount, error) {
	where, ok := deskViews[c.Query("view")]
	if !ok {
		respond.BadRequest(c, errUnknownView)
		return nil, nil
	}
	var rows []deskAccount
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// Each figure is the same function the money rules use, so the desk
		// never shows a balance the counter would compute differently.
		q := tx.Table(`(SELECT a.admission_id AS id,a.hospital_id,a.patient_id,a.status,a.opened_at,a.closed_at,a.created_at,
 billing_discharge_ordered(a.tenant_id,a.admission_id) AS discharge_ordered,
 billing_owed(a.tenant_id,a.admission_id,'INR') - billing_received(a.tenant_id,a.admission_id,'INR','payment') - billing_applied(a.tenant_id,a.admission_id,'INR') AS balance_minor,
 billing_held(a.tenant_id,a.admission_id,'INR') AS held_minor,
 CASE WHEN EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=a.tenant_id AND b.admission_id=a.admission_id AND b.kind='final')
  THEN GREATEST(billing_refundable(a.tenant_id,a.admission_id,'INR'),0) ELSE 0 END AS refundable_minor,
 EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=a.tenant_id AND b.admission_id=a.admission_id AND b.kind='final') AS has_final_bill
 FROM billing_admission_accounts a WHERE a.tenant_id=? AND a.hospital_id=?) AS accounts`, p.TenantID, hospital).Where(where)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
