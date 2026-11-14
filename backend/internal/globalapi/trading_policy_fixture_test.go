package globalapi

import (
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
	"testing"
	"time"
)

// approveSyntheticTradingPolicy exercises the policy owner; its invalid source URL is test evidence only.
func approveSyntheticTradingPolicy(t *testing.T, f operatingDutyFixture, jurisdiction, category, operation string) string {
	t.Helper()
	now := time.Now().UTC()
	scope, version := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	requirements := []tradingpolicy.Requirement{}
	for _, domain := range []string{"licensing", "product_registration", "prescription", "controlled", "cold_chain", "tax_invoice", "credit_note", "retention", "privacy", "residency", "reporting"} {
		requirements = append(requirements, tradingpolicy.Requirement{Domain: domain, Applicability: "required", Reason: "TEST ONLY synthetic reviewed requirement; not legal approval", SourceKeys: []string{"TEST-source"}})
	}
	path := "/v1/workspace/country-policies"
	f.call("POST", path, "owner", tradingpolicy.Submission{ID: version, ScopeID: scope, ExpectedRevision: 0, Country: "IN", Jurisdiction: jurisdiction, Category: category, Operation: operation, Currency: "INR", EffectiveFrom: now.Add(-time.Hour), EffectiveUntil: now.Add(time.Hour), Sources: []tradingpolicy.Source{{Key: "TEST-source", Title: "TEST ONLY source", Url: "https://regulator.example.invalid/test", ReviewedAt: now.Add(-time.Hour)}}, Requirements: requirements}, 202)
	f.call("POST", path+"/"+scope+"/access", "owner", tradingpolicy.AccessChange{ID: uuid.Must(uuid.NewV7()).String(), ExpectedRevision: 1, Subject: "user-reviewer", Role: "reviewer"}, 202)
	f.call("POST", path+"/"+scope+"/review", "reviewer", tradingpolicy.Review{ID: uuid.Must(uuid.NewV7()).String(), ExpectedRevision: 2, VersionID: version, Action: "approved", Reason: "TEST independent policy review"}, 202)
	return scope
}
