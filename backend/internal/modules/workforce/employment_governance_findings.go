package workforce

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func employmentFindingGovernanceProof(p authn.Principal, hospital, staff uuid.UUID, basis EmploymentPersonFindingSearchInput, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	if staff == uuid.Nil || basis.StaffVersion == nil || *basis.StaffVersion < 1 || *basis.StaffVersion > 9007199254740990 || basis.EmploymentEpoch == nil || *basis.EmploymentEpoch < 1 || *basis.EmploymentEpoch > 9007199254740990 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := employmentDiscoveryProof(p, hospital, PermEmploymentPrepare, source)
	if err != nil {
		return nil, err
	}
	proof["purpose"], proof["staff_id"], proof["staff_version"], proof["employment_epoch"] = "employment_finding_selection", staff, *basis.StaffVersion, *basis.EmploymentEpoch
	return proof, nil
}

//go:embed employment_governance_findings.sql
var employmentGovernanceFindingsSchema string

const governanceFindingProofKey = "workforce.employment_finding.proof"
const governanceFindingDeadlineKey = "workforce.employment_finding.deadline"

func installGovernanceFindingAuthority(tx *gorm.DB, raw []byte) error {
	var installed bool
	if err := tx.Raw(`SELECT to_regprocedure('public.workforce_governance_finding_source(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('public.workforce_governance_finding_file(uuid,uuid,uuid,uuid)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.workforce_person_reviews') AND indexrelid=to_regclass('public.workforce_employment_person_selection') AND indisvalid AND indisready)
 AND NOT EXISTS(SELECT 1 FROM (VALUES('workforce_staff'),('workforce_enrollment_profile'),('workforce_person_reviews'),('workforce_person_review_evidence'),('workforce_documents'),('workforce_document_classifications'),('workforce_document_contributors')) AS required(table_name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||required.table_name) AND p.polpermissive AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid) LIKE '%workforce_governance_finding_%'))
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_audit') AND polpermissive AND polcmd='*' AND pg_get_expr(polwithcheck,polrelid) LIKE '%workforce_governance_finding_source%')
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_finding_purpose' AND NOT p.polpermissive AND p.polcmd='*' AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='governance_finding_delete' AND NOT p.polpermissive AND p.polcmd='d' AND p.polqual IS NOT NULL)))`).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return tx.Exec("SELECT set_config('app.workforce_governance_finding_authority',?,true)", string(raw)).Error
}

func (h *handlers) withGovernanceFindingSelection(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, basis EmploymentPersonFindingSearchInput, fn func(*gorm.DB, IdentitySource, []byte) error) error {
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
	deadline := minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second))
	if value, present := c.Get(governanceFindingDeadlineKey); present {
		var valid bool
		deadline, valid = value.(time.Time)
		if !valid || !deadline.After(time.Now()) {
			return authz.ErrScopedAuthorizationUnavailable
		}
	} else {
		c.Set(governanceFindingDeadlineKey, deadline)
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
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
		proof, err := employmentFindingGovernanceProof(p, hospital, staff, basis, source)
		if err != nil {
			callbackErr = err
			return err
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			callbackErr = err
			return err
		}
		if value, present := c.Get(governanceFindingProofKey); present {
			original, valid := value.([]byte)
			if !valid || !bytes.Equal(original, raw) {
				callbackErr = authz.ErrScopedCommandDenied
				return callbackErr
			}
		}
		callbackErr = h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
			if err := installGovernanceFindingAuthority(tx, raw); err != nil {
				return err
			}
			source, err := loadIdentitySource(tx, p, hospital, staff)
			if err != nil {
				return err
			}
			if source.StaffVersion != *basis.StaffVersion || source.EmploymentEpoch != *basis.EmploymentEpoch {
				return errEmploymentReviewConflict
			}
			if err := fn(tx, source, raw); err != nil {
				return err
			}
			c.Set(governanceFindingProofKey, raw)
			return nil
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

func (h *handlers) governanceEmploymentFindingCandidates(c *gin.Context, page pagination.Params) ([]EmploymentPersonFindingOption, error) {
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	basis, err := employmentFindingBasis(c)
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	c.Set(employmentFindingBasisKey, basis)
	rows := []EmploymentPersonFindingOption{}
	err = h.withGovernanceFindingSelection(c, p, hospital, staff, basis, func(tx *gorm.DB, source IdentitySource, _ []byte) error {
		return tenantdb.ApplyKeyset(tx, page).Table("workforce_person_reviews").Select("id,revision_id,hospital_id,staff_id,version,employment_epoch,identity_hash,policy_reference,finding_reference,valid_until,access_intent,declared_subject,created_at").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND status='approved' AND employment_epoch=? AND identity_hash=? AND valid_until>clock_timestamp()", p.TenantID, hospital, staff, source.EmploymentEpoch, source.IdentityHash).Find(&rows).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) authorizedGovernanceEmploymentFindings(c *gin.Context, candidates []EmploymentPersonFindingOption) ([]EmploymentPersonFindingOption, error) {
	p, hospital, staff, _, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	value, present := c.Get(employmentFindingBasisKey)
	basis, valid := value.(EmploymentPersonFindingSearchInput)
	if !present || !valid {
		employmentReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	rows := make([]EmploymentPersonFindingOption, 0, len(candidates))
	err := h.withGovernanceFindingSelection(c, p, hospital, staff, basis, func(tx *gorm.DB, source IdentitySource, proof []byte) error {
		for _, candidate := range candidates {
			_, err := employmentReviewPerson(tx, p, source, candidate.ID, candidate.RevisionID, candidate.Version)
			if errors.Is(err, errEmploymentReviewPerson) || errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			rows = append(rows, candidate)
		}
		return tx.Exec("INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_review.governance_person_selection.read',?,jsonb_build_object('authority',?::jsonb,'rows',?::integer))", p.TenantID, hospital, p.Subject, staff, string(proof), len(rows)).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
