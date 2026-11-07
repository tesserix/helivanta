package supply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"gorm.io/gorm"
)

func stockIdentityHash(actor string, in WholesaleIdentityInput) (string, error) {
	data, err := json.Marshal(struct {
		Actor string
		Input WholesaleIdentityInput
	}{actor, in})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
func readStockIdentity(tx *gorm.DB, tenant string, party, premise, receipt, id uuid.UUID, hash string) (WholesaleIdentityCheck, error) {
	var row struct {
		Document  []byte
		InputHash string
	}
	var result WholesaleIdentityCheck
	if err := tx.Table("supply_stock_identity_checks").Select("document,input_hash").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=? AND id=?", tenant, party, premise, receipt, id).Take(&row).Error; err != nil {
		return result, err
	}
	if hash != "" && hash != row.InputHash {
		return result, errWholesaleReceiptConflict
	}
	if err := json.Unmarshal(row.Document, &result); err != nil {
		return result, err
	}
	if result.OwnerTenantID.String() != tenant || result.PartyID != party || result.PremiseID != premise || result.ReceiptID != receipt || result.ID != id || result.Status != "matched" {
		return result, errors.New("stock identity evidence differs")
	}
	return result, nil
}
func stockIdentityError(c *gin.Context, err error) {
	if errors.Is(err, stockproduct.ErrIneligible) {
		respond.Conflict(c, "The selected product version is no longer current and active. Reconcile the physical item with the catalogue.")
		return
	}
	wholesaleStockError(c, err)
}
func (h *handlers) putStockIdentity(c *gin.Context) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	receipt, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	id, ok := resourceID(c, "check")
	if !ok {
		return
	}
	var in WholesaleIdentityInput
	if !decode(c, &in) {
		return
	}
	if in.ExpectedVersion < 0 || in.ExpectedVersion > 9007199254740990 || in.ProductVersionID == uuid.Nil || (in.UnitKind != "pack" && in.UnitKind != "unit") || in.ExpectedPackSize < 1 || in.ExpectedPackSize > 9007199254740991 || in.ExpectedBaseUnits < 1 || in.ExpectedBaseUnits > 9007199254740991 || !validStockText(in.Reason, 500) {
		respond.Error(c, 400, "invalid_request", "Choose an exact product version, unit interpretation, pack size, base-unit count and reason.")
		return
	}
	hash, err := stockIdentityHash(p.Subject, in)
	if err != nil {
		stockIdentityError(c, err)
		return
	}
	var saved WholesaleIdentityCheck
	var original wholesaleReceiptRow
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
	recoverReceipt := func(tx *gorm.DB) error {
		found, err := readStockIdentity(tx, p.TenantID, party, premise, receipt, id, hash)
		if err != nil {
			return err
		}
		saved = found
		recovered = true
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_identity_recovered','supply_stock_identity_checks',?)`, p.TenantID, p.Subject, id).Error
	}
	err = h.stockTransaction(c, p, party, premise, PermOperatingWrite, func(tx *gorm.DB) error {
		if err := load(tx); err != nil {
			return err
		}
		err := recoverReceipt(tx)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		stockIdentityError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
		return
	}
	if h.stockProducts == nil {
		stockIdentityError(c, stockproduct.ErrUnavailable)
		return
	}
	err = h.withStockProduct(c, p, party, premise, in.ProductVersionID, func(ctx context.Context, source stockproduct.Reference) error {
		factor := int64(1)
		if in.UnitKind == "pack" {
			factor = source.PackSize
		}
		if source.PackSize != in.ExpectedPackSize || factor < 1 || original.QuantityUnits > 9007199254740991/factor || original.QuantityUnits*factor != in.ExpectedBaseUnits || (in.UnitKind == "pack" && original.UnitCode != "pack") || (in.UnitKind == "unit" && original.UnitCode != source.UnitOfIssue) {
			return errWholesaleReceiptConflict
		}
		return h.stockOwnerTransaction(ctx, p, party, func(tx *gorm.DB) error {
			if err := load(tx); err != nil {
				return err
			}
			if err := tx.Exec(`SELECT id FROM supply_stock_receipts WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=? FOR UPDATE`, p.TenantID, party, premise, receipt).Error; err != nil {
				return err
			}
			err := recoverReceipt(tx)
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var version int64
			if err := tx.Raw(`SELECT COALESCE(max(version),0) FROM supply_stock_identity_checks WHERE tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?`, p.TenantID, party, premise, receipt).Scan(&version).Error; err != nil {
				return err
			}
			if version != in.ExpectedVersion {
				return errWholesaleReceiptConflict
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			saved = WholesaleIdentityCheck{ID: id, OwnerTenantID: uuid.MustParse(p.TenantID), PartyID: party, PremiseID: premise, ReceiptID: receipt, Input: in, Version: version + 1, QuantityBaseUnits: in.ExpectedBaseUnits, Source: source, Status: "matched", ActorSubject: p.Subject, CreatedAt: now}
			doc, err := json.Marshal(saved)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO supply_stock_identity_checks(tenant_id,party_id,premise_id,receipt_id,id,version,product_version_id,quantity_base_units,actor_subject,input_hash,document,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?::jsonb,?)`, p.TenantID, party, premise, receipt, id, saved.Version, source.VersionID, saved.QuantityBaseUnits, p.Subject, hash, string(doc), now).Error; err != nil {
				return err
			}
			if h.bus == nil {
				return errors.New("stock identity outbox unavailable")
			}
			data, err := json.Marshal(supplycontract.StockIdentityRecordedData{PartyID: party.String(), PremiseID: premise.String(), ReceiptID: receipt.String(), CheckID: id.String(), Version: saved.Version})
			if err != nil {
				return err
			}
			return h.bus.Publish(tx, supplycontract.SubjectStockIdentityRecorded, events.Event{Type: supplycontract.SubjectStockIdentityRecorded, Version: 1, TenantID: p.TenantID, Data: data})
		})
	})
	if err != nil {
		stockIdentityError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
	} else {
		respond.Created(c, saved)
	}
}
func (h *handlers) getStockIdentity(c *gin.Context) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	receipt, ok := resourceID(c, "receipt")
	if !ok {
		return
	}
	id, ok := resourceID(c, "check")
	if !ok {
		return
	}
	var saved WholesaleIdentityCheck
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockHistoryParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		var err error
		saved, err = readStockIdentity(tx, p.TenantID, party, premise, receipt, id, "")
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_identity_read','supply_stock_identity_checks',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		stockIdentityError(c, err)
		return
	}
	respond.OK(c, saved)
}
