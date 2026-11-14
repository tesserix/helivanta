package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed operating_staff_choices.sql
var operatingStaffChoicesSchema string

type operatingStaffChoiceRow struct {
	Choice    workforcecontract.OperatingGovernanceStaffChoice `gorm:"embedded"`
	CreatedAt time.Time
}

func (row operatingStaffChoiceRow) MarshalJSON() ([]byte, error) { return json.Marshal(row.Choice) }
func (row operatingStaffChoiceRow) PageKey() (time.Time, uuid.UUID) {
	return row.CreatedAt, row.Choice.StaffID
}

func operatingStaffChoiceProof(p authn.Principal, root establishment, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	now := time.Now()
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != p.TenantID || !validLoginSubject(p.Subject) || root.id.Version() != 7 || root.id.Variant() != uuid.RFC4122 || (root.kind != pharmacyEstablishmentKind && root.kind != supplierEstablishmentKind) || p.AuthTime.IsZero() || p.AuthTime.After(now) || !p.IdleDeadline.After(now) || !source.ExpiresAt.After(now) || !validWorkforceGovernanceSource(p, root, source, "iam.operating_access_reviewer.v1") {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof := map[string]any{"purpose": "staff_choices", "tenant_id": p.TenantID, "employer_id": root.id, "employer_kind": root.kind, "actor_subject": p.Subject, "capability": authz.GovernanceOperatingManage, "authority_kind": source.Kind, "membership_epoch": source.MembershipEpoch, "owner_count": source.OwnerCount, "source_expires_at": source.ExpiresAt, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline}
	if source.Kind == authz.GovernanceOwnerSource {
		proof["owner_membership_id"] = source.OwnerMembershipID
	} else {
		proof["source_grant_id"], proof["source_grant_version"], proof["template_key"] = source.GrantID, source.Version, source.TemplateKey
	}
	return proof, nil
}

func installOperatingStaffChoices(tx *gorm.DB, proof map[string]any) error {
	var available bool
	if err := tx.Raw(`SELECT CASE WHEN to_regprocedure('public.workforce_operating_selection_support()') IS NULL THEN false ELSE public.workforce_operating_selection_support() END`).Scan(&available).Error; err != nil {
		return err
	}
	if !available {
		return authz.ErrScopedAuthorizationUnavailable
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return tx.Exec(`SELECT set_config('app.workforce_operating_selection',?,true)`, string(raw)).Error
}

func operatingStaffChoiceQuery(c *gin.Context) bool {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if !contains(key, "status", "limit", "cursor") || len(values) != 1 || values[0] == "" || key == "status" && values[0] != "active" {
			return false
		}
	}
	return true
}

func (h *handlers) listOperatingStaffChoices(c *gin.Context, page pagination.Params) ([]operatingStaffChoiceRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before selecting Staff.")
		return nil, nil
	}
	if !operatingStaffChoiceQuery(c) {
		respond.Error(c, 400, "invalid_request", "Choose one active Staff page without extra filters.")
		return nil, nil
	}
	root := establishment{kind: c.Param("kind")}
	id, err := uuid.Parse(c.Param("employer"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param("employer") || (root.kind != pharmacyEstablishmentKind && root.kind != supplierEstablishmentKind) {
		respond.NotFound(c, "employer")
		return nil, nil
	}
	root.id = id
	if !authn.Refresh(c) {
		return nil, nil
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before selecting Staff.")
		return nil, nil
	}
	p = current
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return nil, nil
	}
	if !permissions.Has(authz.Permission(authz.GovernanceOperatingManage)) {
		respond.Forbidden(c, "Independent employer duty administration is required.")
		return nil, nil
	}
	if h.db == nil || h.governanceSources == nil || p.IdleDeadline.IsZero() {
		operatingDiscoveryError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second)))
	defer cancel()
	rows := []operatingStaffChoiceRow{}
	callbacks := 0
	var callbackErr error
	err = h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: authz.GovernanceOperatingManage, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: root.kind, ID: &root.id}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
			callbackErr = authz.ErrScopedAuthorizationUnavailable
			return callbackErr
		}
		proof, proofErr := operatingStaffChoiceProof(p, root, source)
		if proofErr != nil {
			callbackErr = proofErr
			return callbackErr
		}
		callbackErr = h.transaction(commandCtx, p, root.id, func(tx *gorm.DB) (err error) {
			if err := root.requireKind(tx, p.TenantID); err != nil {
				return err
			}
			var previous string
			if err := tx.Raw(`SELECT COALESCE(current_setting('app.workforce_operating_selection',true),'')`).Scan(&previous).Error; err != nil {
				return err
			}
			if err := installOperatingStaffChoices(tx, proof); err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config('app.workforce_operating_selection',?,true)`, previous).Error)
			}()
			raw, err := json.Marshal(proof)
			if err != nil {
				return err
			}

			query := `SELECT s.id AS staff_id,s.display_name,s.version AS staff_version,l.subject,l.id AS link_id,l.version AS link_version,s.created_at FROM workforce_staff s JOIN workforce_login_links l ON(l.tenant_id,l.hospital_id,l.staff_id)=(s.tenant_id,s.hospital_id,s.id) WHERE s.tenant_id=? AND s.hospital_id=? AND s.employment_status='active' AND l.status='active' AND l.employment_generation_id IS NULL AND l.independent_staff_version=s.version AND l.version>=2`
			args := []any{p.TenantID, root.id}
			if page.After != nil {
				query += ` AND(s.created_at,s.id)<(?,?)`
				args = append(args, page.After.CreatedAt, page.After.ID)
			}
			query += ` ORDER BY s.created_at DESC,s.id DESC LIMIT ? FOR SHARE OF s,l`
			args = append(args, page.Limit+1)
			if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
				return err
			}
			audits := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				value := row.Choice
				if value.StaffID == uuid.Nil || value.StaffID.Variant() != uuid.RFC4122 || value.LinkID.Version() != 7 || !validBenefitText(value.DisplayName, 1, 150) || !validLoginSubject(value.Subject) || value.StaffVersion < 1 || value.StaffVersion > 9007199254740991 || value.LinkVersion < 2 || value.LinkVersion > 9007199254740991 {
					return authz.ErrScopedAuthorizationUnavailable
				}
				audits = append(audits, map[string]any{"staff_id": value.StaffID, "staff_version": value.StaffVersion, "link_id": value.LinkID, "link_version": value.LinkVersion})
			}
			if len(audits) == 0 {
				return nil
			}
			batch, err := json.Marshal(audits)
			if err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 SELECT ?::uuid,?::uuid,?::text,'workforce.operating_staff_choice.read',v.staff_id,jsonb_build_object('authority',?::jsonb,'staff_version',v.staff_version,'link_id',v.link_id,'link_version',v.link_version)
 FROM jsonb_to_recordset(?::jsonb)AS v(staff_id uuid,staff_version bigint,link_id uuid,link_version bigint)`, p.TenantID, root.id, p.Subject, string(raw), string(batch)).Error
		})
		return callbackErr
	})
	if callbackErr != nil {
		err = callbackErr
	}
	if err == nil && callbacks != 1 {
		err = authz.ErrScopedAuthorizationUnavailable
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, authz.ErrGovernanceScopeHidden) || errors.Is(err, authz.ErrScopedCommandDenied) {
			respond.NotFound(c, "employer")
		} else {
			operatingDiscoveryError(c, err)
		}
		return nil, nil
	}
	return rows, nil
}
