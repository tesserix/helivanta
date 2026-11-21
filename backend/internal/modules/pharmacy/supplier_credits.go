package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

var errSupplierCreditConflict = errors.New("supplier credit identity or state conflict")
var errSupplierCreditEvidence = errors.New("supplier credit evidence unavailable")

type supplierCreditDocumentRow struct {
	ID, TenantID, OutletID                                                       uuid.UUID
	Fields                                                                       SupplierCreditDocumentInput `gorm:"embedded"`
	ReturnNumber                                                                 int64
	PayeeReference, PayeeName, OriginalBillReference, IntentSHA256, ActorSubject string
	CreatedAt                                                                    time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierCreditDocumentRow) TableName() string { return "pharmacy_supplier_credit_documents" }

const supplierCreditDocumentColumns = `id,tenant_id,outlet_id,return_id,document_reference,to_char(issued_on,'YYYY-MM-DD') AS issued_on,amount_minor,currency,reason,correction_of,return_number,payee_reference,payee_name,original_bill_reference,intent_sha256,actor_subject,created_at`

func (r supplierCreditDocumentRow) receipt() SupplierCreditDocumentReceipt {
	return SupplierCreditDocumentReceipt{SupplierCreditDocumentInput: r.Fields, ID: r.ID, OutletID: r.OutletID, ReturnNumber: r.ReturnNumber, PayeeReference: r.PayeeReference, PayeeName: r.PayeeName, OriginalBillReference: r.OriginalBillReference, RecordedBy: r.ActorSubject, RecordedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

type supplierCreditViewRow struct {
	Document                                              supplierCreditDocumentRow `gorm:"embedded"`
	Version, MatchedMinor, AllocatedMinor, AvailableMinor int64
	State                                                 string
}

func (r supplierCreditViewRow) PageKey() (time.Time, uuid.UUID) {
	return r.Document.CreatedAt, r.Document.ID
}
func (r supplierCreditViewRow) view() (SupplierCreditDocumentView, error) {
	amount := r.Document.Fields.AmountMinor
	if amount == nil || *amount < 1 || *amount > 1000000000000 || r.Version < 1 || r.Version > 1000000000000 || r.MatchedMinor < 0 || r.MatchedMinor > *amount || r.AllocatedMinor < 0 || r.AllocatedMinor > *amount {
		return SupplierCreditDocumentView{}, errSupplierCreditEvidence
	}
	available := int64(0)
	if r.State == "active" {
		available = *amount - r.AllocatedMinor
	} else if (r.State != "voided" && r.State != "corrected") || r.MatchedMinor != 0 || r.AllocatedMinor != 0 {
		return SupplierCreditDocumentView{}, errSupplierCreditEvidence
	}
	if r.AvailableMinor != available {
		return SupplierCreditDocumentView{}, errSupplierCreditEvidence
	}
	return SupplierCreditDocumentView{SupplierCreditDocumentReceipt: r.Document.receipt(), Version: r.Version, State: r.State, MatchedMinor: r.MatchedMinor, AllocatedMinor: r.AllocatedMinor, AvailableMinor: r.AvailableMinor}, nil
}
func (r supplierCreditViewRow) MarshalJSON() ([]byte, error) {
	v, err := r.view()
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func supplierCreditRoutes(s *platform.Router, h *catalogueHandlers) {
	r := s.Group("/supplier-credits")
	platform.ResourceListRoute(r, "", PermAccountsRead, 50, h.listSupplierCredits)
	r.PUT("/:credit", PermAccountsAdjust, h.putSupplierCreditDocument)
	r.GET("/:credit", PermAccountsRead, h.getSupplierCreditDocument)
	r.GET("/:credit/recovery", PermAccountsRead, h.recoverSupplierCreditDocument)
	r.GET("/:credit/void", PermAccountsRead, h.getSupplierCreditVoid)
	r.PUT("/:credit/voids/:entry", PermAccountsAdjust, h.putSupplierCreditVoid)
	r.GET("/:credit/voids/:entry/recovery", PermAccountsRead, h.recoverSupplierCreditVoid)
}
func supplierCreditID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil || !validSupplierCreditID(id) {
		respond.NotFound(c, "supplier_credit")
		return uuid.Nil, false
	}
	return id, true
}
func supplierCreditLock(tx *gorm.DB, tenant string, outlet, id uuid.UUID) error {
	return stockLock(tx, tenant+":"+outlet.String()+":supplier-credit:"+id.String())
}
func loadSupplierCreditDocument(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (supplierCreditDocumentRow, error) {
	var row supplierCreditDocumentRow
	err := tx.Select(supplierCreditDocumentColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Take(&row).Error
	return row, err
}
func loadSupplierCreditView(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (SupplierCreditDocumentView, error) {
	var row supplierCreditViewRow
	err := tx.Table("pharmacy_supplier_credit_balances").Select(supplierCreditDocumentColumns+",version,state,matched_minor,allocated_minor,available_minor").Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Take(&row).Error
	if err != nil {
		return SupplierCreditDocumentView{}, err
	}
	return row.view()
}
func supplierCreditError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "supplier_credit")
		return
	}
	var pg *pgconn.PgError
	conflict := errors.Is(err, errSupplierCreditConflict) || (errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514") && strings.HasPrefix(pg.ConstraintName, "pharmacy_supplier_credit") && pg.ConstraintName != "pharmacy_supplier_credit_projection_complete")
	if conflict {
		respond.Error(c, http.StatusConflict, "supplier_credit_conflict", "The credit identity, source or current state changed. Recheck the original reference and current record.")
		return
	}
	respond.Error(c, http.StatusServiceUnavailable, "supplier_credit_outcome_unknown", "The credit result is unconfirmed. Check the original reference before trying again.")
}
