package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"maps"
)

//go:embed employment_binding_preparation.sql
var employmentBindingPreparationSchema string

type employmentPreparationBindings struct {
	h         *handlers
	principal authn.Principal
	hospital  uuid.UUID
	selected  employmentBindingSelection
	command   *employmentBindingCommand
	original  map[string]any
	phase     int64
}

func (g *employmentPreparationBindings) prepare(action string, value any, digest string) error {
	if string(g.selected.Operation) != action {
		return authz.ErrScopedAuthorizationUnavailable
	}
	command := &employmentBindingCommand{Digest: digest}
	switch input := value.(type) {
	case EmploymentEvidenceAttachInput:
		if action != "attach" {
			return authz.ErrScopedAuthorizationUnavailable
		}
		command.ID, command.Document = input.CommandID, input.DocumentID
	case EmploymentEvidenceWithdrawInput:
		if action != "withdraw" || input.ExpectedVersion == nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		command.ID, command.ExpectedVersion = input.CommandID, *input.ExpectedVersion
	default:
		return authz.ErrScopedAuthorizationUnavailable
	}
	g.command = command
	return nil
}
func (g *employmentPreparationBindings) transaction(c *gin.Context, fn func(*gorm.DB) error) error {
	g.phase++
	return g.h.withEmploymentPreparationSource(c, g.principal, g.hospital, func(ctx context.Context, source authz.GovernanceSourceEvidence) error {
		proof, err := employmentBindingProof(g.principal, g.hospital, g.selected, g.command, source)
		if err != nil {
			return err
		}
		if g.original != nil && !sameEmploymentBindingSource(g.original, proof) {
			return authz.ErrScopedCommandDenied
		}
		if g.original == nil {
			g.original = maps.Clone(proof)
		}
		proof["phase"] = g.phase
		return g.h.transaction(ctx, g.principal, g.hospital, func(tx *gorm.DB) error {
			if err := installEmploymentBinding(tx, proof); err != nil {
				return err
			}
			if err := (establishment{kind: hospitalEstablishmentKind, id: g.hospital}).requireKind(tx, g.principal.TenantID); err != nil {
				return err
			}
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, g.principal.TenantID, g.hospital).Error; err != nil {
				return err
			}
			var current struct{ Version, EmploymentEpoch int64 }
			if err := tx.Table("workforce_staff").Select("version,employment_epoch").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", g.principal.TenantID, g.hospital, g.selected.Staff).Take(&current).Error; err != nil {
				return err
			}
			if current.Version != g.selected.StaffVersion || current.EmploymentEpoch != g.selected.EmploymentEpoch {
				return errEmploymentEvidenceConflict
			}
			if g.command != nil {
				var receipt struct{ ID uuid.UUID }
				err := tx.Table("workforce_employment_evidence_history").Select("id").Where("tenant_id=? AND hospital_id=? AND revision_id=?", g.principal.TenantID, g.hospital, g.command.ID).Take(&receipt).Error
				if err == nil {
					var saved struct{ Authority json.RawMessage }
					err = tx.Raw("SELECT changes->'authority' AS authority FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.employment_binding.revision' LIMIT 1", g.principal.TenantID, g.hospital, g.command.ID).Take(&saved).Error
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return errEmploymentEvidenceConflict
					}
					if err != nil {
						return err
					}
					var original map[string]any
					if json.Unmarshal(saved.Authority, &original) != nil || !sameEmploymentBindingSource(original, proof) {
						return errEmploymentEvidenceConflict
					}
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_binding.phase',?,jsonb_build_object('authority',NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb))`, g.principal.TenantID, g.hospital, g.principal.Subject, g.selected.Period).Error
		})
	})
}
func installEmploymentBinding(tx *gorm.DB, proof map[string]any) error {
	var supported bool
	err := tx.Raw(`SELECT to_regprocedure('public.workforce_governance_binding_source(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_binding_write(workforce_employment_evidence_bindings)') IS NOT NULL
 AND pg_get_functiondef(to_regprocedure('public.workforce_employment_evidence_guard()')) LIKE '%workforce_governance_binding_write%'
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_staff') AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polwithcheck IS NOT NULL AND pg_get_expr(polqual,polrelid) LIKE '%workforce_governance_binding_source%')
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_evidence_bindings','workforce_employment_evidence_guard',31),('workforce_employment_evidence_bindings','workforce_employment_evidence_snapshot',21),('workforce_employment_evidence_history','workforce_employment_evidence_history_guard',31),('workforce_employment_evidence_history','workforce_governance_binding_provenance',5),('workforce_documents','workforce_document_identity',19),('workforce_document_contributors','workforce_document_contributor_guard',31),('workforce_audit','workforce_audit_immutable',58)) required(table_name,trigger_name,trigger_type) WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.'||required.table_name) AND tgname=required.trigger_name AND tgtype=required.trigger_type AND tgfoid=to_regprocedure('public.'||required.trigger_name||'()') AND NOT tgisinternal AND tgenabled IN('O','A')))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_evidence_page','workforce_employment_evidence_bindings'),('workforce_employment_evidence_receipt','workforce_employment_evidence_history'),('workforce_employment_evidence_history_page','workforce_employment_evidence_history'),('workforce_employment_preparation_receipt','workforce_audit')) required(index_name,table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.'||required.table_name) AND indexrelid=to_regclass('public.'||required.index_name) AND indisvalid AND indisready))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_hospitals'),('workforce_staff'),('workforce_employment_period_drafts'),('workforce_employment_period_history'),('workforce_documents'),('workforce_employment_evidence_bindings'),('workforce_employment_evidence_history'),('workforce_audit')) required(table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid) LIKE '%workforce_governance_binding_%'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_employment_evidence_bindings'),('workforce_employment_evidence_history'),('workforce_audit')) required(table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polwithcheck,p.polrelid) LIKE '%workforce_governance_binding_%'))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_binding_purpose' AND NOT p.polpermissive AND p.polcmd='*' AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_binding_delete' AND NOT p.polpermissive AND p.polcmd='d' AND p.polqual IS NOT NULL)))`).Scan(&supported).Error
	if err != nil {
		return err
	}
	if !supported {
		return authz.ErrScopedAuthorizationUnavailable
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err := tx.Exec("SELECT set_config('app.workforce_governance_binding_authority',?,true)", string(raw)).Error; err != nil {
		return err
	}
	var installed bool
	if err := tx.Raw("SELECT workforce_governance_binding_source(?::uuid,?::uuid,?::uuid)", proof["tenant_id"], proof["hospital_id"], proof["staff_id"]).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return nil
}
