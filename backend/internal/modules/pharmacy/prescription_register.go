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

// The prescription register (#1062; Drugs Rules 1945, Rule 65(3)(1)): every
// supply on a prescription, Schedule G, H and H1, read from its sale line,
// which is written in the sale's transaction and never changed. Like the H1
// register, it is never a second copy of the facts. Its serial is the
// invoice and line, from the invoice's gap-free series (decision 1).

type rxRegisterEntry struct {
	ID                     uuid.UUID  `gorm:"column:id" json:"id"`
	CreatedAt              time.Time  `gorm:"column:created_at" json:"-"`
	Serial                 string     `gorm:"column:serial" json:"serial"`
	SupplyDate             string     `gorm:"column:supply_date" json:"supply_date"`
	InvoiceNumber          string     `gorm:"column:invoice_number" json:"invoice_number"`
	SaleID                 uuid.UUID  `gorm:"column:sale_id" json:"sale_id"`
	LineNumber             int        `gorm:"column:line_number" json:"line_number"`
	RegulatoryClass        string     `gorm:"column:regulatory_class" json:"regulatory_class"`
	PrescriptionID         uuid.UUID  `gorm:"column:prescription_id" json:"prescription_id"`
	PrescribedOn           string     `gorm:"column:prescribed_on" json:"prescribed_on"`
	PrescriberName         string     `gorm:"column:prescriber_name" json:"prescriber_name"`
	PrescriberRegistration string     `gorm:"column:prescriber_registration" json:"prescriber_registration"`
	PrescriberAddress      *string    `gorm:"column:prescriber_address" json:"prescriber_address"`
	PatientName            string     `gorm:"column:patient_name" json:"patient_name"`
	PatientAddress         *string    `gorm:"column:patient_address" json:"patient_address"`
	PatientID              *uuid.UUID `gorm:"column:patient_id" json:"patient_id"`
	ProductID              uuid.UUID  `gorm:"column:product_id" json:"product_id"`
	SKU                    string     `gorm:"column:sku" json:"sku"`
	BrandName              string     `gorm:"column:brand_name" json:"brand_name"`
	GenericName            string     `gorm:"column:generic_name" json:"generic_name"`
	Manufacturer           string     `gorm:"column:manufacturer" json:"manufacturer"`
	LotNumber              string     `gorm:"column:lot_number" json:"lot_number"`
	ExpiresOn              string     `gorm:"column:expires_on" json:"expires_on"`
	QuantityUnits          int64      `gorm:"column:quantity_units" json:"quantity_units"`
	ReturnedUnits          int64      `gorm:"column:returned_units" json:"returned_units"`
	Pharmacist             string     `gorm:"column:pharmacist" json:"pharmacist"`
}

func (e rxRegisterEntry) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

// rxRegisterEntries is every prescription supply of an outlet in [from, to],
// narrowed by class, product and registered patient when given (NULL is all).
// The join to the cited prescription is what leaves OTC lines out; a cited
// prescription always exists (B-2b), so it drops nothing else.
const rxRegisterEntries = `(SELECT l.id, s.created_at, s.invoice_number||' · line '||l.line_number AS serial, to_char(s.sale_date,'YYYY-MM-DD') AS supply_date,
 s.invoice_number, s.id AS sale_id, l.line_number, l.regulatory_class, p.id AS prescription_id, to_char(p.prescribed_on,'YYYY-MM-DD') AS prescribed_on,
 p.prescriber_name, p.prescriber_registration, p.prescriber_address, p.patient_name, p.patient_address, s.patient_id,
 l.product_id, c.sku, c.brand_name, c.generic_name, c.manufacturer, l.lot_number, to_char(l.expires_on,'YYYY-MM-DD') AS expires_on, l.quantity_units,
 (SELECT COALESCE(sum(r.quantity_units),0) FROM pharmacy_credit_note_lines r WHERE r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id
  AND r.sale_id=l.sale_id AND r.sale_line=l.line_number) AS returned_units, s.actor_subject AS pharmacist
 FROM pharmacy_sale_lines l
 JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
 JOIN pharmacy_prescriptions p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.prescription_id
 JOIN pharmacy_catalogue_products c ON c.tenant_id=l.tenant_id AND c.outlet_id=l.outlet_id AND c.id=l.product_id
 WHERE l.tenant_id=? AND l.outlet_id=? AND s.sale_date BETWEEN ?::date AND ?::date
  AND (?::text IS NULL OR l.regulatory_class=?::text) AND (?::uuid IS NULL OR l.product_id=?::uuid) AND (?::uuid IS NULL OR s.patient_id=?::uuid)) e`

// rxRegisterExportRows bounds an export; a var only so a test can reach the bound.
var rxRegisterExportRows = 1000

type rxRegisterFilter struct {
	from, to         string
	class            *string
	product, patient *uuid.UUID
}

func (f rxRegisterFilter) args(tenant string, outlet uuid.UUID) []any {
	return []any{tenant, outlet, f.from, f.to, f.class, f.class, f.product, f.product, f.patient, f.patient}
}

// logged is the filter as audit log pairs; an absent filter is "all".
func (f rxRegisterFilter) logged() []string {
	text := func(v fmt.Stringer, isNil bool) string {
		if isNil {
			return "all"
		}
		return v.String()
	}
	class := "all"
	if f.class != nil {
		class = *f.class
	}
	return []string{"from", f.from, "to", f.to, "class", class, "product_id", text(f.product, f.product == nil), "patient_id", text(f.patient, f.patient == nil)}
}

func rxRegisterRoutes(s *platform.Router, h *catalogueHandlers) {
	platform.ListRoute(s, "/registers/prescriptions", PermPrescriptionRead, h.listRxRegister)
	s.GET("/registers/prescriptions/export", PermPrescriptionRead, h.exportRxRegister)
}

func rxRegisterQuery(c *gin.Context) (rxRegisterFilter, bool) {
	from, to, ok := outletPeriod(c, "the prescription register")
	if !ok {
		return rxRegisterFilter{}, false
	}
	f := rxRegisterFilter{from: from, to: to}
	if v := c.Query("class"); v != "" {
		if v != "G" && v != "H" && v != "H1" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "class is G, H or H1")
			return f, false
		}
		f.class = &v
	}
	for name, dst := range map[string]**uuid.UUID{"product_id": &f.product, "patient_id": &f.patient} {
		if v := c.Query(name); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				respond.Error(c, http.StatusBadRequest, "invalid_request", name+" is an id")
				return f, false
			}
			*dst = &id
		}
	}
	return f, true
}

func (h *catalogueHandlers) listRxRegister(c *gin.Context, params pagination.Params) ([]rxRegisterEntry, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	f, ok := rxRegisterQuery(c)
	if !ok {
		return nil, nil
	}
	var rows []rxRegisterEntry
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx.Table(rxRegisterEntries, f.args(p.TenantID, outlet)...), params).Find(&rows).Error; err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "prescription_register", p.TenantID, p.Subject, outlet, "register_read", f.logged()...)
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

var errRxRegisterTooLarge = errors.New("prescription register export exceeds its row limit")

func (h *catalogueHandlers) exportRxRegister(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	f, ok := rxRegisterQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var rows []rxRegisterEntry
	var data []byte
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyOrderedKeyset(tx.Table(rxRegisterEntries, f.args(p.TenantID, outlet)...), pagination.Params{Limit: rxRegisterExportRows},
			tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > rxRegisterExportRows {
			return errRxRegisterTooLarge
		}
		var err error
		if data, err = encodeRxRegisterCSV(rows); err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "prescription_register", p.TenantID, p.Subject, outlet, "register_export", f.logged()...)
	})
	switch {
	case errors.Is(err, errRxRegisterTooLarge), errors.Is(err, csvsafe.ErrTooLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "register_export_too_large", fmt.Sprintf("the prescription register export exceeds %d entries; narrow the period", rxRegisterExportRows))
		return
	case err != nil:
		stockError(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.prescription_register.exported", "prescription register CSV generated", "tenant_id", p.TenantID,
		"subject", p.Subject, "pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet, "from", f.from, "to", f.to, "row_count", len(rows), "sha256", digest)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-prescription-register-%s-%s-%s.csv"`, outlet, f.from, f.to))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(len(rows)))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func orNotRecorded(v *string) string {
	if v == nil {
		return "not recorded"
	}
	return csvsafe.Text(*v)
}

func encodeRxRegisterCSV(rows []rxRegisterEntry) ([]byte, error) {
	w := csvsafe.NewWriter(h1ExportBytes)
	if err := w.Write([]string{"serial", "supply_date", "regulatory_class", "prescriber_name", "prescriber_registration", "prescriber_address", "patient_name",
		"patient_address", "prescribed_on", "sku", "brand_name", "generic_name", "manufacturer", "lot_number", "expires_on", "quantity_units", "returned_units",
		"pharmacist"}); err != nil {
		return nil, err
	}
	for _, e := range rows {
		if err := w.Write([]string{csvsafe.Text(e.Serial), e.SupplyDate, e.RegulatoryClass, csvsafe.Text(e.PrescriberName), csvsafe.Text(e.PrescriberRegistration),
			orNotRecorded(e.PrescriberAddress), csvsafe.Text(e.PatientName), orNotRecorded(e.PatientAddress), e.PrescribedOn, csvsafe.Text(e.SKU),
			csvsafe.Text(e.BrandName), csvsafe.Text(e.GenericName), csvsafe.Text(e.Manufacturer), csvsafe.Text(e.LotNumber), e.ExpiresOn,
			strconv.FormatInt(e.QuantityUnits, 10), strconv.FormatInt(e.ReturnedUnits, 10), csvsafe.Text(e.Pharmacist)}); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
