package supply

import (
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

//go:embed orders.sql
var purchaseOrdersSchema string

type localPurchaseOrder struct {
	Order                          supplierorder.SupplierOrder
	DepartmentID                   *uuid.UUID
	ProposalInput, ProposalReceipt json.RawMessage
}

func purchaseOrderLock(tx *gorm.DB, tenant string, id uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, tenant+":purchase-order:"+id.String()).Error
}
func loadPurchaseOrder(tx *gorm.DB, tenant string, id uuid.UUID) (localPurchaseOrder, error) {
	var row struct {
		Body                           json.RawMessage
		DepartmentID                   *uuid.UUID
		ProposalInput, ProposalReceipt json.RawMessage
	}
	var result localPurchaseOrder
	if err := tx.Table("supply_purchase_orders").Select("body,department_id,proposal_input,proposal_receipt").Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error; err != nil {
		return result, err
	}
	if suppliertrade.DecodeObject(row.Body, &result.Order) != nil || supplierorder.ValidateRecord(result.Order) != nil || result.Order.OwnerTenantID.String() != tenant || result.Order.ID != id {
		return result, supplierorder.ErrUnavailable
	}
	result.DepartmentID, result.ProposalInput, result.ProposalReceipt = row.DepartmentID, row.ProposalInput, row.ProposalReceipt
	return result, nil
}
func savePurchaseOrder(tx *gorm.DB, local localPurchaseOrder, actor string, staff *uuid.UUID, event *uuid.UUID) error {
	order := local.Order
	if supplierorder.ValidateRecord(order) != nil || order.Terms.Buyer.HospitalID == nil {
		return supplierorder.ErrInvalid
	}
	body, err := json.Marshal(order)
	if err != nil {
		return err
	}
	if order.Version == 1 {
		err = tx.Exec(`INSERT INTO supply_purchase_orders(tenant_id,id,origin_tenant_id,origin_record_id,side,hospital_id,buyer_outlet_id,department_id,source_product,source_organization_id,source_location_id,relationship_id,version,body,proposal_input,proposal_receipt,actor_subject,actor_staff_id,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,?::jsonb,?,?,?,?)`, order.OwnerTenantID, order.ID, order.Terms.Buyer.TenantID, order.ID, order.Side, *order.Terms.Buyer.HospitalID, order.Terms.Buyer.LocationID, local.DepartmentID, order.Terms.Seller.Product, order.Terms.Seller.OrganizationID, order.Terms.Seller.LocationID, order.Input.RelationshipID, order.Version, string(body), jsonArgument(local.ProposalInput), jsonArgument(local.ProposalReceipt), actor, staff, order.CreatedAt, order.UpdatedAt).Error
	} else {
		result := tx.Exec(`UPDATE supply_purchase_orders SET body=?::jsonb,version=?,actor_subject=?,actor_staff_id=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, string(body), order.Version, actor, staff, order.UpdatedAt, order.OwnerTenantID, order.ID, order.Version-1)
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			return supplierorder.ErrConflict
		}
	}
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO supply_purchase_order_history(tenant_id,origin_tenant_id,origin_record_id,order_id,version,body,actor_subject,source_event_id,created_at)VALUES(?,?,?,?,?,?::jsonb,?,?,?)`, order.OwnerTenantID, order.Terms.Buyer.TenantID, order.ID, order.ID, order.Version, string(body), actor, event, order.UpdatedAt).Error
}
func jsonArgument(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
func readPurchaseOrderCommand(tx *gorm.DB, tenant string, order, id uuid.UUID) (supplierorder.SupplierOrderCommandReceipt, error) {
	var row struct{ Receipt json.RawMessage }
	var result supplierorder.SupplierOrderCommandReceipt
	if err := tx.Table("supply_purchase_order_commands").Select("receipt").Where("tenant_id=? AND order_id=? AND id=?", tenant, order, id).Take(&row).Error; err != nil {
		return result, err
	}
	if suppliertrade.DecodeObject(row.Receipt, &result) != nil || result.CommandID != id || result.Order.ID != order || result.Order.OwnerTenantID.String() != tenant || supplierorder.ValidateRecord(result.Order) != nil {
		return result, supplierorder.ErrUnavailable
	}
	return result, nil
}
func savePurchaseOrderCommand(tx *gorm.DB, receipt supplierorder.SupplierOrderCommandReceipt) error {
	input, err := json.Marshal(receipt.Input)
	if err != nil {
		return err
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO supply_purchase_order_commands(tenant_id,id,order_id,actor_subject,actor_staff_id,input,receipt,created_at)VALUES(?,?,?,?,?,?::jsonb,?::jsonb,?)`, receipt.Order.OwnerTenantID, receipt.CommandID, receipt.Order.ID, receipt.ActorSubject, receipt.ActorStaffID, string(input), string(body), receipt.RecordedAt).Error
}
func recoverPurchaseOrderCommand(tx *gorm.DB, tenant string, order, id uuid.UUID, actor supplierorder.Actor, input *supplierorder.SupplierOrderDecisionInput) (supplierorder.SupplierOrderCommandReceipt, error) {
	saved, err := readPurchaseOrderCommand(tx, tenant, order, id)
	if err != nil {
		return saved, err
	}
	if saved.ActorSubject != actor.Subject || saved.ActorStaffID != actor.StaffID || (input != nil && saved.Input != *input) {
		return saved, supplierorder.ErrConflict
	}
	return saved, tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'purchase_order_command_recovered','supply_purchase_order_commands',?)`, tenant, actor.Subject, id).Error
}
func purchaseOrderMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
