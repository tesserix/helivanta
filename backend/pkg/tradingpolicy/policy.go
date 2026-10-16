// Package tradingpolicy owns current, explicitly reviewed transaction eligibility.
package tradingpolicy

import (
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/countryprofile"
)

type Scope struct {
	Tenant       string `json:"-"`
	Country      string `json:"country"`
	Jurisdiction string `json:"jurisdiction"`
	Category     string `json:"category"`
	Operation    string `json:"operation"`
	Currency     string `json:"currency"`
}

type Version struct {
	ID             string        `json:"id"`
	Scope          Scope         `json:"scope"`
	State          string        `json:"state"`
	Author         string        `json:"author"`
	SubmittedBy    string        `json:"submitted_by"`
	ApprovedBy     string        `json:"approved_by"`
	ApprovedAt     time.Time     `json:"approved_at"`
	EffectiveFrom  time.Time     `json:"effective_from"`
	EffectiveUntil time.Time     `json:"effective_until"`
	Sources        []Source      `json:"sources"`
	Requirements   []Requirement `json:"requirements"`
}

type Decision struct {
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason"`
	PolicyVersion string `json:"policy_version,omitempty"`
}

func Evaluate(request Scope, policy *Version, now time.Time) Decision {
	deny := func(reason string) Decision { return Decision{Reason: reason} }
	if now.IsZero() || !validScope(request) {
		return deny("country_policy_scope_invalid")
	}
	if policy == nil {
		return deny("country_policy_required")
	}
	if policy.Scope != request {
		return deny("country_policy_scope_mismatch")
	}
	if policy.State != "approved" {
		return deny("country_policy_not_approved")
	}
	id, err := uuid.Parse(policy.ID)
	if err != nil || id.Version() != 7 || !text(policy.Author, 200) || !text(policy.SubmittedBy, 200) || !text(policy.ApprovedBy, 200) || policy.ApprovedBy == policy.Author || policy.ApprovedBy == policy.SubmittedBy || policy.ApprovedAt.IsZero() || policy.ApprovedAt.After(now) {
		return deny("country_policy_review_invalid")
	}
	if policy.EffectiveFrom.IsZero() || !policy.EffectiveUntil.After(policy.EffectiveFrom) || now.Before(policy.EffectiveFrom) || !now.Before(policy.EffectiveUntil) {
		return deny("country_policy_not_effective")
	}
	if !completeRequirements(policy) {
		return deny("country_policy_requirements_incomplete")
	}
	return Decision{Allowed: true, Reason: "country_policy_approved", PolicyVersion: policy.ID}
}

// ValidSubmission validates a complete snapshot before independent review.
func ValidSubmission(policy Version, now time.Time) bool {
	id, err := uuid.Parse(policy.ID)
	policy.ApprovedAt = now
	return !now.IsZero() && validScope(policy.Scope) && err == nil && id.Version() == 7 &&
		text(policy.Author, 200) && !policy.EffectiveFrom.IsZero() && policy.EffectiveUntil.After(policy.EffectiveFrom) &&
		policy.EffectiveUntil.After(now) && completeRequirements(&policy)
}

func text(value string, limit int) bool {
	return value != "" && !strings.ContainsRune(value, '\x00') && value == strings.TrimSpace(value) && len([]rune(value)) <= limit
}

func validScope(scope Scope) bool {
	tenant, err := uuid.Parse(scope.Tenant)
	country, known := countryprofile.Lookup(scope.Country)
	return err == nil && tenant != uuid.Nil && known && scope.Currency == country.Currency && text(scope.Jurisdiction, 100) &&
		slices.Contains(policyCategories, scope.Category) &&
		slices.Contains(policyOperations, scope.Operation)
}

func completeRequirements(policy *Version) bool {
	domains := requirementDomains
	if len(policy.Requirements) != len(domains) || len(policy.Sources) == 0 || len(policy.Sources) > 30 {
		return false
	}
	sources := make(map[string]bool, len(policy.Sources))
	for _, source := range policy.Sources {
		parsed, err := url.Parse(source.Url)
		if !text(source.Key, 64) || !text(source.Title, 200) || len(source.Url) > 2048 || err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || source.ReviewedAt.IsZero() || source.ReviewedAt.After(policy.ApprovedAt) || sources[source.Key] {
			return false
		}
		sources[source.Key] = true
	}
	seen := make(map[string]bool, len(domains))
	for _, requirement := range policy.Requirements {
		if !slices.Contains(domains, requirement.Domain) || seen[requirement.Domain] || !slices.Contains([]string{"required", "not_applicable"}, requirement.Applicability) || !text(requirement.Reason, 4000) || len(requirement.SourceKeys) == 0 || len(requirement.SourceKeys) > len(sources) {
			return false
		}
		seen[requirement.Domain] = true
		references := make(map[string]bool, len(requirement.SourceKeys))
		for _, key := range requirement.SourceKeys {
			if !sources[key] || references[key] {
				return false
			}
			references[key] = true
		}
	}
	return true
}
