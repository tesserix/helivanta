package tradingpolicy

import (
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

func policyPageKey(at time.Time, id string) (time.Time, uuid.UUID) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return time.Time{}, uuid.Nil
	}
	return at, parsed
}

func (s PolicyScope) PageKey() (time.Time, uuid.UUID) { return policyPageKey(s.CreatedAt, s.ID) }
func (r Receipt) PageKey() (time.Time, uuid.UUID)     { return policyPageKey(r.CreatedAt, r.ID) }

type VersionSummary struct {
	ID             string    `json:"id"`
	AuthorSubject  string    `json:"author"`
	State          string    `json:"state"`
	EffectiveFrom  time.Time `json:"effective_from"`
	EffectiveUntil time.Time `json:"effective_until"`
	CreatedAt      time.Time `json:"created_at"`
}

func (v VersionSummary) PageKey() (time.Time, uuid.UUID) { return policyPageKey(v.CreatedAt, v.ID) }

// ListScopes returns only owned or explicitly delegated scopes.
func ListScopes(tx *gorm.DB, tenant, actor string, page pagination.Params) ([]PolicyScope, error) {
	var out []PolicyScope
	q := tx.Model(&PolicyScope{}).Select("id,tenant_id,country,jurisdiction,category,operation,currency,owner_subject,status,current_version,revision,created_at,CASE WHEN owner_subject=? THEN 'owner' ELSE (SELECT role FROM workspace_country_policy_access a WHERE a.tenant_id=workspace_country_policy_scopes.tenant_id AND a.scope_id=workspace_country_policy_scopes.id AND a.subject=?) END AS access_role", actor, actor).Where(`tenant_id=? AND (owner_subject=? OR EXISTS(SELECT 1 FROM workspace_country_policy_access a WHERE a.tenant_id=workspace_country_policy_scopes.tenant_id AND a.scope_id=workspace_country_policy_scopes.id AND a.subject=?))`, tenant, actor, actor)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	err := q.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&out).Error
	for i := range out {
		out[i].CreatedAt = out[i].CreatedAt.UTC()
	}
	return out, err
}

// ListVersions returns a bounded immutable-version history for an accessible scope.
func ListVersions(tx *gorm.DB, tenant, actor, scopeID string, page pagination.Params) ([]VersionSummary, error) {
	if _, err := ReadScope(tx, tenant, actor, scopeID); err != nil {
		return nil, err
	}
	var out []VersionSummary
	q := tx.Table("workspace_country_policy_versions v").Select("v.id,v.author_subject,v.effective_from,v.effective_until,v.created_at,COALESCE(a.action,'submitted') AS state").Joins("LEFT JOIN workspace_country_policy_actions a ON a.tenant_id=v.tenant_id AND a.version_id=v.id AND a.action IN ('approved','rejected')").Where("v.tenant_id=? AND v.scope_id=?", tenant, scopeID)
	if page.After != nil {
		q = q.Where("(v.created_at,v.id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	err := q.Order("v.created_at DESC,v.id DESC").Limit(page.Limit + 1).Scan(&out).Error
	for i := range out {
		out[i].CreatedAt = out[i].CreatedAt.UTC()
		out[i].EffectiveFrom = out[i].EffectiveFrom.UTC()
		out[i].EffectiveUntil = out[i].EffectiveUntil.UTC()
	}
	return out, err
}

// ListActions returns a bounded append-only decision and delegation history.
func ListActions(tx *gorm.DB, tenant, actor, scopeID string, page pagination.Params) ([]Receipt, error) {
	if _, err := ReadScope(tx, tenant, actor, scopeID); err != nil {
		return nil, err
	}
	var out []Receipt
	q := tx.Model(&Receipt{}).Select("id,scope_id,version_id,actor_subject,action,reason,expected_revision,result_revision,created_at").Where("tenant_id=? AND scope_id=?", tenant, scopeID)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	err := q.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&out).Error
	for i := range out {
		out[i].CreatedAt = out[i].CreatedAt.UTC()
	}
	return out, err
}
