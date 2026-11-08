package supplierstock

import (
	"encoding/json"
	"github.com/google/uuid"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

// Cancel is called only inside the source order owner's cancellation publication fence.
func (s Store) Cancel(tx *gorm.DB, o supplierorder.SupplierOrder) error {
	if s.validate() != nil || supplierorder.ValidateRecord(o) != nil || o.Side != "seller" || o.Status != "cancellation_requested" || o.Terms.Seller.Product != string(s.Owner) {
		return f.ErrInvalid
	}
	var rows []struct{ ID uuid.UUID }
	if err := tx.Table(s.Owner.table("allocations")).Select("id").Where("tenant_id=? AND order_id=? AND reserved_units>0", o.OwnerTenantID, o.ID).Order("id").Limit(100001).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 100000 {
		return f.ErrUnavailable
	}
	for _, row := range rows {
		if err := s.AllocationLock(tx, o.OwnerTenantID, row.ID); err != nil {
			return err
		}
	}
	for _, row := range rows {
		a, err := s.Current(tx, o.OwnerTenantID, row.ID)
		if err != nil {
			return err
		}
		if a.ReservedUnits == 0 {
			continue
		}
		if err = lock(tx, o.OwnerTenantID.String()+":"+string(s.Owner)+":supplier-order-line:"+o.ID.String()+":"+a.Plan.Input.LineID.String()); err != nil {
			return err
		}
		a.ReleasedUnits += a.ReservedUnits
		a.ReservedUnits = 0
		a.Status = "released"
		if a.DispatchedUnits > 0 {
			a.Status = "dispatched"
		}
		a.Version++
		a.UpdatedAt, err = Clock(tx)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(a)
		if err != nil {
			return err
		}
		result := tx.Table(s.Owner.table("allocations")).Where("tenant_id=? AND id=? AND version=?", o.OwnerTenantID, row.ID, a.Version-1).Updates(map[string]any{"version": a.Version, "reserved_units": 0, "released_units": a.ReleasedUnits, "body": string(raw), "actor_subject": "system:supplier-fulfilment", "actor_staff_id": nil, "updated_at": a.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return f.ErrConflict
		}
		if err = s.history(tx, a, "order_cancelled", "system:supplier-fulfilment", nil); err != nil {
			return err
		}
	}
	return nil
}
