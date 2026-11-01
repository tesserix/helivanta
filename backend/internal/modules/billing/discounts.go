package billing

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
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

// Discounts on room charges (#200 slice 1): a hospital's reasons carry the
// desk's and the approver's limits; the database decides each request,
// re-checks it on approval and splits it over the exempt, uninvoiced room
// charges as discount entries (discounts.sql).

var (
	discountCode    = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	errNoReason     = errors.New("no such discount reason here")
	errNoDiscount   = errors.New("no such discount here")
	errDecided      = errors.New("the discount was already decided")
	errOwnDiscount  = errors.New("a discount is decided by someone other than who asked for it")
	errReasonActive = errors.New("an active reason already has this code")
)

type discountReason struct {
	ID                uuid.UUID  `json:"id"`
	HospitalID        uuid.UUID  `json:"hospital_id"`
	Code              string     `json:"code"`
	Name              string     `json:"name"`
	DeskPercentBp     int        `json:"desk_percent_bp"`
	DeskMaxMinor      int64      `json:"desk_max_minor"`
	ApprovalPercentBp int        `json:"approval_percent_bp"`
	ApprovalMaxMinor  int64      `json:"approval_max_minor"`
	Currency          string     `json:"currency"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	RetiredBy         *string    `json:"retired_by"`
	RetiredAt         *time.Time `json:"retired_at"`
}

func (discountReason) TableName() string                 { return "billing_discount_reasons" }
func (r discountReason) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type discount struct {
	ID            uuid.UUID  `json:"id"`
	AdmissionID   uuid.UUID  `json:"admission_id"`
	PatientID     uuid.UUID  `json:"patient_id" gorm:"->"`
	ReasonID      uuid.UUID  `json:"reason_id"`
	ReasonCode    string     `json:"reason_code" gorm:"->"`
	ReasonName    string     `json:"reason_name" gorm:"->"`
	AmountMinor   int64      `json:"amount_minor"`
	Currency      string     `json:"currency"`
	Justification string     `json:"justification"`
	Status        string     `json:"status"`
	RequestedBy   string     `json:"requested_by"`
	RequestedAt   time.Time  `json:"requested_at"`
	DecidedBy     *string    `json:"decided_by"`
	DecidedAt     *time.Time `json:"decided_at"`
	DecisionNote  *string    `json:"decision_note"`
	AppliedAt     *time.Time `json:"applied_at"`
	// What its lines take off now, less withdrawals from voided days.
	NetMinor int64 `json:"net_minor" gorm:"->"`
}

func (d discount) PageKey() (time.Time, uuid.UUID) { return d.RequestedAt, d.ID }

const discountColumns = `SELECT d.id,d.admission_id,a.patient_id,d.reason_id,r.code AS reason_code,r.name AS reason_name,d.amount_minor,d.currency,d.justification,d.status,
 d.requested_by,d.requested_at,d.decided_by,d.decided_at,d.decision_note,d.applied_at,
 COALESCE((SELECT sum(CASE x.kind WHEN 'reversal' THEN x.amount_minor ELSE -x.amount_minor END) FROM billing_account_entries x WHERE x.tenant_id=d.tenant_id AND x.discount_id=d.id),0) AS net_minor
 FROM billing_discounts d JOIN billing_discount_reasons r ON r.tenant_id=d.tenant_id AND r.id=d.reason_id
 JOIN billing_admission_accounts a ON a.tenant_id=d.tenant_id AND a.admission_id=d.admission_id`

func discountOf(tx *gorm.DB, tenant string, hospital, id uuid.UUID) ([]discount, error) {
	var rows []discount
	err := tx.Raw(discountColumns+` WHERE d.tenant_id=? AND d.hospital_id=? AND d.id=?`, tenant, hospital, id).Scan(&rows).Error
	return rows, err
}

func discountFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	is := func(name string) bool { return errors.As(err, &pg) && pg.ConstraintName == name }
	switch {
	case errors.Is(err, errNoReason):
		respond.NotFound(c, "discount reason")
	case errors.Is(err, errNoDiscount):
		respond.NotFound(c, "discount")
	case errors.Is(err, errDecided):
		respond.Error(c, http.StatusConflict, "already_decided", errDecided.Error())
	case errors.Is(err, errOwnDiscount), is("billing_discount_own_request"):
		respond.Error(c, http.StatusConflict, "own_request", errOwnDiscount.Error())
	case errors.Is(err, errReasonActive), errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "billing_discount_reasons_code":
		respond.Error(c, http.StatusConflict, "code_in_use", errReasonActive.Error())
	case is("billing_discount_reason_retired"):
		respond.Error(c, http.StatusConflict, "reason_retired", "the reason is retired; choose an active one")
	case is("billing_discount_room"):
		respond.Error(c, http.StatusConflict, "nothing_to_discount", "the amount is more than the exempt room charges not yet invoiced carry")
	case is("billing_discount_balance"):
		respond.Error(c, http.StatusConflict, "discount_over_balance", "the amount is more than the balance payable")
	case is("billing_discount_limit"):
		respond.Error(c, http.StatusConflict, "discount_over_limit", "the amount is beyond what the reason allows")
	default:
		cashFailure(c, err)
	}
}

// putDiscountReason sets a reason once; the same id and content again replays.
func (h *handlers) putDiscountReason(c *gin.Context) {
	id, ok := v7(c, "reason")
	var in struct {
		Code              string  `json:"code"`
		Name              *string `json:"name"`
		DeskPercentBp     *int    `json:"desk_percent_bp"`
		DeskMaxMinor      *int64  `json:"desk_max_minor"`
		ApprovalPercentBp *int    `json:"approval_percent_bp"`
		ApprovalMaxMinor  *int64  `json:"approval_max_minor"`
	}
	bindErr := c.ShouldBindJSON(&in)
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	bp := func(v *int) bool { return v != nil && *v >= 0 && *v <= 10000 }
	minor := func(v *int64) bool { return v != nil && *v >= 0 && *v <= 1_000_000_000 }
	if !ok || bindErr != nil || !discountCode.MatchString(in.Code) || name == "" || len([]rune(name)) > 100 ||
		!bp(in.DeskPercentBp) || !bp(in.ApprovalPercentBp) || !minor(in.DeskMaxMinor) || !minor(in.ApprovalMaxMinor) ||
		*in.DeskPercentBp > *in.ApprovalPercentBp || *in.DeskMaxMinor > *in.ApprovalMaxMinor {
		respond.BadRequest(c, errors.New("a code (a-z, 0-9, _ or -, up to 32), a name up to 100 characters, and desk limits within approval limits (basis points 0 to 10000, amounts 0 to 1000000000) are required"))
		return
	}
	var out discountReason
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var held []discountReason
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Limit(1).Find(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			r := held[0]
			if r.HospitalID != hospital || r.Code != in.Code || r.Name != name || r.DeskPercentBp != *in.DeskPercentBp || r.DeskMaxMinor != *in.DeskMaxMinor ||
				r.ApprovalPercentBp != *in.ApprovalPercentBp || r.ApprovalMaxMinor != *in.ApprovalMaxMinor || r.CreatedBy != p.Subject {
				return errCashConflict
			}
			out = r
			return nil
		}
		if err := tx.Exec(`INSERT INTO billing_discount_reasons (tenant_id,hospital_id,id,code,name,desk_percent_bp,desk_max_minor,approval_percent_bp,approval_max_minor,currency,created_by)
 VALUES (?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, id, in.Code, name, *in.DeskPercentBp, *in.DeskMaxMinor, *in.ApprovalPercentBp, *in.ApprovalMaxMinor, shiftCurrency, p.Subject).Error; err != nil {
			return err
		}
		created = true
		return tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error
	})
	switch {
	case err != nil:
		discountFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

// retireDiscountReason retires a reason once; retiring it again answers it.
func (h *handlers) retireDiscountReason(c *gin.Context) {
	id, ok := v7(c, "reason")
	if !ok {
		respond.NotFound(c, "discount reason")
		return
	}
	var out discountReason
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := tx.Exec(`UPDATE billing_discount_reasons SET retired_by=? WHERE tenant_id=? AND hospital_id=? AND id=? AND retired_at IS NULL`,
			p.Subject, p.TenantID, hospital, id).Error; err != nil {
			return err
		}
		var rows []discountReason
		if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoReason
		}
		out = rows[0]
		return nil
	})
	if err != nil {
		discountFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) discountReasons(c *gin.Context, params pagination.Params) ([]discountReason, error) {
	var rows []discountReason
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

// putDiscount asks for a discount; the database applies it within the desk's
// limit or leaves it pending. The same id and content again replays.
func (h *handlers) putDiscount(c *gin.Context) {
	admission, err := uuid.Parse(c.Param("admission"))
	id, ok := v7(c, "discount")
	var in struct {
		ReasonID      uuid.UUID `json:"reason_id"`
		AmountMinor   int64     `json:"amount_minor"`
		Justification *string   `json:"justification"`
	}
	bindErr := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Justification)
	if err != nil || !ok || bindErr != nil || !whyOK || in.ReasonID == uuid.Nil || in.AmountMinor < 1 || in.AmountMinor > 1_000_000_000 {
		respond.BadRequest(c, errors.New("a reason, an amount of 1 to 1000000000 minor units and a justification of 1 to 500 characters are required"))
		return
	}
	var out discount
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		held, err := discountOf(tx, p.TenantID, hospital, id)
		if err != nil {
			return err
		}
		if len(held) == 1 {
			d := held[0]
			if d.AdmissionID != admission || d.ReasonID != in.ReasonID || d.AmountMinor != in.AmountMinor || d.Justification != why || d.RequestedBy != p.Subject {
				return errCashConflict
			}
			out = d
			return nil
		}
		var known struct{ Account, Reason int64 }
		if err := tx.Raw(`SELECT (SELECT count(*) FROM billing_admission_accounts WHERE tenant_id=? AND hospital_id=? AND admission_id=?) AS account,
 (SELECT count(*) FROM billing_discount_reasons WHERE tenant_id=? AND hospital_id=? AND id=?) AS reason`,
			p.TenantID, hospital, admission, p.TenantID, hospital, in.ReasonID).Scan(&known).Error; err != nil {
			return err
		}
		if known.Account == 0 {
			return errNoAccount
		}
		if known.Reason == 0 {
			return errNoReason
		}
		// Status, time and the split are the database's.
		if err := tx.Exec(`INSERT INTO billing_discounts (tenant_id,hospital_id,admission_id,id,reason_id,amount_minor,currency,justification,status,requested_by,requested_at)
 VALUES (?,?,?,?,?,?,?,?,'pending',?,clock_timestamp())`, p.TenantID, hospital, admission, id, in.ReasonID, in.AmountMinor, shiftCurrency, why, p.Subject).Error; err != nil {
			return err
		}
		created = true
		rows, err := discountOf(tx, p.TenantID, hospital, id)
		if err == nil && len(rows) == 1 {
			out = rows[0]
		}
		return err
	})
	switch {
	case err != nil:
		discountFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

// decideDiscount approves or rejects a pending discount, once, as someone
// other than who asked; the same decision again replays.
func (h *handlers) decideDiscount(c *gin.Context) {
	id, ok := v7(c, "discount")
	var in struct {
		Approve *bool   `json:"approve"`
		Note    *string `json:"note"`
	}
	bindErr := c.ShouldBindJSON(&in)
	note, noteOK := reason(in.Note)
	if !ok || bindErr != nil || in.Approve == nil || !noteOK {
		respond.BadRequest(c, errors.New("approve (true or false) and a note of 1 to 500 characters are required"))
		return
	}
	status := "rejected"
	if *in.Approve {
		status = "applied"
	}
	var out discount
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		rows, err := discountOf(tx, p.TenantID, hospital, id)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoDiscount
		}
		if err := lockAdmission(tx, p.TenantID, rows[0].AdmissionID); err != nil {
			return err
		}
		if rows, err = discountOf(tx, p.TenantID, hospital, id); err != nil {
			return err
		}
		d := rows[0]
		switch {
		case d.Status != "pending" && d.DecidedBy != nil && *d.DecidedBy == p.Subject && d.Status == status && d.DecisionNote != nil && *d.DecisionNote == note:
			out = d
			return nil
		case d.Status != "pending":
			return errDecided
		}
		if err := exactlyOne(tx.Exec(`UPDATE billing_discounts SET status=?,decided_by=?,decision_note=? WHERE tenant_id=? AND id=? AND status='pending'`,
			status, p.Subject, note, p.TenantID, id)); err != nil {
			return err
		}
		if rows, err = discountOf(tx, p.TenantID, hospital, id); err == nil {
			out = rows[0]
		}
		return err
	})
	if err != nil {
		discountFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) discounts(c *gin.Context, params pagination.Params) ([]discount, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	return h.discountPage(c, params, "d.admission_id=?", admission)
}

// pendingDiscounts is the approvers' queue for a hospital.
func (h *handlers) pendingDiscounts(c *gin.Context, params pagination.Params) ([]discount, error) {
	return h.discountPage(c, params, "d.status='pending'")
}

func (h *handlers) discountPage(c *gin.Context, params pagination.Params, where string, args ...any) ([]discount, error) {
	var rows []discount
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table("("+discountColumns+" WHERE d.tenant_id=? AND d.hospital_id=? AND "+where+") AS discounts", append([]any{p.TenantID, hospital}, args...)...)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "requested_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
