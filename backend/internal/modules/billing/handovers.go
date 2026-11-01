package billing

import (
	"encoding/json"
	"errors"
	"net/http"
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

// Handover between cashiers (#198 slice 5): two blind counts of one drawer,
// and only equal counts, or a supervisor's count after a dispute, close the
// one shift and open the next with the count as its float.

var (
	errNoHandover      = errors.New("handover not found")
	errHandoverDecided = errors.New("the handover has already been decided")
	errNotDisputed     = errors.New("the handover is not disputed")
	errOwnHandover     = errors.New("a disputed handover is settled by someone other than its two cashiers")
	errHandoverLarge   = errors.New("a handover passes on at most 1000000000 minor units; pick up the excess first")
)

// maxFloatMinor is the most a shift opens with (billing_cash_shifts).
const maxFloatMinor = 1_000_000_000

// handover never carries either cashier's count: each counts blind, and the
// agreed count is the only one shown, once there is one.
type handover struct {
	ID               uuid.UUID       `json:"id"`
	HospitalID       uuid.UUID       `json:"hospital_id"`
	FromShiftID      uuid.UUID       `json:"from_shift_id"`
	CounterID        uuid.UUID       `json:"counter_id"`
	CounterCode      string          `json:"counter_code" gorm:"->"`
	FromCashier      string          `json:"from_cashier"`
	ToCashier        string          `json:"to_cashier"`
	Currency         string          `json:"currency"`
	Status           string          `json:"status"`
	OutgoingCounts   json.RawMessage `json:"-"`
	IncomingCounts   json.RawMessage `json:"-"`
	ToShiftID        *uuid.UUID      `json:"to_shift_id"`
	AgreedCounts     json.RawMessage `json:"agreed_counts"`
	AgreedMinor      *int64          `json:"agreed_minor"`
	SettledBy        *string         `json:"settled_by"`
	SettlementReason *string         `json:"settlement_reason"`
	OfferedAt        time.Time       `json:"offered_at"`
	DecidedAt        *time.Time      `json:"decided_at"`
	SettledAt        *time.Time      `json:"settled_at"`
}

func (h handover) PageKey() (time.Time, uuid.UUID) { return h.OfferedAt, h.ID }

const handoverSelect = `SELECT h.id,h.hospital_id,h.from_shift_id,h.counter_id,c.code AS counter_code,h.from_cashier,h.to_cashier,h.currency,h.status,
 h.outgoing_counts,h.incoming_counts,h.to_shift_id,h.agreed_counts,h.agreed_minor,h.settled_by,h.settlement_reason,h.offered_at,h.decided_at,h.settled_at
 FROM billing_cash_handovers h JOIN billing_counters c ON c.tenant_id=h.tenant_id AND c.id=h.counter_id`

func loadHandover(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (handover, error) {
	var rows []handover
	if err := tx.Raw(handoverSelect+` WHERE h.tenant_id=? AND h.hospital_id=? AND h.id=?`, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return handover{}, err
	}
	if len(rows) == 0 {
		return handover{}, errNoHandover
	}
	return rows[0], nil
}

// lockHandover takes the shift's lock before reading the handover, the
// order every shift change takes, so a withdrawal and an acceptance queue.
func lockHandover(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (handover, shift, error) {
	h, err := loadHandover(tx, tenant, hospital, id)
	if err != nil {
		return handover{}, shift{}, err
	}
	s, err := loadShift(tx, tenant, hospital, h.FromShiftID, true)
	if err != nil {
		return handover{}, shift{}, err
	}
	h, err = loadHandover(tx, tenant, hospital, id)
	return h, s, err
}

// offerHandover puts the caller's open shift into handover to another
// cashier with the caller's blind count; the shift takes nothing meanwhile.
func (h *handlers) offerHandover(c *gin.Context) {
	shiftID, ok1 := v7(c, "shift")
	handoverID, ok2 := v7(c, "handover")
	var in struct {
		To     string    `json:"to"`
		Counts cashCount `json:"counts"`
	}
	err := c.ShouldBindJSON(&in)
	to := strings.TrimSpace(in.To)
	if !ok1 || !ok2 || err != nil || to == "" || len([]rune(to)) > 200 || !in.Counts.valid() {
		respond.BadRequest(c, errors.New("a handover has a UUIDv7 id, the incoming cashier and a count of the drawer"))
		return
	}
	var out handover
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if to == p.Subject {
			return errBadHandoverTo
		}
		held, err := loadHandover(tx, p.TenantID, hospital, handoverID)
		if err == nil {
			if held.FromCashier != p.Subject || held.FromShiftID != shiftID || held.ToCashier != to || !in.Counts.same(held.OutgoingCounts) {
				return errCashConflict
			}
			out = held
			return nil
		}
		if !errors.Is(err, errNoHandover) {
			return err
		}
		s, err := loadShift(tx, p.TenantID, hospital, shiftID, true)
		if err != nil {
			return err
		}
		// Another cashier's shift is not theirs to hand over: the same answer as none.
		if s.Cashier != p.Subject {
			return errShiftUnknown
		}
		if s.Status != "open" {
			return errShiftClosed
		}
		total, err := in.Counts.total(tx, s.Currency)
		if err != nil {
			return err
		}
		if total > maxFloatMinor {
			return errHandoverLarge
		}
		if err := tx.Exec(`INSERT INTO billing_cash_handovers (tenant_id,hospital_id,id,from_shift_id,counter_id,from_cashier,to_cashier,currency,outgoing_counts)
 VALUES (?,?,?,?,?,?,?,?,?::jsonb)`, p.TenantID, hospital, handoverID, shiftID, s.CounterID, p.Subject, to, s.Currency, in.Counts.json()).Error; err != nil {
			return err
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_shifts SET status='handing_over' WHERE tenant_id=? AND id=? AND status='open'`, p.TenantID, shiftID)); err != nil {
			return err
		}
		created = true
		out, err = loadHandover(tx, p.TenantID, hospital, handoverID)
		return err
	})
	switch {
	case err != nil:
		handoverFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

var errBadHandoverTo = errors.New("a shift is handed over to another cashier")

// withdrawHandover reopens the caller's shift while the offer still stands.
func (h *handlers) withdrawHandover(c *gin.Context) {
	handoverID, ok := v7(c, "handover")
	if !ok {
		respond.BadRequest(c, errors.New("a handover has a UUIDv7 id"))
		return
	}
	h.decide(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (handover, error) {
		ho, _, err := lockHandover(tx, p.TenantID, hospital, handoverID)
		if err != nil {
			return handover{}, err
		}
		if ho.FromCashier != p.Subject {
			return handover{}, errNoHandover
		}
		if ho.Status == "withdrawn" {
			return ho, nil
		}
		if ho.Status != "offered" {
			return handover{}, errHandoverDecided
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_handovers SET status='withdrawn',decided_at=clock_timestamp() WHERE tenant_id=? AND id=? AND status='offered'`, p.TenantID, handoverID)); err != nil {
			return handover{}, err
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_shifts SET status='open' WHERE tenant_id=? AND id=? AND status='handing_over'`, p.TenantID, ho.FromShiftID)); err != nil {
			return handover{}, err
		}
		return loadHandover(tx, p.TenantID, hospital, handoverID)
	})
}

// acceptHandover is the incoming cashier's one blind count. Equal note for
// note, the drawer moves; otherwise the handover waits for a supervisor.
// The answer says which, and never either count.
func (h *handlers) acceptHandover(c *gin.Context) {
	handoverID, ok := v7(c, "handover")
	var in struct {
		Shift  uuid.UUID `json:"shift"`
		Counts cashCount `json:"counts"`
	}
	if err := c.ShouldBindJSON(&in); !ok || err != nil || in.Shift.Version() != 7 || !in.Counts.valid() {
		respond.BadRequest(c, errors.New("an acceptance has the new shift's UUIDv7 id and a count of the drawer"))
		return
	}
	h.decide(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (handover, error) {
		ho, s, err := lockHandover(tx, p.TenantID, hospital, handoverID)
		if err != nil {
			return handover{}, err
		}
		// A handover to someone else is not theirs to see.
		if ho.ToCashier != p.Subject {
			return handover{}, errNoHandover
		}
		if ho.Status != "offered" {
			// A retried acceptance is answered from the record; a second count is not.
			if ho.ToShiftID != nil && *ho.ToShiftID == in.Shift && in.Counts.same(ho.IncomingCounts) {
				return ho, nil
			}
			return handover{}, errHandoverDecided
		}
		agreed, err := in.Counts.total(tx, s.Currency)
		if err != nil {
			return handover{}, err
		}
		// Checked before the counts are compared, so a busy cashier never
		// turns a handover into a dispute no one can settle.
		var busy int64
		if err := tx.Table("billing_cash_shifts").Where("tenant_id=? AND hospital_id=? AND cashier=? AND status<>'closed'", p.TenantID, hospital, p.Subject).Count(&busy).Error; err != nil {
			return handover{}, err
		}
		if busy > 0 {
			return handover{}, errShiftOpen
		}
		if !in.Counts.same(ho.OutgoingCounts) {
			if err := changedOne(tx.Exec(`UPDATE billing_cash_handovers SET status='disputed',incoming_counts=?::jsonb,to_shift_id=?,decided_at=clock_timestamp()
 WHERE tenant_id=? AND id=? AND status='offered'`, in.Counts.json(), in.Shift, p.TenantID, handoverID)); err != nil {
				return handover{}, err
			}
			return loadHandover(tx, p.TenantID, hospital, handoverID)
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_handovers SET status='accepted',incoming_counts=?::jsonb,to_shift_id=?,agreed_counts=?::jsonb,agreed_minor=?,decided_at=clock_timestamp()
 WHERE tenant_id=? AND id=? AND status='offered'`, in.Counts.json(), in.Shift, in.Counts.json(), agreed, p.TenantID, handoverID)); err != nil {
			return handover{}, err
		}
		return completeHandover(tx, p.TenantID, hospital, handoverID)
	})
}

// settleHandover records a supervisor's count of a disputed drawer: it is
// the cash, so the outgoing shift closes against it and the incoming shift
// opens with it.
func (h *handlers) settleHandover(c *gin.Context) {
	handoverID, ok := v7(c, "handover")
	var in struct {
		Counts cashCount `json:"counts"`
		Reason *string   `json:"reason"`
	}
	err := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Reason)
	if !ok || err != nil || !whyOK || !in.Counts.valid() {
		respond.BadRequest(c, errors.New("a settlement has a count of the drawer and a reason of 1 to 500 characters"))
		return
	}
	h.decide(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (handover, error) {
		ho, s, err := lockHandover(tx, p.TenantID, hospital, handoverID)
		if err != nil {
			return handover{}, err
		}
		if p.Subject == ho.FromCashier || p.Subject == ho.ToCashier {
			return handover{}, errOwnHandover
		}
		if ho.Status == "settled" {
			if ho.SettledBy == nil || *ho.SettledBy != p.Subject || ho.SettlementReason == nil || *ho.SettlementReason != why || !in.Counts.same(ho.AgreedCounts) {
				return handover{}, errCashConflict
			}
			return ho, nil
		}
		if ho.Status != "disputed" {
			return handover{}, errNotDisputed
		}
		agreed, err := in.Counts.total(tx, s.Currency)
		if err != nil {
			return handover{}, err
		}
		if agreed > maxFloatMinor {
			return handover{}, errHandoverLarge
		}
		if err := changedOne(tx.Exec(`UPDATE billing_cash_handovers SET status='settled',agreed_counts=?::jsonb,agreed_minor=?,settled_by=?,settlement_reason=?,settled_at=clock_timestamp()
 WHERE tenant_id=? AND id=? AND status='disputed'`, in.Counts.json(), agreed, p.Subject, why, p.TenantID, handoverID)); err != nil {
			return handover{}, err
		}
		return completeHandover(tx, p.TenantID, hospital, handoverID)
	})
}

// completeHandover closes the outgoing shift against the agreed count and
// opens the incoming one with it as the float, in the caller's transaction.
func completeHandover(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (handover, error) {
	ho, err := loadHandover(tx, tenant, hospital, id)
	if err != nil {
		return handover{}, err
	}
	var expected struct{ V int64 }
	if err := tx.Raw(`SELECT billing_shift_expected(?,?) AS v`, tenant, ho.FromShiftID).Scan(&expected).Error; err != nil {
		return handover{}, err
	}
	status := "closed"
	if expected.V != *ho.AgreedMinor {
		status = "pending_authorisation"
	}
	if err := changedOne(tx.Exec(`UPDATE billing_cash_shifts SET status=?,expected_minor=?,declared_minor=?,declared_counts=?::jsonb,closed_at=clock_timestamp(),handed_over=true
 WHERE tenant_id=? AND id=? AND status='handing_over'`, status, expected.V, *ho.AgreedMinor, string(ho.AgreedCounts), tenant, ho.FromShiftID)); err != nil {
		return handover{}, err
	}
	if err := tx.Exec(`INSERT INTO billing_cash_shifts (tenant_id,hospital_id,id,counter_id,cashier,currency,opening_float_minor,handover_id) VALUES (?,?,?,?,?,?,?,?)`,
		tenant, hospital, *ho.ToShiftID, ho.CounterID, ho.ToCashier, ho.Currency, *ho.AgreedMinor, ho.ID).Error; err != nil {
		return handover{}, err
	}
	return ho, nil
}

// decide runs one handover decision and answers with the handover.
func (h *handlers) decide(c *gin.Context, run func(*gorm.DB, authn.Principal, uuid.UUID) (handover, error)) {
	var out handover
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = run(tx, p, hospital)
		return err
	})
	if err != nil {
		handoverFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func handoverFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isConstraint := func(err error, name string) bool { return errors.As(err, &pg) && pg.ConstraintName == name }
	switch {
	case errors.Is(err, errNoHandover):
		respond.NotFound(c, "handover")
	case errors.Is(err, errBadHandoverTo), errors.Is(err, errHandoverLarge):
		respond.BadRequest(c, err)
	case errors.Is(err, errHandoverDecided), isConstraint(err, "billing_cash_handover_actor"):
		respond.Error(c, http.StatusConflict, "handover_decided", errHandoverDecided.Error())
	case errors.Is(err, errNotDisputed):
		respond.Error(c, http.StatusConflict, "not_disputed", errNotDisputed.Error())
	case errors.Is(err, errOwnHandover):
		respond.Error(c, http.StatusConflict, "own_handover", errOwnHandover.Error())
	case isConstraint(err, "billing_cash_handover_offer"), isConstraint(err, "billing_cash_handover_live"):
		respond.Error(c, http.StatusConflict, "shift_closed", errShiftClosed.Error())
	case isConstraint(err, "billing_cash_handover_to_shift"):
		respond.Conflict(c, errCashConflict.Error())
	default:
		cashFailure(c, err)
	}
}

// incomingHandovers lists the offers waiting for the caller, newest first.
func (h *handlers) incomingHandovers(c *gin.Context, params pagination.Params) ([]handover, error) {
	return h.listHandovers(c, params, "h.status='offered' AND h.to_cashier=?", true)
}

// disputedHandovers lists the handovers waiting for a supervisor.
func (h *handlers) disputedHandovers(c *gin.Context, params pagination.Params) ([]handover, error) {
	return h.listHandovers(c, params, "h.status='disputed'", false)
}

func (h *handlers) listHandovers(c *gin.Context, params pagination.Params, where string, mine bool) ([]handover, error) {
	var rows []handover
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		args := []any{p.TenantID, hospital}
		if mine {
			args = append(args, p.Subject)
		}
		q := tx.Table("(?) AS handovers", tx.Raw(handoverSelect+` WHERE h.tenant_id=? AND h.hospital_id=? AND `+where, args...))
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "offered_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
