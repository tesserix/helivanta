package workforce

import (
	"context"
	_ "embed"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"

	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type employmentPreparationOperation string

const (
	preparationTargets employmentPreparationOperation = "targets"
	preparationSource  employmentPreparationOperation = "source"
	preparationList    employmentPreparationOperation = "list"
	preparationRead    employmentPreparationOperation = "read"
	preparationHistory employmentPreparationOperation = "history"
	preparationSave    employmentPreparationOperation = "save"
)

func employmentPreparationProof(p authn.Principal, hospital, staff, period uuid.UUID, operation employmentPreparationOperation, input *EmploymentPeriodDraftInput, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	if (operation == preparationTargets) != (staff == uuid.Nil) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	switch operation {
	case preparationTargets:
		if period != uuid.Nil || input != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case preparationSource, preparationList:
		if period != uuid.Nil || input != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case preparationRead, preparationHistory:
		if period.Version() != 7 || input != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case preparationSave:
		if period.Version() != 7 || input == nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		normalized := *input
		if !normalizeEmploymentPeriod(&normalized, staff) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		raw, err := json.Marshal(normalized)
		original, originalErr := json.Marshal(input)
		if err != nil || originalErr != nil || !bytes.Equal(raw, original) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := employmentDiscoveryProof(p, hospital, PermEmploymentPrepare, source)
	if err != nil {
		return nil, err
	}
	proof["purpose"], proof["operation"] = "employment_preparation", operation
	if staff != uuid.Nil {
		proof["staff_id"] = staff
	}
	if period != uuid.Nil {
		proof["period_id"] = period
	}
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(raw)
		proof["command_id"], proof["expected_version"], proof["staff_version"], proof["employment_epoch"], proof["input_hash"] = input.CommandID, *input.ExpectedVersion, input.StaffVersion, input.EmploymentEpoch, hex.EncodeToString(digest[:])
		if input.SponsorID != "" {
			proof["sponsor_id"] = input.SponsorID
		}
	}
	return proof, nil
}

func sameEmploymentPreparationSource(original, current map[string]any) bool {
	for _, key := range []string{"purpose", "operation", "tenant_id", "hospital_id", "actor_subject", "capability", "authority_kind", "owner_membership_id", "source_grant_id", "source_grant_version", "template_key", "membership_epoch", "owner_count", "staff_id", "period_id", "command_id", "expected_version", "staff_version", "employment_epoch", "input_hash", "sponsor_id"} {
		a, aErr := json.Marshal(original[key])
		b, bErr := json.Marshal(current[key])
		if aErr != nil || bErr != nil || !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}

//go:embed employment_preparation_authority.sql
var employmentPreparationSchema string

func installEmploymentPreparation(tx *gorm.DB, proof map[string]any) error {
	var installed bool
	if err := tx.Raw(`SELECT to_regprocedure('public.workforce_governance_draft_source(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_draft_staff_read(uuid,uuid,uuid,text)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_staff') AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polcmd='w' AND polwithcheck IS NOT NULL AND pg_get_expr(polqual,polrelid) LIKE '%workforce_governance_draft_staff_read%')
 AND to_regprocedure('public.workforce_governance_draft_write(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,text,text,text)') IS NOT NULL
 AND pg_get_functiondef(to_regprocedure('public.workforce_employment_period_guard()')) LIKE '%workforce_governance_draft_write%'
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.workforce_employment_period_history') AND tgname='workforce_governance_draft_provenance' AND tgfoid=to_regprocedure('public.workforce_governance_draft_provenance()') AND tgtype=5 AND NOT tgisinternal AND tgenabled IN('O','A'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_period_drafts','workforce_employment_period_guard',31),('workforce_employment_period_drafts','workforce_employment_period_snapshot',21),('workforce_employment_period_history','workforce_employment_period_history_guard',31)) required(table_name,trigger_name,trigger_type)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.'||required.table_name) AND tgname=required.trigger_name AND tgfoid=to_regprocedure('public.'||required.trigger_name||'()') AND tgtype=required.trigger_type AND NOT tgisinternal AND tgenabled IN('O','A')))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_hospitals'),('workforce_staff'),('workforce_employment_period_drafts'),('workforce_employment_period_history'),('workforce_audit')) required(table_name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid) LIKE '%workforce_governance_draft_%'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_period_drafts'),('workforce_employment_period_history'),('workforce_audit')) required(table_name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polwithcheck,p.polrelid) LIKE '%workforce_governance_draft_%'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_period_drafts_page','workforce_employment_period_drafts'),('workforce_employment_period_history_page','workforce_employment_period_history'),('workforce_employment_preparation_receipt','workforce_audit')) required(index_name,table_name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.'||required.table_name) AND indexrelid=to_regclass('public.'||required.index_name) AND indisvalid AND indisready))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_draft_purpose' AND NOT p.polpermissive AND p.polcmd='*' AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_draft_delete' AND NOT p.polpermissive AND p.polcmd='d' AND p.polqual IS NOT NULL)))`).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	selectedStaff := ""
	if value, present := proof["staff_id"]; present {
		staff, ok := value.(uuid.UUID)
		if !ok || staff == uuid.Nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		selectedStaff = staff.String()
	}
	return tx.Exec("SELECT set_config('app.workforce_governance_draft_authority',?,true),set_config('app.workforce_identity_staff',?,true)", string(raw), selectedStaff).Error
}

func requireEmploymentPreparationReceipt(tx *gorm.DB, p authn.Principal, hospital, staff, period uuid.UUID, input EmploymentPeriodDraftInput, proof map[string]any) error {
	var receipt struct{ ID uuid.UUID }
	err := tx.Table("workforce_employment_period_history").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND revision_id=?", p.TenantID, hospital, staff, period, input.CommandID).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var retained struct{ Authority json.RawMessage }
	err = tx.Raw("SELECT changes->'authority' AS authority FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.employment_period.governance_revision' LIMIT 1", p.TenantID, hospital, input.CommandID).Take(&retained).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errEmploymentPeriodConflict
	}
	if err != nil {
		return err
	}
	var original map[string]any
	if json.Unmarshal(retained.Authority, &original) != nil || !sameEmploymentPreparationSource(original, proof) {
		return errEmploymentPeriodConflict
	}
	return nil
}

func (h *handlers) withEmploymentPreparation(c *gin.Context, p authn.Principal, hospital, staff, period uuid.UUID, operation employmentPreparationOperation, input *EmploymentPeriodDraftInput, fn func(*gorm.DB) error) error {
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		return errIntakeWorkspaceRequired
	}
	if !authn.Refresh(c) || !p.IdleDeadline.After(time.Now()) || h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !permissions.Has(PermEmploymentPrepare) {
		return authz.ErrScopedCommandDenied
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second)))
	defer cancel()
	callbacks := 0
	var callbackErr error
	selectedHospital := hospital
	err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: authz.GovernanceEmploymentPrepare, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &selectedHospital}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || commandCtx == nil || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
			callbackErr = authz.ErrScopedAuthorizationUnavailable
			return callbackErr
		}
		proof, err := employmentPreparationProof(p, hospital, staff, period, operation, input, source)
		if err != nil {
			callbackErr = err
			return err
		}
		callbackErr = h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
			if err := installEmploymentPreparation(tx, proof); err != nil {
				return err
			}
			var selectedStaff any
			if staff != uuid.Nil {
				selectedStaff = staff
			}
			var installedProof bool
			if err := tx.Raw("SELECT workforce_governance_draft_source(?::uuid,?::uuid,?::uuid)", p.TenantID, hospital, selectedStaff).Scan(&installedProof).Error; err != nil {
				return err
			}
			if !installedProof {
				return authz.ErrScopedAuthorizationUnavailable
			}

			if input != nil {
				if err := requireEmploymentPreparationReceipt(tx, p, hospital, staff, period, *input, proof); err != nil {
					return err
				}
			}
			return fn(tx)
		})
		return callbackErr
	})
	if callbackErr != nil {
		err = callbackErr
	}
	if err == nil && callbacks != 1 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return gorm.ErrRecordNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && (pg.ConstraintName == "workforce_employment_period_receipt" || pg.ConstraintName == "workforce_employment_period_drafts_pkey") {
		return errEmploymentPeriodConflict
	}
	return err
}
