package billing

import (
	"errors"
	"net/http"
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

// Credit notes for an invoice's reversed room days (#193 slice 2). What a
// note credits, its number and whether it is still in time are decided in
// the database (credit_notes.sql); these handlers issue and read them.

var (
	errNoCreditNote    = errors.New("credit note not found")
	errNothingToCredit = errors.New("nothing on this invoice is reversed and uncredited")
	errCreditConflict  = errors.New("this credit note id was already used for another invoice")
)

func creditNoteFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.ConstraintName == "billing_credit_note_deadline":
		respond.Error(c, http.StatusConflict, "credit_note_deadline_passed", "credit notes for this invoice's financial year closed on 30 November after it ended")
	case errors.Is(err, errNothingToCredit), errors.As(err, &pg) && (pg.ConstraintName == "billing_credit_note_complete" || pg.ConstraintName == "billing_credit_note_line"):
		respond.Error(c, http.StatusConflict, "nothing_to_credit", errNothingToCredit.Error())
	case errors.Is(err, errCreditConflict), errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "billing_credit_notes_pkey":
		respond.Conflict(c, errCreditConflict.Error())
	case errors.Is(err, errNoTaxInvoice):
		respond.NotFound(c, "tax invoice")
	case errors.Is(err, errNoCreditNote):
		respond.NotFound(c, "credit note")
	default:
		failure(c, err)
	}
}

type creditNote struct {
	ID               uuid.UUID `json:"id"`
	HospitalID       uuid.UUID `json:"hospital_id"`
	AdmissionID      uuid.UUID `json:"admission_id"`
	InvoiceID        uuid.UUID `json:"invoice_id"`
	InvoiceNumber    string    `json:"invoice_number"`
	InvoiceDate      string    `json:"invoice_date"`
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
	Lines            int       `json:"lines"`
	ValueMinor       int64     `json:"value_minor"`
	TaxableMinor     int64     `json:"taxable_minor"`
	ExemptMinor      int64     `json:"exempt_minor"`
	CGSTMinor        int64     `json:"cgst_minor" gorm:"column:cgst_minor"`
	SGSTMinor        int64     `json:"sgst_minor" gorm:"column:sgst_minor"`
	TotalMinor       int64     `json:"total_minor"`
	Currency         string    `json:"currency" gorm:"-"`
}

func (n creditNote) PageKey() (time.Time, uuid.UUID) { return n.IssuedAt, n.ID }

const noteSelect = `SELECT n.id,n.hospital_id,n.admission_id,n.invoice_id,n.invoice_number,to_char(n.invoice_date,'YYYY-MM-DD') AS invoice_date,
 n.number,to_char(n.issue_date,'YYYY-MM-DD') AS issue_date,n.issued_at,n.issued_by,
 n.gstin,n.supplier_name,n.supplier_address,n.state_code,n.recipient_name,n.recipient_address,
 count(l.entry_id) AS lines,COALESCE(sum(l.value_minor),0) AS value_minor,
 COALESCE(sum(l.value_minor) FILTER (WHERE l.taxable),0) AS taxable_minor,COALESCE(sum(l.value_minor) FILTER (WHERE NOT l.taxable),0) AS exempt_minor,
 COALESCE(sum(l.cgst_minor),0) AS cgst_minor,COALESCE(sum(l.sgst_minor),0) AS sgst_minor,
 COALESCE(sum(l.value_minor+l.cgst_minor+l.sgst_minor),0) AS total_minor
 FROM billing_credit_notes n LEFT JOIN billing_credit_note_lines l ON l.tenant_id=n.tenant_id AND l.credit_note_id=n.id`

const noteGroup = ` GROUP BY n.tenant_id,n.id`

func loadCreditNote(tx *gorm.DB, tenant string, admission, id uuid.UUID) (creditNote, error) {
	var rows []creditNote
	if err := tx.Raw(noteSelect+` WHERE n.tenant_id=? AND n.admission_id=? AND n.id=?`+noteGroup, tenant, admission, id).Scan(&rows).Error; err != nil {
		return creditNote{}, err
	}
	if len(rows) == 0 {
		return creditNote{}, errNoCreditNote
	}
	rows[0].Currency = shiftCurrency
	return rows[0], nil
}

// issueCreditNote credits every reversed, uncredited line of an invoice,
// under the admission's lock; the same id again replays.
func (h *handlers) issueCreditNote(c *gin.Context) {
	id, ok := v7(c, "note")
	admission, err1 := uuid.Parse(c.Param("admission"))
	invoice, err2 := uuid.Parse(c.Param("invoice"))
	if !ok || err1 != nil || err2 != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a UUIDv7 credit note id and a tax invoice are required")
		return
	}
	var out creditNote
	created := false
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var found []uuid.UUID
		if err := tx.Raw(`SELECT id FROM billing_tax_invoices WHERE tenant_id=? AND hospital_id=? AND admission_id=? AND id=?`, p.TenantID, hospital, admission, invoice).Scan(&found).Error; err != nil {
			return err
		}
		if len(found) == 0 {
			return errNoTaxInvoice
		}
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		if existing, err := loadCreditNote(tx, p.TenantID, admission, id); err == nil {
			if existing.InvoiceID != invoice {
				return errCreditConflict
			}
			out = existing
			return nil
		} else if !errors.Is(err, errNoCreditNote) {
			return err
		}
		var lines []uuid.UUID
		if err := tx.Raw(`SELECT entry_id FROM billing_creditable(?,?)`, p.TenantID, invoice).Scan(&lines).Error; err != nil {
			return err
		}
		// The supplier, recipient, number and dates are set by the database.
		if err := tx.Exec(`INSERT INTO billing_credit_notes (tenant_id,hospital_id,admission_id,id,invoice_id,issued_by) VALUES (?,?,?,?,?,?)`, p.TenantID, hospital, admission, id, invoice, p.Subject).Error; err != nil {
			return err
		}
		// After the header, so a passed deadline is reported as such.
		if len(lines) == 0 {
			return errNothingToCredit
		}
		for _, e := range lines {
			if err := tx.Exec(`INSERT INTO billing_credit_note_lines (tenant_id,hospital_id,admission_id,credit_note_id,invoice_id,entry_id) VALUES (?,?,?,?,?,?)`, p.TenantID, hospital, admission, id, invoice, e).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`SET CONSTRAINTS billing_credit_note_complete IMMEDIATE`).Error; err != nil {
			return err
		}
		created = true
		var err error
		out, err = loadCreditNote(tx, p.TenantID, admission, id)
		return err
	})
	if err != nil {
		creditNoteFailure(c, err)
		return
	}
	if created {
		respond.Created(c, out)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) creditNotes(c *gin.Context, params pagination.Params) ([]creditNote, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []creditNote
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table("(?) AS notes", tx.Raw(noteSelect+` WHERE n.tenant_id=? AND n.hospital_id=? AND n.admission_id=?`+noteGroup, p.TenantID, hospital, admission))
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

func (h *handlers) creditNote(c *gin.Context) {
	admission, err1 := uuid.Parse(c.Param("admission"))
	id, err2 := uuid.Parse(c.Param("note"))
	if err1 != nil || err2 != nil {
		respond.NotFound(c, "credit note")
		return
	}
	var out creditNote
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = loadCreditNote(tx, p.TenantID, admission, id)
		if err == nil && out.HospitalID != hospital {
			return errNoCreditNote
		}
		return err
	})
	if err != nil {
		creditNoteFailure(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) creditNoteLines(c *gin.Context, params pagination.Params) ([]taxInvoiceLine, error) {
	admission, err1 := uuid.Parse(c.Param("admission"))
	id, err2 := uuid.Parse(c.Param("note"))
	if err1 != nil || err2 != nil {
		respond.NotFound(c, "credit note")
		return nil, nil
	}
	var rows []taxInvoiceLine
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table(`(SELECT entry_id AS id,to_char(census_date,'YYYY-MM-DD') AS census_date,census_date::timestamptz AS sort_at,bed_class,sac,policy_version,value_minor,taxable,cgst_bp,sgst_bp,cgst_minor,sgst_minor
 FROM billing_credit_note_lines WHERE tenant_id=? AND hospital_id=? AND admission_id=? AND credit_note_id=?) AS lines`, p.TenantID, hospital, admission, id)
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "sort_at", Direction: pagination.OldestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
