package supply

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type partnerShipmentHandlers struct {
	db            *tenantdb.DB
	authority     authz.PartnerScopeAuthorizer
	presentations suppliertrade.CurrentEndpointPresentationGuard
}
type partnerShipmentOrder struct {
	supplieraccess.PartnerShipmentOrder
}

func (r partnerShipmentOrder) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type partnerShipment struct{ supplieraccess.PartnerShipment }

func (r partnerShipment) PageKey() (time.Time, uuid.UUID) { return r.DispatchedAt, r.ID }
func partnerShipmentRoutes(r *platform.Router, deps platform.Deps) {
	presentations, _ := deps.TradingEndpoints.(suppliertrade.CurrentEndpointPresentationGuard)
	h := &partnerShipmentHandlers{db: deps.DB, authority: deps.PartnerScopes, presentations: presentations}
	root := "/hospitals/:hospital/partner-grants/:product/:grant/terms/:terms/shipment-orders"
	platform.ResourceListRoute(r, root, authz.Public, 50, h.orders)
	platform.ResourceListRoute(r, root+"/:order/shipments", authz.Public, 50, h.shipments)
}
func partnerShipmentRequest(c *gin.Context) (authn.Principal, authz.PartnerScopeRequest, bool) {
	p, ok := purchaseOrderPrincipal(c, true)
	request := authz.PartnerScopeRequest{Principal: p, Product: c.Param("product"), Permission: authz.PartnerShipmentRead}
	if !ok || !orderListQuery(c) {
		return p, request, false
	}
	hospital, ok := purchaseOrderID(c, "hospital", true)
	if !ok {
		return p, request, false
	}
	grant, ok := purchaseOrderID(c, "grant", false)
	if !ok {
		return p, request, false
	}
	if !partnerShipmentHash(c.Param("terms")) {
		respond.NotFound(c, "shared shipments")
		return p, request, false
	}
	if request.Product != "pharmacy" && request.Product != "supply" {
		respond.NotFound(c, "shared shipments")
		return p, request, false
	}
	request.HospitalID = hospital
	request.GrantID = grant
	return p, request, true
}
func partnerShipmentError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credential_revoked", "Sign in again before viewing shared shipments.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, suppliertrade.ErrIneligible):
		respond.NotFound(c, "shared shipments")
	default:
		respond.Error(c, 503, "partner_shipment_unavailable", "Current sharing authority or shipment evidence could not be verified. Previously loaded evidence must be hidden.")
	}
}
func partnerShipmentScope(g authz.PartnerScopeGrant) supplieraccess.SupplierGrantScope {
	return supplieraccess.SupplierGrantScope{SourceTenantID: uuid.MustParse(g.SourceTenantID), RecipientTenantID: uuid.MustParse(g.RecipientTenantID), HospitalID: g.HospitalID, SourceProduct: g.Product, SourceOrganizationID: g.SourceOrganizationID, SourceLocationID: g.SourceLocationID}
}
func partnerShipmentHash(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == 32 && hex.EncodeToString(digest) == value
}
func validPartnerShipmentGrant(request authz.PartnerScopeRequest, g authz.PartnerScopeGrant) bool {
	if !suppliertrade.ValidEndpointUse(g.SourceEndpoint, g.Category, "order") || g.SourceEndpoint.TenantID.String() != g.SourceTenantID || g.SourceEndpoint.Product != g.Product || g.SourceEndpoint.OrganizationID != g.SourceOrganizationID || g.SourceEndpoint.LocationID != g.SourceLocationID {
		return false
	}
	for _, hash := range []string{g.ProjectionHash, g.TermsSha256} {
		if !partnerShipmentHash(hash) {
			return false
		}
	}
	for _, id := range []uuid.UUID{g.GrantID, g.RelationshipID, g.SourceOrganizationID, g.SourceLocationID, g.BuyerOrganizationID, g.BuyerLocationID} {
		if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return false
		}
	}

	source, err := uuid.Parse(g.SourceTenantID)
	if err != nil || source == uuid.Nil || source.String() != g.SourceTenantID {
		return false
	}
	home, err := uuid.Parse(g.RecipientTenantID)
	return err == nil && home != uuid.Nil && home.String() == g.RecipientTenantID && g.SourceTenantID != g.RecipientTenantID && g.RecipientTenantID == request.Principal.TenantID && g.HospitalID == request.HospitalID && g.GrantID == request.GrantID && g.Product == request.Product && g.Permission == authz.PartnerShipmentRead && g.RelationshipID.Version() == 7 && g.SourceOrganizationID.Version() == 7 && g.SourceLocationID.Version() == 7 && g.BuyerOrganizationID.Version() == 7 && g.BuyerLocationID.Version() == 7 && len(g.ProjectionHash) == 64 && time.Now().Before(g.ValidUntil)
}
func (h *partnerShipmentHandlers) with(ctx context.Context, request authz.PartnerScopeRequest, terms string, action func(*gorm.DB, authz.PartnerScopeGrant, suppliertrade.EndpointPresentation) error) error {
	if h.db == nil || h.authority == nil || h.presentations == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.authority.WithPartnerScope(ctx, request, func(held context.Context, g authz.PartnerScopeGrant) error {
		deadline, bounded := held.Deadline()
		if !bounded || g.ValidUntil.After(deadline) || !h.db.GuardedCommandActive(held) || tenantdb.ActorFrom(held) != request.Principal.Subject || !validPartnerShipmentGrant(request, g) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if g.TermsSha256 != terms {
			return authz.ErrScopedCommandDenied
		}
		return h.presentations.WithCurrentTradingEndpointPresentation(held, g.SourceEndpoint, g.Category, "order", func(current context.Context, reference suppliertrade.EndpointReference, presentation suppliertrade.EndpointPresentation) error {
			if !h.db.GuardedCommandActive(current) || tenantdb.ActorFrom(current) != request.Principal.Subject || !suppliertrade.EndpointEqual(reference.Endpoint, g.SourceEndpoint) || reference.Category != g.Category || reference.Operation != "order" || !suppliertrade.ValidEndpointPresentation(presentation) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			deadline, bounded := current.Deadline()
			if !bounded {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if deadline.Before(g.ValidUntil) {
				g.ValidUntil = deadline
			}
			return h.db.WithTenantCommand(current, g.SourceTenantID, func(tx *gorm.DB) error {
				return partnerShipmentFrame(tx, g, request.Principal.Subject, func() error { return action(tx, g, presentation) })
			})
		})
	})
}
func partnerShipmentFrame(tx *gorm.DB, g authz.PartnerScopeGrant, actor string, action func() error) error {
	proof, err := json.Marshal(map[string]any{"scope": partnerShipmentScope(g), "grant_id": g.GrantID, "relationship_id": g.RelationshipID, "terms_sha256": g.TermsSha256, "buyer_organization_id": g.BuyerOrganizationID, "buyer_location_id": g.BuyerLocationID, "actor_subject": actor, "permission": string(g.Permission), "projection_hash": g.ProjectionHash, "valid_until": g.ValidUntil.UTC()})
	if err != nil {
		return err
	}
	return purchaseOrderFrame(tx, map[string]string{"app.supply_partner_shipments": string(proof), "app.supply_order_mode": ""}, func() error {
		var supported bool
		if err := tx.Raw("SELECT supply_partner_shipment_support()").Scan(&supported).Error; err != nil {
			return err
		}
		if !supported {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if err := action(); err != nil {
			return err
		}
		if !time.Now().Before(g.ValidUntil) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return nil
	})
}

func partnerShipmentOrderQuery(tx *gorm.DB, g authz.PartnerScopeGrant) *gorm.DB {
	return tx.Table("supply_purchase_orders").Where("tenant_id=? AND side='seller' AND origin_tenant_id=? AND hospital_id=? AND buyer_outlet_id=? AND relationship_id=? AND source_product=? AND source_organization_id=? AND source_location_id=? AND body#>>'{terms,buyer,organization_id}'=?", g.SourceTenantID, g.RecipientTenantID, g.HospitalID, g.BuyerLocationID, g.RelationshipID, g.Product, g.SourceOrganizationID, g.SourceLocationID, g.BuyerOrganizationID.String()).Where("body#>>'{input,terms_sha256}'=?", g.TermsSha256).Where("EXISTS(SELECT 1 FROM supply_supplier_dispatched_shipments d WHERE d.tenant_id=supply_purchase_orders.tenant_id AND d.order_id=supply_purchase_orders.id AND d.buyer_tenant_id=?)", g.RecipientTenantID)
}
func (h *partnerShipmentHandlers) orders(c *gin.Context, page pagination.Params) ([]partnerShipmentOrder, error) {
	p, request, ok := partnerShipmentRequest(c)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	result := []partnerShipmentOrder{}
	err := h.with(ctx, request, c.Param("terms"), func(tx *gorm.DB, g authz.PartnerScopeGrant, presentation suppliertrade.EndpointPresentation) error {
		var rows []struct {
			ID        uuid.UUID
			Reference string
			CreatedAt time.Time
		}
		if err := tenantdb.ApplyKeyset(partnerShipmentOrderQuery(tx, g), page).Select("id,body#>>'{input,purchase_order_reference}' AS reference,created_at").Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if r.ID.Version() != 7 || !suppliertrade.Text(r.Reference, 200) || r.CreatedAt.IsZero() {
				return f.ErrUnavailable
			}
			result = append(result, partnerShipmentOrder{supplieraccess.PartnerShipmentOrder{ID: r.ID, Scope: partnerShipmentScope(g), GrantID: g.GrantID, RelationshipID: g.RelationshipID, TermsSha256: g.TermsSha256, SourceOrganizationName: presentation.OrganizationName, SourceLocationName: presentation.LocationName, PurchaseOrderReference: r.Reference, CreatedAt: r.CreatedAt}})
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'partner_shipment_orders_read','supply_partner_shipments',?)`, g.SourceTenantID, p.Subject, g.GrantID).Error
	})
	if err != nil {
		partnerShipmentError(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *partnerShipmentHandlers) shipments(c *gin.Context, page pagination.Params) ([]partnerShipment, error) {
	p, request, ok := partnerShipmentRequest(c)
	if !ok {
		return nil, nil
	}
	order, ok := purchaseOrderID(c, "order", false)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	result := []partnerShipment{}
	err := h.with(ctx, request, c.Param("terms"), func(tx *gorm.DB, g authz.PartnerScopeGrant, presentation suppliertrade.EndpointPresentation) error {
		var visible struct{ ID uuid.UUID }
		if err := partnerShipmentOrderQuery(tx, g).Select("id").Where("id=?", order).Take(&visible).Error; err != nil {
			return err
		}
		parent, err := loadPurchaseOrder(tx, g.SourceTenantID, order)
		if err != nil {
			return err
		}
		rows, err := shipmentPage(tx.Table("supply_supplier_dispatched_shipments").Where("tenant_id=? AND buyer_tenant_id=? AND order_id=?", g.SourceTenantID, g.RecipientTenantID, order), page)
		if err != nil {
			return err
		}
		buyer := parent.Order
		buyer.Side = "buyer"
		buyer.OwnerTenantID = uuid.MustParse(g.RecipientTenantID)
		for _, r := range rows {
			if !shipmentMatchesBuyerOrder(r.Shipment, buyer) {
				return supplierorder.ErrUnavailable
			}
			result = append(result, partnerShipment{supplieraccess.PartnerShipment{ID: r.ID, Scope: partnerShipmentScope(g), GrantID: g.GrantID, RelationshipID: g.RelationshipID, TermsSha256: g.TermsSha256, SourceOrganizationName: presentation.OrganizationName, SourceLocationName: presentation.LocationName, OrderID: order, PurchaseOrderReference: parent.Order.Input.PurchaseOrderReference, ProductDescription: r.ProductDescription, UnitCode: r.UnitCode, PackSize: r.PackSize, QuantityUnits: r.QuantityUnits, LotNumber: r.LotNumber, ExpiresOn: r.ExpiresOn, HandoffReference: r.HandoffReference, DispatchedAt: r.DispatchedAt}})
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'partner_shipments_read','supply_purchase_orders',?)`, g.SourceTenantID, p.Subject, order).Error
	})
	if err != nil {
		partnerShipmentError(c, err)
		return nil, nil
	}
	return result, nil
}

func partnerShipmentIndexes() []tenantdb.ConcurrentIndex {
	return []tenantdb.ConcurrentIndex{{Name: "supply_partner_shipment_order_page", Table: "supply_purchase_orders", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "relationship_id"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}}}}
}
