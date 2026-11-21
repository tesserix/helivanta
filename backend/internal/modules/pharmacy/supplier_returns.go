package pharmacy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const (
	PermSupplierReturnRead     authz.Permission = "pharmacy.supplier_returns.read"
	PermSupplierReturnPrepare  authz.Permission = "pharmacy.supplier_returns.prepare"
	PermSupplierReturnDispatch authz.Permission = "pharmacy.supplier_returns.dispatch"
)

var (
	errSupplierReturnConflict    = errors.New("supplier return identity or state conflict")
	errSupplierReturnUnsupported = errors.New("platform supplier return protocol required")
	errSupplierReturnEvidence    = errors.New("supplier return read evidence unavailable")
)

type supplierReturnRow struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	OutletID            uuid.UUID
	Number              int64
	PayableID           uuid.UUID
	PayeeReference      string
	PayeeName           string
	DocumentReference   string
	Currency            string
	LineCount           int
	ExpectedCreditMinor int64
	Reason              string
	IntentSHA256        string
	ActorSubject        string
	CreatedAt           time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierReturnRow) TableName() string { return "pharmacy_supplier_returns" }

const supplierReturnColumns = "id,tenant_id,outlet_id,number,payable_id,payee_reference,payee_name,document_reference,currency,line_count,expected_credit_minor,reason,intent_sha256,actor_subject,created_at"

type supplierReturnLineRow struct {
	TenantID uuid.UUID
	OutletID uuid.UUID
	ReturnID uuid.UUID
	Position int
	Fields   SupplierReturnLine `gorm:"embedded"`
}

func (supplierReturnLineRow) TableName() string { return "pharmacy_supplier_return_lines" }

const supplierReturnLineColumns = `tenant_id,outlet_id,return_id,position,id,receipt_id,quarantine_location_id,quantity_units,expected_credit_minor,batch_id,product_id,product_version,product_name,generic_name,unit_code,lot_number,to_char(expires_on,'YYYY-MM-DD') AS expires_on,receipt_units,source_reference,to_char(source_created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS source_created_at`

func supplierReturnRoutes(s *platform.Router, h *catalogueHandlers) {
	r := s.Group("/supplier-returns")
	platform.ListRoute(r, "", PermSupplierReturnRead, h.listSupplierReturns)
	r.PUT("/:return", PermSupplierReturnPrepare, h.putSupplierReturn)
	r.GET("/:return", PermSupplierReturnRead, h.getSupplierReturn)
	r.PUT("/:return/cancellations/:cancellation", PermSupplierReturnPrepare, h.cancelSupplierReturn)
	r.GET("/:return/cancellations/:cancellation", PermSupplierReturnRead, h.getSupplierReturnCancellation)
	r.PUT("/:return/dispatches/:dispatch", PermSupplierReturnDispatch, h.dispatchSupplierReturn)
	r.GET("/:return/dispatches/:dispatch", PermSupplierReturnRead, h.getSupplierReturnDispatch)
}
func supplierReturnScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return p, outlet, false
	}
	subject, tenant := c.Request.Header.Values("X-Helivanta-Expected-Subject"), c.Request.Header.Values("X-Helivanta-Expected-Tenant")
	if len(subject) != 1 || subject[0] != p.Subject || len(tenant) != 1 || tenant[0] != p.TenantID {
		respond.Error(c, http.StatusConflict, "session_changed", "The financial account changed. Reload before preparing supplier returns.")
		return p, outlet, false
	}
	c.Header("Cache-Control", "private, no-store")
	return p, outlet, true
}
func supplierReturnID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "supplier return")
		return uuid.Nil, false
	}
	return id, true
}
func supplierReturnReadRequest(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Supplier return reads do not accept a body or query.")
		return false
	}
	return true
}
func loadSupplierReturn(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (supplierReturnRow, []SupplierReturnLine, error) {
	var row supplierReturnRow
	if err := tx.Select(supplierReturnColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Take(&row).Error; err != nil {
		return row, nil, err
	}
	var stored []supplierReturnLineRow
	if err := tx.Select(supplierReturnLineColumns).Where("tenant_id=? AND outlet_id=? AND return_id=?", tenant, outlet, id).Order("position").Limit(101).Find(&stored).Error; err != nil {
		return row, nil, err
	}
	if len(stored) != row.LineCount {
		return row, nil, errSupplierReturnEvidence
	}
	lines := make([]SupplierReturnLine, len(stored))
	var total int64
	for i, line := range stored {
		if line.Fields.ExpectedCreditMinor == nil || line.Fields.QuantityUnits == nil {
			return row, nil, errSupplierReturnEvidence
		}
		lines[i] = line.Fields
		total += *line.Fields.ExpectedCreditMinor
	}
	if total != row.ExpectedCreditMinor {
		return row, nil, errSupplierReturnEvidence
	}
	return row, lines, nil
}
func (r supplierReturnRow) receipt(lines []SupplierReturnLine) SupplierReturnCreationReceipt {
	return SupplierReturnCreationReceipt{ID: r.ID, OutletID: r.OutletID, Number: r.Number, PayableID: r.PayableID, PayeeReference: r.PayeeReference, PayeeName: r.PayeeName, DocumentReference: r.DocumentReference, Currency: r.Currency, ExpectedCreditMinor: r.ExpectedCreditMinor, Lines: lines, Reason: r.Reason, PreparedBy: r.ActorSubject, PreparedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
func (h *catalogueHandlers) getSupplierReturn(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "return")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnView
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		row, lines, err := loadSupplierReturn(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		receipt = SupplierReturnView{SupplierReturnCreationReceipt: row.receipt(lines), State: "draft", Version: 0}
		var cancelled int64
		if err := tx.Model(&supplierReturnCancellationRow{}).Where("tenant_id=? AND outlet_id=? AND return_id=?", p.TenantID, outlet, id).Count(&cancelled).Error; err != nil {
			return err
		}
		var dispatched int64
		if err := tx.Model(&supplierReturnDispatchRow{}).Where("tenant_id=? AND outlet_id=? AND return_id=?", p.TenantID, outlet, id).Count(&dispatched).Error; err != nil {
			return err
		}
		if dispatched > 0 {
			receipt.State = "dispatched"
			receipt.Version = 1
		}
		if cancelled > 0 {
			receipt.State = "cancelled"
			receipt.Version = 1
		}
		if err := auditRegisterRead(c, tx, "supplier_returns", p.TenantID, p.Subject, id, "read"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func supplierReturnError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "supplier return")
		return
	}
	if errors.Is(err, errSupplierReturnConflict) {
		respond.Conflict(c, "The supplier return command or state changed. Recover the original command before retrying.")
		return
	}
	if errors.Is(err, errSupplierReturnUnsupported) {
		respond.Error(c, http.StatusUnprocessableEntity, "supplier_return_protocol_required", "Platform supplier receipts require an acknowledged return workflow.")
		return
	}
	var pg *pgconn.PgError
	if !errors.Is(err, errSupplierReturnEvidence) && errors.As(err, &pg) && pg.TableName != "audit_events" && pg.TableName != "outbox_events" && (strings.HasPrefix(pg.ConstraintName, "pharmacy_supplier_return") || pg.ConstraintName == "pharmacy_stock_quantity_bounds" || pg.Code == "23505") {
		respond.Conflict(c, "Supplier return evidence changed or could not be completed.")
		return
	}
	respond.Error(c, http.StatusServiceUnavailable, "supplier_return_unavailable", "Supplier return authority or evidence is unavailable. Recover the original command before retrying.")
}
