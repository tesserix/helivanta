package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"maps"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed employment_document_preparation.sql
var employmentDocumentPreparationSchema string

type employmentPreparationDocuments struct {
	h                     *handlers
	principal             authn.Principal
	hospital, staff, file uuid.UUID
	version, epoch        int64
	operation             documentPreparationOperation
	phase                 int64
	original, active      map[string]any
}

func (g *employmentPreparationDocuments) permissions(bool) (authz.Permission, authz.Permission) {
	return PermEmploymentPrepare, authz.Public
}
func (g *employmentPreparationDocuments) view(d document) any { return d }
func (g *employmentPreparationDocuments) downloadAction() string {
	return "workforce.employment_document.downloaded"
}
func (g *employmentPreparationDocuments) owns(_ *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, kind string, record uuid.UUID) error {
	if p.TenantID != g.principal.TenantID || p.Subject != g.principal.Subject || hospital != g.hospital || staff != g.staff || kind != "enrollment" || record != staff {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func (g *employmentPreparationDocuments) authorize(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, _ bool) error {
	if err := g.owns(tx, p, hospital, staff, "enrollment", staff); err != nil {
		return err
	}
	var valid bool
	if err := tx.Raw("SELECT workforce_governance_document_source(?::uuid,?::uuid,?::uuid)", p.TenantID, hospital, staff).Scan(&valid).Error; err != nil {
		return err
	}
	if !valid {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return (establishment{kind: hospitalEstablishmentKind, id: hospital}).requireKind(tx, p.TenantID)
}
func (g *employmentPreparationDocuments) bind(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, kind string, record uuid.UUID) (int64, error) {
	if err := g.owns(tx, p, hospital, staff, kind, record); err != nil {
		return 0, err
	}
	return g.version, nil
}
func (g *employmentPreparationDocuments) transaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, fn func(*gorm.DB) error) error {
	if p.TenantID != g.principal.TenantID || p.Subject != g.principal.Subject || hospital != g.hospital {
		return authz.ErrScopedAuthorizationUnavailable
	}
	g.phase++
	return g.h.withEmploymentPreparationSource(c, p, hospital, func(ctx context.Context, source authz.GovernanceSourceEvidence) error {
		proof, err := employmentDocumentProof(p, hospital, g.staff, g.file, g.version, g.epoch, g.operation, source)
		if err != nil {
			return err
		}
		proof["phase"] = g.phase
		if g.original != nil && !sameEmploymentPreparationSource(g.original, proof) {
			return authz.ErrScopedCommandDenied
		}
		if g.original == nil {
			g.original = maps.Clone(proof)
		}
		g.active = proof
		return g.h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			if err := installEmploymentDocument(tx, proof); err != nil {
				return err
			}
			if err := (establishment{kind: hospitalEstablishmentKind, id: hospital}).requireKind(tx, p.TenantID); err != nil {
				return err
			}
			var current struct{ Version, EmploymentEpoch int64 }
			if err := tx.Table("workforce_staff").Select("version,employment_epoch").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, g.staff).Take(&current).Error; err != nil {
				return err
			}
			if current.Version != g.version || current.EmploymentEpoch != g.epoch {
				return errEmploymentDocumentBasis
			}
			if err := fn(tx); err != nil {
				return err
			}
			resource, action := g.file, "workforce.employment_document.phase"
			if g.operation == documentPreparationList {
				resource, action = g.staff, "workforce.employment_document.list"
			}
			return g.audit(tx, resource, action)
		})
	})
}
func (g *employmentPreparationDocuments) audit(tx *gorm.DB, resource uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('authority',NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb,'version',?::bigint))`, g.principal.TenantID, g.hospital, g.principal.Subject, action, resource, g.version).Error
}
func (g *employmentPreparationDocuments) prepareMutation(tx *gorm.DB, mutation documentMutation) error {
	if g.active == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	var input documentInput
	if mutation.Input != nil {
		input = *mutation.Input
	} else if mutation.Saved != nil {
		row := mutation.Saved
		input = documentInput{RecordKind: row.RecordKind, RecordID: row.RecordID, Category: row.Category, Filename: row.Filename, ContentType: row.ContentType, Size: row.Size, SHA256: row.SHA256}
	} else {
		return authz.ErrScopedAuthorizationUnavailable
	}
	var digest string
	if err := tx.Raw("SELECT workforce_governance_document_hash(?,?,?, ?,?,?,?)", input.RecordKind, input.RecordID, input.Category, input.Filename, input.ContentType, input.Size, input.SHA256).Scan(&digest).Error; err != nil {
		return err
	}
	proof, err := employmentDocumentMutationProof(g.active, mutation, digest)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err := tx.Exec("SELECT set_config('app.workforce_governance_document_authority',?,true)", string(raw)).Error; err != nil {
		return err
	}
	g.active = proof
	return nil
}
func installEmploymentDocument(tx *gorm.DB, proof map[string]any) error {
	var installed bool
	err := tx.Raw(`SELECT to_regprocedure('public.workforce_governance_document_source(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_document_write(workforce_documents)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_document_hash(text,uuid,text,text,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_document_audit(uuid,uuid,text,text,uuid,jsonb)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_staff') AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polwithcheck IS NOT NULL AND pg_get_expr(polqual,polrelid) LIKE '%workforce_governance_document_source%')
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_document_creation',7),('workforce_document_identity',19),('workforce_document_contributor_record',21),('workforce_documents_audit',29),('workforce_governance_document_provenance',21)) required(trigger_name,trigger_type) WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.workforce_documents') AND tgname=required.trigger_name AND tgtype=required.trigger_type AND tgfoid=to_regprocedure('public.'||CASE required.trigger_name WHEN 'workforce_documents_audit' THEN 'audit_record_change' ELSE required.trigger_name END||'()') AND NOT tgisinternal AND tgenabled IN('O','A')))
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.workforce_document_contributors') AND tgname='workforce_document_contributor_guard' AND tgtype=31 AND tgfoid=to_regprocedure('public.workforce_document_contributor_guard()') AND NOT tgisinternal AND tgenabled IN('O','A'))
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.workforce_audit') AND tgname='workforce_audit_immutable' AND tgtype=58 AND tgfoid=to_regprocedure('public.workforce_audit_immutable()') AND NOT tgisinternal AND tgenabled IN('O','A'))
 AND EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.workforce_documents') AND indexrelid=to_regclass('public.workforce_documents_page') AND indisvalid AND indisready)
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_hospitals'),('workforce_staff'),('workforce_documents'),('workforce_document_contributors')) required(table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid) LIKE '%workforce_governance_document_%'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_documents'),('workforce_document_contributors'),('workforce_audit')) required(table_name) WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polwithcheck,p.polrelid) LIKE '%workforce_governance_document_%'))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_document_purpose' AND NOT p.polpermissive AND p.polcmd='*' AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_document_delete' AND NOT p.polpermissive AND p.polcmd='d' AND p.polqual IS NOT NULL)))`).Scan(&installed).Error
	if err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err := tx.Exec("SELECT set_config('app.workforce_governance_document_authority',?,true)", string(raw)).Error; err != nil {
		return err
	}
	var valid bool
	if err := tx.Raw("SELECT workforce_governance_document_source(?::uuid,?::uuid,?::uuid)", proof["tenant_id"], proof["hospital_id"], proof["staff_id"]).Scan(&valid).Error; err != nil {
		return err
	}
	if !valid {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return nil
}
