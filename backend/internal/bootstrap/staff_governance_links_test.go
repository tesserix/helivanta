package bootstrap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestGovernanceAccountLinkReviewConsumesExactEmployerSourceWithoutManagerAccess(t *testing.T) {
	graph, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, graph.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, graph.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleTenantAdmin))
	for _, subject := range []string{"user-admin", "user-owner2"} {
		require.NoError(t, graph.GrantRole(t.Context(), testutil.TenantA, subject, authz.RoleTenantAdmin))
	}

	for _, subject := range []string{"user-reviewer", "user-auditor", "user-owner2"} {
		require.NoError(t, graph.GrantRole(t.Context(), testutil.TenantA, subject, authz.RoleStaffMember))
	}
	for _, kind := range []string{"hospital", "pharmacy", "supplier"} {
		t.Run(kind, func(t *testing.T) {
			native := &governanceNativeIdentity{}
			router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
				Tokens: map[string]string{"admin": testutil.TenantA, "owner2": testutil.TenantA, "auditor": testutil.TenantA},
				Perms: map[string][]authz.Permission{
					"admin":   {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, workforce.PermSupplierManage, workforce.PermEnrollmentManage, workforce.PermLoginLinkManage, workforce.PermLoginLinkReview, workforce.PermIndependentLoginManage, workforce.PermIndependentLoginReview, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, supply.PermPartyWrite, supply.PermOrganizationManage},
					"owner2":  {iam.PermGovernanceReview, workforce.PermLoginLinkReview, workforce.PermIndependentLoginReview},
					"auditor": {workforce.PermLoginLinkReview, workforce.PermIndependentLoginReview},
				},
				Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New(), pharmacy.New(), supply.New()},
				Writer:  graph, Roles: graph, Membership: graph,
			})
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-owner2','tenant_admin'),(?,'user-auditor','staff_member'),(?,'user-reviewer','staff_member')", testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
			}))
			roots := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
			family := map[string]string{"hospital": "hospitals", "pharmacy": "pharmacies", "supplier": "suppliers"}[kind]
			people := make([]uuid.UUID, 2)
			links := make([]string, 2)
			for i, root := range roots {
				base := "/v1/workforce/" + family + "/" + root.String()
				if kind == "hospital" {
					created := governanceRequest(router, http.MethodPut, base, "admin", `{"name":"Synthetic governed hospital"}`)
					require.Equal(t, 200, created.Code, created.Body.String())
				} else {
					var productPath string
					var productInput map[string]any
					if kind == "pharmacy" {
						productPath = "/v1/pharmacy/pharmacies/" + root.String()
						productInput = map[string]any{"code": "SYN-GOV-RX-" + strconv.Itoa(i), "name": "Synthetic Independent Pharmacy", "ownership_type": "independent"}
					} else {
						productPath = "/v1/supply/parties/" + root.String()
						productInput = map[string]any{"expected_version": 0, "code": "SYN-GOV-SUP-" + strconv.Itoa(i), "record_kind": "owned_organisation", "legal_name": "Synthetic Supplier", "trading_name": "Synthetic Supplier", "business_kind": "wholesaler", "country_code": "IN", "jurisdiction": "IN-OD", "registration_number": "TEST-REG-" + strconv.Itoa(i), "corporate_group": "", "website": "", "default_credit_days": 30, "contacts": []any{}, "addresses": []any{}, "tax_registrations": []any{}}
					}
					raw, err := json.Marshal(productInput)
					require.NoError(t, err)
					created := governanceRequest(router, http.MethodPut, productPath, "admin", string(raw))
					require.Equal(t, 200, created.Code, created.Body.String())
					registered := governanceRequest(router, http.MethodPut, base, "admin", `{}`)
					require.Equal(t, 200, registered.Code, registered.Body.String())
				}
				if kind == "hospital" {
					ownerAccess := governanceRequest(router, http.MethodPut, base+"/access/user-admin", "admin", `{"level":"manager"}`)
					require.Equal(t, 200, ownerAccess.Code, ownerAccess.Body.String())
				}
				person := uuid.Must(uuid.NewV7())
				people[i] = person
				staffPath := base + "/staff/" + person.String()
				body := `{"employee_code":"SYN-LINK","display_name":"Synthetic account-link target","job_category":"pharmacist","employment_status":"active"}`
				if kind != "hospital" {
					staffPath += "?directory_context=v1"
					body = `{"employee_code":"SYN-LINK","display_name":"Synthetic account-link target","job_category":"pharmacist","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`
				}
				created := governanceRequest(router, http.MethodPut, staffPath, "admin", body)
				require.Equal(t, 200, created.Code, created.Body.String())
				resource := base + "/staff/" + person.String()
				if kind == "hospital" {
					resource += "/login-links/"
				} else {
					resource += "/account-links/"
				}
				links[i] = resource + uuid.Must(uuid.NewV7()).String()
				proposal := `{"subject":"user-reviewer","reason":"Synthetic verified account"}`
				if kind != "hospital" {
					proposal = `{"expected_staff_version":1,"subject":"user-reviewer","reason":"Synthetic verified account"}`
				}
				proposed := governanceRequest(router, http.MethodPut, links[i], "admin", proposal)
				expected := 200
				if kind != "hospital" {
					expected = 201
				}
				require.Equal(t, expected, proposed.Code, proposed.Body.String())
			}
			id := uuid.Must(uuid.NewV7())
			input := iam.GovernanceGrantInput{Subject: "user-auditor", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: kind, ScopeID: &roots[0], Reason: "Synthetic exact employer account-link review", ValidFrom: time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			issued := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", string(raw))
			require.Equal(t, 202, issued.Code, issued.Body.String())
			approved := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", `{"expected_version":1,"decision":"approve","reason":"Independent owner confirms narrow source"}`)
			require.Equal(t, 202, approved.Code, approved.Body.String())
			decide := func(index int, reason string) *httptest.ResponseRecorder {
				path, method := links[index]+"/decision", http.MethodPost
				if kind != "hospital" {
					path, method = links[index]+"/commands/"+uuid.Must(uuid.NewV7()).String(), http.MethodPut
				}
				body, err := json.Marshal(map[string]any{"expected_version": 1, "decision": "approve", "reason": reason})
				require.NoError(t, err)
				return governanceRequest(router, method, path, "auditor", string(body))
			}
			foreign := decide(1, "Wrong employer must be denied")
			require.Equal(t, 403, foreign.Code, foreign.Body.String())
			result := decide(0, "Independently checked target account")
			expected := 200
			if kind != "hospital" {
				expected = 201
			}
			require.Equal(t, expected, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), "active")
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject='user-auditor'", testutil.TenantA).Count(&count).Error)
				require.Zero(t, count, "a governance reviewer has no private directory manager access")
				require.NoError(t, tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)", roots[0].String()).Error)
				var evidence struct {
					AuthorityKind                                         string
					SourceGrantID                                         uuid.UUID
					SourceGrantVersion, ActorMembershipEpoch, LinkVersion int64
					StaffID                                               uuid.UUID
				}
				require.NoError(t, tx.Table("workforce_login_link_authorities").Where("tenant_id=? AND employer_id=? AND staff_id=?", testutil.TenantA, roots[0], people[0]).Take(&evidence).Error)
				require.Equal(t, "grant", evidence.AuthorityKind)
				require.Equal(t, id, evidence.SourceGrantID)
				require.EqualValues(t, 2, evidence.SourceGrantVersion)
				require.EqualValues(t, 1, evidence.ActorMembershipEpoch)
				require.EqualValues(t, 2, evidence.LinkVersion)
				require.Equal(t, people[0], evidence.StaffID)
				return nil
			}))
			base := "/v1/workforce/" + family + "/" + roots[0].String() + "/staff/" + people[0].String()
			denied := governanceRequest(router, http.MethodGet, base, "auditor", "")
			require.True(t, denied.Code == 403 || denied.Code == 404, denied.Body.String())
			withdrawn := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":2,"decision":"revoke","reason":"Withdraw independent account-link source"}`)
			require.Equal(t, 202, withdrawn.Code, withdrawn.Body.String())
			lost := decide(0, "Independently checked target account")
			require.Equal(t, 403, lost.Code, lost.Body.String())
		})
	}
}
