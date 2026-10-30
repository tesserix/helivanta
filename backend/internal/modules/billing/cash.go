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
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Cash counters (#198 slice 1): counters, shifts closed blind, and cash
// receipts against an admission's account.

var (
	errCashConflict = errors.New("this id was already used for something else")
	errShiftOpen    = errors.New("a shift is already open here")
	errNoShift      = errors.New("you have no open shift at this hospital")
	errShiftUnknown = errors.New("shift not found")
	errShiftClosed  = errors.New("the shift is no longer open")
	errOverBalance  = errors.New("the amount is more than the balance due")
	errOwnVariance  = errors.New("a variance is authorised by someone other than the cashier")
	errNotPending   = errors.New("the shift has no variance waiting for authorisation")
	errNoCounter    = errors.New("counter not found")
	errNoReceipt    = errors.New("receipt not found")
	counterCode     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,19}$`)
)

// shiftCurrency is the only currency a shift takes today: every rate and
// charge in the system is INR.
const shiftCurrency = "INR"

type counter struct {
	ID         uuid.UUID `json:"id"`
	HospitalID uuid.UUID `json:"hospital_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

func (counter) TableName() string                 { return "billing_counters" }
func (c counter) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

// shift is a cashier's shift. Expected cash is computed only at the close,
// so an open shift has none to show: the close is blind.
type shift struct {
	ID                  uuid.UUID  `json:"id"`
	HospitalID          uuid.UUID  `json:"hospital_id"`
	CounterID           uuid.UUID  `json:"counter_id"`
	CounterCode         string     `json:"counter_code" gorm:"->"`
	Cashier             string     `json:"cashier"`
	Currency            string     `json:"currency"`
	OpeningFloatMinor   int64      `json:"opening_float_minor"`
	OpenedAt            time.Time  `json:"opened_at"`
	Status              string     `json:"status"`
	ExpectedMinor       *int64     `json:"expected_minor"`
	DeclaredMinor       *int64     `json:"declared_minor"`
	VarianceMinor       *int64     `json:"variance_minor"`
	ClosedAt            *time.Time `json:"closed_at"`
	AuthorisedBy        *string    `json:"authorised_by"`
	AuthorisationReason *string    `json:"authorisation_reason"`
	AuthorisedAt        *time.Time `json:"authorised_at"`
}

func (s shift) PageKey() (time.Time, uuid.UUID) { return s.OpenedAt, s.ID }

const shiftSelect = `SELECT s.id,s.hospital_id,s.counter_id,c.code AS counter_code,s.cashier,s.currency,s.opening_float_minor,s.opened_at,s.status,
 s.expected_minor,s.declared_minor,s.variance_minor,s.closed_at,s.authorised_by,s.authorisation_reason,s.authorised_at
 FROM billing_cash_shifts s JOIN billing_counters c ON c.tenant_id=s.tenant_id AND c.id=s.counter_id`

type receipt struct {
	ID          uuid.UUID  `json:"id"`
	HospitalID  uuid.UUID  `json:"hospital_id"`
	ShiftID     uuid.UUID  `json:"shift_id"`
	CounterID   uuid.UUID  `json:"counter_id"`
	CounterCode string     `json:"counter_code" gorm:"->"`
	Number      int        `json:"number"`
	Cashier     string     `json:"cashier"`
	AdmissionID uuid.UUID  `json:"admission_id"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	ReceivedAt  time.Time  `json:"received_at"`
	VoidID      *uuid.UUID `json:"-"`
	VoidReason  *string    `json:"void_reason"`
	VoidedAt    *time.Time `json:"voided_at"`
}

func (r receipt) PageKey() (time.Time, uuid.UUID) { return r.ReceivedAt, r.ID }

const receiptSelect = `SELECT r.id,r.hospital_id,r.shift_id,r.counter_id,c.code AS counter_code,r.number,r.cashier,r.admission_id,r.amount_minor,r.currency,
 r.received_at,r.void_id,r.void_reason,r.voided_at FROM billing_cash_receipts r JOIN billing_counters c ON c.tenant_id=r.tenant_id AND c.id=r.counter_id`

func cashFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errCashConflict):
		respond.Conflict(c, err.Error())
	case errors.Is(err, errShiftOpen), errors.As(err, &pg) && pg.Code == "23505" && strings.HasPrefix(pg.ConstraintName, "billing_cash_shift_"):
		respond.Error(c, http.StatusConflict, "shift_open", "a shift is already open on this counter or for this cashier; close it first")
	case errors.Is(err, errNoShift):
		respond.Error(c, http.StatusConflict, "no_open_shift", err.Error())
	case errors.Is(err, errShiftClosed), errors.As(err, &pg) && pg.ConstraintName == "billing_cash_receipt_shift":
		respond.Error(c, http.StatusConflict, "shift_closed", errShiftClosed.Error())
	case errors.Is(err, errOverBalance), errors.As(err, &pg) && pg.ConstraintName == "billing_cash_receipt_balance":
		respond.Error(c, http.StatusConflict, "over_balance", "the amount is more than the balance due; an advance is a deposit")
	case errors.Is(err, errOwnVariance), errors.As(err, &pg) && pg.ConstraintName == "billing_cash_shift_own_variance":
		respond.Error(c, http.StatusConflict, "own_variance", errOwnVariance.Error())
	case errors.Is(err, errNotPending):
		respond.Error(c, http.StatusConflict, "not_pending", err.Error())
	case errors.Is(err, errNoCounter):
		respond.NotFound(c, "counter")
	case errors.Is(err, errShiftUnknown):
		respond.NotFound(c, "shift")
	case errors.Is(err, errNoReceipt):
		respond.NotFound(c, "receipt")
	case errors.As(err, &pg) && pg.Code == "23505" && (strings.HasSuffix(pg.ConstraintName, "_pkey") || pg.ConstraintName == "billing_cash_receipt_void_id" || pg.ConstraintName == "billing_counters_tenant_id_hospital_id_code_key"):
		respond.Conflict(c, errCashConflict.Error())
	default:
		failure(c, err)
	}
}

func v7(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	return id, err == nil && id.Version() == 7
}

func reason(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	t := strings.TrimSpace(*s)
	return t, t != "" && len([]rune(t)) <= 500
}

// putCounter sets up a counter once; the same again replays.
func (h *handlers) putCounter(c *gin.Context) {
	id, ok := v7(c, "counter")
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); !ok || err != nil {
		respond.BadRequest(c, errors.New("a counter has a UUIDv7 id, a code and a name"))
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !counterCode.MatchString(in.Code) || in.Name == "" || len([]rune(in.Name)) > 100 {
		respond.BadRequest(c, errors.New("a counter code is up to 20 capital letters, digits and dashes; a name up to 100 characters"))
		return
	}
	var out counter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var held []counter
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Limit(1).Find(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			if held[0].HospitalID != hospital || held[0].Code != in.Code || held[0].Name != in.Name {
				return errCashConflict
			}
			out = held[0]
			return nil
		}
		if err := tx.Exec(`INSERT INTO billing_counters (tenant_id,hospital_id,id,code,name,created_by) VALUES (?,?,?,?,?,?)`, p.TenantID, hospital, id, in.Code, in.Name, p.Subject).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&out).Error
	})
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) counters(c *gin.Context, params pagination.Params) ([]counter, error) {
	var rows []counter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).
			Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

func loadShift(tx *gorm.DB, tenant string, hospital, id uuid.UUID, lock bool) (shift, error) {
	q := shiftSelect + ` WHERE s.tenant_id=? AND s.hospital_id=? AND s.id=?`
	if lock {
		q += ` FOR UPDATE OF s`
	}
	var rows []shift
	if err := tx.Raw(q, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return shift{}, err
	}
	if len(rows) == 0 {
		return shift{}, errShiftUnknown
	}
	return rows[0], nil
}

// openShift opens the caller's shift on a counter with a declared float.
func (h *handlers) openShift(c *gin.Context) {
	counterID, ok1 := v7(c, "counter")
	shiftID, ok2 := v7(c, "shift")
	var in struct {
		OpeningFloatMinor *int64 `json:"opening_float_minor"`
	}
	if err := c.ShouldBindJSON(&in); !ok1 || !ok2 || err != nil || in.OpeningFloatMinor == nil || *in.OpeningFloatMinor < 0 || *in.OpeningFloatMinor > 1_000_000_000 {
		respond.BadRequest(c, errors.New("an opening float of 0 to 1000000000 minor units is required"))
		return
	}
	var out shift
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_counters").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, counterID).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoCounter
		}
		held, err := loadShift(tx, p.TenantID, hospital, shiftID, false)
		if err == nil {
			if held.CounterID != counterID || held.Cashier != p.Subject || held.OpeningFloatMinor != *in.OpeningFloatMinor {
				return errCashConflict
			}
			out = held
			return nil
		}
		if !errors.Is(err, errShiftUnknown) {
			return err
		}
		if err := tx.Exec(`INSERT INTO billing_cash_shifts (tenant_id,hospital_id,id,counter_id,cashier,currency,opening_float_minor) VALUES (?,?,?,?,?,?,?)`,
			p.TenantID, hospital, shiftID, counterID, p.Subject, shiftCurrency, *in.OpeningFloatMinor).Error; err != nil {
			return err
		}
		created = true
		out, err = loadShift(tx, p.TenantID, hospital, shiftID, false)
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

// myShift is the caller's shift not yet closed at the hospital: 404 when none.
func (h *handlers) myShift(c *gin.Context) {
	var out shift
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var rows []shift
		if err := tx.Raw(shiftSelect+` WHERE s.tenant_id=? AND s.hospital_id=? AND s.cashier=? AND s.status<>'closed'`, p.TenantID, hospital, p.Subject).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoShift
		}
		out = rows[0]
		return nil
	})
	if errors.Is(err, errNoShift) {
		respond.Error(c, http.StatusNotFound, "no_open_shift", errNoShift.Error())
		return
	}
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// closeShift records the cashier's blind count. The expected cash is
// computed here and checked by the database; the answer reveals both.
func (h *handlers) closeShift(c *gin.Context) {
	shiftID, ok := v7(c, "shift")
	var in struct {
		DeclaredMinor *int64 `json:"declared_minor"`
	}
	if err := c.ShouldBindJSON(&in); !ok || err != nil || in.DeclaredMinor == nil || *in.DeclaredMinor < 0 || *in.DeclaredMinor > 100_000_000_000 {
		respond.BadRequest(c, errors.New("the counted cash, 0 or more minor units, is required"))
		return
	}
	var out shift
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		s, err := loadShift(tx, p.TenantID, hospital, shiftID, true)
		if err != nil {
			return err
		}
		// Another cashier's shift is not theirs to close: the same answer as none.
		if s.Cashier != p.Subject {
			return errShiftUnknown
		}
		if s.Status != "open" {
			// A retried close is answered from the record; a different count is not.
			if s.DeclaredMinor != nil && *s.DeclaredMinor == *in.DeclaredMinor {
				out = s
				return nil
			}
			return errShiftClosed
		}
		var expected struct{ V int64 }
		if err := tx.Raw(`SELECT billing_shift_expected(?,?) AS v`, p.TenantID, shiftID).Scan(&expected).Error; err != nil {
			return err
		}
		status := "closed"
		if expected.V != *in.DeclaredMinor {
			status = "pending_authorisation"
		}
		if err := tx.Exec(`UPDATE billing_cash_shifts SET status=?,expected_minor=?,declared_minor=?,closed_at=clock_timestamp() WHERE tenant_id=? AND id=? AND status='open'`,
			status, expected.V, *in.DeclaredMinor, p.TenantID, shiftID).Error; err != nil {
			return err
		}
		out, err = loadShift(tx, p.TenantID, hospital, shiftID, false)
		return err
	})
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// authoriseShift closes a shift whose count differed, by someone else.
func (h *handlers) authoriseShift(c *gin.Context) {
	shiftID, ok := v7(c, "shift")
	var in struct {
		Reason *string `json:"reason"`
	}
	err := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Reason)
	if !ok || err != nil || !whyOK {
		respond.BadRequest(c, errors.New("a reason of 1 to 500 characters is required"))
		return
	}
	var out shift
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		s, err := loadShift(tx, p.TenantID, hospital, shiftID, true)
		if err != nil {
			return err
		}
		if s.Cashier == p.Subject {
			return errOwnVariance
		}
		if s.Status == "closed" && s.AuthorisedBy != nil {
			if *s.AuthorisedBy != p.Subject || s.AuthorisationReason == nil || *s.AuthorisationReason != why {
				return errCashConflict
			}
			out = s
			return nil
		}
		if s.Status != "pending_authorisation" {
			return errNotPending
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_shifts SET status='closed',authorised_by=?,authorisation_reason=?,authorised_at=clock_timestamp() WHERE tenant_id=? AND id=? AND status='pending_authorisation'`,
			p.Subject, why, p.TenantID, shiftID)); err != nil {
			return err
		}
		out, err = loadShift(tx, p.TenantID, hospital, shiftID, false)
		return err
	})
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// shifts lists the hospital's shifts, newest first, for the supervisor.
// ?status=pending_authorisation lists only the variances waiting, so none is
// hidden behind a page of closed shifts.
func (h *handlers) shifts(c *gin.Context, params pagination.Params) ([]shift, error) {
	status := c.Query("status")
	if status != "" && status != "open" && status != "pending_authorisation" && status != "closed" {
		respond.BadRequest(c, errors.New("status is open, pending_authorisation or closed"))
		return nil, nil
	}
	var rows []shift
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table("(?) AS shifts", tx.Raw(shiftSelect+` WHERE s.tenant_id=? AND s.hospital_id=? AND (?='' OR s.status=?)`, p.TenantID, hospital, status, status))
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "opened_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

// changedOne fails a write that changed no row: a racing command got there first.
func changedOne(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errShiftClosed
	}
	return nil
}

func loadReceipt(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (receipt, error) {
	var rows []receipt
	if err := tx.Raw(receiptSelect+` WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=?`, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return receipt{}, err
	}
	if len(rows) == 0 {
		return receipt{}, errNoReceipt
	}
	return rows[0], nil
}

// takeCash records a cash receipt against the admission's account in the
// caller's open shift.
func (h *handlers) takeCash(c *gin.Context) {
	admission, ok1 := uuid.Parse(c.Param("admission"))
	receiptID, ok2 := v7(c, "receipt")
	var in struct {
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	}
	if err := c.ShouldBindJSON(&in); ok1 != nil || !ok2 || err != nil || in.AmountMinor < 1 || in.AmountMinor > 1_000_000_000 || in.Currency != shiftCurrency {
		respond.BadRequest(c, errors.New("an amount of 1 to 1000000000 minor units in INR is required"))
		return
	}
	var out receipt
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// The admission lock first, as every charge and bill takes it.
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		held, err := loadReceipt(tx, p.TenantID, hospital, receiptID)
		if err == nil {
			if held.AdmissionID != admission || held.AmountMinor != in.AmountMinor || held.Currency != in.Currency || held.Cashier != p.Subject {
				return errCashConflict
			}
			out = held
			return nil
		}
		if !errors.Is(err, errNoReceipt) {
			return err
		}
		var account int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&account).Error; err != nil {
			return err
		}
		if account == 0 {
			return errNoAccount
		}
		var open []struct {
			ID        uuid.UUID
			CounterID uuid.UUID
		}
		if err := tx.Raw(`SELECT id,counter_id FROM billing_cash_shifts WHERE tenant_id=? AND hospital_id=? AND cashier=? AND status='open' FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&open).Error; err != nil {
			return err
		}
		if len(open) == 0 {
			return errNoShift
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("billing_counters").Where("tenant_id=? AND id=?", p.TenantID, open[0].CounterID).Select("id").Find(&[]struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		var due struct{ Due int64 }
		if err := tx.Raw(`SELECT COALESCE((SELECT sum(CASE kind WHEN 'charge' THEN amount_minor ELSE -amount_minor END) FROM billing_account_entries WHERE tenant_id=? AND admission_id=? AND currency=?),0)
 - COALESCE((SELECT sum(amount_minor) FROM billing_cash_receipts WHERE tenant_id=? AND admission_id=? AND currency=? AND voided_at IS NULL),0) AS due`,
			p.TenantID, admission, in.Currency, p.TenantID, admission, in.Currency).Scan(&due).Error; err != nil {
			return err
		}
		if in.AmountMinor > due.Due {
			return errOverBalance
		}
		if err := tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,amount_minor,currency)
 VALUES (?,?,?,?,?,(SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE tenant_id=? AND counter_id=?),?,?,?,?)`,
			p.TenantID, hospital, receiptID, open[0].ID, open[0].CounterID, p.TenantID, open[0].CounterID, p.Subject, admission, in.AmountMinor, in.Currency).Error; err != nil {
			return err
		}
		created = true
		out, err = loadReceipt(tx, p.TenantID, hospital, receiptID)
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

// voidReceipt voids a receipt in its cashier's still-open shift.
func (h *handlers) voidReceipt(c *gin.Context) {
	receiptID, ok1 := v7(c, "receipt")
	voidID, ok2 := v7(c, "void")
	var in struct {
		Reason *string `json:"reason"`
	}
	err := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Reason)
	if !ok1 || !ok2 || err != nil || !whyOK {
		respond.BadRequest(c, errors.New("a reason of 1 to 500 characters is required"))
		return
	}
	var out receipt
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		r, err := loadReceipt(tx, p.TenantID, hospital, receiptID)
		if err != nil {
			return err
		}
		if err := lockAdmission(tx, p.TenantID, r.AdmissionID); err != nil {
			return err
		}
		if r, err = loadReceipt(tx, p.TenantID, hospital, receiptID); err != nil {
			return err
		}
		if r.Cashier != p.Subject {
			return errNoReceipt
		}
		if r.VoidedAt != nil {
			if *r.VoidID != voidID || *r.VoidReason != why {
				return errCashConflict
			}
			out = r
			return nil
		}
		var open int64
		if err := tx.Table("billing_cash_shifts").Where("tenant_id=? AND id=? AND status='open'", p.TenantID, r.ShiftID).Count(&open).Error; err != nil {
			return err
		}
		if open == 0 {
			return errShiftClosed
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason=?,voided_at=clock_timestamp() WHERE tenant_id=? AND id=? AND voided_at IS NULL`,
			voidID, why, p.TenantID, receiptID)); err != nil {
			return err
		}
		out, err = loadReceipt(tx, p.TenantID, hospital, receiptID)
		return err
	})
	if err != nil {
		cashFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// receipts lists an admission's receipts, newest first, voided ones included.
func (h *handlers) receipts(c *gin.Context, params pagination.Params) ([]receipt, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []receipt
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		q := tx.Table("(?) AS receipts", tx.Raw(receiptSelect+` WHERE r.tenant_id=? AND r.hospital_id=? AND r.admission_id=?`, p.TenantID, hospital, admission))
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "received_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
