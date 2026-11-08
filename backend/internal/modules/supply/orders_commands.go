package supply

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
	"time"
)

func samePurchaseOrderProposal(local localPurchaseOrder, input supplierorder.SupplierOrderInput, actor supplierorder.Actor) (supplierorder.SupplierOrder, error) {
	var original supplierorder.SupplierOrderInput
	var receipt supplierorder.SupplierOrder
	if supplierorder.DecodeObject(local.ProposalInput, &original) != nil || supplierorder.DecodeObject(local.ProposalReceipt, &receipt) != nil {
		return receipt, supplierorder.ErrUnavailable
	}
	left, _ := json.Marshal(original)
	right, _ := json.Marshal(input)
	if string(left) != string(right) || receipt.PreparedBy != actor.Subject || receipt.PreparedStaffID == nil || *receipt.PreparedStaffID != actor.StaffID {
		return receipt, supplierorder.ErrConflict
	}
	return receipt, nil
}
func (h *purchaseOrderHandlers) prepareBuyer(c *gin.Context) {
	p, scope, ok := buyerPurchaseOrderRequest(c)
	if !ok {
		return
	}
	var input supplierorder.SupplierOrderInput
	if !decodePurchaseOrder(c, &input) {
		return
	}
	input.ExpiresAt = input.ExpiresAt.UTC().Truncate(time.Microsecond)
	var saved supplierorder.SupplierOrder
	recovered := false
	recoverProposal := func(tx *gorm.DB, actor supplierorder.Actor) error {
		local, err := loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		saved, err = samePurchaseOrderProposal(local, input, actor)
		if err != nil {
			return err
		}
		recovered = true
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'purchase_order_proposal_recovered','supply_purchase_orders',?)`, p.TenantID, p.Subject, scope.order).Error
	}
	err := h.buyer(c.Request.Context(), p, scope, PermPurchaseOrderPrepare, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		err := recoverProposal(tx, actor)
		if purchaseOrderMissing(err) {
			return nil
		}
		return err
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
		return
	}
	proof, err := h.verifiedRelationship(c.Request.Context(), p, input.RelationshipID, input.TermsSha256)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	snapshot := proof.Snapshot()
	if snapshot.Side != "buyer" || snapshot.OwnerTenantID.String() != p.TenantID {
		purchaseOrderError(c, supplierorder.ErrIneligible)
		return
	}
	err = h.buyer(c.Request.Context(), p, scope, PermPurchaseOrderPrepare, func(ctx context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		if err := purchaseOrderLock(tx, p.TenantID, scope.order); err != nil {
			return err
		}
		if err := recoverProposal(tx, actor); err == nil {
			return nil
		} else if !purchaseOrderMissing(err) {
			return err
		}
		return h.relationships.WithVerifiedRelationship(ctx, proof, func(current context.Context, relationship suppliertrade.Relationship) error {
			var err error
			saved, err = supplierorder.Prepare(scope.order, relationship, input, actor, purchaseOrderNow())
			if err != nil {
				return err
			}
			return h.withBuyerEndpoint(current, saved, scope, func(context.Context) error {
				saved.BuyerDepartmentID = &scope.department
				normalized, err := json.Marshal(saved.Input)
				if err != nil {
					return err
				}
				receipt, err := json.Marshal(saved)
				if err != nil {
					return err
				}
				return savePurchaseOrder(tx, localPurchaseOrder{Order: saved, DepartmentID: &scope.department, ProposalInput: normalized, ProposalReceipt: receipt}, actor.Subject, &actor.StaffID, nil)
			})
		})
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
	} else {
		respond.Created(c, saved)
	}
}
func validPurchaseOrderDecision(input supplierorder.SupplierOrderDecisionInput) bool {
	return input.ExpectedVersion > 0 && input.ExpectedVersion <= 9007199254740991 && (input.Decision == "approve" || input.Decision == "reject" || input.Decision == "cancel") && suppliertrade.Text(input.Reason, 500)
}
func (h *purchaseOrderHandlers) decideBuyer(c *gin.Context) {
	p, scope, ok := buyerPurchaseOrderRequest(c)
	if !ok {
		return
	}
	var input supplierorder.SupplierOrderDecisionInput
	if !decodePurchaseOrder(c, &input) {
		return
	}
	if !validPurchaseOrderDecision(input) {
		purchaseOrderError(c, supplierorder.ErrInvalid)
		return
	}
	permission := PermPurchaseOrderReview
	if input.Decision == "cancel" {
		permission = PermPurchaseOrderCancel
	}
	var local localPurchaseOrder
	var saved supplierorder.SupplierOrderCommandReceipt
	recovered := false
	err := h.buyer(c.Request.Context(), p, scope, permission, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		var err error
		local, err = loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		saved, err = recoverPurchaseOrderCommand(tx, p.TenantID, scope.order, scope.command, actor, &input)
		if err == nil {
			recovered = true
			return nil
		}
		if purchaseOrderMissing(err) {
			return nil
		}
		return err
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
		return
	}
	var proof suppliertrade.RelationshipAuthorization
	if input.Decision == "approve" {
		proof, err = h.verifiedRelationship(c.Request.Context(), p, local.Order.Input.RelationshipID, local.Order.Input.TermsSha256)
		if err != nil {
			purchaseOrderError(c, err)
			return
		}
	}
	err = h.buyer(c.Request.Context(), p, scope, permission, func(ctx context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		if err := purchaseOrderLock(tx, p.TenantID, scope.order); err != nil {
			return err
		}
		receipt, err := recoverPurchaseOrderCommand(tx, p.TenantID, scope.order, scope.command, actor, &input)
		if err == nil {
			saved, recovered = receipt, true
			return nil
		}
		if !purchaseOrderMissing(err) {
			return err
		}
		local, err = loadPurchaseOrder(tx, p.TenantID, scope.order)
		if err != nil {
			return err
		}
		apply := func(current context.Context) error {
			next, event, err := supplierorder.Decide(local.Order, input, actor, purchaseOrderNow())
			if err != nil {
				return err
			}
			local.Order = next
			if err = savePurchaseOrder(tx, local, actor.Subject, &actor.StaffID, nil); err != nil {
				return err
			}
			saved = supplierorder.SupplierOrderCommandReceipt{CommandID: scope.command, Input: input, Order: next, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, RecordedAt: purchaseOrderNow()}
			if err = savePurchaseOrderCommand(tx, saved); err != nil {
				return err
			}
			if event != nil {
				return h.publishOrderExchange(tx, *event)
			}
			return nil
		}
		if proof != nil {
			return h.relationships.WithVerifiedRelationship(ctx, proof, func(current context.Context, r suppliertrade.Relationship) error {
				if r.ID != local.Order.Input.RelationshipID || r.TermsSha256 != local.Order.Input.TermsSha256 || r.Side != "buyer" {
					return supplierorder.ErrIneligible
				}
				return h.withBuyerEndpoint(current, local.Order, scope, apply)
			})
		}
		return apply(ctx)
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
	} else {
		respond.Created(c, saved)
	}
}
func (h *purchaseOrderHandlers) decideSeller(c *gin.Context) {
	p, ok := purchaseOrderPrincipal(c)
	if !ok {
		return
	}
	id, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return
	}
	command, ok := purchaseOrderID(c, "command", false)
	if !ok {
		return
	}
	var input supplierorder.SupplierOrderDecisionInput
	if !decodePurchaseOrder(c, &input) {
		return
	}
	if !validPurchaseOrderDecision(input) || input.Decision == "cancel" {
		purchaseOrderError(c, supplierorder.ErrInvalid)
		return
	}
	scope, err := h.lookupSeller(c.Request.Context(), p, id)
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	var local localPurchaseOrder
	var saved supplierorder.SupplierOrderCommandReceipt
	recovered := false
	err = h.seller(c.Request.Context(), p, scope, PermSupplierOrderAccept, func(_ context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		var err error
		local, err = loadPurchaseOrder(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		saved, err = recoverPurchaseOrderCommand(tx, p.TenantID, id, command, actor, &input)
		if err == nil {
			recovered = true
			return nil
		}
		if purchaseOrderMissing(err) {
			return nil
		}
		return err
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
		return
	}
	var proof suppliertrade.RelationshipAuthorization
	if input.Decision == "approve" {
		proof, err = h.verifiedRelationship(c.Request.Context(), p, local.Order.Input.RelationshipID, local.Order.Input.TermsSha256)
		if err != nil {
			purchaseOrderError(c, err)
			return
		}
		if h.products == nil {
			purchaseOrderError(c, supplierorder.ErrUnavailable)
			return
		}
	}
	err = h.seller(c.Request.Context(), p, scope, PermSupplierOrderAccept, func(ctx context.Context, tx *gorm.DB, actor supplierorder.Actor) error {
		if err := purchaseOrderLock(tx, p.TenantID, id); err != nil {
			return err
		}
		receipt, err := recoverPurchaseOrderCommand(tx, p.TenantID, id, command, actor, &input)
		if err == nil {
			saved, recovered = receipt, true
			return nil
		}
		if !purchaseOrderMissing(err) {
			return err
		}
		local, err = loadPurchaseOrder(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		apply := func(context.Context) error {
			next, event, err := supplierorder.Decide(local.Order, input, actor, purchaseOrderNow())
			if err != nil {
				return err
			}
			local.Order = next
			if err = savePurchaseOrder(tx, local, actor.Subject, &actor.StaffID, nil); err != nil {
				return err
			}
			saved = supplierorder.SupplierOrderCommandReceipt{CommandID: command, Input: input, Order: next, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, RecordedAt: purchaseOrderNow()}
			if err = savePurchaseOrderCommand(tx, saved); err != nil {
				return err
			}
			if event == nil {
				return errors.New("supplier decision exchange unavailable")
			}
			return h.publishOrderExchange(tx, *event)
		}
		if proof != nil {
			return h.relationships.WithVerifiedRelationship(ctx, proof, func(current context.Context, r suppliertrade.Relationship) error {
				if r.Side != "seller" || r.ID != local.Order.Input.RelationshipID || r.TermsSha256 != local.Order.Input.TermsSha256 {
					return supplierorder.ErrIneligible
				}
				return h.products.WithCurrentOrderProducts(current, r.Terms.Seller, local.Order.Input.Lines, apply)
			})
		}
		return apply(ctx)
	})
	if err != nil {
		purchaseOrderError(c, err)
		return
	}
	if recovered {
		respond.OK(c, saved)
	} else {
		respond.Created(c, saved)
	}
}
