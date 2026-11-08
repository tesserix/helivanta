package supply

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

//go:embed stock_quality.sql
var stockQualitySchema string

func registerStockQualityRoutes(g *platform.Router, h *handlers) {
	path := "/parties/:party/operating-locations/:premise/stock/receipts/:receipt/quality-decisions/:decision"
	g.PUT(path, authz.Public, h.putStockQuality)
	g.GET(path, authz.Public, h.getStockQuality)
	g.GET("/parties/:party/operating-locations/:premise/stock/receipts/:receipt/inspection", authz.Public, h.getStockInspection)
}

func stockQualityHash(actor string, in WholesaleQualityInput) (string, error) {
	raw, err := json.Marshal(struct {
		Actor string
		Input WholesaleQualityInput
	}{actor, in})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func readStockQuality(tx *gorm.DB, tenant string, party, premise, receipt, id uuid.UUID, hash string) (WholesaleQualityDecision, error) {
	var row struct {
		Document  []byte
		InputHash string
	}
	var result WholesaleQualityDecision
	if err := tx.Table("supply_stock_quality_decisions").Select("document,input_hash").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=? AND id=?", tenant, party, premise, receipt, id).Take(&row).Error; err != nil {
		return result, err
	}
	if hash != "" && hash != row.InputHash {
		return result, errWholesaleReceiptConflict
	}
	if err := json.Unmarshal(row.Document, &result); err != nil {
		return result, err
	}
	if result.OwnerTenantID.String() != tenant || result.PartyID != party || result.PremiseID != premise || result.ReceiptID != receipt || result.ID != id {
		return result, errors.New("stock quality evidence differs")
	}
	return result, nil
}
func stockQualityError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "supply_stock_quality_guard" {
		err = errWholesaleReceiptConflict
	}
	if errors.Is(err, errWholesaleReceiptConflict) || errors.Is(err, stockproduct.ErrIneligible) {
		respond.Error(c, 409, "stock_quality_conflict", "The count, product, reviewer, expiry or quality version changed. Refresh the stock evidence.")
		return
	}
	wholesaleStockError(c, err)
}
func (h *handlers) putStockQuality(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	receipt, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	id, ok := resourceID(c, "decision")
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("quality decisions do not accept query parameters"))
		return
	}
	var in WholesaleQualityInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil || suppliertrade.DecodeObject(raw, &in) != nil {
		respond.BadRequest(c, errors.New("provide one bounded quality decision object"))
		return
	}
	if !validStockQualityInput(in) {
		respond.BadRequest(c, errors.New("provide an exact current identity check, decision, version and reason"))
		return
	}
	hash, err := stockQualityHash(p.Subject, in)
	if err != nil {
		stockQualityError(c, err)
		return
	}
	var saved WholesaleQualityDecision
	var original wholesaleReceiptRow
	var identity WholesaleIdentityCheck
	recovered := false
	load := func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockParent(tx, p, party, premise, []string{"administrator", "inventory"}); err != nil {
			return err
		}
		return tx.Select(wholesaleReceiptColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, party, premise, receipt).Take(&original).Error
	}
	recoverCommand := func(tx *gorm.DB) error {
		result, err := readStockQuality(tx, p.TenantID, party, premise, receipt, id, hash)
		if err != nil {
			return err
		}
		saved, recovered = result, true
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_quality_recovered','supply_stock_quality_decisions',?)`, p.TenantID, p.Subject, id).Error
	}
	err = h.stockTransaction(c, p, party, premise, PermStockReview, func(tx *gorm.DB) error {
		if err := load(tx); err != nil {
			return err
		}
		if err := recoverCommand(tx); err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var err error
		identity, err = readStockIdentity(tx, p.TenantID, party, premise, receipt, in.IdentityCheckID, "")
		return err
	})
	if err != nil {
		stockQualityError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
		return
	}
	write := func(tx *gorm.DB) error {
		if err := load(tx); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT id FROM supply_stock_receipts WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=? FOR UPDATE`, p.TenantID, party, premise, receipt).Error; err != nil {
			return err
		}
		if err := recoverCommand(tx); err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var latest WholesaleIdentityCheck
		var row struct{ Document []byte }
		if err := tx.Table("supply_stock_identity_checks").Select("document").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.TenantID, party, premise, receipt).Order("version DESC").Take(&row).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(row.Document, &latest); err != nil {
			return err
		}
		var head int64
		if err := tx.Raw(`SELECT COALESCE(max(version),0)FROM supply_stock_quality_decisions WHERE tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?`, p.TenantID, party, premise, receipt).Scan(&head).Error; err != nil {
			return err
		}
		var err error
		saved, err = decideStockQuality(original, latest, head, in, p.Subject, time.Now())
		if err != nil {
			return err
		}
		// The product guard checked this exact observation; a newer match must be reviewed anew.
		if latest.ID != identity.ID || latest.Source != identity.Source {
			return errWholesaleReceiptConflict
		}
		saved.ID = id
		doc, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO supply_stock_quality_decisions(tenant_id,party_id,premise_id,receipt_id,id,version,identity_check_id,identity_version,decision,released_base_units,reason,actor_subject,input_hash,document,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?)`, p.TenantID, party, premise, receipt, id, saved.Version, in.IdentityCheckID, in.IdentityVersion, in.Decision, saved.ReleasedBaseUnits, in.Reason, p.Subject, hash, string(doc), saved.CreatedAt).Error; err != nil {
			return err
		}
		if h.bus == nil {
			return errors.New("stock quality outbox unavailable")
		}
		data, err := json.Marshal(supplycontract.StockQualityRecordedData{PartyID: party.String(), PremiseID: premise.String(), ReceiptID: receipt.String(), DecisionID: id.String(), Version: saved.Version})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, supplycontract.SubjectStockQualityRecorded, events.Event{Type: supplycontract.SubjectStockQualityRecorded, Version: 1, TenantID: p.TenantID, Data: data})
	}
	if in.Decision == "release" {
		if h.stockProducts == nil {
			stockQualityError(c, stockproduct.ErrUnavailable)
			return
		}
		err = h.withStockProduct(c, p, party, premise, identity.Source.VersionID, PermStockReview, func(ctx context.Context, current stockproduct.Reference) error {
			if current != identity.Source {
				return errWholesaleReceiptConflict
			}
			return h.stockOwnerTransaction(ctx, p, party, write)
		})
	} else {
		err = h.stockTransaction(c, p, party, premise, PermStockReview, write)
	}
	if err != nil {
		stockQualityError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
	} else {
		respond.Created(c, saved)
	}
}
func (h *handlers) getStockQuality(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	receipt, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	id, ok := resourceID(c, "decision")
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("quality recovery does not accept query parameters"))
		return
	}
	var saved WholesaleQualityDecision
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockHistoryParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		var err error
		saved, err = readStockQuality(tx, p.TenantID, party, premise, receipt, id, "")
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_quality_read','supply_stock_quality_decisions',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		stockQualityError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) getStockInspection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	receipt, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("inspection does not accept query parameters"))
		return
	}
	result := WholesaleStockInspection{OwnerTenantID: uuid.MustParse(p.TenantID), PartyID: party, PremiseID: premise, ReceiptID: receipt}
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockHistoryParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		var r wholesaleReceiptRow
		if err := tx.Raw(`SELECT `+wholesaleReceiptColumns+` FROM supply_stock_receipts WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=? FOR SHARE`, p.TenantID, party, premise, receipt).Scan(&r).Error; err != nil {
			return err
		}
		if r.ID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		var row struct{ Document []byte }
		err := tx.Table("supply_stock_identity_checks").Select("document").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.TenantID, party, premise, receipt).Order("version DESC").Take(&row).Error
		if err == nil {
			result.Identity = new(WholesaleIdentityCheck)
			if err = json.Unmarshal(row.Document, result.Identity); err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row.Document = nil
		err = tx.Table("supply_stock_quality_decisions").Select("document").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.TenantID, party, premise, receipt).Order("version DESC").Take(&row).Error
		if err == nil {
			result.Quality = new(WholesaleQualityDecision)
			if err = json.Unmarshal(row.Document, result.Quality); err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_inspection_read','supply_stock_receipts',?)`, p.TenantID, p.Subject, receipt).Error
	})
	if err != nil {
		stockQualityError(c, err)
		return
	}
	respond.OK(c, result)
}
