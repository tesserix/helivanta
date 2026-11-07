package supply_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // external test exercises its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestSupplierTradeSourceCannotUseAnUnreviewedOrForeignLicence(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": {supply.PermPartyWrite, supply.PermOrganizationManage, supply.PermPremiseWrite, supply.PermLicenseWrite}}, Modules: []platform.Module{supply.New()}})
	guard := supply.New().TradingEndpoints(db, nil)
	endpoint := suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantA), Product: "supply", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"}
	root := structureParty(t, r, "TEST-TRADE-SOURCE", "owner")
	endpoint.OrganizationID = uuid.MustParse(root[len("/v1/supply/parties/"):])
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+endpoint.LocationID.String(), "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/licenses/"+endpoint.LicenceID.String(), "owner", licenseBody(0, endpoint.LocationID.String())).Code)
	calls := 0
	run := func(ctx context.Context) error {
		return guard.WithCurrentTradingEndpoint(ctx, endpoint, "prescription", "order", func(context.Context, suppliertrade.EndpointReference) error { calls++; return nil })
	}
	require.ErrorIs(t, run(t.Context()), suppliertrade.ErrUnavailable)
	err := db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, run)
	require.ErrorIs(t, err, suppliertrade.ErrIneligible)
	require.Zero(t, calls)
}

func TestSupplierTradingSourceRequiresCurrentIndependentLicenceAndPolicyReview(t *testing.T) {
	s := releaseReviewEvidence(t, newEvidenceScenarioConfigured(t, true))
	evidencePath := strings.Split(s.path, "/")
	endpoint := suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantA), Product: "supply", OrganizationID: uuid.MustParse(evidencePath[4]), LocationID: uuid.Nil, OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse(evidencePath[6]), LicenceVersion: 1, Jurisdiction: "IN-MH"}
	// The licence owner publishes its exact premise; no consumer searches foreign or another module's tables.
	response := testutil.Do(s.r, "GET", s.licence, "owner", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var licence struct {
		PremiseID uuid.UUID `json:"premise_id"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &licence))
	endpoint.LocationID = licence.PremiseID
	guard := supply.New().TradingEndpoints(s.db, nil)
	calls := 0
	run := func() error {
		return s.db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return guard.WithCurrentTradingEndpoint(ctx, endpoint, "prescription", "order", func(ctx context.Context, ref suppliertrade.EndpointReference) error {
				calls++
				require.Equal(t, endpoint, ref.Endpoint)
				require.Equal(t, int64(2), ref.LicenceReviewVersion)
				require.NotEqual(t, uuid.Nil, ref.PolicyVersion)
				return s.db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					var purpose string
					err := tx.Raw(`SELECT COALESCE(current_setting('app.supply_trade_source',true),'')`).Scan(&purpose).Error
					require.Empty(t, purpose, "private source purpose must be restored before caller action")
					return err
				})
			})
		})
	}
	require.ErrorIs(t, run(), suppliertrade.ErrIneligible, "declared and scanned is not independently reviewed")
	require.Equal(t, 200, testutil.Do(s.r, "PUT", s.root+"/access/user-auditor/compliance", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(s.r, "PUT", s.root+"/access/user-owner/compliance", "owner", "").Code)
	for _, step := range []struct{ actor, body string }{{"owner", reviewBody(s, 0, "submit")}, {"auditor", reviewBody(s, 1, "verify")}} {
		verified := testutil.Do(s.r, "PUT", s.licence+"/reviews/"+uuid.Must(uuid.NewV7()).String(), step.actor, step.body)
		require.Equal(t, 201, verified.Code, verified.Body.String())
	}
	require.ErrorIs(t, run(), suppliertrade.ErrIneligible, "licence approval alone is not an approved trade policy")
	now := time.Now().UTC()
	scope, version := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	requirements := []map[string]any{}
	for _, domain := range []string{"licensing", "product_registration", "prescription", "controlled", "cold_chain", "tax_invoice", "credit_note", "retention", "privacy", "residency", "reporting"} {
		requirements = append(requirements, map[string]any{"domain": domain, "applicability": "required", "reason": "TEST ONLY synthetic scope requirement", "source_keys": []string{"TEST-source"}})
	}
	submit, _ := json.Marshal(map[string]any{"id": version, "scope_id": scope, "expected_revision": 0, "country": "IN", "jurisdiction": "IN-MH", "category": "prescription", "operation": "order", "currency": "INR", "effective_from": now.Add(-time.Hour), "effective_until": now.Add(time.Hour), "sources": []map[string]any{{"key": "TEST-source", "title": "TEST ONLY synthetic source; not legal approval", "url": "https://regulator.example.invalid/test", "reviewed_at": now.Add(-time.Hour)}}, "requirements": requirements})
	path := "/v1/workspace/country-policies"
	for _, step := range []struct{ path, actor, body string }{
		{path, "owner", string(submit)},
		{path + "/" + scope + "/access", "owner", fmt.Sprintf(`{"id":"%s","expected_revision":1,"subject":"user-auditor","role":"reviewer"}`, uuid.Must(uuid.NewV7()))},
		{path + "/" + scope + "/review", "auditor", fmt.Sprintf(`{"id":"%s","expected_revision":2,"version_id":"%s","action":"approved","reason":"TEST independent synthetic policy approval"}`, uuid.Must(uuid.NewV7()), version)},
	} {
		w := testutil.Do(s.r, "POST", step.path, step.actor, step.body)
		require.Equal(t, 202, w.Code, w.Body.String())
	}
	require.NoError(t, run())
	require.Equal(t, 1, calls)
	suspended := testutil.Do(s.r, "POST", path+"/"+scope+"/review", "auditor", fmt.Sprintf(`{"id":"%s","expected_revision":3,"action":"suspended","reason":"TEST immediate policy cutoff"}`, uuid.Must(uuid.NewV7())))
	require.Equal(t, 202, suspended.Code, suspended.Body.String())
	require.ErrorIs(t, run(), suppliertrade.ErrIneligible)
	require.Equal(t, 1, calls, "historical approval must not call the action after suspension")
}
