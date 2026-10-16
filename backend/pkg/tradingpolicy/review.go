package tradingpolicy

import (
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
)

var ErrIndependentReview = errors.New("independent country policy review required")

// ChangeAccess records explicit scope delegation; route permission is still required.
func ChangeAccess(tx *gorm.DB, tenant, actor, scopeID string, in AccessChange, now time.Time) (Receipt, error) {
	if !versionID(in.ID) || !versionID(scopeID) || in.ExpectedRevision < 1 || in.ExpectedRevision > maxPolicyRevision || !text(in.Subject, 200) || (!in.Revoke && !slices.Contains([]string{"reader", "editor", "reviewer"}, in.Role)) || (in.Revoke && in.Role != "") {
		return Receipt{}, ErrInvalid
	}
	scope, err := loadScope(tx, tenant, scopeID, true)
	if err != nil {
		return Receipt{}, err
	}
	if scope.OwnerSubject != actor {
		return Receipt{}, gorm.ErrRecordNotFound
	}
	hash, err := requestHash(struct {
		ScopeID string
		Change  AccessChange
	}{scopeID, in})
	if err != nil {
		return Receipt{}, err
	}
	prior, err := replay(tx, tenant, actor, in.ID, hash)
	if err != nil {
		return Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	if scope.Revision != in.ExpectedRevision {
		return Receipt{}, ErrConflict
	}
	action := "access_granted"
	if in.Revoke {
		action = "access_revoked"
		err = tx.Exec(`DELETE FROM workspace_country_policy_access WHERE tenant_id=? AND scope_id=? AND subject=?`, tenant, scopeID, in.Subject).Error
	} else {
		err = tx.Exec(`INSERT INTO workspace_country_policy_access(tenant_id,scope_id,subject,role) VALUES(?,?,?,?) ON CONFLICT(tenant_id,scope_id,subject) DO UPDATE SET role=EXCLUDED.role,updated_at=now()`, tenant, scopeID, in.Subject, in.Role).Error
	}
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ID: in.ID, TenantID: tenant, ScopeID: scopeID, ActorSubject: actor, Action: action, Reason: in.Subject + ":" + in.Role, ExpectedRevision: in.ExpectedRevision, ResultRevision: in.ExpectedRevision + 1, RequestHash: hash, CreatedAt: now.UTC().Truncate(time.Microsecond)}
	return receipt, record(tx, receipt)
}

func loadVersion(tx *gorm.DB, scope PolicyScope, id string) (Version, error) {
	var row struct {
		ID, AuthorSubject             string
		EffectiveFrom, EffectiveUntil time.Time
	}
	err := tx.Table("workspace_country_policy_versions").Select("id,author_subject,effective_from,effective_until").Where("tenant_id=? AND scope_id=? AND id=?", scope.TenantID, scope.ID, id).Take(&row).Error
	if err != nil {
		return Version{}, err
	}
	out := Version{ID: row.ID, Scope: Scope{Tenant: scope.TenantID, Country: scope.Country, Jurisdiction: scope.Jurisdiction, Category: scope.Category, Operation: scope.Operation, Currency: scope.Currency}, Author: row.AuthorSubject, SubmittedBy: row.AuthorSubject, State: "submitted", EffectiveFrom: row.EffectiveFrom.UTC(), EffectiveUntil: row.EffectiveUntil.UTC()}
	if err := tx.Table("workspace_country_policy_sources").Select("key,title,url,reviewed_at").Where("tenant_id=? AND version_id=?", scope.TenantID, id).Order("key").Find(&out.Sources).Error; err != nil {
		return Version{}, err
	}
	for i := range out.Sources {
		out.Sources[i].ReviewedAt = out.Sources[i].ReviewedAt.UTC()
	}
	if err := tx.Table("workspace_country_policy_requirements").Select("domain,applicability,reason").Where("tenant_id=? AND version_id=?", scope.TenantID, id).Order("domain").Find(&out.Requirements).Error; err != nil {
		return Version{}, err
	}
	var citations []struct{ Domain, SourceKey string }
	if err := tx.Table("workspace_country_policy_citations").Select("domain,source_key").Where("tenant_id=? AND version_id=?", scope.TenantID, id).Order("domain,source_key").Find(&citations).Error; err != nil {
		return Version{}, err
	}
	keys := make(map[string][]string)
	for _, citation := range citations {
		keys[citation.Domain] = append(keys[citation.Domain], citation.SourceKey)
	}
	for i := range out.Requirements {
		out.Requirements[i].SourceKeys = keys[out.Requirements[i].Domain]
	}
	var decision Receipt
	err = tx.Select("action,actor_subject,created_at").Where("tenant_id=? AND version_id=? AND action IN ?", scope.TenantID, id, []string{"approved", "rejected"}).Take(&decision).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return Version{}, err
	}
	if err == nil {
		out.State = decision.Action
		out.ApprovedBy = decision.ActorSubject
		out.ApprovedAt = decision.CreatedAt.UTC()
	}
	return out, nil
}

// ReadVersion returns an immutable policy with its citations and review provenance.
func ReadVersion(tx *gorm.DB, tenant, actor, scopeID, id string) (Version, error) {
	scope, err := ReadScope(tx, tenant, actor, scopeID)
	if err != nil {
		return Version{}, err
	}
	return loadVersion(tx, scope, id)
}

// Decide applies independent review or suspension while locking the policy scope.
func Decide(tx *gorm.DB, tenant, actor, scopeID string, in Review, now time.Time) (Receipt, error) {
	now = now.UTC().Truncate(time.Microsecond)
	if !versionID(in.ID) || !versionID(scopeID) || in.ExpectedRevision < 1 || in.ExpectedRevision > maxPolicyRevision || !text(in.Reason, 4000) || !slices.Contains([]string{"approved", "rejected", "suspended"}, in.Action) || (in.Action != "suspended" && !versionID(in.VersionID)) {
		return Receipt{}, ErrInvalid
	}
	scope, err := loadScope(tx, tenant, scopeID, true)
	if err != nil {
		return Receipt{}, err
	}
	if err := scopeAccess(tx, scope, actor, "reviewer"); err != nil {
		return Receipt{}, err
	}
	hash, err := requestHash(struct {
		ScopeID string
		Review  Review
	}{scopeID, in})
	if err != nil {
		return Receipt{}, err
	}
	prior, err := replay(tx, tenant, actor, in.ID, hash)
	if err != nil {
		return Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	if scope.Revision != in.ExpectedRevision {
		return Receipt{}, ErrConflict
	}
	var policyVersion *string
	if in.Action == "suspended" {
		if scope.Status != "active" || in.VersionID != "" {
			return Receipt{}, ErrConflict
		}
		policyVersion = scope.CurrentVersion
		err = tx.Exec(`UPDATE workspace_country_policy_scopes SET status='suspended' WHERE tenant_id=? AND id=?`, tenant, scopeID).Error
	} else {
		policy, loadErr := loadVersion(tx, scope, in.VersionID)
		if loadErr != nil {
			return Receipt{}, loadErr
		}
		if policy.State != "submitted" {
			return Receipt{}, ErrConflict
		}
		if policy.Author == actor || policy.SubmittedBy == actor {
			return Receipt{}, ErrIndependentReview
		}
		if in.Action == "approved" {
			if !ValidSubmission(policy, now) {
				return Receipt{}, ErrInvalid
			}
			err = tx.Exec(`UPDATE workspace_country_policy_scopes SET status='active',current_version=? WHERE tenant_id=? AND id=?`, in.VersionID, tenant, scopeID).Error
		}
		policyVersion = &in.VersionID
	}
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ID: in.ID, TenantID: tenant, ScopeID: scopeID, VersionID: policyVersion, ActorSubject: actor, Action: in.Action, Reason: in.Reason, ExpectedRevision: in.ExpectedRevision, ResultRevision: in.ExpectedRevision + 1, RequestHash: hash, CreatedAt: now}
	return receipt, record(tx, receipt)
}

// Check holds a shared scope lock through the caller's business transaction.
func Check(tx *gorm.DB, request Scope, now time.Time) (Decision, error) {
	if !validScope(request) {
		return Decision{Reason: "country_policy_scope_invalid"}, nil
	}
	var scope PolicyScope
	err := tx.Raw(`SELECT id FROM workspace_country_policy_scopes WHERE tenant_id=? AND country=? AND jurisdiction=? AND category=? AND operation=? AND currency=? FOR SHARE`, request.Tenant, request.Country, request.Jurisdiction, request.Category, request.Operation, request.Currency).Scan(&scope).Error
	if err != nil {
		return Decision{}, err
	}
	// Missing or suspended authority must never authorize a trading commitment.
	if scope.ID == "" {
		return Decision{Reason: "country_policy_required"}, nil
	}
	scope, err = loadScope(tx, request.Tenant, scope.ID, false)
	if err != nil {
		return Decision{}, err
	}
	if scope.Status != "active" || scope.CurrentVersion == nil {
		return Decision{Reason: "country_policy_not_approved"}, nil
	}
	policy, err := loadVersion(tx, scope, *scope.CurrentVersion)
	if err != nil {
		return Decision{}, err
	}
	return Evaluate(request, &policy, now), nil
}
