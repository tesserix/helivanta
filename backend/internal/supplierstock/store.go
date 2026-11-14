// Package supplierstock implements the same allocation evidence rules for two stock owners.
// Each module supplies its own namespace, scoped transaction and physical-stock callbacks.
package supplierstock

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"reflect"
	"time"
)

type Owner string

const (
	Warehouse Owner = "supply"
	Outlet    Owner = "pharmacy"
)

func (o Owner) valid() bool                { return o == Warehouse || o == Outlet }
func (o Owner) table(suffix string) string { return string(o) + "_supplier_" + suffix }

// Store has no connection or authority of its own. Calls require the module's scoped owner transaction.
type Store struct{ Owner Owner }

func (s Store) validate() error {
	if !s.Owner.valid() {
		return f.ErrInvalid
	}
	return nil
}
func lock(tx *gorm.DB, key string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, key).Error
}
func Clock(tx *gorm.DB) (time.Time, error) {
	var v time.Time
	err := tx.Raw(`SELECT clock_timestamp()`).Scan(&v).Error
	return v.UTC().Truncate(time.Microsecond), err
}
func (s Store) AllocationLock(tx *gorm.DB, tenant, id uuid.UUID) error {
	if s.validate() != nil {
		return f.ErrInvalid
	}
	return lock(tx, tenant.String()+":"+string(s.Owner)+":supplier-allocation:"+id.String())
}
func (s Store) CommandLock(tx *gorm.DB, tenant, id uuid.UUID) error {
	if s.validate() != nil {
		return f.ErrInvalid
	}
	return lock(tx, tenant.String()+":"+string(s.Owner)+":supplier-command:"+id.String())
}

type allocationRow struct {
	Body, Original json.RawMessage
	IntentHash     string
}

func (s Store) row(tx *gorm.DB, tenant, id uuid.UUID) (allocationRow, error) {
	var r allocationRow
	if s.validate() != nil {
		return r, f.ErrInvalid
	}
	err := tx.Table(s.Owner.table("allocations")).Select("body,original,intent_hash").Where("tenant_id=? AND id=?", tenant, id).Take(&r).Error
	return r, err
}
func (s Store) Current(tx *gorm.DB, tenant, id uuid.UUID) (f.Allocation, error) {
	row, err := s.row(tx, tenant, id)
	if err != nil {
		return f.Allocation{}, err
	}
	var a f.Allocation
	if json.Unmarshal(row.Body, &a) != nil || f.ValidateAllocation(a) != nil || a.Plan.OwnerTenantID != tenant || a.Plan.ID != id || a.Plan.Product.Owner != string(s.Owner) {
		return a, f.ErrUnavailable
	}
	return a, nil
}
func (s Store) Original(tx *gorm.DB, tenant, id uuid.UUID, in f.ReserveInput, actor supplierorder.Actor) (f.Allocation, error) {
	row, err := s.row(tx, tenant, id)
	if err != nil {
		return f.Allocation{}, err
	}
	var a f.Allocation
	if json.Unmarshal(row.Original, &a) != nil || f.ValidateAllocation(a) != nil || a.Version != 1 || a.Plan.ID != id || a.Plan.OwnerTenantID != tenant || a.Plan.Product.Owner != string(s.Owner) {
		return a, f.ErrUnavailable
	}
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	hash, err := f.IntentHash(actor, in)
	if err != nil || hash != row.IntentHash || a.Plan.ActorSubject != actor.Subject || a.Plan.ActorStaffID != actor.StaffID {
		return a, f.ErrConflict
	}
	return a, nil
}

// Create serializes the line budget before invoking the module's stock-bucket lock/read.
func (s Store) Create(tx *gorm.DB, id uuid.UUID, order supplierorder.SupplierOrder, in f.ReserveInput, actor supplierorder.Actor, stock func(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error)) (f.Allocation, bool, error) {
	var empty f.Allocation
	if s.validate() != nil || order.Terms.Seller.Product != string(s.Owner) || stock == nil {
		return empty, false, f.ErrInvalid
	}
	if err := s.AllocationLock(tx, order.OwnerTenantID, id); err != nil {
		return empty, false, err
	}
	prior, err := s.Original(tx, order.OwnerTenantID, id, in, actor)
	if err == nil {
		return prior, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return empty, false, err
	}
	if err = lock(tx, order.OwnerTenantID.String()+":"+string(s.Owner)+":supplier-order-line:"+order.ID.String()+":"+in.LineID.String()); err != nil {
		return empty, false, err
	}
	at, err := Clock(tx)
	if err != nil {
		return empty, false, err
	}
	provisional, err := f.Plan(id, order, in, actor, f.Budget{}, at)
	if err != nil {
		return empty, false, err
	}
	evidence, err := stock(tx, provisional)
	if err != nil {
		return empty, false, err
	}
	at, err = Clock(tx)
	if err != nil {
		return empty, false, err
	}
	var b struct{ Reserved, Dispatched, Records int64 }
	err = tx.Table(s.Owner.table("allocations")).Select("COALESCE(SUM(CASE WHEN expires_at>clock_timestamp() THEN reserved_units ELSE 0 END),0) AS reserved, COALESCE(SUM(dispatched_units),0) AS dispatched,COUNT(*) AS records").Where("tenant_id=? AND order_id=? AND line_id=?", order.OwnerTenantID, order.ID, in.LineID).Scan(&b).Error
	if err != nil {
		return empty, false, err
	}
	if b.Records >= 1000 {
		return empty, false, f.ErrConflict
	}
	plan, err := f.Plan(id, order, in, actor, f.Budget{ReservedUnits: b.Reserved, DispatchedUnits: b.Dispatched}, at)
	if err != nil {
		return empty, false, err
	}
	a, err := f.Reserve(plan, evidence, at)
	if err != nil {
		return empty, false, err
	}
	hash, err := f.IntentHash(actor, plan.Input)
	if err != nil {
		return empty, false, err
	}
	body, err := json.Marshal(a)
	if err != nil {
		return empty, false, err
	}
	location := uuid.Nil
	if in.StockLocationID != nil {
		location = *in.StockLocationID
	}
	sql := fmt.Sprintf(`INSERT INTO %s(tenant_id,id,organization_id,location_id,order_id,line_id,stock_id,stock_location_id,version,reserved_units,dispatched_units,released_units,expires_at,body,original,intent_hash,actor_subject,actor_staff_id,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,?,?,?,?,?)`, s.Owner.table("allocations"))
	if err = tx.Exec(sql, plan.OwnerTenantID, id, plan.SourceOrganizationID, plan.SourceLocationID, plan.OrderID, in.LineID, in.StockID, location, a.Version, a.ReservedUnits, a.DispatchedUnits, a.ReleasedUnits, plan.Input.ExpiresAt, string(body), string(body), hash, actor.Subject, actor.StaffID, at, at).Error; err != nil {
		return empty, false, err
	}
	return a, false, s.history(tx, a, "reserved", actor.Subject, nil)
}
func (s Store) update(tx *gorm.DB, a f.Allocation, actor supplierorder.Actor, command uuid.UUID) error {
	if f.ValidateAllocation(a) != nil {
		return f.ErrInvalid
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	result := tx.Table(s.Owner.table("allocations")).Where("tenant_id=? AND id=? AND version=?", a.Plan.OwnerTenantID, a.Plan.ID, a.Version-1).Updates(map[string]any{"version": a.Version, "reserved_units": a.ReservedUnits, "dispatched_units": a.DispatchedUnits, "released_units": a.ReleasedUnits, "body": string(raw), "actor_subject": actor.Subject, "actor_staff_id": actor.StaffID, "updated_at": a.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return f.ErrConflict
	}
	return s.history(tx, a, a.Status, actor.Subject, &command)
}
func (s Store) history(tx *gorm.DB, a f.Allocation, action, actor string, command *uuid.UUID) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return tx.Exec(fmt.Sprintf(`INSERT INTO %s(tenant_id,allocation_id,version,action,body,actor_subject,command_id,created_at)VALUES(?,?,?,?,?::jsonb,?,?,?)`, s.Owner.table("allocation_history")), a.Plan.OwnerTenantID, a.Plan.ID, a.Version, action, string(raw), actor, command, a.UpdatedAt).Error
}
func (s Store) Recover(tx *gorm.DB, tenant, allocation, command uuid.UUID, actor supplierorder.Actor, in *f.AllocationDecisionInput) (f.AllocationCommandReceipt, error) {
	var r f.AllocationCommandReceipt
	var row struct {
		Body       json.RawMessage
		IntentHash string
	}
	if s.validate() != nil {
		return r, f.ErrInvalid
	}
	err := tx.Table(s.Owner.table("commands")).Select("body,intent_hash").Where("tenant_id=? AND allocation_id=? AND id=?", tenant, allocation, command).Take(&row).Error
	if err != nil {
		return r, err
	}
	if json.Unmarshal(row.Body, &r) != nil || f.ValidateAllocation(r.Allocation) != nil || r.CommandID != command || r.Allocation.Plan.ID != allocation || r.Allocation.Plan.OwnerTenantID != tenant {
		return r, f.ErrUnavailable
	}
	if r.ActorSubject != actor.Subject || r.ActorStaffID != actor.StaffID {
		return r, f.ErrConflict
	}
	if in != nil {
		hash, err := f.IntentHash(actor, *in)
		if err != nil || hash != row.IntentHash {
			return r, f.ErrConflict
		}
	}
	return r, nil
}

// PhysicalEffect is supplied by the stock owner; Lock uses the same bucket key as sales/corrections.
type PhysicalEffect interface {
	Lock(*gorm.DB, f.Allocation) error
	Apply(*gorm.DB, f.AllocationCommandReceipt) error
}

// Command writes logical release/dispatch and the module's physical effect in one transaction.
func (s Store) Command(tx *gorm.DB, tenant, allocation, command uuid.UUID, in f.AllocationDecisionInput, actor supplierorder.Actor, physical PhysicalEffect) (f.AllocationCommandReceipt, bool, error) {
	var empty f.AllocationCommandReceipt
	if err := s.CommandLock(tx, tenant, command); err != nil {
		return empty, false, err
	}
	prior, err := s.Recover(tx, tenant, allocation, command, actor, &in)
	if err == nil {
		return prior, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return empty, false, err
	}
	if err = s.AllocationLock(tx, tenant, allocation); err != nil {
		return empty, false, err
	}
	a, err := s.Current(tx, tenant, allocation)
	if err != nil {
		return empty, false, err
	}
	if err = lock(tx, tenant.String()+":"+string(s.Owner)+":supplier-order-line:"+a.Plan.OrderID.String()+":"+a.Plan.Input.LineID.String()); err != nil {
		return empty, false, err
	}
	if physical == nil {
		return empty, false, f.ErrUnavailable
	}
	if err = physical.Lock(tx, a); err != nil {
		return empty, false, err
	}
	at, err := Clock(tx)
	if err != nil {
		return empty, false, err
	}
	next, err := f.Decide(a, in, actor, at)
	if err != nil {
		return empty, false, err
	}
	r := f.AllocationCommandReceipt{CommandID: command, Input: in, Allocation: next, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, RecordedAt: next.UpdatedAt}
	if err = s.update(tx, next, actor, command); err != nil {
		return empty, false, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return empty, false, err
	}
	hash, err := f.IntentHash(actor, in)
	if err != nil {
		return empty, false, err
	}
	err = tx.Exec(fmt.Sprintf(`INSERT INTO %s(tenant_id,id,allocation_id,intent_hash,body,actor_subject,actor_staff_id,created_at,updated_at)VALUES(?,?,?,?,?::jsonb,?,?,?,?)`, s.Owner.table("commands")), tenant, command, allocation, hash, string(raw), actor.Subject, actor.StaffID, at, at).Error
	if err != nil {
		return empty, false, err
	}
	if err = physical.Apply(tx, r); err != nil {
		return empty, false, err
	}
	return r, false, nil
}

// Held returns only current owner-unit commitments; physical stock roles see no partner record body.
func (s Store) Held(tx *gorm.DB, tenant, location, stock, bucket uuid.UUID) (int64, error) {
	if s.validate() != nil {
		return 0, f.ErrInvalid
	}
	var total int64
	err := tx.Table(s.Owner.table("holds")).Select("COALESCE(SUM(reserved_units),0)").Where("tenant_id=? AND location_id=? AND stock_id=? AND stock_location_id=? AND expires_at>clock_timestamp() AND reserved_units>0", tenant, location, stock, bucket).Scan(&total).Error
	return total, err
}

// VerifyShipment re-derives disclosure evidence from the stock owner's immutable dispatch command.
func (s Store) VerifyShipment(tx *gorm.DB, shipment f.Shipment) error {
	if s.validate() != nil || f.ValidateShipment(shipment) != nil || shipment.Product.Owner != string(s.Owner) {
		return f.ErrInvalid
	}
	var row struct{ Body json.RawMessage }
	if err := tx.Table(s.Owner.table("commands")).Select("body").Where("tenant_id=? AND allocation_id=? AND id=?", shipment.SourceTenantID, shipment.AllocationID, shipment.ID).Take(&row).Error; err != nil {
		return err
	}
	var receipt f.AllocationCommandReceipt
	if supplierorder.DecodeObject(row.Body, &receipt) != nil {
		return f.ErrUnavailable
	}
	derived, err := f.ShipmentFor(receipt, shipment.MrpMinor)
	if err != nil || !reflect.DeepEqual(derived, shipment) {
		return f.ErrUnavailable
	}
	return nil
}
