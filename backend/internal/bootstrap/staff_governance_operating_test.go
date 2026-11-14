package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGovernanceOperatingSourceUsesExactIndependentEmployerWithoutLegacyManager(t *testing.T) {
	for _, kind := range []string{"pharmacy", "supplier"} {
		t.Run(kind, func(t *testing.T) {
			modules := []platform.Module{iam.New(nil).SetMembershipIdentityReader(&governanceNativeIdentity{}), workforce.New(), pharmacy.New(), supply.New()}
			router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
				Tokens:  map[string]string{"admin": testutil.TenantA},
				Perms:   map[string][]authz.Permission{"admin": {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, workforce.PermSupplierManage, pharmacy.PermCatalogueWrite, supply.PermPartyWrite, supply.PermOrganizationManage}},
				Modules: modules,
			})
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member')`, testutil.TenantA, testutil.TenantA).Error
			}))
			roots := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
			for index, root := range roots {
				family := "pharmacies"
				productPath := "/v1/pharmacy/pharmacies/" + root.String()
				input := map[string]any{"code": "SYN-OPERATING-" + strconv.Itoa(index), "name": "Synthetic independent pharmacy", "ownership_type": "independent"}
				if kind == "supplier" {
					family = "suppliers"
					productPath = "/v1/supply/parties/" + root.String()
					input = map[string]any{"expected_version": 0, "code": "SYN-OPERATING-" + strconv.Itoa(index), "record_kind": "owned_organisation", "legal_name": "Synthetic supplier", "trading_name": "Synthetic supplier", "business_kind": "wholesaler", "country_code": "IN", "jurisdiction": "IN-OD", "registration_number": "TEST-REG-" + strconv.Itoa(index), "corporate_group": "", "website": "", "default_credit_days": 30, "contacts": []any{}, "addresses": []any{}, "tax_registrations": []any{}}
				}
				raw, err := json.Marshal(input)
				require.NoError(t, err)
				response := governanceRequest(router, http.MethodPut, productPath, "admin", string(raw))
				require.Equal(t, http.StatusOK, response.Code, "root %d: %s", index, response.Body.String())
				registered := governanceRequest(router, http.MethodPut, "/v1/workforce/"+family+"/"+root.String(), "admin", `{}`)
				require.Equal(t, http.StatusOK, registered.Code, registered.Body.String())
			}
			sources := platform.GovernanceSources(modules, db)
			principal := authn.Principal{TenantID: testutil.TenantA, Subject: "user-reviewer", AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}
			req := authz.GovernanceSourceRequest{Principal: principal, Capability: authz.GovernanceOperatingManage, Scope: authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: kind, ID: &roots[0]}}
			called := false
			err := sources.WithGovernanceSource(ctx, req, func(context.Context, authz.GovernanceSourceEvidence) error { called = true; return nil })
			require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
			require.False(t, called)
			start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			grant := iam.GovernanceGrantInput{Subject: principal.Subject, ExpectedMembershipEpoch: 1, TemplateKey: "iam.operating_access_reviewer.v1", ScopeKind: kind, ScopeID: &roots[0], Reason: "Synthetic employer operating-access governance", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour)}
			raw, err := json.Marshal(grant)
			require.NoError(t, err)
			grantID := uuid.Must(uuid.NewV7())
			grantPath := "/v1/iam/governance-grants/" + grantID.String()
			issued := governanceRequest(router, http.MethodPut, grantPath, "admin", string(raw))
			require.Equal(t, http.StatusAccepted, issued.Code, issued.Body.String())
			for _, capability := range []authz.GovernanceCapability{authz.GovernanceOperatingManage, authz.GovernanceOperatingReview} {
				req.Capability = capability
				calls := 0
				require.NoError(t, sources.WithGovernanceSource(ctx, req, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
					calls++
					require.True(t, db.GuardedCommandActive(commandCtx))
					require.Equal(t, principal.Subject, tenantdb.ActorFrom(commandCtx))
					require.Equal(t, authz.GovernanceGrantSource, source.Kind)
					require.Equal(t, grantID, source.GrantID)
					require.EqualValues(t, 1, source.Version)
					require.Equal(t, grant.TemplateKey, source.TemplateKey)
					require.Equal(t, req.Scope, source.Scope)
					return nil
				}))
				require.Equal(t, 1, calls)
			}
			ownerRequest := req
			ownerRequest.Principal.Subject = "user-admin"
			require.NoError(t, sources.WithGovernanceSource(ctx, ownerRequest, func(_ context.Context, source authz.GovernanceSourceEvidence) error {
				require.Equal(t, authz.GovernanceOwnerSource, source.Kind)
				require.NotEqual(t, uuid.Nil, source.OwnerMembershipID)
				require.Equal(t, uuid.Nil, source.GrantID)
				return nil
			}))
			for name, mutate := range map[string]func(*authz.GovernanceSourceRequest){
				"sibling employer":         func(r *authz.GovernanceSourceRequest) { r.Scope.ID = &roots[1] },
				"hospital":                 func(r *authz.GovernanceSourceRequest) { r.Scope.Kind = "hospital" },
				"foreign tenant":           func(r *authz.GovernanceSourceRequest) { r.Scope.TenantID = testutil.TenantB },
				"business permission":      func(r *authz.GovernanceSourceRequest) { r.Capability = "iam.operating.review" },
				"other governance purpose": func(r *authz.GovernanceSourceRequest) { r.Capability = authz.GovernanceAccountLinkReview },
			} {
				t.Run(name, func(t *testing.T) {
					wrong := req
					mutate(&wrong)
					invoked := false
					err := sources.WithGovernanceSource(ctx, wrong, func(context.Context, authz.GovernanceSourceEvidence) error { invoked = true; return nil })
					require.Error(t, err)
					require.False(t, invoked)
				})
			}
			unavailable := iam.New(nil).GovernanceSources(db, nil)
			require.ErrorIs(t, unavailable.WithGovernanceSource(ctx, req, func(context.Context, authz.GovernanceSourceEvidence) error {
				t.Fatal("missing topology cannot authorize")
				return nil
			}), authz.ErrScopedAuthorizationUnavailable)
			revoked := governanceRequest(router, http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":1,"decision":"revoke","reason":"Synthetic source cutoff"}`)
			require.Equal(t, http.StatusAccepted, revoked.Code, revoked.Body.String())
			require.ErrorIs(t, sources.WithGovernanceSource(ctx, req, func(context.Context, authz.GovernanceSourceEvidence) error {
				t.Fatal("revoked source cannot authorize")
				return nil
			}), authz.ErrScopedCommandDenied)
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject=?", testutil.TenantA, principal.Subject).Count(&count).Error)
				require.Zero(t, count, "source governance never creates legacy manager access")
				return nil
			}))
		})
	}
}
