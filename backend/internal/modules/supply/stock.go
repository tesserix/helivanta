package supply

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

//go:embed stock.sql
var stockSchema string

var stockUnitCode = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,39}$`)
var errWholesaleReceiptConflict = errors.New("wholesale receipt intent differs")

func stockScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	if !authn.Refresh(c) {
		return authn.Principal{}, uuid.Nil, uuid.Nil, false
	}
	p, party, premise, ok := binScope(c)
	if !ok {
		return p, party, premise, false
	}
	for _, expected := range []struct{ header, value string }{{"X-Helivanta-Expected-Tenant", p.TenantID}, {"X-Helivanta-Expected-Subject", p.Subject}} {
		values, present := c.Request.Header[expected.header]
		if present && (len(values) != 1 || values[0] != expected.value) {
			respond.Error(c, 409, "session_changed", "Your workspace or signed-in account changed. Refresh before recording stock.")
			return p, party, premise, false
		}
	}
	return p, party, premise, true
}

func lockStockAccess(tx *gorm.DB, tenant string, party uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, tenant+":supply-access-admin:"+party.String()).Error
}

func registerStockRoutes(g *platform.Router, h *handlers) {
	g.GET("/parties/:party/operating-locations/:premise/stock/products/by-gtin/:gtin", authz.Public, h.stockProductByBarcode)
	p := "/parties/:party/operating-locations/:premise/stock/receipts"
	g.PUT(p+"/:receipt", authz.Public, h.putStockReceipt)
	g.GET(p+"/:receipt", authz.Public, h.getStockReceipt)
	platform.ListRoute(g, p, authz.Public, h.listStockReceipts)
	g.PUT(p+"/:receipt/identity-checks/:check", authz.Public, h.putStockIdentity)
	g.GET(p+"/:receipt/identity-checks/:check", authz.Public, h.getStockIdentity)
}

type wholesaleReceiptRow struct {
	TenantID              uuid.UUID
	ID                    uuid.UUID
	PartyID               uuid.UUID
	PremiseID             uuid.UUID
	WholesaleReceiptInput `gorm:"embedded"`
	ActorSubject          string
	CreatedAt             time.Time
}

func (wholesaleReceiptRow) TableName() string { return "supply_stock_receipts" }

const wholesaleReceiptColumns = "id,tenant_id,party_id,premise_id,product_version_id,lot_number,to_char(expires_on,'YYYY-MM-DD') AS expires_on,unit_code,quantity_units,source_reference,reason,actor_subject,created_at"

func (r wholesaleReceiptRow) receipt() WholesaleReceipt {
	return WholesaleReceipt{ID: r.ID, OwnerTenantID: r.TenantID, PartyID: r.PartyID, PremiseID: r.PremiseID, Input: r.WholesaleReceiptInput, StockStatus: "quarantine", ActorSubject: r.ActorSubject, CreatedAt: r.CreatedAt.UTC()}
}
func (r WholesaleReceipt) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func validStockText(value string, limit int) bool {
	return value != "" && strings.TrimSpace(value) == value && len([]rune(value)) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validStockReceipt(in WholesaleReceiptInput) bool {
	expiry, err := time.Parse(time.DateOnly, in.ExpiresOn)
	if err != nil || expiry.Year() < 1900 || in.ProductVersionID == uuid.Nil || !stockUnitCode.MatchString(in.UnitCode) || in.QuantityUnits < 1 || in.QuantityUnits > 9007199254740991 {
		return false
	}
	for _, f := range []struct {
		s   string
		max int
	}{{in.LotNumber, 100}, {in.SourceReference, 200}, {in.Reason, 500}} {
		if !validStockText(f.s, f.max) {
			return false
		}
	}
	return true
}
func wholesaleStockError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrScopedCommandDenied):
		respond.NotFound(c, "stock receipt")
	case errors.Is(err, errWholesaleReceiptConflict), errors.As(err, &pg) && pg.Code == "23505":
		respond.Conflict(c, "Receipt identity or source line already retains different intent. Recover the original receipt.")
	default:
		respond.Error(c, 503, "stock_unavailable", "Stock evidence is unavailable. Recover or retry the same receipt identifier.")
	}
}
func (h *handlers) putStockReceipt(c *gin.Context) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	var in WholesaleReceiptInput
	if !decode(c, &in) {
		return
	}
	if !validStockReceipt(in) {
		respond.Error(c, 400, "invalid_request", "Provide exact product reference, lot, expiry, unit, positive count and source line.")
		return
	}
	var row wholesaleReceiptRow
	created := false
	err := h.stockTransaction(c, p, party, premise, PermOperatingWrite, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockParent(tx, p, party, premise, []string{"administrator", "inventory"}); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-stock-receipt:"+party.String()+":"+premise.String()+":"+id.String()).Error; err != nil {
			return err
		}
		err := tx.Select(wholesaleReceiptColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, party, premise, id).Take(&row).Error
		if err == nil {
			if row.WholesaleReceiptInput != in || row.ActorSubject != p.Subject {
				return errWholesaleReceiptConflict
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_receipt_recovered','supply_stock_receipts',?)`, p.TenantID, p.Subject, id).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO supply_stock_receipts(tenant_id,party_id,premise_id,id,product_version_id,lot_number,expires_on,unit_code,quantity_units,source_reference,reason,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, party, premise, id, in.ProductVersionID, in.LotNumber, in.ExpiresOn, in.UnitCode, in.QuantityUnits, in.SourceReference, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Select(wholesaleReceiptColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, party, premise, id).Take(&row).Error; err != nil {
			return err
		}
		if h.bus == nil {
			return errors.New("stock outbox unavailable")
		}
		data, err := json.Marshal(supplycontract.StockReceiptRecordedData{PartyID: party.String(), PremiseID: premise.String(), ReceiptID: id.String()})
		if err != nil {
			return err
		}
		created = true
		return h.bus.Publish(tx, supplycontract.SubjectStockReceiptRecorded, events.Event{Type: supplycontract.SubjectStockReceiptRecorded, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		wholesaleStockError(c, err)
		return
	}
	if created {
		respond.Created(c, row.receipt())
	} else {
		respond.OK(c, row.receipt())
	}
}
func (h *handlers) getStockReceipt(c *gin.Context) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	var row wholesaleReceiptRow
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockHistoryParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		if err := tx.Select(wholesaleReceiptColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, party, premise, id).Take(&row).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_receipt_read','supply_stock_receipts',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		wholesaleStockError(c, err)
		return
	}
	respond.OK(c, row.receipt())
}
func (h *handlers) listStockReceipts(c *gin.Context, page pagination.Params) ([]WholesaleReceipt, error) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return nil, nil
	}
	result := []WholesaleReceipt{}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "Use one bounded page limit and optional continuation cursor.")
			return nil, nil
		}
	}
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockHistoryParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		var rows []wholesaleReceiptRow
		q := tx.Select(wholesaleReceiptColumns).Where("tenant_id=? AND party_id=? AND premise_id=?", p.TenantID, party, premise).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
		if page.After != nil {
			q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			result = append(result, row.receipt())
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_receipt_list_read','supply_stock_receipts',?)`, p.TenantID, p.Subject, premise).Error
	})
	if err != nil {
		wholesaleStockError(c, err)
	}
	return result, err
}
