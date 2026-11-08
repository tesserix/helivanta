package supply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module event contracts
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

const PermStockFulfil authz.Permission = "supply.stock.fulfil"

type warehouseFulfilment struct {
	db        *tenantdb.DB
	operating authz.OperatingScopeAuthorizer
	bus       *events.Bus
}

// SupplierStock returns the physical owner adapter used by the registered fulfilment API.
func SupplierStock(deps platform.Deps) *warehouseFulfilment {
	return &warehouseFulfilment{db: deps.DB, operating: deps.OperatingAuthorizer, bus: deps.Bus}
}
func registerSupplierFulfilment(r *platform.Router, deps platform.Deps) {
	adapter := SupplierStock(deps)
	(supplierstock.HTTP{Owner: supplierstock.Warehouse, DB: deps.DB, Orders: deps.FulfilmentOrders, Adapter: adapter, OrganizationParam: "party", LocationParam: "premise"}).Register(r, "/parties/:party/operating-locations/:premise/supplier-orders/:order/allocations/:allocation")
}
func (h *warehouseFulfilment) Admit(ctx context.Context, p authn.Principal, s f.SourceScope, action func(context.Context, supplierorder.Actor) error) error {
	if h.db == nil || h.operating == nil || s.TenantID.String() != p.TenantID || s.Product != "supply" {
		return f.ErrUnavailable
	}
	scope := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: "supplier", EmployerID: s.OrganizationID, LocationID: s.LocationID}
	return h.operating.WithOperatingScope(tenantdb.WithActor(ctx, p.Subject), authz.OperatingScopeRequest{Principal: p, EmployerKind: "supplier", EmployerID: s.OrganizationID, LocationID: s.LocationID, Permission: PermStockFulfil}, func(ctx context.Context, g authz.OperatingScopeGrant) error {
		if g.Scope != scope || g.Permission != PermStockFulfil || g.Identity.StaffID == uuid.Nil {
			return f.ErrUnavailable
		}
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
			var old string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.supply_subject','app.supply_party','app.supply_stock_access','app.supply_stock_party','app.supply_stock_premise','app.supply_stock_permission'])AS key`).Scan(&old).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, old).Error)
			}()
			if err = tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_party',?,true),set_config('app.supply_stock_access','authorized',true),set_config('app.supply_stock_party',?,true),set_config('app.supply_stock_premise',?,true),set_config('app.supply_stock_permission',?,true)`, p.Subject, s.OrganizationID.String(), s.OrganizationID.String(), s.LocationID.String(), string(PermStockFulfil)).Error; err != nil {
				return err
			}
			return action(ctx, supplierorder.Actor{Subject: p.Subject, StaffID: g.Identity.StaffID})
		})
	})
}
func (h *warehouseFulfilment) Lock(tx *gorm.DB, a f.Allocation) error {
	var receipt uuid.UUID
	err := tx.Raw(`SELECT id FROM supply_stock_receipts WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=? FOR UPDATE`, a.Plan.OwnerTenantID, a.Plan.SourceOrganizationID, a.Plan.SourceLocationID, a.Plan.Input.StockID).Scan(&receipt).Error
	if err != nil {
		return err
	}
	if receipt == uuid.Nil {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func (h *warehouseFulfilment) Snapshot(tx *gorm.DB, p f.AllocationPlan) (f.StockEvidence, error) {
	if err := h.Lock(tx, f.Allocation{Plan: p}); err != nil {
		return f.StockEvidence{}, err
	}
	return warehouseSupplierEvidence(tx, p)
}

func warehouseSupplierEvidence(tx *gorm.DB, p f.AllocationPlan) (f.StockEvidence, error) {
	var receipt wholesaleReceiptRow
	if err := tx.Raw(`SELECT `+wholesaleReceiptColumns+` FROM supply_stock_receipts WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=?`, p.OwnerTenantID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID).Scan(&receipt).Error; err != nil {
		return f.StockEvidence{}, err
	}
	var identity WholesaleIdentityCheck
	var quality WholesaleQualityDecision
	var row struct{ Document json.RawMessage }
	err := tx.Table("supply_stock_identity_checks").Select("document").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.OwnerTenantID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID).Order("version DESC").Take(&row).Error
	if err != nil {
		return f.StockEvidence{}, err
	}
	if json.Unmarshal(row.Document, &identity) != nil {
		return f.StockEvidence{}, f.ErrUnavailable
	}
	row.Document = nil
	err = tx.Table("supply_stock_quality_decisions").Select("document").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.OwnerTenantID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID).Order("version DESC").Take(&row).Error
	if err != nil {
		return f.StockEvidence{}, err
	}
	if json.Unmarshal(row.Document, &quality) != nil {
		return f.StockEvidence{}, f.ErrUnavailable
	}
	if identity.Status != "matched" || quality.Status != "released" || quality.Input.IdentityCheckID != identity.ID || quality.Input.IdentityVersion != identity.Version || quality.ReleasedBaseUnits != identity.QuantityBaseUnits || p.Product.VersionID == nil || identity.Source.VersionID != *p.Product.VersionID || identity.Source.ProductID != p.Product.ProductID || identity.Source.UnitOfIssue != p.UnitCode {
		return f.StockEvidence{}, f.ErrConflict
	}
	var used int64
	if err = tx.Table("supply_stock_fulfilment_balances").Select("COALESCE(dispatched_units,0)").Where("tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?", p.OwnerTenantID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID).Scan(&used).Error; err != nil {
		return f.StockEvidence{}, err
	}
	held, err := (supplierstock.Store{Owner: supplierstock.Warehouse}).Held(tx, p.OwnerTenantID, p.SourceLocationID, p.Input.StockID, uuid.Nil)
	if err != nil {
		return f.StockEvidence{}, err
	}
	if used < 0 || used > quality.ReleasedBaseUnits || held < 0 || held > quality.ReleasedBaseUnits-used {
		return f.StockEvidence{}, f.ErrUnavailable
	}
	description := fmt.Sprintf("%s | %s | %s | pack %d %s", identity.Source.BrandName, identity.Source.Manufacturer, identity.Source.FormulationLabel, identity.Source.PackSize, identity.Source.UnitOfIssue)
	return f.StockEvidence{LotNumber: receipt.LotNumber, ExpiresOn: receipt.ExpiresOn, AvailableUnits: quality.ReleasedBaseUnits - used - held, Description: description, PackSize: identity.Source.PackSize}, nil
}
func (h *warehouseFulfilment) Apply(tx *gorm.DB, r f.AllocationCommandReceipt) error {
	if h.bus == nil {
		return f.ErrUnavailable
	}
	if r.Input.Decision == "release" {
		return nil
	}
	p := r.Allocation.Plan
	// update already reduced this command's hold; current quality and original lot remain mandatory.
	fact, err := h.Snapshot(tx, p)
	if err != nil {
		return err
	}
	if fact.LotNumber != r.Allocation.LotNumber || fact.ExpiresOn != r.Allocation.ExpiresOn || fact.AvailableUnits < r.Input.QuantityUnits {
		return f.ErrConflict
	}
	if err = tx.Exec(`INSERT INTO supply_stock_dispatches(tenant_id,id,party_id,premise_id,receipt_id,allocation_id,quantity_units,actor_subject,created_at)VALUES(?,?,?,?,?,?,?,?,?)`, p.OwnerTenantID, r.CommandID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID, p.ID, r.Input.QuantityUnits, r.ActorSubject, r.RecordedAt).Error; err != nil {
		return err
	}
	return publishWarehouseDispatch(tx, r)
}
func publishWarehouseDispatch(tx *gorm.DB, r f.AllocationCommandReceipt) error {
	shipment, err := f.ShipmentFor(r, nil)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(shipment)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, supplycontract.SubjectSupplierDispatchRecorded, events.Event{ID: id.String(), Type: supplycontract.SubjectSupplierDispatchRecorded, Version: 1, OccurredAt: time.Now().UTC(), TenantID: shipment.SourceTenantID.String(), Data: raw})
}
