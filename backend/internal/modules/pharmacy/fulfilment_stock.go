package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
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

const PermStockFulfil authz.Permission = "pharmacy.stock.fulfil"
const PermHospitalFulfilmentUse authz.Permission = "pharmacy.hospital_fulfilment.use"

type outletFulfilment struct {
	db          *tenantdb.DB
	operating   authz.OperatingScopeAuthorizer
	bus         *events.Bus
	hospital    f.HospitalSourceAuthority
	departments authz.DepartmentReferenceGuard
}

func registerSupplierFulfilment(r *platform.Router, deps platform.Deps) {
	a := &outletFulfilment{db: deps.DB, operating: deps.OperatingAuthorizer, bus: deps.Bus, hospital: deps.HospitalSourceAuthority, departments: deps.DepartmentReferences}
	(supplierstock.HTTP{Owner: supplierstock.Outlet, DB: deps.DB, Orders: deps.FulfilmentOrders, Adapter: a, OrganizationParam: "pharmacy", LocationParam: "outlet"}).Register(r, "/pharmacies/:pharmacy/outlets/:outlet/supplier-orders/:order/allocations/:allocation")
	(supplierstock.HTTP{Owner: supplierstock.Outlet, DB: deps.DB, Orders: deps.FulfilmentOrders, Adapter: a, OrganizationParam: "pharmacy", LocationParam: "outlet"}).Register(r, "/hospitals/:hospital/departments/:department/pharmacies/:pharmacy/outlets/:outlet/supplier-orders/:order/allocations/:allocation")
}
func (h *outletFulfilment) Admit(ctx context.Context, p authn.Principal, s f.SourceScope, action func(context.Context, supplierorder.Actor) error) error {
	if h.db == nil || h.operating == nil || s.TenantID.String() != p.TenantID || s.Product != "pharmacy" {
		return f.ErrUnavailable
	}
	if s.HospitalID != uuid.Nil || s.DepartmentID != uuid.Nil {
		if s.HospitalID == uuid.Nil || s.DepartmentID == uuid.Nil || h.hospital == nil {
			return f.ErrUnavailable
		}
		return h.hospital.WithOutlet(ctx, p, s.HospitalID, s.DepartmentID, s.LocationID, func(ctx context.Context, actor supplierorder.Actor) error {
			return h.hospitalFrame(ctx, p, s, actor, action)
		})
	}
	handler := catalogueHandlers{db: h.db, operating: h.operating}
	return handler.operatingStock(ctx, p, s.OrganizationID, s.LocationID, PermStockFulfil, func(tx *gorm.DB, u operatingStockUse) error {
		if u.grant.Identity.StaffID == uuid.Nil {
			return f.ErrUnavailable
		}
		if err := u.audit(tx, "supplier_fulfilment_access", "supplier_order", &s.OrderID); err != nil {
			return err
		}
		return action(tx.Statement.Context, supplierorder.Actor{Subject: p.Subject, StaffID: u.grant.Identity.StaffID})
	})
}

func (h *outletFulfilment) Lock(tx *gorm.DB, a f.Allocation) error {
	p := a.Plan
	if p.Input.StockLocationID == nil {
		return f.ErrInvalid
	}
	return stockLock(tx, p.OwnerTenantID.String()+":"+p.SourceLocationID.String()+":"+p.Input.StockLocationID.String()+":"+p.Input.StockID.String())
}
func (h *outletFulfilment) Snapshot(tx *gorm.DB, p f.AllocationPlan) (f.StockEvidence, error) {
	if err := h.Lock(tx, f.Allocation{Plan: p}); err != nil {
		return f.StockEvidence{}, err
	}
	var product catalogueProduct
	var batch stockBatch
	if err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", p.OwnerTenantID, p.SourceLocationID, p.Product.ProductID).Take(&product).Error; err != nil {
		return f.StockEvidence{}, err
	}
	if err := tx.Where("tenant_id=? AND outlet_id=? AND id=?", p.OwnerTenantID, p.SourceLocationID, p.Input.StockID).Take(&batch).Error; err != nil {
		return f.StockEvidence{}, err
	}
	if p.Product.Owner != "pharmacy" || p.Product.Version == nil || product.Version != *p.Product.Version || product.Status != "active" || product.UnitOfIssue != p.UnitCode || batch.BatchFields.ProductID != product.ID {
		return f.StockEvidence{}, f.ErrConflict
	}
	today := time.Now().In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)
	var available int64
	if err := tx.Raw(`SELECT COALESCE(SUM(on_hand),0) FROM pharmacy_sellable_buckets(?,?,?,?::date) WHERE location_id=? AND batch_id=?`, p.OwnerTenantID, p.SourceLocationID, p.Product.ProductID, today, *p.Input.StockLocationID, p.Input.StockID).Scan(&available).Error; err != nil {
		return f.StockEvidence{}, err
	}
	ingredients, err := json.Marshal(product.Ingredients)
	if err != nil {
		return f.StockEvidence{}, err
	}
	description := fmt.Sprintf("%s | %s | %s | %s | %s | %s | pack %d %s", product.BrandName, product.GenericName, product.Manufacturer, product.DosageForm, product.Route, string(ingredients), product.PackSize, product.UnitOfIssue)
	return f.StockEvidence{LotNumber: batch.BatchFields.LotNumber, ExpiresOn: batch.BatchFields.ExpiresOn, AvailableUnits: available, Description: description, PackSize: int64(product.PackSize)}, nil
}
func (h *outletFulfilment) Apply(tx *gorm.DB, r f.AllocationCommandReceipt) error {
	if h.bus == nil {
		return f.ErrUnavailable
	}
	if r.Input.Decision == "release" {
		return nil
	}
	p := r.Allocation.Plan
	evidence, err := h.Snapshot(tx, p)
	if err != nil {
		return err
	}
	if evidence.AvailableUnits < r.Input.QuantityUnits || evidence.LotNumber != r.Allocation.LotNumber || evidence.ExpiresOn != r.Allocation.ExpiresOn {
		return f.ErrConflict
	}
	var batch stockBatch
	if err = tx.Where("tenant_id=? AND outlet_id=? AND id=?", p.OwnerTenantID, p.SourceLocationID, p.Input.StockID).Take(&batch).Error; err != nil {
		return err
	}
	if err = tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at)VALUES(?,?,?,?,?,'supplier_dispatch',?,?,?,?,?)`, p.OwnerTenantID, p.SourceLocationID, r.CommandID, *p.Input.StockLocationID, p.Input.StockID, -r.Input.QuantityUnits, r.Input.Reason, "supplier-dispatch:"+r.CommandID.String(), r.ActorSubject, r.RecordedAt).Error; err != nil {
		return err
	}
	shipment, err := f.ShipmentFor(r, &batch.BatchFields.MRPMinor)
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
	return events.Enqueue(tx, pharmacycontract.SubjectSupplierDispatchRecorded, events.Event{ID: id.String(), Type: pharmacycontract.SubjectSupplierDispatchRecorded, Version: 1, OccurredAt: time.Now().UTC(), TenantID: p.OwnerTenantID.String(), Data: raw})
}

func (h *outletFulfilment) hospitalFrame(ctx context.Context, p authn.Principal, s f.SourceScope, a supplierorder.Actor, action func(context.Context, supplierorder.Actor) error) error {
	return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
		return pharmacyStockFrame(tx, p.Subject, s.OrganizationID, s.LocationID, PermStockFulfil, func() (err error) {
			var old string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_fulfilment_hospital','app.pharmacy_fulfilment_department'])AS key`).Scan(&old).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, old).Error)
			}()
			if h.departments == nil {
				return f.ErrUnavailable
			}
			if err = tx.Exec(`SELECT set_config('app.pharmacy_fulfilment_hospital',?,true),set_config('app.pharmacy_fulfilment_department',?,true)`, s.HospitalID.String(), s.DepartmentID.String()).Error; err != nil {
				return err
			}
			if err = tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":pharmacy-staff-bindings:"+s.LocationID.String()).Error; err != nil {
				return err
			}
			var binding struct{ DepartmentRevisionID uuid.UUID }
			if err = tx.Raw(`SELECT b.department_revision_id FROM pharmacy_staff_department_bindings b JOIN pharmacy_outlets o ON(o.tenant_id,o.id)=(b.tenant_id,b.outlet_id) JOIN pharmacy_organizations p ON(p.tenant_id,p.id)=(o.tenant_id,o.pharmacy_id) WHERE b.tenant_id=? AND b.outlet_id=? AND b.hospital_id=? AND b.department_id=? AND b.status='approved' AND o.pharmacy_id=? AND p.ownership_type='hospital_owned' FOR SHARE OF b,o,p`, p.TenantID, s.LocationID, s.HospitalID, s.DepartmentID, s.OrganizationID).Take(&binding).Error; err != nil {
				return err
			}
			return h.departments.WithCurrentDepartment(ctx, authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.HospitalID, DepartmentID: s.DepartmentID, DepartmentRevision: binding.DepartmentRevisionID}, func(current context.Context) error { return action(current, a) })
		})
	})
}
