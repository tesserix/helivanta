package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed supplier_grants.sql
var supplierGrantsSchema string

//go:embed assignment_supplier_access_templates_generated.sql
var supplierAccessTemplatesSchema string

const (
	PermSupplierGrantRead    authz.Permission = "iam.supplier_grant.read"
	PermSupplierGrantPrepare authz.Permission = "iam.supplier_grant.prepare"
	PermSupplierGrantReview  authz.Permission = "iam.supplier_grant.review"
	PermSupplierGrantAccept  authz.Permission = "iam.supplier_grant.accept"
	PermSupplierGrantInbox   authz.Permission = "iam.supplier_grant.inbox.read"
)

type supplierGrantHandlers struct {
	db            *tenantdb.DB
	admins        authz.SourceAdministrationGuard
	products      suppliertrade.CurrentEndpointGuard
	relationships suppliertrade.CurrentRelationshipGuard
	members       *partnerMemberHandlers
	recipients    authz.PartnerRecipientSource
}

func (m *Module) registerSupplierGrants(r *platform.Router, deps platform.Deps) {
	h := &supplierGrantHandlers{db: deps.DB, admins: deps.SourceAdministration, products: deps.TradingEndpoints, relationships: deps.TradingRelationships, members: &partnerMemberHandlers{partnerGroupHandlers: &partnerGroupHandlers{db: deps.DB, bus: deps.Bus}, authority: deps.ScopedAuthorizer, source: deps.PartnerMembers}}
	h.recipients, _ = deps.PartnerScopes.(authz.PartnerRecipientSource)
	r.PUT("/supplier-grants/:id", PermSupplierGrantPrepare, h.prepare)
	r.GET("/supplier-grants/:id", PermSupplierGrantRead, h.getSource)
	r.PUT("/supplier-grants/:id/commands/:command", PermSupplierGrantReview, h.decideSource)
	r.GET("/supplier-grants/:id/commands/:command", PermSupplierGrantRead, h.sourceReceipt)
	platform.ListRoute(r, "/supplier-sources/:product/:organization/locations/:location/grants", PermSupplierGrantRead, h.listSource)
	r.GET("/hospitals/:hospital/supplier-grants/:id", authz.Public, h.getHospitalGrant)
	home := "/hospitals/:hospital/supplier-grant-offers"
	r.GET(home+"/:id", authz.Public, h.getHospitalOffer)
	platform.ListRoute(r, home, authz.Public, h.listHospitalOffers)
	r.PUT(home+"/:id/commands/:command", authz.Public, h.decideHospital)
	r.GET(home+"/:id/commands/:command", authz.Public, h.hospitalReceipt)
}
func grantBody(c *gin.Context, target any) bool {
	if c.Request.URL.RawQuery != "" || c.ContentType() != "application/json" {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || suppliertrade.DecodeObject(raw, target) != nil {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return false
	}
	return true
}
func grantRecordID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param(name) {
		respond.NotFound(c, "supplier grant")
		return uuid.Nil, false
	}
	return id, true
}
func supplierGrantError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, supplieraccess.ErrInvalid):
		respond.BadRequest(c, supplieraccess.ErrInvalid)
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, suppliertrade.ErrIneligible):
		respond.NotFound(c, "supplier grant")
	case errors.Is(err, supplieraccess.ErrConflict), errors.Is(err, suppliertrade.ErrConflict), errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514"):
		respond.Conflict(c, "This supplier grant or command changed. Recover its retained result before retrying.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_revoked", "Sign in again before managing supplier access.")
	case errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, supplieraccess.ErrIneligible):
		respond.Forbidden(c, "Current authority for this supplier, hospital and recipient is required.")
	default:
		respond.Error(c, 503, "verification_unavailable", "Current supplier access could not be verified. No authority was inferred.")
	}
}
func endpointForGrant(grant supplieraccess.SupplierGrant) suppliertrade.Endpoint {
	return suppliertrade.Endpoint{TenantID: grant.Scope.SourceTenantID, Product: grant.Scope.SourceProduct, OrganizationID: grant.Scope.SourceOrganizationID, LocationID: grant.Scope.SourceLocationID}
}
func (h *supplierGrantHandlers) sourceTransaction(ctx context.Context, p authn.Principal, permission authz.Permission, endpoint suppliertrade.Endpoint, writing bool, action func(context.Context, *gorm.DB) error) error {
	if h.db == nil || h.admins == nil || h.products == nil {
		return suppliertrade.ErrUnavailable
	}
	resources, ok := h.products.(suppliertrade.AdministrationGuard)
	if !ok {
		return suppliertrade.ErrUnavailable
	}
	return h.admins.WithSourceAdministration(ctx, p, permission, func(current context.Context) error {
		return resources.WithTradingAdministration(current, p, endpoint, writing, func(current context.Context) error {
			return h.db.WithTenantCommand(current, p.TenantID, func(tx *gorm.DB) error {
				if err := supplierGrantFrame(tx, "source", uuid.Nil); err != nil {
					return err
				}
				return action(current, tx)
			})
		})
	})
}
func (h *supplierGrantHandlers) readSource(c *gin.Context, p authn.Principal, id uuid.UUID, permission authz.Permission, writing bool, action func(context.Context, *gorm.DB, supplieraccess.SupplierGrant) error) error {
	if h.db == nil || h.admins == nil || h.products == nil {
		return suppliertrade.ErrUnavailable
	}
	resources, ok := h.products.(suppliertrade.AdministrationGuard)
	if !ok {
		return suppliertrade.ErrUnavailable
	}
	return h.admins.WithSourceAdministration(c.Request.Context(), p, permission, func(ctx context.Context) error {
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
			if err := supplierGrantFrame(tx, "source", uuid.Nil); err != nil {
				return err
			}
			grant, err := loadSupplierGrant(tx, p.TenantID, id)
			if err != nil {
				return err
			}
			if grant.Side != "source" {
				return gorm.ErrRecordNotFound
			}
			return resources.WithTradingAdministration(ctx, p, endpointForGrant(grant), writing, func(current context.Context) error { return action(current, tx, grant) })
		})
	})
}
func (h *supplierGrantHandlers) prepare(c *gin.Context) {
	p, id, ok := operatingScope(c, PermSupplierGrantPrepare)
	if !ok {
		return
	}
	var input supplieraccess.SupplierGrantInput
	if !grantBody(c, &input) {
		return
	}
	if !supplieraccess.ValidInput(input) {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return
	}
	if h.relationships == nil {
		supplierGrantError(c, suppliertrade.ErrUnavailable)
		return
	}
	proof, err := h.relationships.VerifyRelationship(c.Request.Context(), suppliertrade.RelationshipScope{TenantID: p.TenantID, ID: input.RelationshipID, TermsSha256: input.TermsSha256, Purpose: "new_business"})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	snapshot := proof.Snapshot()
	candidate, err := supplieraccess.Prepare(id, snapshot, input, p.Subject, time.Now())
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	var result supplieraccess.SupplierGrant
	created := false
	err = h.sourceTransaction(c.Request.Context(), p, PermSupplierGrantPrepare, endpointForGrant(candidate), true, func(ctx context.Context, tx *gorm.DB) error {
		if e := supplierGrantLock(tx, p.TenantID, id); e != nil {
			return e
		}
		previous, e := loadSupplierGrant(tx, p.TenantID, id)
		if e == nil {
			if previous.PreparedBy != p.Subject || previous.InputSha256 != candidate.InputSha256 {
				return supplieraccess.ErrConflict
			}
			result = previous
			return grantReadAudit(tx, p, id, "supplier_grant_prepare_recovery")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		return h.relationships.WithVerifiedRelationship(ctx, proof, func(_ context.Context, current suppliertrade.Relationship) error {
			fresh, e := supplieraccess.Prepare(id, current, input, p.Subject, time.Now())
			if e != nil {
				return e
			}
			if e = saveSupplierGrant(tx, fresh, p.Subject, nil, true); e != nil {
				return e
			}
			result = fresh
			created = true
			return nil
		})
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}
func grantReadAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,?,'iam_supplier_grants',?)`, p.TenantID, p.Subject, action, id).Error
}
func (h *supplierGrantHandlers) getSource(c *gin.Context) {
	p, id, ok := operatingScope(c, PermSupplierGrantRead)
	if !ok {
		return
	}
	var result supplieraccess.SupplierGrant
	err := h.readSource(c, p, id, PermSupplierGrantRead, false, func(_ context.Context, tx *gorm.DB, grant supplieraccess.SupplierGrant) error {
		result = grant
		return grantReadAudit(tx, p, id, "supplier_grant_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}
func readSupplierGrantCommand(tx *gorm.DB, p authn.Principal, grant, id uuid.UUID, hash string) (supplieraccess.SupplierGrantCommandReceipt, error) {
	var row struct {
		Receipt                 []byte
		InputHash, ActorSubject string
	}
	var result supplieraccess.SupplierGrantCommandReceipt
	if err := tx.Table("iam_supplier_grant_commands").Select("receipt,input_hash,actor_subject").Where("tenant_id=? AND grant_id=? AND id=?", p.TenantID, grant, id).Take(&row).Error; err != nil {
		return result, err
	}
	if row.ActorSubject != p.Subject || (hash != "" && hash != row.InputHash) {
		return result, supplieraccess.ErrConflict
	}
	if suppliertrade.DecodeObject(row.Receipt, &result) != nil || result.ActorSubject != p.Subject || result.ID != id || result.GrantID != grant || result.Grant.ID != grant || supplieraccess.ValidateRecord(result.Grant) != nil {
		return result, suppliertrade.ErrUnavailable
	}
	return result, nil
}
func saveSupplierGrantCommand(tx *gorm.DB, p authn.Principal, command uuid.UUID, hash string, grant supplieraccess.SupplierGrant, decision supplieraccess.SupplierGrantDecisionInput) (supplieraccess.SupplierGrantCommandReceipt, error) {
	receipt := supplieraccess.SupplierGrantCommandReceipt{ID: command, GrantID: grant.ID, ActorSubject: p.Subject, ExpectedVersion: decision.ExpectedVersion, Operation: decision.Operation, Reason: decision.Reason, Grant: grant, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return receipt, err
	}
	err = tx.Exec(`INSERT INTO iam_supplier_grant_commands(tenant_id,id,grant_id,hospital_id,actor_subject,input_hash,receipt,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?)`, p.TenantID, command, grant.ID, grant.Scope.HospitalID, p.Subject, hash, string(raw), receipt.CreatedAt, receipt.CreatedAt).Error
	return receipt, err
}
func (h *supplierGrantHandlers) sourceReceipt(c *gin.Context) {
	p, id, ok := operatingScope(c, PermSupplierGrantRead)
	if !ok {
		return
	}
	command, ok := grantRecordID(c, "command")
	if !ok {
		return
	}
	var result supplieraccess.SupplierGrantCommandReceipt
	err := h.readSource(c, p, id, PermSupplierGrantRead, false, func(_ context.Context, tx *gorm.DB, _ supplieraccess.SupplierGrant) error {
		var e error
		result, e = readSupplierGrantCommand(tx, p, id, command, "")
		if e != nil {
			return e
		}
		return grantReadAudit(tx, p, id, "supplier_grant_command_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierGrantHandlers) decideSource(c *gin.Context) {
	p, id, ok := operatingScope(c, PermSupplierGrantReview)
	if !ok {
		return
	}
	command, ok := grantRecordID(c, "command")
	if !ok {
		return
	}
	var input supplieraccess.SupplierGrantDecisionInput
	if !grantBody(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion >= 9007199254740991 || !suppliertrade.Text(input.Reason, 500) {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return
	}
	hash, err := supplierCommandHash(p.Subject, input)
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	var original supplieraccess.SupplierGrant
	var result supplieraccess.SupplierGrantCommandReceipt
	recovered := false
	err = h.readSource(c, p, id, PermSupplierGrantReview, true, func(_ context.Context, tx *gorm.DB, row supplieraccess.SupplierGrant) error {
		receipt, e := readSupplierGrantCommand(tx, p, id, command, hash)
		if e == nil {
			result = receipt
			recovered = true
			return grantReadAudit(tx, p, id, "supplier_grant_command_recovery")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		original = row
		_, _, e = supplieraccess.Decide(row, input.ExpectedVersion, input.Operation, p.Subject, "", time.Now())
		return e
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	if recovered {
		respond.OK(c, result)
		return
	}
	var proof suppliertrade.RelationshipAuthorization
	if input.Operation == "approve" {
		if h.relationships == nil {
			supplierGrantError(c, suppliertrade.ErrUnavailable)
			return
		}
		proof, err = h.relationships.VerifyRelationship(c.Request.Context(), suppliertrade.RelationshipScope{TenantID: p.TenantID, ID: original.Input.RelationshipID, TermsSha256: original.Input.TermsSha256, Purpose: "new_business"})
		if err != nil {
			supplierGrantError(c, err)
			return
		}
	}
	err = h.sourceTransaction(c.Request.Context(), p, PermSupplierGrantReview, endpointForGrant(original), true, func(ctx context.Context, tx *gorm.DB) error {
		if e := supplierGrantLock(tx, p.TenantID, id); e != nil {
			return e
		}
		receipt, e := readSupplierGrantCommand(tx, p, id, command, hash)
		if e == nil {
			result = receipt
			return grantReadAudit(tx, p, id, "supplier_grant_command_recovery")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		row, e := loadSupplierGrant(tx, p.TenantID, id)
		if e != nil {
			return e
		}
		next, event, e := supplieraccess.Decide(row, input.ExpectedVersion, input.Operation, p.Subject, "", time.Now())
		if e != nil {
			return e
		}
		commit := func() error {
			if e := saveSupplierGrant(tx, next, p.Subject, nil, false); e != nil {
				return e
			}
			var e error
			result, e = saveSupplierGrantCommand(tx, p, command, hash, next, input)
			if e != nil {
				return e
			}
			return enqueueSupplierGrant(tx, event)
		}
		if proof != nil {
			return h.relationships.WithVerifiedRelationship(ctx, proof, func(_ context.Context, relationship suppliertrade.Relationship) error {
				if _, e := supplieraccess.Prepare(row.ID, relationship, row.Input, row.PreparedBy, time.Now()); e != nil {
					return e
				}
				return commit()
			})
		}
		return commit()
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}

type supplierGrantPage struct{ supplieraccess.SupplierGrant }

func (r supplierGrantPage) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *supplierGrantHandlers) listSource(c *gin.Context, page pagination.Params) ([]supplierGrantPage, error) {
	p, _, ok := operatingScope(c, PermSupplierGrantRead)
	if !ok {
		return nil, nil
	}
	org, oe := uuid.Parse(c.Param("organization"))
	location, le := uuid.Parse(c.Param("location"))
	product := c.Param("product")
	if oe != nil || le != nil || org.Version() != 7 || location.Version() != 7 || (product != "supply" && product != "pharmacy") {
		respond.NotFound(c, "supplier source")
		return nil, nil
	}
	for key, value := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(value) != 1 || value[0] == "" {
			respond.BadRequest(c, supplieraccess.ErrInvalid)
			return nil, nil
		}
	}
	result := []supplierGrantPage{}
	endpoint := suppliertrade.Endpoint{TenantID: uuid.MustParse(p.TenantID), Product: product, OrganizationID: org, LocationID: location}
	err := h.sourceTransaction(c.Request.Context(), p, PermSupplierGrantRead, endpoint, false, func(_ context.Context, tx *gorm.DB) error {
		var rows []struct{ Document []byte }
		query := tx.Table("iam_supplier_grants").Select("document").Where("tenant_id=? AND document->>'side'='source' AND document#>>'{scope,source_product}'=? AND document#>>'{scope,source_organization_id}'=? AND document#>>'{scope,source_location_id}'=?", p.TenantID, product, org.String(), location.String()).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if e := query.Find(&rows).Error; e != nil {
			return e
		}
		for _, row := range rows {
			var record supplieraccess.SupplierGrant
			if suppliertrade.DecodeObject(row.Document, &record) != nil || supplieraccess.ValidateRecord(record) != nil {
				return suppliertrade.ErrUnavailable
			}
			result = append(result, supplierGrantPage{record})
		}
		return grantReadAudit(tx, p, location, "supplier_grant_directory_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	return result, nil
}
