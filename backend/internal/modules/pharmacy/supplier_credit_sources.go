package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func supplierCreditSourceRoutes(r *platform.Router, h *catalogueHandlers) {
	r.GET("/:credit/context", PermAccountsRead, h.getSupplierCreditContext)
	platform.ResourceListRoute(r, "/:credit/eligible-payables", PermAccountsRead, 50, h.listSupplierCreditPayableChoices)
	platform.ResourceListRoute(r, "/:credit/eligible-returns", PermAccountsRead, 50, h.listSupplierCreditReturnChoices)
}

func (h *catalogueHandlers) getSupplierCreditContext(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result SupplierCreditContext
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var err error
		result, err = loadSupplierCreditContext(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "supplier_credit_context", p.TenantID, p.Subject, credit, "viewed")
	})
	if err != nil {
		supplierCreditError(c, err)
		return
	}
	respond.OK(c, result)
}

type supplierCreditPayableChoiceRow struct {
	SupplierCreditPayableChoice `gorm:"embedded"`
	CreatedAt                   time.Time `json:"-"`
}

func (r supplierCreditPayableChoiceRow) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.ID
}
func (h *catalogueHandlers) listSupplierCreditPayableChoices(c *gin.Context, params pagination.Params) ([]supplierCreditPayableChoiceRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok || !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierCreditPayableChoiceRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		doc, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_payable_balances").Select(supplierCreditPayableColumns).Where("tenant_id=? AND outlet_id=? AND payee_reference=? AND currency=? AND outstanding_minor>0", p.TenantID, outlet, doc.PayeeReference, doc.Fields.Currency).Where("pharmacy_supplier_credit_payable_eligible(tenant_id,outlet_id,id)")
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "supplier_credit_payable_choices", p.TenantID, p.Subject, credit, "list_read")
	})
	if err != nil {
		supplierCreditError(c, err)
		return nil, nil
	}
	return rows, nil
}

type supplierCreditReturnChoiceRow struct {
	SupplierCreditReturnChoice `gorm:"embedded"`
	CreatedAt                  time.Time `json:"-"`
}

func (r supplierCreditReturnChoiceRow) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.ID
}
func (h *catalogueHandlers) listSupplierCreditReturnChoices(c *gin.Context, params pagination.Params) ([]supplierCreditReturnChoiceRow, error) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return nil, nil
	}
	credit, ok := supplierCreditID(c, "credit")
	if !ok || !pharmacyFinancialCursorRead(c) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rows := []supplierCreditReturnChoiceRow{}
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		doc, err := loadSupplierCreditDocument(tx, p.TenantID, outlet, credit)
		if err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Table("pharmacy_supplier_returns").Select("id,outlet_id,number,payee_name,document_reference AS original_bill_reference,currency,expected_credit_minor,pharmacy_supplier_return_matched(tenant_id,outlet_id,id) AS matched_minor,created_at").Where("tenant_id=? AND outlet_id=? AND payee_reference=? AND currency=?", p.TenantID, outlet, doc.PayeeReference, doc.Fields.Currency).Where("EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatches d WHERE d.tenant_id=pharmacy_supplier_returns.tenant_id AND d.outlet_id=pharmacy_supplier_returns.outlet_id AND d.return_id=pharmacy_supplier_returns.id)")
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "supplier_credit_return_choices", p.TenantID, p.Subject, credit, "list_read")
	})
	if err != nil {
		supplierCreditError(c, err)
		return nil, nil
	}
	return rows, nil
}

func supplierCreditSourceIndexes() []tenantdb.ConcurrentIndex {
	return []tenantdb.ConcurrentIndex{
		{Name: "pharmacy_supplier_credit_payable_choices", Table: "pharmacy_payables", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "outlet_id"}, {Name: "payee_reference"}, {Name: "currency"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}}},
		{Name: "pharmacy_supplier_credit_return_choices", Table: "pharmacy_supplier_returns", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "outlet_id"}, {Name: "payee_reference"}, {Name: "currency"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}}},
	}
}

func loadSupplierCreditContext(tx *gorm.DB, tenant string, outlet, credit uuid.UUID) (SupplierCreditContext, error) {
	view, err := loadSupplierCreditView(tx, tenant, outlet, credit)
	if err != nil {
		return SupplierCreditContext{}, err
	}
	var clock struct {
		AsOf                time.Time
		LocalDate, Timezone string
	}
	if err := tx.Table("pharmacy_outlets").Select("transaction_timestamp() AS as_of,to_char(transaction_timestamp() AT TIME ZONE timezone,'YYYY-MM-DD') AS local_date,timezone").Where("tenant_id=? AND id=?", tenant, outlet).Take(&clock).Error; err != nil {
		return SupplierCreditContext{}, err
	}
	return SupplierCreditContext{SupplierCreditDocumentView: view, LocalDate: clock.LocalDate, AsOf: clock.AsOf.UTC().Format(time.RFC3339Nano), Timezone: clock.Timezone}, nil
}

const supplierCreditPayableColumns = "id,outlet_id,payee_name,document_reference,to_char(issued_on,'YYYY-MM-DD') AS issued_on,to_char(due_on,'YYYY-MM-DD') AS due_on,currency,total_minor,paid_minor,credit_minor,outstanding_minor,pharmacy_supplier_payable_version(tenant_id,outlet_id,id) AS version,created_at" // #nosec G101 -- Static SQL identifiers; no credential values.
