package supply

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type purchaseOrderHandlers struct {
	db            *tenantdb.DB
	bus           *events.Bus
	authority     authz.ScopedAuthorizer
	actors        authz.CurrentPurchaseOrderActorGuard
	departments   authz.DepartmentScopeGuard
	operating     authz.OperatingScopeAuthorizer
	relationships suppliertrade.CurrentRelationshipGuard
	endpoints     suppliertrade.CurrentEndpointGuard
	products      supplierorder.CurrentOrderProducts
}
type buyerOrderScope struct{ hospital, department, outlet, order, command uuid.UUID }
type sellerOrderScope struct {
	product                                string
	organization, location, order, command uuid.UUID
	hospital, department                   uuid.UUID
}

var orderFrameKeys = []string{"app.actor_subject", "app.supply_order_mode", "app.supply_order_id", "app.supply_order_hospital", "app.supply_order_department", "app.supply_order_outlet", "app.supply_order_product", "app.supply_order_organization", "app.supply_order_location", "app.supply_order_actor_staff", "app.supply_order_origin"}

func purchaseOrderFrame(tx *gorm.DB, values map[string]string, action func() error) (err error) {
	raw, err := json.Marshal(orderFrameKeys)
	if err != nil {
		return err
	}
	var previous string
	if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(raw)).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
	}()
	settings, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, string(settings)).Error; err != nil {
		return err
	}
	return action()
}
func (h *purchaseOrderHandlers) buyer(ctx context.Context, p authn.Principal, scope buyerOrderScope, permission authz.Permission, action func(context.Context, *gorm.DB, supplierorder.Actor) error, extra ...authz.Permission) error {
	guard, ok := h.authority.(authz.CompoundScopedCommandGuard)
	if !ok || h.db == nil || h.actors == nil || h.departments == nil || action == nil {
		return supplierorder.ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(ctx, p.Subject), p.IdleDeadline)
	defer cancel()
	path, err := h.departments.CurrentPath(ctx, p.TenantID, scope.hospital, scope.department)
	if err != nil {
		return err
	}
	if len(path) == 0 || len(path) > authz.MaxDepartmentPath || path[0] != scope.department {
		return authz.ErrScopedAuthorizationUnavailable
	}
	pathStrings := make([]string, len(path))
	for i, id := range path {
		pathStrings[i] = id.String()
	}
	checks := make([]authz.ScopeCheck, 0, 2)
	for _, perm := range append([]authz.Permission{permission, authz.PurchaseOrderActorUse}, extra...) {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital.String(), DepartmentPath: pathStrings, Permission: perm}
		grant, err := h.authority.AuthorizeScope(ctx, request)
		if err != nil {
			return err
		}
		if grant.AssignmentID == "" {
			return authz.ErrScopedCommandDenied
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(current context.Context) error {
		return h.departments.WithinCurrentDepartment(current, p.TenantID, scope.hospital, scope.department, scope.department, func(current context.Context) error {
			return h.actors.WithCurrentPurchaseOrderActor(current, authz.StaffActorScope{TenantID: p.TenantID, HospitalID: scope.hospital, Subject: p.Subject, DepartmentPath: pathStrings}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
				if actor.StaffID == uuid.Nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				mode := "buyer_read"
				if permission == PermPurchaseOrderPrepare || permission == PermPurchaseOrderReview || permission == PermPurchaseOrderCancel {
					mode = "buyer_write"
				}
				return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
					return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": mode, "app.supply_order_hospital": scope.hospital.String(), "app.supply_order_department": scope.department.String(), "app.supply_order_outlet": scope.outlet.String(), "app.supply_order_actor_staff": actor.StaffID.String()}, func() error {
						return action(actorCtx, tx, supplierorder.Actor{Subject: p.Subject, StaffID: actor.StaffID})
					})
				})
			})
		})
	})
}
func (h *purchaseOrderHandlers) seller(ctx context.Context, p authn.Principal, scope sellerOrderScope, permission authz.Permission, action func(context.Context, *gorm.DB, supplierorder.Actor) error) error {
	if scope.hospital != uuid.Nil {
		return h.hospitalSeller(ctx, p, scope, permission, action)
	}
	if h.db == nil || h.operating == nil || action == nil {
		return supplierorder.ErrUnavailable
	}
	kind := "supplier"
	if scope.product == "pharmacy" {
		kind = "pharmacy"
	} else if scope.product != "supply" {
		return supplierorder.ErrInvalid
	}
	expected := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: kind, EmployerID: scope.organization, LocationID: scope.location}
	return h.operating.WithOperatingScope(tenantdb.WithActor(ctx, p.Subject), authz.OperatingScopeRequest{Principal: p, EmployerKind: kind, EmployerID: scope.organization, LocationID: scope.location, Permission: permission}, func(current context.Context, grant authz.OperatingScopeGrant) error {
		if grant.Scope != expected || grant.Permission != permission || grant.Identity.StaffID == uuid.Nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		mode := "seller_write"
		if permission == PermSupplierOrderRead {
			mode = "seller_read"
		}
		return h.db.WithTenantCommand(current, p.TenantID, func(tx *gorm.DB) error {
			return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": mode, "app.supply_order_product": scope.product, "app.supply_order_organization": scope.organization.String(), "app.supply_order_location": scope.location.String(), "app.supply_order_actor_staff": grant.Identity.StaffID.String()}, func() error {
				return action(current, tx, supplierorder.Actor{Subject: p.Subject, StaffID: grant.Identity.StaffID})
			})
		})
	})
}
func (h *purchaseOrderHandlers) lookupSeller(ctx context.Context, p authn.Principal, id uuid.UUID) (sellerOrderScope, error) {
	var result sellerOrderScope
	if h.db == nil {
		return result, supplierorder.ErrUnavailable
	}
	err := h.db.WithTenant(tenantdb.WithActor(ctx, p.Subject), p.TenantID, func(tx *gorm.DB) error {
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "lookup", "app.supply_order_id": id.String()}, func() error {
			var row struct {
				Side, SourceProduct                    string
				SourceOrganizationID, SourceLocationID uuid.UUID
			}
			if err := tx.Table("supply_purchase_orders").Select("side,source_product,source_organization_id,source_location_id").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; err != nil {
				return err
			}
			if row.Side != "seller" {
				return gorm.ErrRecordNotFound
			}
			result = sellerOrderScope{product: row.SourceProduct, organization: row.SourceOrganizationID, location: row.SourceLocationID, order: id}
			return nil
		})
	})
	return result, err
}
func (h *purchaseOrderHandlers) verifiedRelationship(ctx context.Context, p authn.Principal, id uuid.UUID, hash string) (suppliertrade.RelationshipAuthorization, error) {
	if h.relationships == nil {
		return nil, supplierorder.ErrUnavailable
	}
	return h.relationships.VerifyRelationship(ctx, suppliertrade.RelationshipScope{TenantID: p.TenantID, ID: id, TermsSha256: hash, Purpose: "new_business"})
}
func (h *purchaseOrderHandlers) withBuyerEndpoint(ctx context.Context, order supplierorder.SupplierOrder, scope buyerOrderScope, action func(context.Context) error) error {
	owner, ok := h.endpoints.(suppliertrade.HospitalDepartmentGuard)
	if !ok {
		return supplierorder.ErrUnavailable
	}
	if order.Terms.Buyer.HospitalID == nil || *order.Terms.Buyer.HospitalID != scope.hospital || order.Terms.Buyer.LocationID != scope.outlet {
		return supplierorder.ErrIneligible
	}
	return owner.WithHospitalDepartment(ctx, order.Terms.Buyer, scope.department, action)
}
func purchaseOrderNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
