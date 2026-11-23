package iam

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const stocktakeWorkerRead authz.Permission = "pharmacy.stocktake.read"

type stocktakeWorkerLocationRow struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	TemplateKey string
	Document    json.RawMessage
	Deadline    time.Time                   `gorm:"-"`
	Choice      StocktakeDutyLocationChoice `gorm:"-"`
}

func (r stocktakeWorkerLocationRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Choice) }
func (r stocktakeWorkerLocationRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func registerStocktakeWorkerLocationRoutes(r *platform.Router, h *operatingHandlers) {
	platform.SealedFilteredListRoute(r, "/me/pharmacy-stocktake-locations", authz.Public, 20, h.scanStocktakeWorkerLocations, h.filterStocktakeWorkerLocations)
}

func stocktakeWorkerQuery(c *gin.Context) bool {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery && c.Request.URL.RawQuery == "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		return false
	}
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 || entries[0] == "" {
			return false
		}
	}
	return true
}

func (h *operatingHandlers) scanStocktakeWorkerLocations(c *gin.Context, page pagination.Params) ([]stocktakeWorkerLocationRow, error) {
	c.Header("Cache-Control", "private, no-store")
	if !authn.Refresh(c) {
		return nil, nil
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants := c.Request.Header.Values("X-Helivanta-Expected-Tenant")
	subjects := c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your signed-in account changed. Reload before choosing a working location.")
		return nil, nil
	}
	if !stocktakeWorkerQuery(c) {
		respond.Error(c, 400, "invalid_request", "Choose one current-duty location page.")
		return nil, nil
	}
	if h.db == nil || h.operating == nil || h.locations == nil {
		operatingError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := time.Now().Add(4 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), deadline)
	defer cancel()
	rows := []stocktakeWorkerLocationRow{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
			return err
		}
		query := `SELECT d.id,d.created_at,d.template_key,s.document FROM iam_operating_duties d
JOIN iam_operating_snapshots s ON(s.tenant_id,s.duty_id,s.version,s.authority_epoch)=(d.tenant_id,d.id,d.version-1,d.authority_epoch)
JOIN iam_operating_projection_acks k ON(k.tenant_id,k.duty_id,k.version,k.authority_epoch)=(s.tenant_id,s.duty_id,s.version,s.authority_epoch)
WHERE d.tenant_id=? AND d.subject=? AND d.employer_kind='pharmacy' AND d.status='active'
AND d.template_key IN('pharmacy.stocktake_reader.v1','pharmacy.stocktake_counter.v1','pharmacy.stocktake_reviewer.v1')
AND jsonb_exists(s.document->'Permissions','pharmacy.stocktake.read')`
		args := []any{p.TenantID, p.Subject}
		if page.After != nil {
			query += ` AND(d.created_at,d.id)>(?,?)`
			args = append(args, page.After.CreatedAt, page.After.ID)
		}
		query += ` ORDER BY d.created_at,d.id LIMIT ?`
		args = append(args, page.Limit+1)
		return tx.Raw(query, args...).Scan(&rows).Error
	})
	if err != nil {
		operatingError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	for i := range rows {
		rows[i].Deadline = deadline
	}
	return rows, nil
}

func (h *operatingHandlers) filterStocktakeWorkerLocations(c *gin.Context, candidates []stocktakeWorkerLocationRow) ([]stocktakeWorkerLocationRow, error) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	rows := make([]stocktakeWorkerLocationRow, 0, len(candidates))
	if len(candidates) == 0 {
		return rows, nil
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), candidates[0].Deadline)
	defer cancel()
	for _, candidate := range candidates {
		var snapshot authz.OperatingAssignment
		template, known := operatingTemplate(candidate.TemplateKey, "pharmacy")
		if err := json.Unmarshal(candidate.Document, &snapshot); err != nil || !known || snapshot.ID != candidate.ID.String() || snapshot.TenantID != p.TenantID || snapshot.Subject != p.Subject || snapshot.EmployerKind != "pharmacy" {
			operatingError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		assigned := make([]string, 0, len(snapshot.Permissions))
		for _, permission := range snapshot.Permissions {
			assigned = append(assigned, string(permission))
		}
		if !slices.Equal(assigned, template.Permissions) {
			operatingError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		employer, employerErr := uuid.Parse(snapshot.EmployerID)
		location, locationErr := uuid.Parse(snapshot.LocationID)
		if employerErr != nil || locationErr != nil {
			operatingError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		scope := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: "pharmacy", EmployerID: employer, LocationID: location}
		calls := 0
		var callbackErr error
		err := h.operating.WithOperatingScope(ctx, authz.OperatingScopeRequest{Principal: p, EmployerKind: "pharmacy", EmployerID: employer, LocationID: location, Permission: stocktakeWorkerRead, DutyID: candidate.ID}, func(sourceCtx context.Context, grant authz.OperatingScopeGrant) error {
			calls++
			if calls != 1 || grant.Scope != scope || grant.Permission != stocktakeWorkerRead || grant.DutyID != candidate.ID || grant.SnapshotVersion != snapshot.Version || grant.AuthorityEpoch != snapshot.ScopeEpoch || grant.Identity != snapshot.Identity {
				callbackErr = authz.ErrScopedAuthorizationUnavailable
				return callbackErr
			}
			ref, err := h.locations.CurrentOperatingLocation(sourceCtx, scope)
			if err != nil {
				callbackErr = err
				return err
			}
			if ref.Scope != scope || ref.EmployerVersion != snapshot.SourceVersion || ref.LocationVersion != snapshot.LocationVersion || ref.Country != "IN" || !stocktakeWorkerText(ref.EmployerName, 200) || !stocktakeWorkerText(ref.LocationName, 200) || !stocktakeWorkerText(ref.LocationCode, 40) || !stocktakeWorkerText(ref.TimeZone, 100) {
				callbackErr = authz.ErrScopedAuthorizationUnavailable
				return callbackErr
			}
			if _, err := time.LoadLocation(ref.TimeZone); err != nil {
				callbackErr = authz.ErrScopedAuthorizationUnavailable
				return callbackErr
			}
			owner, err := uuid.Parse(p.TenantID)
			if err != nil {
				callbackErr = authz.ErrScopedAuthorizationUnavailable
				return callbackErr
			}
			callbackErr = h.db.WithTenantCommand(sourceCtx, p.TenantID, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'iam.operating.location_read','iam_operating_duties',?)`, p.TenantID, p.Subject, candidate.ID).Error
			})
			if callbackErr != nil {
				return callbackErr
			}
			candidate.Choice = StocktakeDutyLocationChoice{OwnerTenantID: owner, DutyID: candidate.ID, EmployerID: employer, LocationID: location, EmployerName: ref.EmployerName, LocationName: ref.LocationName, LocationCode: ref.LocationCode, TimeZone: ref.TimeZone, CountryCode: ref.Country, TemplateKey: template.Key, TemplateLabel: template.Label, AssignedPermissions: assigned, SnapshotVersion: grant.SnapshotVersion, AuthorityEpoch: grant.AuthorityEpoch, ExpiresAt: snapshot.ExpiresAt}
			return nil
		})
		if callbackErr != nil {
			err = callbackErr
		}
		if err == nil && calls != 1 {
			err = authz.ErrScopedAuthorizationUnavailable
		}
		if errors.Is(err, authz.ErrScopedCommandDenied) {
			continue
		}
		if err != nil {
			if errors.Is(err, authz.ErrScopedCredentialRevoked) {
				operatingError(c, err)
			} else {
				operatingError(c, authz.ErrScopedAuthorizationUnavailable)
			}
			return nil, nil
		}
		rows = append(rows, candidate)
	}
	return rows, nil
}

func stocktakeWorkerText(value string, max int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && !strings.ContainsRune(value, '\x00') && utf8.RuneCountInString(value) <= max
}
