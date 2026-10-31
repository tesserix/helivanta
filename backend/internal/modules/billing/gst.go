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

// GST on hospital rooms and the admission tax invoice (#193 slice 1). What is
// taxable, at what rate, and every invoice figure are decided in the
// database (gst.sql); these handlers record the registration, issue invoices
// and read them back.

var (
	gstinPattern     = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
	errTaxConflict   = errors.New("this invoice id was already used for something else")
	errNoTaxInvoice  = errors.New("tax invoice not found")
	errNothingToBill = errors.New("there is no room charge to invoice")
)

type gstRegistration struct {
	HospitalID    uuid.UUID  `json:"hospital_id"`
	GSTIN         string     `json:"gstin" gorm:"column:gstin"`
	LegalName     string     `json:"legal_name"`
	Address       string     `json:"address"`
	EffectiveFrom string     `json:"effective_from"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	AppliedAt     *time.Time `json:"applied_at"`
}

const gstRegistrationSelect = `SELECT hospital_id,gstin,legal_name,address,to_char(effective_from,'YYYY-MM-DD') AS effective_from,created_by,created_at,applied_at
 FROM billing_gst_registrations WHERE tenant_id=? AND hospital_id=?`

func gstFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.ConstraintName == "billing_gstin":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "the GSTIN is not valid: check its state code, PAN and check character")
	case errors.As(err, &pg) && pg.ConstraintName == "billing_gst_backdated":
		respond.Error(c, http.StatusConflict, "registration_backdated", "a GST registration takes effect from the hospital's tomorrow or later")
	case errors.As(err, &pg) && pg.ConstraintName == "billing_gst_inr":
		respond.Error(c, http.StatusConflict, "rates_not_inr", "a GST-registered hospital's bed rates are in INR")
	case errors.As(err, &pg) && pg.Code == "23503" && pg.ConstraintName == "billing_gst_registrations_tenant_id_hospital_id_fkey":
		respond.Error(c, http.StatusConflict, "not_configured", "set the hospital's timezone (bed charging) first")
	case errors.Is(err, errSetDifferent):
		respond.Error(c, http.StatusConflict, "already_set", "this hospital's GST registration is set and cannot change")
	case errors.As(err, &pg) && pg.ConstraintName == "billing_tax_invoice_registered":
		respond.Error(c, http.StatusConflict, "gst_unregistered", "the hospital's GST registration is not recorded or not yet applied")
	case errors.As(err, &pg) && pg.ConstraintName == "billing_tax_invoice_final_bill":
		respond.Error(c, http.StatusConflict, "no_final_bill", "a tax invoice is issued once a final bill exists")
	case errors.As(err, &pg) && pg.ConstraintName == "billing_tax_invoice_credit_note":
		respond.Error(c, http.StatusConflict, "credit_note_required", "an invoiced room charge was reversed; issue that invoice's credit note first")
	case errors.Is(err, errNothingToBill), errors.As(err, &pg) && (pg.ConstraintName == "billing_tax_invoice_complete" || pg.ConstraintName == "billing_tax_invoice_line"):
		respond.Error(c, http.StatusConflict, "nothing_to_invoice", errNothingToBill.Error())
	case errors.Is(err, errTaxConflict), errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "billing_tax_invoices_pkey":
		respond.Conflict(c, errTaxConflict.Error())
	case errors.Is(err, errNoTaxInvoice):
		respond.NotFound(c, "tax invoice")
	default:
		failure(c, err)
	}
}

func singleLine(s string, max int) (string, bool) {
	t := strings.TrimSpace(s)
	return t, t != "" && len([]rune(t)) <= max && !strings.ContainsAny(t, "\x00")
}

// setGstRegistration records the hospital's GSTIN, once; the same again
// replays. The census then taxes its rooms from the effective date.
func (h *handlers) setGstRegistration(c *gin.Context) {
	var in struct {
		GSTIN         string `json:"gstin"`
		LegalName     string `json:"legal_name"`
		Address       string `json:"address"`
		EffectiveFrom string `json:"effective_from"`
	}
	err := c.ShouldBindJSON(&in)
	name, okName := singleLine(in.LegalName, 200)
	address, okAddress := singleLine(in.Address, 500)
	d, errDate := time.Parse(time.DateOnly, in.EffectiveFrom)
	if err != nil || !gstinPattern.MatchString(in.GSTIN) || !okName || !okAddress || errDate != nil || d.Format(time.DateOnly) != in.EffectiveFrom {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "gstin (15 characters), legal_name, address and effective_from (YYYY-MM-DD) are required")
		return
	}
	var out gstRegistration
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// A recorded registration answers a retry, on any later day: its
		// insert trigger would refuse the date before the conflict is seen.
		var held []gstRegistration
		if err := tx.Raw(gstRegistrationSelect, p.TenantID, hospital).Scan(&held).Error; err != nil {
			return err
		}
		if len(held) == 0 {
			if err := tx.Exec(`INSERT INTO billing_gst_registrations (tenant_id,hospital_id,gstin,legal_name,address,effective_from,created_by) VALUES (?,?,?,?,?,?,?)
 ON CONFLICT (tenant_id,hospital_id) DO NOTHING`, p.TenantID, hospital, in.GSTIN, name, address, in.EffectiveFrom, p.Subject).Error; err != nil {
				return err
			}
			if err := tx.Raw(gstRegistrationSelect, p.TenantID, hospital).Scan(&held).Error; err != nil {
				return err
			}
		}
		out = held[0]
		if out.GSTIN != in.GSTIN || out.LegalName != name || out.Address != address || out.EffectiveFrom != in.EffectiveFrom {
			return errSetDifferent
		}
		return nil
	})
	if err != nil {
		gstFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) gstRegistration(c *gin.Context) {
	var out []gstRegistration
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tx.Raw(gstRegistrationSelect, p.TenantID, hospital).Scan(&out).Error
	})
	switch {
	case err != nil:
		failure(c, err)
	case len(out) == 0:
		respond.Error(c, http.StatusNotFound, "not_configured", "this hospital is not registered for GST")
	default:
		respond.OK(c, out[0])
	}
}

type taxInvoice struct {
	ID               uuid.UUID `json:"id"`
	HospitalID       uuid.UUID `json:"hospital_id"`
	AdmissionID      uuid.UUID `json:"admission_id"`
	Number           string    `json:"number"`
	IssueDate        string    `json:"issue_date"`
	IssuedAt         time.Time `json:"issued_at"`
	IssuedBy         string    `json:"issued_by"`
	GSTIN            string    `json:"gstin" gorm:"column:gstin"`
	SupplierName     string    `json:"supplier_name"`
	SupplierAddress  string    `json:"supplier_address"`
	StateCode        string    `json:"state_code"`
	RecipientName    string    `json:"recipient_name"`
	RecipientAddress string    `json:"recipient_address"`
	// From the lines: what was supplied, the taxable part, the exempt part,
	// each tax and the total, in INR paise.
	Lines        int   `json:"lines"`
	ValueMinor   int64 `json:"value_minor"`
	TaxableMinor int64 `json:"taxable_minor"`
	ExemptMinor  int64 `json:"exempt_minor"`
	CGSTMinor    int64 `json:"cgst_minor" gorm:"column:cgst_minor"`
	SGSTMinor    int64 `json:"sgst_minor" gorm:"column:sgst_minor"`
	TotalMinor   int64 `json:"total_minor"`
	// Reversed lines awaiting a credit note.
	CreditableLines int    `json:"creditable_lines"`
	Currency        string `json:"currency" gorm:"-"`
}

func (t taxInvoice) PageKey() (time.Time, uuid.UUID) { return t.IssuedAt, t.ID }

const taxInvoiceSelect = `SELECT i.id,i.hospital_id,i.admission_id,i.number,to_char(i.issue_date,'YYYY-MM-DD') AS issue_date,i.issued_at,i.issued_by,
 i.gstin,i.supplier_name,i.supplier_address,i.state_code,i.recipient_name,i.recipient_address,
 count(l.entry_id) AS lines,COALESCE(sum(l.value_minor),0) AS value_minor,
 COALESCE(sum(l.value_minor) FILTER (WHERE l.taxable),0) AS taxable_minor,COALESCE(sum(l.value_minor) FILTER (WHERE NOT l.taxable),0) AS exempt_minor,
 COALESCE(sum(l.cgst_minor),0) AS cgst_minor,COALESCE(sum(l.sgst_minor),0) AS sgst_minor,
 COALESCE(sum(l.value_minor+l.cgst_minor+l.sgst_minor),0) AS total_minor,
 (SELECT count(*) FROM billing_creditable(i.tenant_id,i.id)) AS creditable_lines
 FROM billing_tax_invoices i LEFT JOIN billing_tax_invoice_lines l ON l.tenant_id=i.tenant_id AND l.invoice_id=i.id`

const taxInvoiceGroup = ` GROUP BY i.tenant_id,i.id`

func loadTaxInvoice(tx *gorm.DB, tenant string, admission, id uuid.UUID) (taxInvoice, error) {
	var rows []taxInvoice
	if err := tx.Raw(taxInvoiceSelect+` WHERE i.tenant_id=? AND i.admission_id=? AND i.id=?`+taxInvoiceGroup, tenant, admission, id).Scan(&rows).Error; err != nil {
		return taxInvoice{}, err
	}
	if len(rows) == 0 {
		return taxInvoice{}, errNoTaxInvoice
	}
	rows[0].Currency = shiftCurrency
	return rows[0], nil
}

// issueTaxInvoice issues the invoice-cum-bill of supply for every room
// charge the admission can be invoiced for now, under the admission's lock;
// the same id again replays.
func (h *handlers) issueTaxInvoice(c *gin.Context) {
	id, ok := v7(c, "invoice")
	admission, err := uuid.Parse(c.Param("admission"))
	var in struct {
		RecipientName    string `json:"recipient_name"`
		RecipientAddress string `json:"recipient_address"`
	}
	bindErr := c.ShouldBindJSON(&in)
	name, okName := singleLine(in.RecipientName, 200)
	address, okAddress := singleLine(in.RecipientAddress, 500)
	if !ok || err != nil || bindErr != nil || !okName || !okAddress {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a UUIDv7 invoice id, recipient_name and recipient_address are required")
		return
	}
	var out taxInvoice
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var account []struct{ HospitalID uuid.UUID }
		if err := tx.Raw(`SELECT hospital_id FROM billing_admission_accounts WHERE tenant_id=? AND hospital_id=? AND admission_id=?`, p.TenantID, hospital, admission).Scan(&account).Error; err != nil {
			return err
		}
		if len(account) == 0 {
			return errNoAccount
		}
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		if existing, err := loadTaxInvoice(tx, p.TenantID, admission, id); err == nil {
			if existing.RecipientName != name || existing.RecipientAddress != address {
				return errTaxConflict
			}
			out = existing
			return nil
		} else if !errors.Is(err, errNoTaxInvoice) {
			return err
		}
		var lines []uuid.UUID
		if err := tx.Raw(`SELECT entry_id FROM billing_invoiceable(?,?)`, p.TenantID, admission).Scan(&lines).Error; err != nil {
			return err
		}
		// The supplier, number and dates are set by the database.
		if err := tx.Exec(`INSERT INTO billing_tax_invoices (tenant_id,hospital_id,admission_id,id,issued_by,recipient_name,recipient_address) VALUES (?,?,?,?,?,?,?)`, p.TenantID, hospital, admission, id, p.Subject, name, address).Error; err != nil {
			return err
		}
		// After the header, so a missing registration or final bill is
		// reported as such rather than as nothing to invoice.
		if len(lines) == 0 {
			return errNothingToBill
		}
		for _, e := range lines {
			// The line's snapshot is set by the database from the day.
			if err := tx.Exec(`INSERT INTO billing_tax_invoice_lines (tenant_id,hospital_id,admission_id,invoice_id,entry_id) VALUES (?,?,?,?,?)`, p.TenantID, hospital, admission, id, e).Error; err != nil {
				return err
			}
		}
		// The completeness check runs at commit; run it now so its answer
		// is this request's, not a failed commit's.
		if err := tx.Exec(`SET CONSTRAINTS billing_tax_invoice_complete IMMEDIATE`).Error; err != nil {
			return err
		}
		created = true
		out, err = loadTaxInvoice(tx, p.TenantID, admission, id)
		return err
	})
	if err != nil {
		gstFailure(c, err)
		return
	}
	if created {
		respond.Created(c, out)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) taxInvoices(c *gin.Context, params pagination.Params) ([]taxInvoice, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []taxInvoice
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table("(?) AS invoices", tx.Raw(taxInvoiceSelect+` WHERE i.tenant_id=? AND i.hospital_id=? AND i.admission_id=?`+taxInvoiceGroup, p.TenantID, hospital, admission))
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "issued_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].Currency = shiftCurrency
	}
	return rows, nil
}

func (h *handlers) taxInvoice(c *gin.Context) {
	admission, err1 := uuid.Parse(c.Param("admission"))
	id, err2 := uuid.Parse(c.Param("invoice"))
	if err1 != nil || err2 != nil {
		respond.NotFound(c, "tax invoice")
		return
	}
	var out taxInvoice
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = loadTaxInvoice(tx, p.TenantID, admission, id)
		if err == nil && out.HospitalID != hospital {
			return errNoTaxInvoice
		}
		return err
	})
	if err != nil {
		gstFailure(c, err)
		return
	}
	respond.OK(c, out)
}

type taxInvoiceLine struct {
	EntryID       uuid.UUID `json:"entry_id" gorm:"column:id"`
	CensusDate    string    `json:"census_date"`
	BedClass      string    `json:"bed_class"`
	SAC           string    `json:"sac" gorm:"column:sac"`
	PolicyVersion string    `json:"policy_version"`
	ValueMinor    int64     `json:"value_minor"`
	Taxable       bool      `json:"taxable"`
	CGSTBP        int       `json:"cgst_bp" gorm:"column:cgst_bp"`
	SGSTBP        int       `json:"sgst_bp" gorm:"column:sgst_bp"`
	CGSTMinor     int64     `json:"cgst_minor" gorm:"column:cgst_minor"`
	SGSTMinor     int64     `json:"sgst_minor" gorm:"column:sgst_minor"`
	SortAt        time.Time `json:"-"`
}

func (l taxInvoiceLine) PageKey() (time.Time, uuid.UUID) { return l.SortAt, l.EntryID }

func (h *handlers) taxInvoiceLines(c *gin.Context, params pagination.Params) ([]taxInvoiceLine, error) {
	admission, err1 := uuid.Parse(c.Param("admission"))
	id, err2 := uuid.Parse(c.Param("invoice"))
	if err1 != nil || err2 != nil {
		respond.NotFound(c, "tax invoice")
		return nil, nil
	}
	var rows []taxInvoiceLine
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table(`(SELECT entry_id AS id,to_char(census_date,'YYYY-MM-DD') AS census_date,census_date::timestamptz AS sort_at,bed_class,sac,policy_version,value_minor,taxable,cgst_bp,sgst_bp,cgst_minor,sgst_minor
 FROM billing_tax_invoice_lines WHERE tenant_id=? AND hospital_id=? AND admission_id=? AND invoice_id=?) AS lines`, p.TenantID, hospital, admission, id)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "sort_at", Direction: pagination.OldestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
