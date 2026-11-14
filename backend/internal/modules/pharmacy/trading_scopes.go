package pharmacy

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"io"
	"net/http"
	"slices"
	"time"
	"unicode/utf8"
)

//go:embed trading_scopes.sql
var tradingScopesSchema string

const (
	PermTradingScopeRead    authz.Permission = "pharmacy.trading_scope.read"
	PermTradingScopePrepare authz.Permission = "pharmacy.trading_scope.prepare"
	PermTradingScopeReview  authz.Permission = "pharmacy.trading_scope.review"
)

var errTradeScopeConflict = errors.New("trade scope basis changed")
var errTradeScopeInvalid = errors.New("invalid trade scope intent")

type tradingScopeHandlers struct {
	db        *tenantdb.DB
	authority authz.SourceAdministrationGuard
}
type tradingScopeRow struct {
	TenantID, PharmacyID, OutletID, ID uuid.UUID
	Input, ProposalReceipt             string
	Status, PreparedBy                 string
	Version                            int64
	CreatedAt, UpdatedAt               time.Time
}

func (tradingScopeRow) TableName() string                          { return "pharmacy_trading_scopes" }
func (s supplierTradingScopePage) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

type supplierTradingScopePage struct {
	suppliertrade.PharmacyTradingScope
}

func tradingScopeRoutes(r *platform.Router, deps platform.Deps) {
	h := &tradingScopeHandlers{db: deps.DB, authority: deps.SourceAdministration}
	path := "/pharmacies/:pharmacy/outlets/:outlet/trading-scopes"
	r.PUT(path+"/:id", PermTradingScopePrepare, h.prepare)
	r.GET(path+"/:id", PermTradingScopeRead, h.get)
	r.GET(path+"/:id/proposal-receipt", PermTradingScopeRead, h.proposal)
	r.PUT(path+"/:id/commands/:command", PermTradingScopeReview, h.decide)
	r.GET(path+"/:id/commands/:command", PermTradingScopeRead, h.receipt)
	platform.ListRoute(r, path, PermTradingScopeRead, h.list)
}
func scopeDecode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(raw) {
		respond.BadRequest(c, errTradeScopeInvalid)
		return false
	}
	if suppliertrade.DecodeObject(raw, target) != nil {
		respond.BadRequest(c, errTradeScopeInvalid)
		return false
	}
	return true
}
func scopeInputValid(in suppliertrade.PharmacyTradingScopeInput) bool {
	return in.LicenceID.Version() == 7 && in.LicenceID.Variant() == uuid.RFC4122 && (in.Jurisdiction == "IN-MH" || in.Jurisdiction == "IN-OD") && suppliertrade.ValidCategory(in.Category) && suppliertrade.Text(in.Reason, 500) && in.AttestPremiseAndLicenceScope
}
func scopeView(r tradingScopeRow) (suppliertrade.PharmacyTradingScope, error) {
	var in suppliertrade.PharmacyTradingScopeInput
	if err := json.Unmarshal([]byte(r.Input), &in); err != nil {
		return suppliertrade.PharmacyTradingScope{}, err
	}
	return suppliertrade.PharmacyTradingScope{OwnerTenantID: r.TenantID, PharmacyID: r.PharmacyID, OutletID: r.OutletID, ID: r.ID, Input: in, Status: r.Status, Version: r.Version, PreparedBy: r.PreparedBy, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC()}, nil
}
func scopeLoad(tx *gorm.DB, p authn.Principal, outlet, id uuid.UUID) (tradingScopeRow, error) {
	var row tradingScopeRow
	err := tx.Raw(`SELECT tenant_id,pharmacy_id,outlet_id,id,input,proposal_receipt,status,version,prepared_by,created_at,updated_at FROM pharmacy_trading_scopes WHERE tenant_id=? AND outlet_id=? AND id=? FOR UPDATE`, p.TenantID, outlet, id).Scan(&row).Error
	if err == nil && row.ID == uuid.Nil {
		err = gorm.ErrRecordNotFound
	}
	return row, err
}
func (h *tradingScopeHandlers) transaction(c *gin.Context, permission authz.Permission, write bool, fn func(context.Context, *gorm.DB, authn.Principal, uuid.UUID) error) error {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil
	}
	if c.Request.URL.RawQuery != "" && !slices.Contains([]authz.Permission{PermTradingScopeRead}, permission) {
		return errTradeScopeInvalid
	}
	if h.authority == nil || h.db == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.authority.WithSourceAdministration(c.Request.Context(), p, permission, func(ctx context.Context) error {
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
			level, err := outletAccess(tx, p, outlet, currentPharmacy(c.Request.Context()), true)
			if err != nil {
				return err
			}
			if write && level != "editor" {
				return gorm.ErrRecordNotFound
			}
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":pharmacy-trade-scope:"+outlet.String()).Error; err != nil {
				return err
			}
			return fn(ctx, tx, p, outlet)
		})
	})
}
func tradeScopeError(c *gin.Context, err error) {
	if err == nil || c.Writer.Written() {
		return
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "trading scope")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Error(c, 403, "forbidden", "Current source administration is required.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_changed", "Sign in again before continuing.")
	case errors.Is(err, errTradeScopeConflict), tradeScopeDatabaseConflict(err):
		respond.Error(c, 409, "trading_scope_conflict", "The trading scope changed. Refresh before continuing.")
	case errors.Is(err, errTradeScopeInvalid), errors.Is(err, authz.ErrInvalidScopedAssignment):
		respond.BadRequest(c, errTradeScopeInvalid)
	default:
		respond.Error(c, 503, "trading_scope_unavailable", "The current trading scope could not be verified.")
	}
}
func (h *tradingScopeHandlers) prepare(c *gin.Context) {
	if _, _, ok := pharmacyPrincipal(c); !ok {
		return
	}
	id, ok := tradingScopeID(c)
	if !ok {
		return
	}
	var input suppliertrade.PharmacyTradingScopeInput
	if !scopeDecode(c, &input) {
		return
	}
	if !scopeInputValid(input) {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	var result suppliertrade.PharmacyTradingScope
	status := 201
	err := h.transaction(c, PermTradingScopePrepare, true, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		row, err := scopeLoad(tx, p, outlet, id)
		if err == nil {
			var original suppliertrade.PharmacyTradingScope
			if err = json.Unmarshal([]byte(row.ProposalReceipt), &original); err != nil {
				return err
			}
			if original.Input != input || original.PreparedBy != p.Subject {
				return errTradeScopeConflict
			}
			result = original
			status = 200
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var licence uuid.UUID
		if err = tx.Raw(`SELECT id FROM pharmacy_outlet_licences WHERE tenant_id=? AND outlet_id=? AND id=? FOR SHARE`, p.TenantID, outlet, input.LicenceID).Row().Scan(&licence); errors.Is(err, sql.ErrNoRows) {
			return gorm.ErrRecordNotFound
		} else if err != nil {
			return err
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		result = suppliertrade.PharmacyTradingScope{OwnerTenantID: uuid.MustParse(p.TenantID), PharmacyID: currentPharmacy(c.Request.Context()), OutletID: outlet, ID: id, Input: input, Status: "pending_review", Version: 1, PreparedBy: p.Subject, CreatedAt: at, UpdatedAt: at}
		body, err := json.Marshal(input)
		if err != nil {
			return err
		}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_trading_scopes(tenant_id,pharmacy_id,outlet_id,id,input,status,version,prepared_by,proposal_receipt,created_at,updated_at)VALUES(?,?,?,?,?::jsonb,'pending_review',1,?,?::jsonb,?,?)`, p.TenantID, result.PharmacyID, outlet, id, string(body), p.Subject, string(receipt), at, at).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		c.JSON(status, result)
	}
}
func scopeLicenceCurrent(tx *gorm.DB, tenant string, outlet, licence uuid.UUID) error {
	var id uuid.UUID
	err := tx.Raw(`SELECT review_id FROM pharmacy_trading_licence_source(?,?,?)`, tenant, outlet, licence).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return errTradeScopeConflict
	}
	return err
}
func (h *tradingScopeHandlers) decide(c *gin.Context) {
	if _, _, ok := pharmacyPrincipal(c); !ok {
		return
	}
	id, ok := tradingScopeID(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 || command.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "trading scope command")
		return
	}
	var input suppliertrade.PharmacyTradingScopeCommand
	if !scopeDecode(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion > 9007199254740990 || !slices.Contains([]string{"approve", "reject", "revoke"}, input.Decision) || !suppliertrade.Text(input.Reason, 500) || (input.Decision == "approve" && !input.AttestPremiseAndLicenceScope) || (input.Decision != "approve" && input.AttestPremiseAndLicenceScope) {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	var result suppliertrade.PharmacyTradingScopeReceipt
	status := 201
	err = h.transaction(c, PermTradingScopeReview, true, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		var retained struct {
			ScopeID uuid.UUID
			Receipt string
		}
		if err := tx.Raw(`SELECT scope_id,receipt FROM pharmacy_trading_scope_commands WHERE tenant_id=? AND id=?`, p.TenantID, command).Scan(&retained).Error; err != nil {
			return err
		}
		if retained.ScopeID != uuid.Nil {
			if retained.ScopeID != id {
				return errTradeScopeConflict
			}
			if err := json.Unmarshal([]byte(retained.Receipt), &result); err != nil {
				return err
			}
			if result.ActorSubject != p.Subject || result.Input != input {
				return errTradeScopeConflict
			}
			status = 200
			return nil
		}
		row, err := scopeLoad(tx, p, outlet, id)
		if err != nil {
			return err
		}
		if row.Version != input.ExpectedVersion {
			return errTradeScopeConflict
		}
		target := ""
		switch input.Decision {
		case "approve", "reject":
			if row.Status != "pending_review" || p.Subject == row.PreparedBy {
				return errTradeScopeConflict
			}
			if input.Decision == "approve" {
				target = "approved"
			} else {
				target = "rejected"
			}
		case "revoke":
			if row.Status != "approved" {
				return errTradeScopeConflict
			}
			target = "revoked"
		}
		view, err := scopeView(row)
		if err != nil {
			return err
		}
		if target == "approved" {
			if err := scopeLicenceCurrent(tx, p.TenantID, outlet, view.Input.LicenceID); err != nil {
				return err
			}
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		if at.Before(row.UpdatedAt) {
			at = row.UpdatedAt
		}
		view.Status = target
		view.Version++
		view.UpdatedAt = at
		if err := tx.Exec(`UPDATE pharmacy_trading_scopes SET status=?,version=?,decided_by=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, target, view.Version, p.Subject, at, p.TenantID, id, row.Version).Error; err != nil {
			return err
		}
		result = suppliertrade.PharmacyTradingScopeReceipt{OwnerTenantID: uuid.MustParse(p.TenantID), ID: command, Input: input, Result: view, ActorSubject: p.Subject, CreatedAt: at}
		request, err := json.Marshal(input)
		if err != nil {
			return err
		}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_trading_scope_commands(tenant_id,outlet_id,scope_id,id,actor_subject,input,receipt,created_at)VALUES(?,?,?,?,?,?::jsonb,?::jsonb,?)`, p.TenantID, outlet, id, command, p.Subject, string(request), string(receipt), at).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		c.JSON(status, result)
	}
}
func (h *tradingScopeHandlers) get(c *gin.Context)      { h.read(c, false) }
func (h *tradingScopeHandlers) proposal(c *gin.Context) { h.read(c, true) }
func (h *tradingScopeHandlers) read(c *gin.Context, original bool) {
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	id, ok := tradingScopeID(c)
	if !ok {
		return
	}
	var result suppliertrade.PharmacyTradingScope
	err := h.transaction(c, PermTradingScopeRead, false, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		row, err := scopeLoad(tx, p, outlet, id)
		if err != nil {
			return err
		}
		if original {
			if row.PreparedBy != p.Subject {
				return gorm.ErrRecordNotFound
			}
			err = json.Unmarshal([]byte(row.ProposalReceipt), &result)
		} else {
			result, err = scopeView(row)
		}
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trading_scope_read','pharmacy_trading_scopes',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
func (h *tradingScopeHandlers) receipt(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errTradeScopeInvalid)
		return
	}
	id, ok := tradingScopeID(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 {
		respond.NotFound(c, "scope decision")
		return
	}
	var result suppliertrade.PharmacyTradingScopeReceipt
	err = h.transaction(c, PermTradingScopeRead, false, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		var raw string
		if err := tx.Raw(`SELECT receipt FROM pharmacy_trading_scope_commands WHERE tenant_id=? AND outlet_id=? AND scope_id=? AND id=? AND actor_subject=?`, p.TenantID, outlet, id, command, p.Subject).Row().Scan(&raw); errors.Is(err, sql.ErrNoRows) {
			return gorm.ErrRecordNotFound
		} else if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trading_scope_receipt_read','pharmacy_trading_scope_commands',?)`, p.TenantID, p.Subject, command).Error
	})
	if err != nil {
		tradeScopeError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
func (h *tradingScopeHandlers) list(c *gin.Context, page pagination.Params) ([]supplierTradingScopePage, error) {
	result := []supplierTradingScopePage{}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, errTradeScopeInvalid)
			return nil, nil
		}
	}
	err := h.transaction(c, PermTradingScopeRead, false, func(_ context.Context, tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		var rows []tradingScopeRow
		q := tx.Table("pharmacy_trading_scopes").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
		if page.After != nil {
			q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			v, err := scopeView(r)
			if err != nil {
				return err
			}
			result = append(result, supplierTradingScopePage{v})
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'trading_scope_directory_read','pharmacy_outlets',?)`, p.TenantID, p.Subject, outlet).Error
	})
	if err != nil {
		tradeScopeError(c, err)
	}
	return result, err
}

func tradeScopeDatabaseConflict(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	return (pg.Code == "23505" && (pg.ConstraintName == "pharmacy_trading_scope_active_category" || pg.ConstraintName == "pharmacy_trading_scopes_pkey" || pg.ConstraintName == "pharmacy_trading_scope_commands_pkey" || pg.ConstraintName == "pharmacy_licence_revocations_pkey" || pg.ConstraintName == "pharmacy_licence_revocation_one_licence")) || (pg.Code == "23514" && (pg.Message == "current outlet jurisdiction conflicts" || pg.Message == "current reviewed licence required"))
}

func tradingScopeID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "trading scope")
		return uuid.Nil, false
	}
	return id, true
}
