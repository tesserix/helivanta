package tradingpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	workspacecontract "github.com/tesserix/helivanta/internal/modules/workspace/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalid  = errors.New("invalid country policy")
	ErrConflict = errors.New("country policy revision or replay conflict")
)

type PolicyScope struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"-"`
	Country        string    `json:"country"`
	Jurisdiction   string    `json:"jurisdiction"`
	Category       string    `json:"category"`
	Operation      string    `json:"operation"`
	Currency       string    `json:"currency"`
	OwnerSubject   string    `json:"owner_subject"`
	AccessRole     string    `json:"access_role" gorm:"->"`
	PolicyDecision *Decision `json:"policy_decision,omitempty" gorm:"-"`
	Status         string    `json:"status"`
	CurrentVersion *string   `json:"current_version"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
}

func (PolicyScope) TableName() string { return "workspace_country_policy_scopes" }

type Receipt struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"-"`
	ScopeID          string    `json:"scope_id"`
	VersionID        *string   `json:"version_id"`
	ActorSubject     string    `json:"actor_subject"`
	Action           string    `json:"action"`
	Reason           string    `json:"reason"`
	ExpectedRevision int64     `json:"expected_revision"`
	ResultRevision   int64     `json:"result_revision"`
	RequestHash      string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
}

func (Receipt) TableName() string { return "workspace_country_policy_actions" }

func versionID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

func requestHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func loadScope(tx *gorm.DB, tenant, id string, write bool) (PolicyScope, error) {
	strength := "SHARE"
	if write {
		strength = "UPDATE"
	}
	var scope PolicyScope
	err := tx.Clauses(clause.Locking{Strength: strength}).Select("id,tenant_id,country,jurisdiction,category,operation,currency,owner_subject,status,current_version,revision,created_at").Where("tenant_id=? AND id=?", tenant, id).Take(&scope).Error
	scope.CreatedAt = scope.CreatedAt.UTC()
	return scope, err
}

func scopeRole(tx *gorm.DB, scope PolicyScope, actor string) (string, error) {
	if scope.OwnerSubject == actor {
		return "owner", nil
	}
	var grant struct{ Role string }
	err := tx.Table("workspace_country_policy_access").Select("role").Where("tenant_id=? AND scope_id=? AND subject=?", scope.TenantID, scope.ID, actor).Take(&grant).Error
	return grant.Role, err
}

func scopeAccess(tx *gorm.DB, scope PolicyScope, actor string, roles ...string) error {
	role, err := scopeRole(tx, scope, actor)
	if err != nil {
		return err
	}
	if role != "owner" && !slices.Contains(roles, role) {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ReadScope returns metadata only after explicit scope authorization.
func ReadScope(tx *gorm.DB, tenant, actor, id string) (PolicyScope, error) {
	scope, err := loadScope(tx, tenant, id, false)
	if err == nil {
		scope.AccessRole, err = scopeRole(tx, scope, actor)
	}
	return scope, err
}

func replay(tx *gorm.DB, tenant, actor, id, hash string) (*Receipt, error) {
	var receipt Receipt
	err := tx.Select("id,tenant_id,scope_id,version_id,actor_subject,action,reason,expected_revision,result_revision,request_hash,created_at").Where("tenant_id=? AND id=?", tenant, id).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.ActorSubject != actor || receipt.RequestHash != hash {
		return nil, ErrConflict
	}
	receipt.CreatedAt = receipt.CreatedAt.UTC()
	return &receipt, nil
}

// Submit stores an immutable reviewed-source snapshot in the caller's transaction.
func Submit(tx *gorm.DB, tenant, actor string, in Submission, now time.Time) (Receipt, error) {
	now = now.UTC().Truncate(time.Microsecond)
	policy := Version{ID: in.ID, Author: actor, Scope: Scope{Tenant: tenant, Country: in.Country, Jurisdiction: in.Jurisdiction, Category: in.Category, Operation: in.Operation, Currency: in.Currency}, EffectiveFrom: in.EffectiveFrom, EffectiveUntil: in.EffectiveUntil, Sources: in.Sources, Requirements: in.Requirements}
	if !versionID(in.ScopeID) || !versionID(in.ID) || in.ExpectedRevision < 0 || in.ExpectedRevision > maxPolicyRevision || !validScope(policy.Scope) || !text(actor, 200) {
		return Receipt{}, ErrInvalid
	}
	hash, err := requestHash(in)
	if err != nil {
		return Receipt{}, err
	}
	if in.ExpectedRevision == 0 {
		if err := tx.Exec(`INSERT INTO workspace_country_policy_scopes(tenant_id,id,country,jurisdiction,category,operation,currency,owner_subject) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, tenant, in.ScopeID, in.Country, in.Jurisdiction, in.Category, in.Operation, in.Currency, actor).Error; err != nil {
			return Receipt{}, err
		}
	}
	scope, err := loadScope(tx, tenant, in.ScopeID, true)
	if err != nil {
		return Receipt{}, err
	}
	if err := scopeAccess(tx, scope, actor, "editor"); err != nil {
		return Receipt{}, err
	}
	prior, err := replay(tx, tenant, actor, in.ID, hash)
	if err != nil {
		return Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	if !ValidSubmission(policy, now) {
		return Receipt{}, ErrInvalid
	}
	if scope.Revision != in.ExpectedRevision || scope.Country != in.Country || scope.Jurisdiction != in.Jurisdiction || scope.Category != in.Category || scope.Operation != in.Operation || scope.Currency != in.Currency {
		return Receipt{}, ErrConflict
	}
	if err := tx.Exec(`INSERT INTO workspace_country_policy_versions(tenant_id,id,scope_id,author_subject,effective_from,effective_until) VALUES(?,?,?,?,?,?)`, tenant, in.ID, in.ScopeID, actor, in.EffectiveFrom, in.EffectiveUntil).Error; err != nil {
		return Receipt{}, err
	}
	sources := make([]map[string]any, 0, len(in.Sources))
	for _, source := range in.Sources {
		sources = append(sources, map[string]any{"tenant_id": tenant, "version_id": in.ID, "key": source.Key, "title": source.Title, "url": source.Url, "reviewed_at": source.ReviewedAt})
	}
	if err := tx.Table("workspace_country_policy_sources").Create(&sources).Error; err != nil {
		return Receipt{}, err
	}
	requirements := make([]map[string]any, 0, len(in.Requirements))
	var citations []map[string]any
	for _, requirement := range in.Requirements {
		requirements = append(requirements, map[string]any{"tenant_id": tenant, "version_id": in.ID, "domain": requirement.Domain, "applicability": requirement.Applicability, "reason": requirement.Reason})
		for _, key := range requirement.SourceKeys {
			citations = append(citations, map[string]any{"tenant_id": tenant, "version_id": in.ID, "domain": requirement.Domain, "source_key": key})
		}
	}
	if err := tx.Table("workspace_country_policy_requirements").Create(&requirements).Error; err != nil {
		return Receipt{}, err
	}
	if err := tx.Table("workspace_country_policy_citations").Create(&citations).Error; err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ID: in.ID, TenantID: tenant, ScopeID: in.ScopeID, VersionID: &in.ID, ActorSubject: actor, Action: "submitted", ExpectedRevision: in.ExpectedRevision, ResultRevision: in.ExpectedRevision + 1, RequestHash: hash, CreatedAt: now}
	if err := record(tx, receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func record(tx *gorm.DB, receipt Receipt) error {
	result := tx.Exec(`UPDATE workspace_country_policy_scopes SET revision=?,updated_at=? WHERE tenant_id=? AND id=? AND revision=?`, receipt.ResultRevision, receipt.CreatedAt, receipt.TenantID, receipt.ScopeID, receipt.ExpectedRevision)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	if err := tx.Create(&receipt).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workspacecontract.CountryPolicyChangedData{ScopeID: receipt.ScopeID, VersionID: receipt.VersionID, Action: receipt.Action, Revision: receipt.ResultRevision})
	if err != nil {
		return fmt.Errorf("encode country policy event: %w", err)
	}
	return events.Enqueue(tx, workspacecontract.SubjectCountryPolicyChanged, events.Event{ID: receipt.ID, Type: "CountryPolicyChanged", Version: 1, TenantID: receipt.TenantID, Data: data})
}
