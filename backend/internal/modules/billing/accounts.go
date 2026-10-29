package billing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type handlers struct{ db *tenantdb.DB }

var (
	errNoHospital = errors.New("hospital not found")
	errNoAccount  = errors.New("account not found")
)

// inHospital runs fn after locking the caller's billing grant at the
// hospital, and sets the subject the account tables' row security reads.
// No grant is the same 404 as an unknown hospital.
func (h *handlers) inHospital(c *gin.Context, fn func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error) error {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return errAborted
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		return errNoHospital
	}
	return h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var granted []struct{ Subject string }
		if err := tx.Raw(`SELECT subject FROM billing_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&granted).Error; err != nil {
			return err
		}
		if len(granted) == 0 {
			return errNoHospital
		}
		if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		return fn(tx, p, hospital)
	})
}

var errAborted = errors.New("request already answered")

func failure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errAborted):
	case errors.Is(err, errNoHospital):
		respond.NotFound(c, "hospital")
	case errors.Is(err, errNoAccount):
		respond.NotFound(c, "account")
	default:
		respond.InternalErr(c, err, "could not complete the billing request")
	}
}

func (h *handlers) changeAccess(c *gin.Context, revoke bool) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	subject := c.Param("subject")
	if err != nil || hospital == uuid.Nil || strings.TrimSpace(subject) != subject || subject == "" || len(subject) > 200 {
		respond.NotFound(c, "hospital")
		return
	}
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if revoke {
			return tx.Exec(`DELETE FROM billing_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=?`, p.TenantID, hospital, subject).Error
		}
		return tx.Exec(`INSERT INTO billing_hospital_access (tenant_id,hospital_id,subject,created_by) VALUES (?,?,?,?) ON CONFLICT (tenant_id,hospital_id,subject) DO NOTHING`,
			p.TenantID, hospital, subject, p.Subject).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not change billing access")
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "subject": subject, "granted": !revoke})
}

func (h *handlers) grantAccess(c *gin.Context)  { h.changeAccess(c, false) }
func (h *handlers) revokeAccess(c *gin.Context) { h.changeAccess(c, true) }

// myAccess answers whether the caller holds a billing grant here: 200, or the
// same 404 as an unknown hospital.
func (h *handlers) myAccess(c *gin.Context) {
	var at uuid.UUID
	if err := h.inHospital(c, func(_ *gorm.DB, _ authn.Principal, hospital uuid.UUID) error {
		at = hospital
		return nil
	}); err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": at, "granted": true})
}

type total struct {
	Currency         string `json:"currency"`
	ChargedMinor     int64  `json:"charged_minor"`
	ReversedMinor    int64  `json:"reversed_minor"`
	OutstandingMinor int64  `json:"outstanding_minor"`
}

type accountView struct {
	AdmissionID uuid.UUID  `json:"admission_id"`
	HospitalID  uuid.UUID  `json:"hospital_id"`
	PatientID   uuid.UUID  `json:"patient_id"`
	Status      string     `json:"status"`
	OpenedAt    *time.Time `json:"opened_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Totals      []total    `json:"totals" gorm:"-"`
	Sources     []source   `json:"sources" gorm:"-"`
	BedDays     bedDayView `json:"bed_days" gorm:"-"`
	EntryCount  int64      `json:"entry_count" gorm:"-"`
}

// source is what is outstanding from one module, in one currency.
type source struct {
	SourceModule     string `json:"source_module"`
	Currency         string `json:"currency"`
	OutstandingMinor int64  `json:"outstanding_minor"`
}

// bedDayView counts the stay's live days. A day not priced has no amount
// and is reported as such, never as zero.
type bedDayView struct {
	Configured bool  `json:"configured"`
	Priced     int64 `json:"priced"`
	NotPriced  int64 `json:"not_priced"`
}

// account is the running position: charges less reversals, per currency.
// Nothing is collected against it yet (#199), so all of it is outstanding.
func (h *handlers) account(c *gin.Context) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return
	}
	var row accountView
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var rows []accountView
		if err := tx.Raw(`SELECT admission_id,hospital_id,patient_id,status,opened_at,closed_at FROM billing_admission_accounts WHERE tenant_id=? AND hospital_id=? AND admission_id=?`,
			p.TenantID, hospital, admission).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return errNoAccount
		}
		row = rows[0]
		row.Totals = []total{}
		if err := tx.Raw(`SELECT currency, COALESCE(sum(amount_minor) FILTER (WHERE kind='charge'),0) AS charged_minor,
 COALESCE(sum(amount_minor) FILTER (WHERE kind='reversal'),0) AS reversed_minor FROM billing_account_entries
 WHERE tenant_id=? AND admission_id=? GROUP BY currency ORDER BY currency`, p.TenantID, admission).Scan(&row.Totals).Error; err != nil {
			return err
		}
		for i := range row.Totals {
			row.Totals[i].OutstandingMinor = row.Totals[i].ChargedMinor - row.Totals[i].ReversedMinor
		}
		row.Sources = []source{}
		if err := tx.Raw(`SELECT source_module,currency,COALESCE(sum(CASE kind WHEN 'charge' THEN amount_minor ELSE -amount_minor END),0) AS outstanding_minor
 FROM billing_account_entries WHERE tenant_id=? AND admission_id=? GROUP BY source_module,currency ORDER BY source_module,currency`, p.TenantID, admission).Scan(&row.Sources).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=?) AS configured,
 count(*) FILTER (WHERE d.priced_at IS NOT NULL) AS priced, count(*) FILTER (WHERE d.priced_at IS NULL) AS not_priced
 FROM billing_bed_days d WHERE d.tenant_id=? AND d.admission_id=? AND d.voided_at IS NULL`, p.TenantID, hospital, p.TenantID, admission).Scan(&row.BedDays).Error; err != nil {
			return err
		}
		return tx.Table("billing_account_entries").Where("tenant_id=? AND admission_id=?", p.TenantID, admission).Count(&row.EntryCount).Error
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}

type entry struct {
	ID              uuid.UUID `json:"id"`
	Kind            string    `json:"kind"`
	SourceModule    string    `json:"source_module"`
	SourceReference uuid.UUID `json:"source_reference"`
	SourceDocument  string    `json:"source_document"`
	ChargeReference uuid.UUID `json:"charge_reference"`
	AmountMinor     int64     `json:"amount_minor"`
	Currency        string    `json:"currency"`
	OccurredAt      time.Time `json:"occurred_at"`
	RecordedAt      time.Time `json:"recorded_at"`
}

func (entry) TableName() string                 { return "billing_account_entries" }
func (e entry) PageKey() (time.Time, uuid.UUID) { return e.RecordedAt, e.ID }

func (h *handlers) entries(c *gin.Context, params pagination.Params) ([]entry, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []entry
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		return tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.NewestFirst}).Where("tenant_id=? AND admission_id=?", p.TenantID, admission).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

// openAccount makes the admission's account if none exists yet. Whichever
// event names the admission first opens it; the patient never changes.
func openAccount(tx *gorm.DB, tenant string, hospital, admission, patient uuid.UUID) error {
	if err := tx.Exec(`INSERT INTO billing_admission_accounts (tenant_id,hospital_id,admission_id,patient_id) VALUES (?,?,?,?) ON CONFLICT (tenant_id,admission_id) DO NOTHING`,
		tenant, hospital, admission, patient).Error; err != nil {
		return err
	}
	var held struct {
		HospitalID uuid.UUID
		PatientID  uuid.UUID
	}
	if err := tx.Raw(`SELECT hospital_id,patient_id FROM billing_admission_accounts WHERE tenant_id=? AND admission_id=? FOR UPDATE`, tenant, admission).Scan(&held).Error; err != nil {
		return err
	}
	if held.HospitalID != hospital || held.PatientID != patient {
		return errors.New("an event names this admission with another hospital or patient")
	}
	return nil
}

func ids(values ...string) ([]uuid.UUID, bool) {
	out := make([]uuid.UUID, len(values))
	for i, v := range values {
		id, err := uuid.Parse(v)
		if err != nil || id == uuid.Nil {
			return nil, false
		}
		out[i] = id
	}
	return out, true
}

func consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "billing-bed-occupancy-started", Subject: medicorecontract.SubjectBedOccupancyStarted, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.BedOccupancyStartedData
			if evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid bed occupancy event")
			}
			v, ok := ids(d.HospitalID, d.AdmissionID, d.PatientID, d.OccupancyID, d.BedID)
			at, err := time.Parse(time.RFC3339Nano, d.StartedAt)
			// A stay without its bed's class cannot be priced; refused, not guessed.
			if !ok || err != nil || !medicorecontract.BedClasses[d.BedClass] {
				return errors.New("invalid bed occupancy event")
			}
			if err := lockAdmission(tx, evt.TenantID, v[1]); err != nil {
				return err
			}
			if err := openAccount(tx, evt.TenantID, v[0], v[1], v[2]); err != nil {
				return err
			}
			// The earliest stay is when the account opened, whichever start arrives
			// first; a transfer changes nothing.
			if err := tx.Exec(`UPDATE billing_admission_accounts SET opened_at=? WHERE tenant_id=? AND admission_id=? AND (opened_at IS NULL OR opened_at>?)`,
				at, evt.TenantID, v[1], at).Error; err != nil {
				return err
			}
			if err := recordStayStart(tx, evt.TenantID, d, v, at); err != nil {
				return err
			}
			return reconcileBedDays(tx, evt.TenantID, v[1])
		}},
		{Name: "billing-admission-released", Subject: medicorecontract.SubjectAdmissionReleased, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.AdmissionReleasedData
			if evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid admission release event")
			}
			v, ok := ids(d.HospitalID, d.AdmissionID, d.PatientID)
			at, err := time.Parse(time.RFC3339Nano, d.ReleasedAt)
			if !ok || err != nil {
				return errors.New("invalid admission release event")
			}
			if err := lockAdmission(tx, evt.TenantID, v[1]); err != nil {
				return err
			}
			if err := openAccount(tx, evt.TenantID, v[0], v[1], v[2]); err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE billing_admission_accounts SET status='closed',closed_at=? WHERE tenant_id=? AND admission_id=? AND status='open'`, at, evt.TenantID, v[1]).Error; err != nil {
				return err
			}
			// Release settles the minimum day of a stay that crossed no midnight.
			return reconcileBedDays(tx, evt.TenantID, v[1])
		}},
		stayEndedConsumer(),
		{Name: "billing-pharmacy-admission-charge", Subject: pharmacycontract.SubjectAdmissionChargePosted, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d pharmacycontract.AdmissionChargePostedData
			if evt.Type != "AdmissionChargePosted" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid admission charge event")
			}
			v, ok := ids(d.HospitalID, d.AdmissionID, d.PatientID, d.SaleID)
			at, err := time.Parse(time.RFC3339Nano, d.PostedAt)
			if !ok || err != nil || d.AmountMinor < 1 || len(d.Currency) != 3 || d.InvoiceNumber == "" {
				return errors.New("invalid admission charge event")
			}
			if err := openAccount(tx, evt.TenantID, v[0], v[1], v[2]); err != nil {
				return err
			}
			if err := lockCharge(tx, evt.TenantID, v[3]); err != nil {
				return err
			}
			if err := postEntry(tx, evt.TenantID, v[0], v[1], "pharmacy", "charge", v[3], v[3], d.InvoiceNumber, d.AmountMinor, d.Currency, at); err != nil {
				return err
			}
			return applyParked(tx, evt.TenantID, v[3])
		}},
		{Name: "billing-pharmacy-admission-reversal", Subject: pharmacycontract.SubjectAdmissionChargeReversed, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d pharmacycontract.AdmissionChargeReversedData
			if evt.Type != "AdmissionChargeReversed" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid admission reversal event")
			}
			v, ok := ids(d.HospitalID, d.AdmissionID, d.PatientID, d.CreditNoteID, d.SaleID)
			at, err := time.Parse(time.RFC3339Nano, d.ReversedAt)
			if !ok || err != nil || d.AmountMinor < 1 || len(d.Currency) != 3 || d.CreditNoteNumber == "" {
				return errors.New("invalid admission reversal event")
			}
			if err := openAccount(tx, evt.TenantID, v[0], v[1], v[2]); err != nil {
				return err
			}
			// A reversal that overtakes its charge is parked until the charge is
			// recorded: never applied against a charge the account has not seen,
			// and never dropped.
			if err := lockCharge(tx, evt.TenantID, v[4]); err != nil {
				return err
			}
			var charged int64
			if err := tx.Table("billing_account_entries").Where("tenant_id=? AND source_module='pharmacy' AND source_reference=? AND kind='charge'", evt.TenantID, v[4]).Count(&charged).Error; err != nil {
				return err
			}
			if charged == 0 {
				return parkReversal(tx, evt.TenantID, v[0], v[1], v[3], v[4], d.CreditNoteNumber, d.AmountMinor, d.Currency, at)
			}
			return postEntry(tx, evt.TenantID, v[0], v[1], "pharmacy", "reversal", v[3], v[4], d.CreditNoteNumber, d.AmountMinor, d.Currency, at)
		}},
	}
}

// postEntry records an entry once: a replay finds it and must match it.
func postEntry(tx *gorm.DB, tenant string, hospital, admission uuid.UUID, module, kind string, source, charge uuid.UUID, document string, amount int64, currency string, at time.Time) error {
	// A replay is answered from the record before the insert, whose bound
	// check would otherwise count the recorded reversal against itself.
	if done, err := sameEntry(tx, tenant, admission, module, kind, source, amount, currency); done || err != nil {
		return err
	}
	res := tx.Exec(`INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,source_module,source_reference,kind) DO NOTHING`,
		tenant, hospital, uuid.Must(uuid.NewV7()), admission, kind, module, source, document, charge, amount, currency, at)
	if res.Error != nil || res.RowsAffected == 1 {
		return res.Error
	}
	_, err := sameEntry(tx, tenant, admission, module, kind, source, amount, currency)
	return err
}

// sameEntry reports whether the entry is already recorded, and refuses a
// replay that says something else.
func sameEntry(tx *gorm.DB, tenant string, admission uuid.UUID, module, kind string, source uuid.UUID, amount int64, currency string) (bool, error) {
	var held []struct {
		AdmissionID uuid.UUID
		AmountMinor int64
		Currency    string
	}
	if err := tx.Raw(`SELECT admission_id,amount_minor,currency FROM billing_account_entries WHERE tenant_id=? AND source_module=? AND source_reference=? AND kind=?`,
		tenant, module, source, kind).Scan(&held).Error; err != nil {
		return false, err
	}
	if len(held) == 0 {
		return false, nil
	}
	if held[0].AdmissionID != admission || held[0].AmountMinor != amount || held[0].Currency != currency {
		return true, errors.New("a replayed entry differs from the one recorded")
	}
	return true, nil
}

// lockCharge serialises everything about one charge: its posting, parking a
// reversal that overtook it, and applying parked reversals. The reversal
// bound trigger takes the same lock.
func lockCharge(tx *gorm.DB, tenant string, charge uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, tenant+":billing-charge:"+charge.String()).Error
}

func parkReversal(tx *gorm.DB, tenant string, hospital, admission, source, charge uuid.UUID, document string, amount int64, currency string, at time.Time) error {
	if err := tx.Exec(`INSERT INTO billing_parked_reversals (tenant_id,source_reference,hospital_id,admission_id,charge_reference,source_document,amount_minor,currency,occurred_at)
 VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,source_reference) DO NOTHING`, tenant, source, hospital, admission, charge, document, amount, currency, at).Error; err != nil {
		return err
	}
	var held struct {
		AdmissionID, ChargeReference uuid.UUID
		AmountMinor                  int64
		Currency                     string
	}
	if err := tx.Raw(`SELECT admission_id,charge_reference,amount_minor,currency FROM billing_parked_reversals WHERE tenant_id=? AND source_reference=?`, tenant, source).Scan(&held).Error; err != nil {
		return err
	}
	if held.AdmissionID != admission || held.ChargeReference != charge || held.AmountMinor != amount || held.Currency != currency {
		return errors.New("a replayed reversal differs from the one parked")
	}
	return nil
}

// applyParked posts the reversals that waited for this charge.
func applyParked(tx *gorm.DB, tenant string, charge uuid.UUID) error {
	var parked []struct {
		SourceReference, HospitalID, AdmissionID uuid.UUID
		SourceDocument                           string
		AmountMinor                              int64
		Currency                                 string
		OccurredAt                               time.Time
	}
	if err := tx.Raw(`SELECT source_reference,hospital_id,admission_id,source_document,amount_minor,currency,occurred_at FROM billing_parked_reversals
 WHERE tenant_id=? AND charge_reference=? AND applied_at IS NULL ORDER BY occurred_at,source_reference FOR UPDATE`, tenant, charge).Scan(&parked).Error; err != nil {
		return err
	}
	for _, r := range parked {
		if err := postEntry(tx, tenant, r.HospitalID, r.AdmissionID, "pharmacy", "reversal", r.SourceReference, charge, r.SourceDocument, r.AmountMinor, r.Currency, r.OccurredAt); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE billing_parked_reversals SET applied_at=clock_timestamp() WHERE tenant_id=? AND source_reference=?`, tenant, r.SourceReference).Error; err != nil {
			return err
		}
	}
	return nil
}
