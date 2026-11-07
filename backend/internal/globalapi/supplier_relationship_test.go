package globalapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
	"time"
)

func TestRelationshipDraftIsActorAndSourceBoundAndCannotBecomeTradingAuthority(t *testing.T) {
	f := newOperatingDutyFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hospital, binding, registration := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	version := int64(2)
	input := suppliertrade.SupplierRelationshipInput{Terms: suppliertrade.Terms{
		Seller:   suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantA), Product: "supply", OrganizationID: f.input.Scope.EmployerID, LocationID: f.input.Scope.LocationID, OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"},
		Buyer:    suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantB), Product: "pharmacy", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-OD", HospitalID: &hospital, BindingID: &binding, BindingVersion: &version, RegistrationID: &registration, RegistrationVersion: &version},
		Category: "prescription", Currency: "INR", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), TermsReference: "TEST ONLY contract intent"}, Reason: "TEST independent supplier collaboration proposal"}
	id := uuid.Must(uuid.NewV7())
	path := "/v1/supply/relationships/" + id.String()
	first := f.call("PUT", path, "owner", input, 201)
	require.Contains(t, first, `"status":"pending_review"`)
	require.JSONEq(t, first, f.call("PUT", path, "owner", input, 200))
	require.JSONEq(t, first, f.call("GET", path+"/proposal-receipt", "owner", nil, 200))
	f.call("GET", path, "receiver", nil, 403)
	f.call("GET", path, "foreign", nil, 403)
	changed := input
	changed.Reason = "TEST changed intent"
	f.call("PUT", path, "owner", changed, 409)
	changed = input
	changed.Terms.Seller.LocationID = uuid.Must(uuid.NewV7())
	f.call("PUT", "/v1/supply/relationships/"+uuid.Must(uuid.NewV7()).String(), "owner", changed, 404)
	f.call("PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", suppliertrade.SupplierRelationshipCommand{ExpectedVersion: 1, Decision: "approve", Reason: "TEST self review"}, 409)
	// A missing canonical platform/peer authority is not approval, a zero report or an active grant.
	f.call("PUT", "/v1/supply/parties/"+f.input.Scope.EmployerID.String()+"/access/user-reviewer/administrator", "owner", nil, 200)
	reviewer := f.principal
	reviewer.Subject = "user-reviewer"
	require.Eventually(t, func() bool {
		return iam.NewSourceAdministrationGuard(f.db, f.graph).WithSourceAdministration(t.Context(), reviewer, supply.PermRelationshipReview, func(context.Context) error { return nil }) == nil
	}, 20*time.Second, 100*time.Millisecond)
	f.call("PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", suppliertrade.SupplierRelationshipCommand{ExpectedVersion: 1, Decision: "approve", Reason: "TEST must await actual authority"}, 503)
	require.Contains(t, f.call("GET", path, "owner", nil, 200), `"status":"pending_review"`)
}
