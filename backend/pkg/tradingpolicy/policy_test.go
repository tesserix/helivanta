package tradingpolicy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
)

var testNow = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

func approvedPolicy(country string) tradingpolicy.Version {
	currency := "INR"
	if country == "PK" {
		currency = "PKR"
	}
	p := tradingpolicy.Version{
		ID:    "01a14400-0000-7000-8000-000000000001",
		Scope: tradingpolicy.Scope{Tenant: "11111111-1111-1111-1111-111111111111", Country: country, Jurisdiction: "TEST-JURISDICTION", Category: "otc", Operation: "order", Currency: currency},
		State: "approved", Author: "TEST-author", SubmittedBy: "TEST-submitter", ApprovedBy: "TEST-independent-reviewer",
		ApprovedAt: testNow.Add(-time.Hour), EffectiveFrom: testNow.Add(-time.Hour), EffectiveUntil: testNow.Add(time.Hour),
		Sources: []tradingpolicy.Source{{Key: "TEST-source", Title: "TEST ONLY synthetic policy; not legal approval", Url: "https://regulator.example.invalid/test-policy", ReviewedAt: testNow.Add(-2 * time.Hour)}},
	}
	for _, domain := range []string{"licensing", "product_registration", "prescription", "controlled", "cold_chain", "tax_invoice", "credit_note", "retention", "privacy", "residency", "reporting"} {
		p.Requirements = append(p.Requirements, tradingpolicy.Requirement{Domain: domain, Applicability: "required", Reason: "TEST ONLY fictional requirement", SourceKeys: []string{"TEST-source"}})
	}
	return p
}

func TestApprovedExactScopeIncludesPolicyProvenance(t *testing.T) {
	for _, country := range []string{"IN", "PK"} {
		t.Run(country, func(t *testing.T) {
			p := approvedPolicy(country)
			result := tradingpolicy.Evaluate(p.Scope, &p, testNow)
			require.True(t, result.Allowed)
			require.Equal(t, p.ID, result.PolicyVersion)
		})
	}
}

func TestOneApprovalCannotAuthorizeAnotherScope(t *testing.T) {
	p := approvedPolicy("IN")
	for _, change := range []struct {
		name string
		edit func(*tradingpolicy.Scope)
	}{
		{"tenant", func(s *tradingpolicy.Scope) { s.Tenant = "22222222-2222-2222-2222-222222222222" }},
		{"country", func(s *tradingpolicy.Scope) { s.Country, s.Currency = "PK", "PKR" }},
		{"jurisdiction", func(s *tradingpolicy.Scope) { s.Jurisdiction = "OTHER-JURISDICTION" }},
		{"category", func(s *tradingpolicy.Scope) { s.Category = "controlled" }},
		{"operation", func(s *tradingpolicy.Scope) { s.Operation = "dispatch" }},
		{"currency", func(s *tradingpolicy.Scope) { s.Currency = "PKR" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			requested := p.Scope
			change.edit(&requested)
			require.False(t, tradingpolicy.Evaluate(requested, &p, testNow).Allowed)
		})
	}
}

func TestMissingIncompleteStaleOrUnreviewedPolicyDenies(t *testing.T) {
	for _, change := range []struct {
		name string
		edit func(*tradingpolicy.Version)
	}{
		{"draft", func(p *tradingpolicy.Version) { p.State = "draft" }},
		{"suspended", func(p *tradingpolicy.Version) { p.State = "suspended" }},
		{"no_approver", func(p *tradingpolicy.Version) { p.ApprovedBy = "" }},
		{"no_author", func(p *tradingpolicy.Version) { p.Author = "" }},
		{"no_submitter", func(p *tradingpolicy.Version) { p.SubmittedBy = "" }},
		{"null_in_requirement", func(p *tradingpolicy.Version) { p.Requirements[0].Reason = "TEST\x00reason" }},
		{"invalid_version", func(p *tradingpolicy.Version) { p.ID = "not-a-version" }},
		{"no_approval_time", func(p *tradingpolicy.Version) { p.ApprovedAt = time.Time{} }},
		{"future_approval", func(p *tradingpolicy.Version) { p.ApprovedAt = testNow.Add(time.Second) }},
		{"author_self_approval", func(p *tradingpolicy.Version) { p.ApprovedBy = p.Author }},
		{"submitter_self_approval", func(p *tradingpolicy.Version) { p.ApprovedBy = p.SubmittedBy }},
		{"expired", func(p *tradingpolicy.Version) { p.EffectiveUntil = testNow }},
		{"not_yet_effective", func(p *tradingpolicy.Version) { p.EffectiveFrom = testNow.Add(time.Second) }},
		{"missing_requirement", func(p *tradingpolicy.Version) { p.Requirements = p.Requirements[1:] }},
		{"duplicate_requirement", func(p *tradingpolicy.Version) { p.Requirements[0] = p.Requirements[1] }},
		{"unknown_requirement", func(p *tradingpolicy.Version) { p.Requirements[0].Domain = "unknown" }},
		{"missing_source", func(p *tradingpolicy.Version) { p.Sources = nil }},
		{"duplicate_source", func(p *tradingpolicy.Version) { p.Sources = append(p.Sources, p.Sources[0]) }},
		{"unknown_source", func(p *tradingpolicy.Version) { p.Requirements[0].SourceKeys = []string{"unknown"} }},
		{"unreviewed_source", func(p *tradingpolicy.Version) { p.Sources[0].ReviewedAt = time.Time{} }},
		{"source_review_after_approval", func(p *tradingpolicy.Version) { p.Sources[0].ReviewedAt = testNow }},
		{"unexplained_non_applicability", func(p *tradingpolicy.Version) {
			p.Requirements[0].Applicability, p.Requirements[0].Reason = "not_applicable", ""
		}},
		{"credential_in_citation", func(p *tradingpolicy.Version) { p.Sources[0].Url = "https://user:secret@example.invalid/policy" }},
		{"unknown_country", func(p *tradingpolicy.Version) { p.Scope.Country = "ZZ" }},
		{"unknown_tenant", func(p *tradingpolicy.Version) { p.Scope.Tenant = "" }},
		{"wrong_country_currency", func(p *tradingpolicy.Version) { p.Scope.Currency = "INR" }},
		{"unknown_operation", func(p *tradingpolicy.Version) { p.Scope.Operation = "unknown" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			p := approvedPolicy("PK")
			change.edit(&p)
			require.False(t, tradingpolicy.Evaluate(p.Scope, &p, testNow).Allowed)
		})
	}
	p := approvedPolicy("PK")
	require.False(t, tradingpolicy.Evaluate(p.Scope, nil, testNow).Allowed)
	require.False(t, tradingpolicy.Evaluate(p.Scope, &p, time.Time{}).Allowed)
}

func TestReviewedNonApplicabilityAndInclusiveStart(t *testing.T) {
	p := approvedPolicy("PK")
	p.EffectiveFrom = testNow
	p.Requirements[0].Applicability = "not_applicable"
	p.Requirements[0].Reason = "TEST ONLY independent reviewer documented fictional non-applicability"
	result := tradingpolicy.Evaluate(p.Scope, &p, testNow)
	require.True(t, result.Allowed)
	require.Equal(t, "country_policy_approved", result.Reason)
}

func TestActionableReasonsDoNotDiscloseOtherPolicy(t *testing.T) {
	p := approvedPolicy("PK")
	missing := tradingpolicy.Evaluate(p.Scope, nil, testNow)
	require.Equal(t, "country_policy_required", missing.Reason)
	requested := p.Scope
	requested.Tenant = "22222222-2222-2222-2222-222222222222"
	denied := tradingpolicy.Evaluate(requested, &p, testNow)
	require.Equal(t, "country_policy_scope_mismatch", denied.Reason)
	require.Empty(t, denied.PolicyVersion)
}
