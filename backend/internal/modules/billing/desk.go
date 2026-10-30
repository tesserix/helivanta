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

// deskViews narrow the list to who the desk needs to see; each matches one
// partial index on the positions (positions.sql). Fixed strings: no caller
// text reaches the query.
var deskViews = map[string]string{
	"":                  "true",
	"open":              "p.status='open' AND NOT p.discharge_ordered",
	"discharge_ordered": "p.status='open' AND p.discharge_ordered",
	"to_collect":        "p.balance_minor > 0",
	"to_refund":         "p.refundable_minor > 0",
	"released":          "p.status='closed'",
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
		// The stored position, kept current by every write that changes it
		// (positions.sql), read through the view's partial index.
		q := tx.Table(`(SELECT p.admission_id AS id,p.hospital_id,a.patient_id,p.status,p.discharge_ordered,a.opened_at,a.closed_at,p.account_created_at AS created_at,
 p.balance_minor,p.deposit_held_minor AS held_minor,p.refundable_minor,p.has_final_bill
 FROM billing_account_positions p JOIN billing_admission_accounts a ON a.tenant_id=p.tenant_id AND a.admission_id=p.admission_id
 WHERE p.tenant_id=? AND p.hospital_id=? AND `+where+`) AS accounts`, p.TenantID, hospital)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
