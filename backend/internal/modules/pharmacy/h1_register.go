package pharmacy

import (
	"context"
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

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/csvsafe"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// The Schedule H1 register (#1386 C-2, #1062; Drugs Rules 1945, Rule
// 65(3)(h)): a read of each H1 sale line, written in its sale's transaction
// and never changed, with what has come back on credit notes. The register
// is never a second copy of the facts, so it cannot drift from them.

type h1Entry struct {
	ID                     uuid.UUID `gorm:"column:id" json:"id"`
	CreatedAt              time.Time `gorm:"column:created_at" json:"-"`
	SupplyDate             string    `gorm:"column:supply_date" json:"supply_date"`
	InvoiceNumber          string    `gorm:"column:invoice_number" json:"invoice_number"`
	SaleID                 uuid.UUID `gorm:"column:sale_id" json:"sale_id"`
	LineNumber             int       `gorm:"column:line_number" json:"line_number"`
	PrescriptionID         uuid.UUID `gorm:"column:prescription_id" json:"prescription_id"`
	PrescribedOn           string    `gorm:"column:prescribed_on" json:"prescribed_on"`
	PrescriberName         string    `gorm:"column:prescriber_name" json:"prescriber_name"`
	PrescriberRegistration string    `gorm:"column:prescriber_registration" json:"prescriber_registration"`
	PrescriberAddress      string    `gorm:"column:prescriber_address" json:"prescriber_address"`
	PatientName            string    `gorm:"column:patient_name" json:"patient_name"`
	PatientAddress         string    `gorm:"column:patient_address" json:"patient_address"`
	ProductID              uuid.UUID `gorm:"column:product_id" json:"product_id"`
	SKU                    string    `gorm:"column:sku" json:"sku"`
	BrandName              string    `gorm:"column:brand_name" json:"brand_name"`
	GenericName            string    `gorm:"column:generic_name" json:"generic_name"`
	LotNumber              string    `gorm:"column:lot_number" json:"lot_number"`
	ExpiresOn              string    `gorm:"column:expires_on" json:"expires_on"`
	QuantityUnits          int64     `gorm:"column:quantity_units" json:"quantity_units"`
	ReturnedUnits          int64     `gorm:"column:returned_units" json:"returned_units"`
	Pharmacist             string    `gorm:"column:pharmacist" json:"pharmacist"`
}

func (e h1Entry) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

// h1Entries is every H1 sale line of an outlet in [from, to] outlet dates.
// An H1 line always cites a prescription with both addresses (B-2b), so the
// inner joins drop nothing the register should hold.
const h1Entries = `(SELECT l.id, s.created_at, to_char(s.sale_date,'YYYY-MM-DD') AS supply_date, s.invoice_number, s.id AS sale_id, l.line_number,
 p.id AS prescription_id, to_char(p.prescribed_on,'YYYY-MM-DD') AS prescribed_on, p.prescriber_name, p.prescriber_registration, p.prescriber_address,
 p.patient_name, p.patient_address, l.product_id, c.sku, c.brand_name, c.generic_name, l.lot_number, to_char(l.expires_on,'YYYY-MM-DD') AS expires_on,
 l.quantity_units, (SELECT COALESCE(sum(r.quantity_units),0) FROM pharmacy_credit_note_lines r WHERE r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id
  AND r.sale_id=l.sale_id AND r.sale_line=l.line_number) AS returned_units, s.actor_subject AS pharmacist
 FROM pharmacy_sale_lines l
 JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
 JOIN pharmacy_prescriptions p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.prescription_id
 JOIN pharmacy_catalogue_products c ON c.tenant_id=l.tenant_id AND c.outlet_id=l.outlet_id AND c.id=l.product_id
 WHERE l.tenant_id=? AND l.outlet_id=? AND l.regulatory_class='H1' AND s.sale_date BETWEEN ?::date AND ?::date) e`

const (
	h1MaxPeriodDays = 366
	h1ExportBytes   = 4 << 20
)

// h1ExportRows bounds an export; a var only so a test can reach the bound.
var h1ExportRows = 1000

func h1RegisterRoutes(s *platform.Router, h *catalogueHandlers) {
	platform.ListRoute(s, "/registers/h1", PermPrescriptionRead, h.listH1Register)
	s.GET("/registers/h1/export", PermPrescriptionRead, h.exportH1Register)
}

// h1Period reads ?from=&to= as outlet dates, at most 366 days apart.
func h1Period(c *gin.Context) (string, string, bool) {
	from, errFrom := time.Parse(time.DateOnly, c.Query("from"))
	to, errTo := time.Parse(time.DateOnly, c.Query("to"))
	if errFrom != nil || errTo != nil || to.Before(from) || to.Sub(from) > (h1MaxPeriodDays-1)*24*time.Hour {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "the H1 register needs from and to dates (YYYY-MM-DD), from on or before to, for a period of at most 366 days")
		return "", "", false
	}
	return from.Format(time.DateOnly), to.Format(time.DateOnly), true
}

// auditH1Read records who read the register; the read and its record commit
// together, so an unrecorded read is never returned. audit_events holds field
// names only, so the period's values go to the structured audit log.
func auditH1Read(c *gin.Context, tx *gorm.DB, tenant, subject string, outlet uuid.UUID, action, from, to string) error {
	if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields) VALUES(?,?,?,'pharmacy_h1_register',?,ARRAY['from','to']::text[])`,
		tenant, subject, action, outlet).Error; err != nil {
		return err
	}
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "pharmacy.h1_register."+action, "H1 register read", "tenant_id", tenant, "subject", subject,
		"outlet_id", outlet, "from", from, "to", to)
	return nil
}

func (h *catalogueHandlers) listH1Register(c *gin.Context, params pagination.Params) ([]h1Entry, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	from, to, ok := h1Period(c)
	if !ok {
		return nil, nil
	}
	var rows []h1Entry
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		// Newest first, as every paged list; the CSV runs oldest first, as a
		// register is read.
		if err := tenantdb.ApplyKeyset(tx.Table(h1Entries, p.TenantID, outlet, from, to), params).Find(&rows).Error; err != nil {
			return err
		}
		return auditH1Read(c, tx, p.TenantID, p.Subject, outlet, "register_read", from, to)
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) exportH1Register(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	from, to, ok := h1Period(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var rows []h1Entry
	var data []byte
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyOrderedKeyset(tx.Table(h1Entries, p.TenantID, outlet, from, to), pagination.Params{Limit: h1ExportRows},
			tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > h1ExportRows {
			return errH1TooLarge
		}
		var err error
		if data, err = encodeH1CSV(rows); err != nil {
			return err
		}
		return auditH1Read(c, tx, p.TenantID, p.Subject, outlet, "register_export", from, to)
	})
	switch {
	case errors.Is(err, errH1TooLarge), errors.Is(err, csvsafe.ErrTooLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "register_export_too_large", fmt.Sprintf("the H1 register export exceeds %d entries; narrow the period", h1ExportRows))
		return
	case err != nil:
		stockError(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.h1_register.exported", "H1 register CSV generated", "tenant_id", p.TenantID, "subject", p.Subject,
		"pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet, "from", from, "to", to, "row_count", len(rows), "sha256", digest)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-h1-register-%s-%s-%s.csv"`, outlet, from, to))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(len(rows)))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

var errH1TooLarge = errors.New("h1 register export exceeds its row limit")

func encodeH1CSV(rows []h1Entry) ([]byte, error) {
	w := csvsafe.NewWriter(h1ExportBytes)
	if err := w.Write([]string{"supply_date", "invoice_number", "line_number", "prescriber_name", "prescriber_registration", "prescriber_address", "patient_name",
		"patient_address", "prescribed_on", "sku", "brand_name", "generic_name", "lot_number", "expires_on", "quantity_units", "returned_units", "pharmacist"}); err != nil {
		return nil, err
	}
	for _, e := range rows {
		if err := w.Write([]string{e.SupplyDate, csvsafe.Text(e.InvoiceNumber), strconv.Itoa(e.LineNumber), csvsafe.Text(e.PrescriberName), csvsafe.Text(e.PrescriberRegistration),
			csvsafe.Text(e.PrescriberAddress), csvsafe.Text(e.PatientName), csvsafe.Text(e.PatientAddress), e.PrescribedOn, csvsafe.Text(e.SKU), csvsafe.Text(e.BrandName),
			csvsafe.Text(e.GenericName), csvsafe.Text(e.LotNumber), e.ExpiresOn, strconv.FormatInt(e.QuantityUnits, 10), strconv.FormatInt(e.ReturnedUnits, 10),
			csvsafe.Text(e.Pharmacist)}); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
