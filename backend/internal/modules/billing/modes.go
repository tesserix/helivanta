package billing

import (
	"errors"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

// Card, netbanking and wallet receipts (#198 slice 6): money the counter's
// terminal or the bank already confirmed, recorded with its reference at a
// counter set up to take it, never in the drawer.

var (
	errModeNotAccepted    = errors.New("this counter does not take that mode; an administrator switches it on")
	errDuplicateReference = errors.New("a receipt standing at this hospital already has this reference")
	receiptReference      = regexp.MustCompile(`^[A-Z0-9][A-Z0-9/-]{3,39}$`)
	approvalCode          = regexp.MustCompile(`^[A-Z0-9]{6}$`)
	walletName            = regexp.MustCompile(`^[a-z0-9]([a-z0-9 .-]{0,28}[a-z0-9])?$`)
	// The modes a counter switches; cash and UPI are not switched here.
	switchedModes = map[string]bool{"card": true, "netbanking": true, "wallet": true}
)

// tender is how a receipt is paid and the reference its mode carries.
type tender struct {
	Mode         string  `json:"mode"`
	Reference    *string `json:"reference"`
	ApprovalCode *string `json:"approval_code"`
	Wallet       *string `json:"wallet"`
}

// normalise puts the references in their one stored form and reports
// whether the mode, its fields and the purpose fit: refunds stay cash, and a
// field another mode needs is refused rather than dropped.
func (t *tender) normalise(purpose string) bool {
	if t.Mode == "" {
		t.Mode = "cash"
	}
	upper := func(s *string) *string {
		if s == nil {
			return nil
		}
		v := strings.ToUpper(strings.TrimSpace(*s))
		return &v
	}
	t.Reference, t.ApprovalCode = upper(t.Reference), upper(t.ApprovalCode)
	if t.Wallet != nil {
		v := strings.ToLower(strings.TrimSpace(*t.Wallet))
		t.Wallet = &v
	}
	if t.Mode == "cash" {
		return t.Reference == nil && t.ApprovalCode == nil && t.Wallet == nil
	}
	return switchedModes[t.Mode] && purpose != "refund" &&
		t.Reference != nil && receiptReference.MatchString(*t.Reference) &&
		(t.ApprovalCode != nil) == (t.Mode == "card") && (t.ApprovalCode == nil || approvalCode.MatchString(*t.ApprovalCode)) &&
		(t.Wallet != nil) == (t.Mode == "wallet") && (t.Wallet == nil || walletName.MatchString(*t.Wallet))
}

// same reports whether a held receipt was paid as t says.
func (t tender) same(r receipt) bool {
	eq := func(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	return r.Mode == t.Mode && eq(r.Reference, t.Reference) && eq(r.ApprovalCode, t.ApprovalCode) && eq(r.Wallet, t.Wallet)
}

// counterQuery reads counters with the modes each takes now.
func counterQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&counter{}).Select("*, billing_counter_mode_list(tenant_id,id) AS modes")
}

// putCounterMode switches card, netbanking or wallet on or off at a counter.
// Asking for the state it is in records nothing, so a retry is harmless.
func (h *handlers) putCounterMode(c *gin.Context) {
	counterID, ok := v7(c, "counter")
	mode := c.Param("mode")
	var in struct {
		Accepted *bool `json:"accepted"`
	}
	if err := c.ShouldBindJSON(&in); !ok || err != nil || !switchedModes[mode] || in.Accepted == nil {
		respond.BadRequest(c, errors.New("a mode of card, netbanking or wallet, and accepted true or false, are required"))
		return
	}
	var out counter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// The counter lock orders the change with every receipt at the counter.
		var held []counter
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, counterID).Find(&held).Error; err != nil {
			return err
		}
		if len(held) == 0 {
			return errNoCounter
		}
		var now struct{ V bool }
		if err := tx.Raw(`SELECT billing_counter_accepts(?,?,?) AS v`, p.TenantID, counterID, mode).Scan(&now).Error; err != nil {
			return err
		}
		if now.V != *in.Accepted {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO billing_counter_modes (tenant_id,hospital_id,id,counter_id,mode,accepted,set_by) VALUES (?,?,?,?,?,?,?)`,
				p.TenantID, hospital, id, counterID, mode, *in.Accepted, p.Subject).Error; err != nil {
				return err
			}
		}
		return counterQuery(tx).Where("tenant_id=? AND id=?", p.TenantID, counterID).Take(&out).Error
	})
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}
