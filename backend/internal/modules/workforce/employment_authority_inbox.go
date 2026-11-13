package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed employment_authority_inbox.sql
var employmentAuthorityInboxSchema string

func employmentInboxProof(p authn.Principal, hospital uuid.UUID, permission authz.Permission, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	proof, err := employmentDiscoveryProof(p, hospital, permission, source)
	if err != nil {
		return nil, err
	}
	proof["purpose"] = "employment_inbox"
	return proof, nil
}

func employmentAuthorityInboxInput(c *gin.Context) (authz.Permission, string, error) {
	query := c.Request.URL.Query()
	for key, values := range query {
		if len(values) != 1 || !contains(key, "mode", "status", "limit", "cursor") {
			return "", "", errIntakeProjection
		}
	}
	mode, status := "review", "pending"
	if value, present := query["mode"]; present {
		mode = value[0]
	}
	if value, present := query["status"]; present {
		status = value[0]
	}
	if !contains(mode, "prepare", "review") || !contains(status, "pending", "approved", "rejected", "withdrawn", "revoked") {
		return "", "", errIntakeProjection
	}
	permission := PermEmploymentReview
	if mode == "prepare" {
		permission = PermEmploymentPrepare
	}
	return permission, status, nil
}

func installEmploymentInboxAuthority(tx *gorm.DB, proof map[string]any) error {
	var installed bool
	if err := tx.Raw(`SELECT to_regprocedure('public.workforce_employment_inbox_source(uuid,uuid)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.workforce_employment_reviews') AND indexrelid=to_regclass('public.workforce_employment_inbox_staff') AND indisvalid AND indisready)
 AND NOT EXISTS(SELECT 1 FROM (VALUES('workforce_hospitals'),('workforce_staff'),('workforce_employment_reviews')) AS required(table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid) LIKE '%workforce_employment_inbox_source%'))
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_audit') AND polpermissive AND polcmd='*' AND pg_get_expr(polwithcheck,polrelid) LIKE '%workforce_employment_inbox_source%')
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='employment_inbox_purpose' AND NOT p.polpermissive AND p.polcmd='*' AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='employment_inbox_delete' AND NOT p.polpermissive AND p.polcmd='d' AND p.polqual IS NOT NULL)))`).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return tx.Exec("SELECT set_config('app.workforce_employment_inbox_authority',?,true)", string(raw)).Error
}

func (h *handlers) withEmploymentAuthorityInbox(c *gin.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, read func(*gorm.DB, map[string]any) error) error {
	if !authn.Refresh(c) || !p.IdleDeadline.After(time.Now()) || h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !permissions.Has(permission) {
		return authz.ErrScopedCommandDenied
	}
	capability, template := employmentReviewCapability(permission)
	if template == "" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second)))
	defer cancel()
	callbacks := 0
	var callbackErr error
	scopeHospital := hospital
	err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &scopeHospital}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || commandCtx == nil || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
			callbackErr = authz.ErrScopedAuthorizationUnavailable
			return callbackErr
		}
		proof, err := employmentInboxProof(p, hospital, permission, source)
		if err != nil {
			callbackErr = err
			return err
		}
		callbackErr = h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
			if err := installEmploymentInboxAuthority(tx, proof); err != nil {
				return err
			}
			return read(tx, proof)
		})
		return callbackErr
	})
	if callbackErr != nil {
		return callbackErr
	}
	if err == nil && callbacks != 1 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return gorm.ErrRecordNotFound
	}
	return err
}

func (h *handlers) listEmploymentAuthorityInbox(c *gin.Context, page pagination.Params) ([]EmploymentReviewSummary, error) {
	p, hospital, ok := personBrowseParams(c)
	if !ok {
		return nil, nil
	}
	permission, status, err := employmentAuthorityInboxInput(c)
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	rows := []EmploymentReviewSummary{}
	err = h.withEmploymentAuthorityInbox(c, p, hospital, permission, func(tx *gorm.DB, proof map[string]any) error {
		query := tx.Table("workforce_employment_reviews r").Select("r.id,r.hospital_id,r.staff_id,s.employee_code,s.display_name,r.status,r.version,r.created_at").Joins("JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(r.tenant_id,r.hospital_id,r.staff_id)").Where("r.tenant_id=? AND r.hospital_id=? AND r.status=?", p.TenantID, hospital, status)
		if page.After != nil {
			query = query.Where("(r.created_at,r.id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Order("r.created_at DESC,r.id DESC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_authority_inbox.read',?,jsonb_build_object('authority',?::jsonb,'rows',?::integer,'status',?::text))", p.TenantID, hospital, p.Subject, hospital, string(raw), min(len(rows), page.Limit), status).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
