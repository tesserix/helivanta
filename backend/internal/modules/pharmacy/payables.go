package pharmacy

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type payable struct {
	ID               uuid.UUID    `json:"id"`
	TenantID         uuid.UUID    `json:"-"`
	OutletID         uuid.UUID    `json:"outlet_id"`
	Fields           payableInput `gorm:"embedded" json:"-"`
	LineCount        int          `json:"line_count"`
	TotalMinor       int64        `json:"total_minor"`
	PaidMinor        int64        `gorm:"->" json:"paid_minor"`
	CreditMinor      int64        `gorm:"->" json:"credit_minor"`
	OutstandingMinor int64        `gorm:"->" json:"outstanding_minor"`
	ActorSubject     string       `json:"actor_subject"`
	CreatedAt        time.Time    `json:"created_at"`
}

func (payable) TableName() string                 { return "pharmacy_payables" }
func (r payable) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (r payable) MarshalJSON() ([]byte, error) {
	type fields payable
	return json.Marshal(struct {
		fields
		payableInput
	}{fields: fields(r), payableInput: r.Fields})
}

type payableLine struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	OutletID  uuid.UUID
	PayableID uuid.UUID
	Position  int
	Fields    payableLineInput `gorm:"embedded"`
}

func (payableLine) TableName() string { return "pharmacy_payable_lines" }

const payableColumns = "id,tenant_id,outlet_id,payee_reference,payee_name,document_reference,to_char(issued_on,'YYYY-MM-DD') AS issued_on,to_char(due_on,'YYYY-MM-DD') AS due_on,currency,line_count,total_minor,actor_subject,created_at,paid_minor,credit_minor,outstanding_minor"

var errPayableConflict = errors.New("payable identity conflict")

func payableRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB, bus: deps.Bus}
	s := g.Group("/pharmacies/:pharmacy/outlets/:outlet/accounts")
	s.PUT("/payables/:id", PermAccountsWrite, h.putPayable)
	s.GET("/payables/:id", PermAccountsRead, h.getPayable)
	s.PUT("/payables/:id/payments/:entry", PermAccountsPayment, h.putPayment)
	s.PUT("/payables/:id/adjustments/:entry", PermAccountsAdjust, h.putAdjustment)
	platform.ListRoute(s, "/payables/:id/settlements", PermAccountsRead, h.listSettlements)
	platform.ListRoute(s, "/payables", PermAccountsRead, h.listPayables)
	platform.ListRoute(s, "/inventory-audit", PermAccountsRead, h.listInventoryAudit)
}
func payableError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, errPayableConflict) || (errors.As(err, &pg) && pg.Code == "23505") {
		respond.Conflict(c, "document, receipt cost or settlement reference already exists; reload before recording")
		return
	}
	if errors.As(err, &pg) && pg.ConstraintName == "pharmacy_settlement_balance" {
		respond.Conflict(c, "settlement exceeds the outstanding balance")
		return
	}
	if errors.As(err, &pg) && pg.Code == "23514" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid payable amounts, dates or inventory receipt")
		return
	}
	catalogueError(c, err)
}
func validPayable(in *payableInput) (int64, bool) {
	in.PayeeReference = strings.ToUpper(strings.TrimSpace(in.PayeeReference))
	in.PayeeName = strings.TrimSpace(in.PayeeName)
	in.DocumentReference = strings.ToUpper(strings.TrimSpace(in.DocumentReference))
	issued, e1 := time.Parse(time.DateOnly, in.IssuedOn)
	due, e2 := time.Parse(time.DateOnly, in.DueOn)
	if e1 != nil || e2 != nil || due.Before(issued) || !currencyPattern.MatchString(in.Currency) || in.PayeeReference == "" || in.PayeeName == "" || in.DocumentReference == "" {
		return 0, false
	}
	var total int64
	for i := range in.Lines {
		line := &in.Lines[i]
		line.Description = strings.TrimSpace(line.Description)
		if line.Description == "" || line.RecoverableTaxMinor > line.TaxMinor || (line.Category == "inventory") != (line.ReceiptID != nil) {
			return 0, false
		}
		if line.ReceiptID != nil && (line.ReceiptID.Version() != 7 || line.ReceiptID.Variant() != uuid.RFC4122) {
			return 0, false
		}
		sum := line.NetMinor + line.TaxMinor + line.LandedMinor
		if sum <= 0 {
			return 0, false
		}
		total += sum
	}
	return total, total > 0 && total <= 1000000000000
}
func loadPayable(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (payable, error) {
	var row payable
	if err := tx.Table("pharmacy_payable_balances").Select(payableColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).First(&row).Error; err != nil {
		return row, err
	}
	var lines []payableLine
	if err := tx.Select("id,tenant_id,outlet_id,payable_id,position,receipt_id,category,description,net_minor,tax_minor,recoverable_tax_minor,landed_minor").Where("tenant_id=? AND outlet_id=? AND payable_id=?", tenant, outlet, id).Order("position").Find(&lines).Error; err != nil {
		return row, err
	}
	row.Fields.Lines = make([]payableLineInput, len(lines))
	for i, line := range lines {
		row.Fields.Lines[i] = line.Fields
	}
	return row, nil
}
func (h *catalogueHandlers) putPayable(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in payableInput
	if !decodeCatalogue(c, &in) {
		return
	}
	total, ok := validPayable(&in)
	if !ok {
		respond.Error(c, 400, "invalid_request", "check payable dates, costs and receipt links")
		return
	}
	var result payable
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":payable:"+id.String()); err != nil {
			return err
		}
		existing, err := loadPayable(tx, p.TenantID, outlet, id)
		if err == nil {
			if !reflect.DeepEqual(existing.Fields, in) {
				return errPayableConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = payable{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, Fields: in, LineCount: len(in.Lines), TotalMinor: total, ActorSubject: p.Subject}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		lines := make([]payableLine, len(in.Lines))
		for i, line := range in.Lines {
			lines[i] = payableLine{ID: uuid.Must(uuid.NewV7()), TenantID: result.TenantID, OutletID: outlet, PayableID: id, Position: i + 1, Fields: line}
		}
		if err := tx.Create(&lines).Error; err != nil {
			return err
		}
		result, err = loadPayable(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.PayableRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), PayableID: id.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectPayableRecorded, events.Event{Type: "PayableRecorded", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		payableError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *catalogueHandlers) getPayable(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var result payable
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		result, err = loadPayable(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		payableError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *catalogueHandlers) listPayables(c *gin.Context, params pagination.Params) ([]payable, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []payable
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Table("pharmacy_payable_balances").Select(payableColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		payableError(c, err)
	}
	return rows, nil
}
