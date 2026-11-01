package billing

import (
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Cash pickups and counting by note (#198 slice 3): cash taken from an open
// drawer to the safe by someone other than its cashier, corrections of an
// overstated pickup, and counts of notes and coins.

var (
	errBadCount   = errors.New("a count is an object of notes and coins from the denomination list to whole counts of 0 to 1000000")
	errOwnPickup  = errors.New("cash is picked up by someone other than the shift's cashier")
	errOverPickup = errors.New("a correction puts back no more than its pickup still counts")
	errNoPickup   = errors.New("pickup not found")
)

// cashCount is a count by note and coin, keyed note:<paise> or coin:<paise>.
// The database owns the list and values it; zero entries are dropped so one
// count has one form.
type cashCount map[string]int64

func (n cashCount) valid() bool {
	if n == nil {
		return false
	}
	for k, v := range n {
		if v < 0 || v > 1_000_000 {
			return false
		}
		if v == 0 {
			delete(n, k)
		}
	}
	return true
}

func (n cashCount) json() string {
	b, _ := json.Marshal(map[string]int64(n))
	return string(b)
}

func (n cashCount) same(stored json.RawMessage) bool {
	var held cashCount
	return len(stored) > 0 && json.Unmarshal(stored, &held) == nil && maps.Equal(held, n)
}

// total values the count; a key outside the list is a bad count, not zero.
func (n cashCount) total(tx *gorm.DB, currency string) (int64, error) {
	var v struct{ V *int64 }
	if err := tx.Raw(`SELECT billing_cash_count_total(?,?::jsonb) AS v`, currency, n.json()).Scan(&v).Error; err != nil {
		return 0, err
	}
	if v.V == nil {
		return 0, errBadCount
	}
	return *v.V, nil
}

type denomination struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	ValueMinor int64  `json:"value_minor"`
}

// denominations lists what a count of the hospital's cash may hold.
func (h *handlers) denominations(c *gin.Context) {
	out := []denomination{}
	err := h.inHospital(c, func(tx *gorm.DB, _ authn.Principal, _ uuid.UUID) error {
		return tx.Raw(`SELECT key,kind,value_minor FROM billing_cash_denominations(?)`, shiftCurrency).Scan(&out).Error
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, out)
}

type pickup struct {
	ID          uuid.UUID       `json:"id"`
	HospitalID  uuid.UUID       `json:"hospital_id"`
	ShiftID     uuid.UUID       `json:"shift_id"`
	CounterID   uuid.UUID       `json:"counter_id"`
	Kind        string          `json:"kind"`
	AmountMinor int64           `json:"amount_minor"`
	Currency    string          `json:"currency"`
	Counts      json.RawMessage `json:"counts"`
	CorrectsID  *uuid.UUID      `json:"corrects_id"`
	Reason      *string         `json:"reason"`
	RecordedBy  string          `json:"recorded_by"`
	RecordedAt  time.Time       `json:"recorded_at"`
}

func (p pickup) PageKey() (time.Time, uuid.UUID) { return p.RecordedAt, p.ID }

func loadPickup(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (pickup, error) {
	var rows []pickup
	if err := tx.Raw(`SELECT id,hospital_id,shift_id,counter_id,kind,amount_minor,currency,counts,corrects_id,reason,recorded_by,recorded_at
 FROM billing_cash_pickups WHERE tenant_id=? AND hospital_id=? AND id=?`, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return pickup{}, err
	}
	if len(rows) == 0 {
		return pickup{}, errNoPickup
	}
	return rows[0], nil
}

// recordPickup records cash counted out of an open drawer by the caller. The
// database refuses the shift's own cashier and more than the drawer holds.
func (h *handlers) recordPickup(c *gin.Context) {
	shiftID, ok1 := v7(c, "shift")
	pickupID, ok2 := v7(c, "pickup")
	var in struct {
		Counts cashCount `json:"counts"`
	}
	if err := c.ShouldBindJSON(&in); !ok1 || !ok2 || err != nil || !in.Counts.valid() || len(in.Counts) == 0 {
		respond.BadRequest(c, errors.New("a pickup has a UUIDv7 id and a count of at least one note or coin"))
		return
	}
	h.writePickup(c, pickupID, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (pickup, error) {
		s, err := loadShift(tx, p.TenantID, hospital, shiftID, false)
		if err != nil {
			return pickup{}, err
		}
		if s.Cashier == p.Subject {
			return pickup{}, errOwnPickup
		}
		amount, err := in.Counts.total(tx, s.Currency)
		if err != nil {
			return pickup{}, err
		}
		return pickup{ShiftID: shiftID, CounterID: s.CounterID, Kind: "pickup", AmountMinor: amount, Currency: s.Currency, Counts: json.RawMessage(in.Counts.json())}, nil
	}, func(held pickup) bool {
		return held.Kind == "pickup" && held.ShiftID == shiftID && in.Counts.same(held.Counts)
	})
}

// correctPickup puts back what a pickup overstated, with a reason.
func (h *handlers) correctPickup(c *gin.Context) {
	pickupID, ok1 := v7(c, "pickup")
	correctionID, ok2 := v7(c, "correction")
	var in struct {
		AmountMinor int64   `json:"amount_minor"`
		Reason      *string `json:"reason"`
	}
	err := c.ShouldBindJSON(&in)
	why, whyOK := reason(in.Reason)
	if !ok1 || !ok2 || err != nil || !whyOK || in.AmountMinor < 1 || in.AmountMinor > 100_000_000_000 {
		respond.BadRequest(c, errors.New("a correction has a UUIDv7 id, an amount of 1 or more minor units and a reason of 1 to 500 characters"))
		return
	}
	h.writePickup(c, correctionID, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (pickup, error) {
		of, err := loadPickup(tx, p.TenantID, hospital, pickupID)
		if err != nil {
			return pickup{}, err
		}
		if of.Kind != "pickup" {
			return pickup{}, errNoPickup
		}
		return pickup{ShiftID: of.ShiftID, CounterID: of.CounterID, Kind: "correction", AmountMinor: in.AmountMinor, Currency: of.Currency, CorrectsID: &of.ID, Reason: &why}, nil
	}, func(held pickup) bool {
		return held.Kind == "correction" && held.CorrectsID != nil && *held.CorrectsID == pickupID && held.AmountMinor == in.AmountMinor && held.Reason != nil && *held.Reason == why
	})
}

// writePickup records a pickup or correction once: the same again replays,
// anything else on the id is a conflict.
func (h *handlers) writePickup(c *gin.Context, id uuid.UUID, build func(*gorm.DB, authn.Principal, uuid.UUID) (pickup, error), same func(pickup) bool) {
	var out pickup
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		held, err := loadPickup(tx, p.TenantID, hospital, id)
		if err == nil {
			if held.RecordedBy != p.Subject || !same(held) {
				return errCashConflict
			}
			out = held
			return nil
		}
		if !errors.Is(err, errNoPickup) {
			return err
		}
		row, err := build(tx, p, hospital)
		if err != nil {
			return err
		}
		var counts *string
		if row.Counts != nil {
			s := string(row.Counts)
			counts = &s
		}
		if err := tx.Exec(`INSERT INTO billing_cash_pickups (tenant_id,hospital_id,id,shift_id,counter_id,kind,amount_minor,currency,counts,corrects_id,reason,recorded_by)
 VALUES (?,?,?,?,?,?,?,?,?::jsonb,?,?,?)`, p.TenantID, hospital, id, row.ShiftID, row.CounterID, row.Kind, row.AmountMinor, row.Currency, counts, row.CorrectsID, row.Reason, p.Subject).Error; err != nil {
			return err
		}
		created = true
		out, err = loadPickup(tx, p.TenantID, hospital, id)
		return err
	})
	switch {
	case errors.Is(err, errNoPickup):
		respond.NotFound(c, "pickup")
	case err != nil:
		cashFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

// pickups lists a shift's pickups and corrections, oldest first.
func (h *handlers) pickups(c *gin.Context, params pagination.Params) ([]pickup, error) {
	shiftID, ok := v7(c, "shift")
	if !ok {
		respond.BadRequest(c, errors.New("a shift has a UUIDv7 id"))
		return nil, nil
	}
	var rows []pickup
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if _, err := loadShift(tx, p.TenantID, hospital, shiftID, false); err != nil {
			return err
		}
		q := tx.Table("billing_cash_pickups").Select("id,hospital_id,shift_id,counter_id,kind,amount_minor,currency,counts,corrects_id,reason,recorded_by,recorded_at").
			Where("tenant_id=? AND hospital_id=? AND shift_id=?", p.TenantID, hospital, shiftID)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.OldestFirst}).Find(&rows).Error
	})
	if err != nil {
		cashFailure(c, err)
		return nil, nil
	}
	return rows, nil
}
