package billing

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/csvsafe"
)

// The daily collection report (#198 slice 2): a hospital-local day's shifts,
// what each took by purpose and whether its drawer matched, for finance to
// sign off and post. Read-only, from billing's own records.

const (
	collectionShiftLimit = 500
	collectionCSVLimit   = 1 << 20
)

var (
	errTimezoneUnset   = errors.New("set the hospital's timezone (bed charging) before reading its collections")
	errCollectionDate  = errors.New("date is a calendar date, YYYY-MM-DD")
	errCollectionLater = errors.New("date is after the hospital's today")
	errCollectionLarge = errors.New("more than 500 shifts opened that day")
)

// collectionShift is one shift's day. Money fields are null while the shift
// is open: its cashier closes blind, and an administrator can be a cashier.
type collectionShift struct {
	ID                  uuid.UUID  `json:"shift_id"`
	CounterCode         string     `json:"counter_code"`
	CounterName         string     `json:"counter_name"`
	Cashier             string     `json:"cashier"`
	Status              string     `json:"status"`
	OpenedAt            time.Time  `json:"opened_at"`
	ClosedAt            *time.Time `json:"closed_at"`
	OpeningFloatMinor   *int64     `json:"opening_float_minor"`
	PaymentsMinor       *int64     `json:"payments_minor"`
	DepositsMinor       *int64     `json:"deposits_minor"`
	RefundsMinor        *int64     `json:"refunds_minor"`
	VoidedCount         *int64     `json:"voided_count"`
	VoidedMinor         *int64     `json:"voided_minor"`
	VoidedRefundsMinor  *int64     `json:"voided_refunds_minor"`
	ExpectedMinor       *int64     `json:"expected_minor"`
	DeclaredMinor       *int64     `json:"declared_minor"`
	VarianceMinor       *int64     `json:"variance_minor"`
	AuthorisedBy        *string    `json:"authorised_by"`
	AuthorisationReason *string    `json:"authorisation_reason"`
	// UPI taken in the shift (#211): at the bank, never in the drawer, so
	// apart from the cash columns. A late payment can add to a closed shift.
	UpiPaymentsMinor *int64 `json:"upi_payments_minor"`
	UpiDepositsMinor *int64 `json:"upi_deposits_minor"`
	// Cash taken to the safe less corrections (#198 slice 4): a movement out
	// of the drawer, not revenue, so it is apart from net.
	PickedUpMinor *int64 `json:"picked_up_minor"`
	// Card, netbanking and wallet (#198 slice 6): at the bank, never in the
	// drawer, apart from the cash columns like UPI.
	CardPaymentsMinor       *int64 `json:"card_payments_minor"`
	CardDepositsMinor       *int64 `json:"card_deposits_minor"`
	NetbankingPaymentsMinor *int64 `json:"netbanking_payments_minor"`
	NetbankingDepositsMinor *int64 `json:"netbanking_deposits_minor"`
	WalletPaymentsMinor     *int64 `json:"wallet_payments_minor"`
	WalletDepositsMinor     *int64 `json:"wallet_deposits_minor"`
}

// collectionTotals cover counted shifts (closed or waiting for
// authorisation); shortages and excesses are kept apart so they never net.
type collectionTotals struct {
	PaymentsMinor  int64 `json:"payments_minor"`
	DepositsMinor  int64 `json:"deposits_minor"`
	RefundsMinor   int64 `json:"refunds_minor"`
	NetMinor       int64 `json:"net_minor"`
	ShortageMinor  int64 `json:"shortage_minor"`
	ExcessMinor    int64 `json:"excess_minor"`
	CountedShifts  int   `json:"counted_shifts"`
	OpenShifts     int   `json:"open_shifts"`
	PendingShifts  int   `json:"pending_shifts"`
	VoidedReceipts int64 `json:"voided_receipts"`
	// Voided takings and voided refunds apart: a voided refund is cash that
	// never left the drawer, not money taken back from a patient.
	VoidedMinor        int64 `json:"voided_minor"`
	VoidedRefundsMinor int64 `json:"voided_refunds_minor"`
	UpiPaymentsMinor   int64 `json:"upi_payments_minor"`
	UpiDepositsMinor   int64 `json:"upi_deposits_minor"`
	PickedUpMinor      int64 `json:"picked_up_minor"`
	// Card, netbanking and wallet, apart from cash and net.
	CardPaymentsMinor       int64 `json:"card_payments_minor"`
	CardDepositsMinor       int64 `json:"card_deposits_minor"`
	NetbankingPaymentsMinor int64 `json:"netbanking_payments_minor"`
	NetbankingDepositsMinor int64 `json:"netbanking_deposits_minor"`
	WalletPaymentsMinor     int64 `json:"wallet_payments_minor"`
	WalletDepositsMinor     int64 `json:"wallet_deposits_minor"`
}

type dailyCollection struct {
	HospitalID uuid.UUID         `json:"hospital_id"`
	Date       string            `json:"date"`
	Timezone   string            `json:"timezone"`
	Currency   string            `json:"currency"`
	Final      bool              `json:"final"`
	Shifts     []collectionShift `json:"shifts"`
	Totals     collectionTotals  `json:"totals"`
}

func collectionFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errTimezoneUnset):
		respond.Error(c, http.StatusConflict, "timezone_unset", err.Error())
	case errors.Is(err, errCollectionDate), errors.Is(err, errCollectionLater):
		respond.BadRequest(c, err)
	case errors.Is(err, errCollectionLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "collection_too_large", err.Error())
	default:
		failure(c, err)
	}
}

// collection reads one hospital-local day. The day's bounds are its
// midnights in the hospital's timezone, computed by Postgres, so a shift
// belongs to the day it opened and a past day's shifts never change. No
// timezone is refused rather than defaulted: a guessed zone moves shifts
// between days.
func (h *handlers) collection(c *gin.Context) (dailyCollection, error) {
	date := c.Param("date")
	if d, err := time.Parse(time.DateOnly, date); err != nil || d.Format(time.DateOnly) != date || d.Year() < 1 {
		return dailyCollection{}, errCollectionDate
	}
	out := dailyCollection{Date: date, Currency: shiftCurrency, Shifts: []collectionShift{}}
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		out.HospitalID = hospital
		var zone []struct{ Timezone, Today string }
		if err := tx.Raw(`SELECT timezone,(now() AT TIME ZONE timezone)::date::text AS today FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=?`,
			p.TenantID, hospital).Scan(&zone).Error; err != nil {
			return err
		}
		if len(zone) == 0 {
			return errTimezoneUnset
		}
		if date > zone[0].Today {
			return errCollectionLater
		}
		out.Timezone = zone[0].Timezone
		if err := tx.Raw(`SELECT s.id,c.code AS counter_code,c.name AS counter_name,s.cashier,s.status,s.opened_at,s.closed_at,
 s.opening_float_minor,s.expected_minor,s.declared_minor,s.variance_minor,s.authorised_by,s.authorisation_reason,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='cash' AND r.purpose='payment'),0) AS payments_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='cash' AND r.purpose='deposit'),0) AS deposits_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='cash' AND r.purpose='refund'),0) AS refunds_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='upi' AND r.purpose='payment'),0) AS upi_payments_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='upi' AND r.purpose='deposit'),0) AS upi_deposits_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='card' AND r.purpose='payment'),0) AS card_payments_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='card' AND r.purpose='deposit'),0) AS card_deposits_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='netbanking' AND r.purpose='payment'),0) AS netbanking_payments_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='netbanking' AND r.purpose='deposit'),0) AS netbanking_deposits_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='wallet' AND r.purpose='payment'),0) AS wallet_payments_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NULL AND r.mode='wallet' AND r.purpose='deposit'),0) AS wallet_deposits_minor,
 count(r.id) FILTER (WHERE r.voided_at IS NOT NULL) AS voided_count,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NOT NULL AND r.purpose<>'refund'),0) AS voided_minor,
 COALESCE(sum(r.amount_minor) FILTER (WHERE r.voided_at IS NOT NULL AND r.purpose='refund'),0) AS voided_refunds_minor,
 billing_shift_picked_up(s.tenant_id,s.id) AS picked_up_minor
 FROM billing_cash_shifts s
 JOIN billing_counters c ON c.tenant_id=s.tenant_id AND c.id=s.counter_id
 LEFT JOIN billing_cash_receipts r ON r.tenant_id=s.tenant_id AND r.shift_id=s.id
 WHERE s.tenant_id=? AND s.hospital_id=?
  AND s.opened_at >= (?::date)::timestamp AT TIME ZONE ? AND s.opened_at < ((?::date)+1)::timestamp AT TIME ZONE ?
 GROUP BY s.tenant_id,s.id,c.code,c.name
 ORDER BY s.opened_at,s.id LIMIT ?`,
			p.TenantID, hospital, date, out.Timezone, date, out.Timezone, collectionShiftLimit+1).Scan(&out.Shifts).Error; err != nil {
			return err
		}
		if len(out.Shifts) > collectionShiftLimit {
			return errCollectionLarge
		}
		out.Final = date < zone[0].Today
		return nil
	})
	if err != nil {
		return dailyCollection{}, err
	}
	out.summarise()
	return out, nil
}

// summarise blinds open shifts and totals the counted ones; a day with a
// shift not yet closed is never final.
func (d *dailyCollection) summarise() {
	for i := range d.Shifts {
		s := &d.Shifts[i]
		t := &d.Totals
		switch s.Status {
		case "closed":
		case "pending_authorisation":
			t.PendingShifts++
			d.Final = false
		default:
			// Blind until counted, being handed over included: nothing that
			// adds up to the expected cash.
			s.OpeningFloatMinor, s.PaymentsMinor, s.DepositsMinor, s.RefundsMinor, s.VoidedCount, s.VoidedMinor, s.VoidedRefundsMinor = nil, nil, nil, nil, nil, nil, nil
			s.UpiPaymentsMinor, s.UpiDepositsMinor, s.PickedUpMinor = nil, nil, nil
			s.CardPaymentsMinor, s.CardDepositsMinor, s.NetbankingPaymentsMinor, s.NetbankingDepositsMinor, s.WalletPaymentsMinor, s.WalletDepositsMinor = nil, nil, nil, nil, nil, nil
			t.OpenShifts++
			d.Final = false
			continue
		}
		t.CountedShifts++
		t.PaymentsMinor += *s.PaymentsMinor
		t.DepositsMinor += *s.DepositsMinor
		t.RefundsMinor += *s.RefundsMinor
		t.VoidedReceipts += *s.VoidedCount
		t.VoidedMinor += *s.VoidedMinor
		t.VoidedRefundsMinor += *s.VoidedRefundsMinor
		t.UpiPaymentsMinor += *s.UpiPaymentsMinor
		t.UpiDepositsMinor += *s.UpiDepositsMinor
		t.PickedUpMinor += *s.PickedUpMinor
		t.CardPaymentsMinor += *s.CardPaymentsMinor
		t.CardDepositsMinor += *s.CardDepositsMinor
		t.NetbankingPaymentsMinor += *s.NetbankingPaymentsMinor
		t.NetbankingDepositsMinor += *s.NetbankingDepositsMinor
		t.WalletPaymentsMinor += *s.WalletPaymentsMinor
		t.WalletDepositsMinor += *s.WalletDepositsMinor
		if v := *s.VarianceMinor; v < 0 {
			t.ShortageMinor -= v
		} else {
			t.ExcessMinor += v
		}
	}
	d.Totals.NetMinor = d.Totals.PaymentsMinor + d.Totals.DepositsMinor - d.Totals.RefundsMinor
}

func (h *handlers) dailyCollection(c *gin.Context) {
	out, err := h.collection(c)
	if err != nil {
		collectionFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// exportDailyCollection is the same day as CSV for the ledger: a row per
// shift and a TOTAL row, amounts in rupees, every text cell made inert.
func (h *handlers) exportDailyCollection(c *gin.Context) {
	out, err := h.collection(c)
	if err != nil {
		collectionFailure(c, err)
		return
	}
	data, err := encodeCollectionCSV(out)
	if errors.Is(err, csvsafe.ErrTooLarge) {
		collectionFailure(c, errCollectionLarge)
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not encode the collection CSV")
		return
	}
	p, _, _ := authn.TenantPrincipal(c)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "billing.collection.exported", "daily collection CSV generated",
		"tenant_id", p.TenantID, "subject", p.Subject, "hospital_id", out.HospitalID, "date", out.Date, "final", out.Final, "row_count", len(out.Shifts), "sha256", digest)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="billing-collection-%s-%s.csv"`, out.HospitalID, out.Date))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(len(out.Shifts)))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

var collectionHeader = []string{"date", "timezone", "final", "counter_code", "counter_name", "cashier", "status", "opened_at", "closed_at",
	"opening_float", "payments", "deposits", "refunds", "net", "voided_receipts", "voided_amount", "voided_refunds", "expected", "declared", "variance",
	"shortage", "excess", "authorised_by", "authorisation_reason", "upi_payments", "upi_deposits", "picked_up",
	"card_payments", "card_deposits", "netbanking_payments", "netbanking_deposits", "wallet_payments", "wallet_deposits"}

func encodeCollectionCSV(d dailyCollection) ([]byte, error) {
	w := csvsafe.NewWriter(collectionCSVLimit)
	day := []string{d.Date, csvsafe.Text(d.Timezone), strconv.FormatBool(d.Final)}
	if err := w.Write(collectionHeader); err != nil {
		return nil, err
	}
	for _, s := range d.Shifts {
		closed := ""
		if s.ClosedAt != nil {
			closed = s.ClosedAt.UTC().Format(time.RFC3339)
		}
		var net, shortage, excess *int64
		if s.PaymentsMinor != nil {
			n, short, over := *s.PaymentsMinor+*s.DepositsMinor-*s.RefundsMinor, max(-*s.VarianceMinor, 0), max(*s.VarianceMinor, 0)
			net, shortage, excess = &n, &short, &over
		}
		row := append(append([]string{}, day...), csvsafe.Text(s.CounterCode), csvsafe.Text(s.CounterName), csvsafe.Text(s.Cashier), s.Status,
			s.OpenedAt.UTC().Format(time.RFC3339), closed, rupees(s.OpeningFloatMinor), rupees(s.PaymentsMinor), rupees(s.DepositsMinor),
			rupees(s.RefundsMinor), rupees(net), integer(s.VoidedCount), rupees(s.VoidedMinor), rupees(s.VoidedRefundsMinor), rupees(s.ExpectedMinor), rupees(s.DeclaredMinor),
			rupees(s.VarianceMinor), rupees(shortage), rupees(excess), csvsafe.Text(deref(s.AuthorisedBy)), csvsafe.Text(deref(s.AuthorisationReason)),
			rupees(s.UpiPaymentsMinor), rupees(s.UpiDepositsMinor), rupees(s.PickedUpMinor),
			rupees(s.CardPaymentsMinor), rupees(s.CardDepositsMinor), rupees(s.NetbankingPaymentsMinor), rupees(s.NetbankingDepositsMinor),
			rupees(s.WalletPaymentsMinor), rupees(s.WalletDepositsMinor))
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	// The TOTAL row covers counted shifts only, as the totals do.
	t := d.Totals
	total := append(append([]string{}, day...), "TOTAL", "", "", "", "", "", "", rupees(&t.PaymentsMinor), rupees(&t.DepositsMinor),
		rupees(&t.RefundsMinor), rupees(&t.NetMinor), integer(&t.VoidedReceipts), rupees(&t.VoidedMinor), rupees(&t.VoidedRefundsMinor), "", "", "", rupees(&t.ShortageMinor), rupees(&t.ExcessMinor), "", "",
		rupees(&t.UpiPaymentsMinor), rupees(&t.UpiDepositsMinor), rupees(&t.PickedUpMinor),
		rupees(&t.CardPaymentsMinor), rupees(&t.CardDepositsMinor), rupees(&t.NetbankingPaymentsMinor), rupees(&t.NetbankingDepositsMinor),
		rupees(&t.WalletPaymentsMinor), rupees(&t.WalletDepositsMinor))
	if err := w.Write(total); err != nil {
		return nil, err
	}
	return w.Bytes()
}

// rupees formats paise as rupees with two decimals; a null figure is empty.
func rupees(minor *int64) string {
	if minor == nil {
		return ""
	}
	v, sign := *minor, ""
	if v < 0 {
		v, sign = -v, "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

func integer(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
