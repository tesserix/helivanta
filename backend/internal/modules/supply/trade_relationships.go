package supply

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

const (
	PermRelationshipRead    authz.Permission = "supply.trade_relationship.read"
	PermRelationshipPrepare authz.Permission = "supply.trade_relationship.prepare"
	PermRelationshipReview  authz.Permission = "supply.trade_relationship.review"
)

type relationshipHandlers struct {
	db       *tenantdb.DB
	actors   authz.SourceAdministrationGuard
	sources  suppliertrade.CurrentEndpointGuard
	peers    suppliertrade.SourceChecker
	identity *organizationidentity.Client
}

func relationshipRoutes(r *platform.Router, deps platform.Deps) {
	h := &relationshipHandlers{db: deps.DB, actors: deps.SourceAdministration, sources: deps.TradingEndpoints, peers: deps.SourceChecks, identity: deps.OrganizationIdentity}
	base := "/relationships/:relationship"
	r.PUT(base, PermRelationshipPrepare, h.prepare)
	r.GET(base, PermRelationshipRead, h.get)
	r.GET(base+"/proposal-receipt", PermRelationshipRead, h.proposal)
	r.PUT(base+"/commands/:command", PermRelationshipReview, h.decide)
	r.GET(base+"/commands/:command", PermRelationshipRead, h.receipt)
	platform.ListRoute(r, "/trading-sources/:product/:organization/locations/:location/relationships", PermRelationshipRead, h.list)
}
func relationshipRequest(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, ok := principal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("relationship"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "supplier relationship")
		return p, uuid.Nil, false
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, suppliertrade.ErrInvalid)
		return p, uuid.Nil, false
	}
	return p, id, true
}
func relationshipBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || suppliertrade.DecodeObject(raw, target) != nil {
		respond.BadRequest(c, suppliertrade.ErrInvalid)
		return false
	}
	return true
}
func localRelationshipEndpoint(r suppliertrade.Relationship) suppliertrade.Endpoint {
	if r.Side == "seller" {
		return r.Terms.Seller
	}
	return r.Terms.Buyer
}
func (h *relationshipHandlers) actor(ctx context.Context, p authn.Principal, permission authz.Permission, action func(context.Context) error) error {
	if h.db == nil || h.actors == nil || h.sources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.actors.WithSourceAdministration(ctx, p, permission, action)
}
func (h *relationshipHandlers) resource(ctx context.Context, p authn.Principal, e suppliertrade.Endpoint, write bool, action func(context.Context) error) error {
	owner, ok := h.sources.(suppliertrade.AdministrationGuard)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return owner.WithTradingAdministration(ctx, p, e, write, action)
}
func relationshipError(c *gin.Context, err error) {
	if err == nil || c.Writer.Written() {
		return
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, suppliertrade.ErrIneligible):
		respond.NotFound(c, "supplier relationship or current source")
	case errors.Is(err, suppliertrade.ErrConflict):
		respond.Error(c, 409, "relationship_conflict", "The relationship basis changed. Refresh before continuing.")
	case errors.Is(err, suppliertrade.ErrInvalid), errors.Is(err, authz.ErrInvalidScopedAssignment):
		respond.BadRequest(c, suppliertrade.ErrInvalid)
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Error(c, 403, "forbidden", "Current relationship administration is required.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_changed", "Sign in again before continuing.")
	default:
		respond.Error(c, 503, "relationship_unavailable", "The current relationship could not be verified.")
	}
}
func (h *relationshipHandlers) prepare(c *gin.Context) {
	p, id, ok := relationshipRequest(c)
	if !ok {
		return
	}
	var input suppliertrade.SupplierRelationshipInput
	if !relationshipBody(c, &input) {
		return
	}
	initial, err := suppliertrade.PrepareRelationship(id, input.Terms, p.Subject, time.Now())
	if err != nil || !suppliertrade.Text(input.Reason, 500) || initial.OwnerTenantID.String() != p.TenantID {
		respond.BadRequest(c, suppliertrade.ErrInvalid)
		return
	}
	input.Terms = initial.Terms
	var result suppliertrade.Relationship
	status := 201
	err = h.actor(c.Request.Context(), p, PermRelationshipPrepare, func(ctx context.Context) error {
		return h.resource(ctx, p, initial.Terms.Seller, true, func(ctx context.Context) error {
			return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
				if err := relationshipLock(tx, initial.OwnerTenantID, id); err != nil {
					return err
				}
				var previous struct{ ProposalInput, ProposalReceipt string }
				if err := tx.Raw(`SELECT proposal_input,proposal_receipt FROM supply_trade_relationships WHERE tenant_id=? AND id=?`, p.TenantID, id).Scan(&previous).Error; err != nil {
					return err
				}
				if previous.ProposalReceipt != "" {
					var original suppliertrade.SupplierRelationshipInput
					if suppliertrade.DecodeObject([]byte(previous.ProposalInput), &original) != nil || suppliertrade.DecodeObject([]byte(previous.ProposalReceipt), &result) != nil {
						return suppliertrade.ErrUnavailable
					}
					left, _ := json.Marshal(original)
					right, _ := json.Marshal(input)
					if string(left) != string(right) || result.PreparedBy != p.Subject {
						return suppliertrade.ErrConflict
					}
					status = 200
					return nil
				}
				result = initial
				return saveTradeRelationship(tx, result, &input, p.Subject, nil)
			})
		})
	})
	if err != nil {
		relationshipError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		c.JSON(status, result)
	}
}
func (h *relationshipHandlers) readLocal(c *gin.Context, p authn.Principal, id uuid.UUID, permission authz.Permission, write bool, action func(context.Context, *gorm.DB, suppliertrade.Relationship) error) error {
	return h.actor(c.Request.Context(), p, permission, func(ctx context.Context) error {
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
			if err := relationshipLock(tx, uuid.MustParse(p.TenantID), id); err != nil {
				return err
			}
			current, err := loadTradeRelationship(tx, uuid.MustParse(p.TenantID), id)
			if err != nil {
				return err
			}
			return h.resource(ctx, p, localRelationshipEndpoint(current), write, func(ctx context.Context) error { return action(ctx, tx, current) })
		})
	})
}
func (h *relationshipHandlers) get(c *gin.Context)      { h.read(c, false) }
func (h *relationshipHandlers) proposal(c *gin.Context) { h.read(c, true) }
func (h *relationshipHandlers) read(c *gin.Context, original bool) {
	p, id, ok := relationshipRequest(c)
	if !ok {
		return
	}
	var result suppliertrade.Relationship
	err := h.readLocal(c, p, id, PermRelationshipRead, false, func(_ context.Context, tx *gorm.DB, r suppliertrade.Relationship) error {
		result = r
		if original {
			if r.PreparedBy != p.Subject || r.Side != "seller" {
				return gorm.ErrRecordNotFound
			}
			var raw string
			if err := tx.Raw(`SELECT proposal_receipt FROM supply_trade_relationships WHERE tenant_id=? AND id=?`, p.TenantID, id).Row().Scan(&raw); err != nil {
				return err
			}
			if err := suppliertrade.DecodeObject([]byte(raw), &result); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trade_relationship_read','supply_trade_relationships',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		relationshipError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
func (h *relationshipHandlers) receipt(c *gin.Context) {
	p, id, ok := relationshipRequest(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 || command.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "relationship decision")
		return
	}
	var result suppliertrade.SupplierRelationshipReceipt
	err = h.readLocal(c, p, id, PermRelationshipRead, false, func(_ context.Context, tx *gorm.DB, _ suppliertrade.Relationship) error {
		var raw string
		err := tx.Raw(`SELECT receipt FROM supply_trade_relationship_commands WHERE tenant_id=? AND relationship_id=? AND id=? AND actor_subject=?`, p.TenantID, id, command, p.Subject).Row().Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return gorm.ErrRecordNotFound
		}
		if err != nil {
			return err
		}
		if err := suppliertrade.DecodeObject([]byte(raw), &result); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trade_relationship_decision_read','supply_trade_relationship_commands',?)`, p.TenantID, p.Subject, command).Error
	})
	if err != nil {
		relationshipError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
func (h *relationshipHandlers) decide(c *gin.Context) {
	p, id, ok := relationshipRequest(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 || command.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "relationship decision")
		return
	}
	var input suppliertrade.SupplierRelationshipCommand
	if !relationshipBody(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion >= 9007199254740991 || !suppliertrade.Text(input.Reason, 500) {
		respond.BadRequest(c, suppliertrade.ErrInvalid)
		return
	}
	var snapshot suppliertrade.Relationship
	var result suppliertrade.SupplierRelationshipReceipt
	recovered := false
	// Read/recover under current owner authority, then release locks before any network call.
	err = h.readLocal(c, p, id, PermRelationshipReview, true, func(_ context.Context, tx *gorm.DB, r suppliertrade.Relationship) error {
		snapshot = r
		var raw string
		err := tx.Raw(`SELECT receipt FROM supply_trade_relationship_commands WHERE tenant_id=? AND relationship_id=? AND id=?`, p.TenantID, id, command).Row().Scan(&raw)
		if err == nil {
			if err := suppliertrade.DecodeObject([]byte(raw), &result); err != nil {
				return err
			}
			if result.ActorSubject != p.Subject || result.Input != input {
				return suppliertrade.ErrConflict
			}
			recovered = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, _, err = suppliertrade.DecideRelationship(r, input.ExpectedVersion, input.Decision, p.Subject, time.Now())
		return err
	})
	if err != nil {
		relationshipError(c, err)
		return
	}
	if recovered {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
		return
	}
	if input.Decision == "approve" {
		if h.identity == nil || h.peers == nil {
			relationshipError(c, suppliertrade.ErrUnavailable)
			return
		}
		for _, tenant := range []uuid.UUID{snapshot.Terms.Seller.TenantID, snapshot.Terms.Buyer.TenantID} {
			if _, err := h.identity.Verify(c.Request.Context(), tenant); err != nil {
				if errors.Is(err, organizationidentity.ErrIneligible) {
					err = suppliertrade.ErrIneligible
				}
				relationshipError(c, err)
				return
			}
		}
		remote := snapshot.Terms.Buyer
		if snapshot.Side == "buyer" {
			remote = snapshot.Terms.Seller
		}
		if _, err := h.peers.Check(c.Request.Context(), suppliertrade.SourceCheckInput{Endpoint: remote, Category: snapshot.Terms.Category, Operation: "order"}); err != nil {
			relationshipError(c, err)
			return
		}
	}
	err = h.readLocal(c, p, id, PermRelationshipReview, true, func(ctx context.Context, tx *gorm.DB, current suppliertrade.Relationship) error {
		var saved struct {
			RelationshipID uuid.UUID
			Receipt        string
		}
		if err := tx.Raw(`SELECT relationship_id,receipt FROM supply_trade_relationship_commands WHERE tenant_id=? AND id=?`, p.TenantID, command).Scan(&saved).Error; err != nil {
			return err
		}
		if saved.RelationshipID != uuid.Nil {
			if saved.RelationshipID != id || suppliertrade.DecodeObject([]byte(saved.Receipt), &result) != nil || result.ActorSubject != p.Subject || result.Input != input {
				return suppliertrade.ErrConflict
			}
			recovered = true
			return nil
		}
		if current.Version != snapshot.Version || current.TermsSha256 != snapshot.TermsSha256 {
			return suppliertrade.ErrConflict
		}
		commit := func(ctx context.Context) error {
			next, event, err := suppliertrade.DecideRelationship(current, input.ExpectedVersion, input.Decision, p.Subject, time.Now())
			if err != nil {
				return err
			}
			if err := saveTradeRelationship(tx, next, nil, p.Subject, nil); err != nil {
				return err
			}
			if err := enqueueRelationshipExchange(tx, event); err != nil {
				return err
			}
			result = suppliertrade.SupplierRelationshipReceipt{OwnerTenantID: next.OwnerTenantID, ID: command, Input: input, Result: next, ActorSubject: p.Subject, CreatedAt: next.UpdatedAt}
			body, err := json.Marshal(input)
			if err != nil {
				return err
			}
			receipt, err := json.Marshal(result)
			if err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO supply_trade_relationship_commands(tenant_id,id,relationship_id,actor_subject,input,receipt,created_at)VALUES(?,?,?,?,?::jsonb,?::jsonb,?)`, p.TenantID, command, id, p.Subject, string(body), string(receipt), next.UpdatedAt).Error
		}
		if input.Decision == "approve" {
			return h.sources.WithCurrentTradingEndpoint(ctx, localRelationshipEndpoint(current), current.Terms.Category, "order", func(ctx context.Context, _ suppliertrade.EndpointReference) error { return commit(ctx) })
		}
		return commit(ctx)
	})
	if err != nil {
		relationshipError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		if recovered {
			c.JSON(200, result)
		} else {
			c.JSON(201, result)
		}
	}
}

func (r relationshipPage) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type relationshipPage struct{ suppliertrade.Relationship }

func (h *relationshipHandlers) list(c *gin.Context, page pagination.Params) ([]relationshipPage, error) {
	p, ok := principal(c)
	if !ok {
		return nil, nil
	}
	organization, oerr := uuid.Parse(c.Param("organization"))
	location, lerr := uuid.Parse(c.Param("location"))
	product := c.Param("product")
	if oerr != nil || lerr != nil || organization.Version() != 7 || location.Version() != 7 || (product != "supply" && product != "pharmacy") {
		respond.NotFound(c, "trading source")
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, suppliertrade.ErrInvalid)
			return nil, nil
		}
	}
	e := suppliertrade.Endpoint{TenantID: uuid.MustParse(p.TenantID), Product: product, OrganizationID: organization, LocationID: location}
	result := []relationshipPage{}
	err := h.actor(c.Request.Context(), p, PermRelationshipRead, func(ctx context.Context) error {
		return h.resource(ctx, p, e, false, func(ctx context.Context) error {
			return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
				var rows []struct{ Body string }
				q := tx.Table("supply_trade_relationships").Select("body").Where("tenant_id=? AND ((body->>'side'='seller'AND body#>>'{terms,seller,product}'=? AND body#>>'{terms,seller,organization_id}'=? AND body#>>'{terms,seller,location_id}'=?)OR(body->>'side'='buyer'AND body#>>'{terms,buyer,product}'=? AND body#>>'{terms,buyer,organization_id}'=? AND body#>>'{terms,buyer,location_id}'=?))", p.TenantID, product, organization.String(), location.String(), product, organization.String(), location.String()).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
				if page.After != nil {
					q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
				}
				if err := q.Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					var r suppliertrade.Relationship
					if suppliertrade.DecodeObject([]byte(row.Body), &r) != nil {
						return suppliertrade.ErrUnavailable
					}
					result = append(result, relationshipPage{r})
				}
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trade_relationship_directory_read','trading_source',?)`, p.TenantID, p.Subject, location).Error
			})
		})
	})
	if err != nil {
		relationshipError(c, err)
	}
	return result, err
}
