package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
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
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type governanceLinkIdentity struct {
	roles      platform.RoleLister
	membership authz.MembershipChecker
	mu         sync.Mutex
	before     func(context.Context) error
}

func (g *governanceLinkIdentity) beforeTarget(ctx context.Context, subject string) error {
	if subject != "user-reviewer" {
		return nil
	}
	g.mu.Lock()
	before := g.before
	g.before = nil
	g.mu.Unlock()
	if before != nil {
		return before(ctx)
	}
	return nil
}
func (g *governanceLinkIdentity) ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error) {
	if err := g.beforeTarget(ctx, subject); err != nil {
		return nil, err
	}
	return g.roles.ListRoles(ctx, subject)
}
func (g *governanceLinkIdentity) IsMember(ctx context.Context, subject, tenant string) (bool, error) {
	if err := g.beforeTarget(ctx, subject); err != nil {
		return false, err
	}
	return g.membership.IsMember(ctx, subject, tenant)
}

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
			identity := &governanceLinkIdentity{roles: graph, membership: graph}
			router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
				Tokens: map[string]string{"admin": testutil.TenantA, "owner2": testutil.TenantA, "auditor": testutil.TenantA},
				Perms: map[string][]authz.Permission{
					"admin":   {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, workforce.PermSupplierManage, workforce.PermEnrollmentManage, workforce.PermLoginLinkManage, workforce.PermLoginLinkReview, workforce.PermIndependentLoginManage, workforce.PermIndependentLoginReview, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, supply.PermPartyWrite, supply.PermOrganizationManage},
					"owner2":  {iam.PermGovernanceReview, workforce.PermLoginLinkReview, workforce.PermIndependentLoginReview},
					"auditor": {workforce.PermLoginLinkReview, workforce.PermIndependentLoginReview},
				},
				Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(native), workforce.New(), pharmacy.New(), supply.New()},
				Writer:  graph, Roles: identity, Membership: identity,
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
				ownerAccess := governanceRequest(router, http.MethodPut, base+"/access/user-admin", "admin", `{"level":"manager"}`)
				require.Equal(t, 200, ownerAccess.Code, ownerAccess.Body.String())
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
			ownerPath, ownerMethod := links[1]+"/decision", http.MethodPost
			if kind != "hospital" {
				ownerPath, ownerMethod = links[1]+"/commands/"+uuid.Must(uuid.NewV7()).String(), http.MethodPut
			}
			ownerReview := governanceRequest(router, ownerMethod, ownerPath, "owner2", `{"expected_version":1,"decision":"approve","reason":"Current owner independently reviews target"}`)
			ownerExpected := 200
			if kind != "hospital" {
				ownerExpected = 201
			}
			require.Equal(t, ownerExpected, ownerReview.Code, ownerReview.Body.String())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				require.NoError(t, tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)", roots[1].String()).Error)
				var evidence struct {
					AuthorityKind     string
					OwnerMembershipID uuid.UUID
					SourceGrantID     *uuid.UUID
				}
				require.NoError(t, tx.Table("workforce_login_link_authorities").Where("tenant_id=? AND employer_id=? AND staff_id=?", testutil.TenantA, roots[1], people[1]).Take(&evidence).Error)
				require.Equal(t, "owner", evidence.AuthorityKind)
				require.Nil(t, evidence.SourceGrantID)
				var owner struct{ ID uuid.UUID }
				require.NoError(t, tx.Table("iam_members").Select("id").Where("tenant_id=? AND subject='user-owner2' AND role_key='tenant_admin'", testutil.TenantA).Take(&owner).Error)
				require.Equal(t, owner.ID, evidence.OwnerMembershipID)
				var managers int64
				require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject='user-owner2'", testutil.TenantA).Count(&managers).Error)
				require.Zero(t, managers, "current owner review uses its actual membership source, not a private manager row")
				return nil
			}))
			for _, fault := range []struct {
				name, install, remove string
				status                int
			}{
				{"recorder_disabled", "ALTER TABLE workforce_login_links DISABLE TRIGGER workforce_login_review_record", "ALTER TABLE workforce_login_links ENABLE TRIGGER workforce_login_review_record", 503},
				{"evidence_unavailable", `CREATE FUNCTION test_governance_link_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic review evidence failure'; END $$; CREATE TRIGGER test_governance_link_fault BEFORE INSERT ON workforce_login_link_authorities FOR EACH ROW EXECUTE FUNCTION test_governance_link_fault()`, "DROP FUNCTION test_governance_link_fault() CASCADE", 500},
			} {
				t.Run(fault.name, func(t *testing.T) {
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_link_" + fault.name, SQL: fault.install}}))
					defer func() {
						require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_link_restore_" + fault.name, SQL: fault.remove}}))
					}()
					failed := decide(0, "Review cannot omit its source evidence")
					require.Equal(t, fault.status, failed.Code, failed.Body.String())
					require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
						if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_login_link_access','authorized',true)", roots[0].String()).Error; err != nil {
							return err
						}
						linkID := uuid.MustParse(links[0][len(links[0])-36:])
						var parent struct {
							Status  string
							Version int64
						}
						require.NoError(t, tx.Table("workforce_login_links").Select("status,version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", testutil.TenantA, roots[0], people[0], linkID).Take(&parent).Error)
						require.Equal(t, "pending", parent.Status)
						require.EqualValues(t, 1, parent.Version)
						var evidence, history, changes, commands int64
						require.NoError(t, tx.Table("workforce_login_link_authorities").Where("tenant_id=? AND employer_id=? AND link_id=?", testutil.TenantA, roots[0], linkID).Count(&evidence).Error)
						require.Zero(t, evidence)
						require.NoError(t, tx.Table("workforce_login_link_history").Where("tenant_id=? AND hospital_id=? AND id=? AND version>1", testutil.TenantA, roots[0], linkID).Count(&history).Error)
						require.Zero(t, history)
						require.NoError(t, tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action IN('workforce.login_link.changed','workforce.login_link.review_authority') AND actor_subject='user-auditor'", testutil.TenantA, roots[0], linkID).Count(&changes).Error)
						require.Zero(t, changes)
						if kind != "hospital" {
							require.NoError(t, tx.Table("workforce_independent_link_commands").Where("tenant_id=? AND employer_id=? AND link_id=?", testutil.TenantA, roots[0], linkID).Count(&commands).Error)
							require.Zero(t, commands, "a failed review cannot retain a successful command receipt")
						}
						return nil
					}))
				})
			}
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
			if kind != "hospital" {
				base += "?directory_context=v1"
			}
			denied := governanceRequest(router, http.MethodGet, base, "auditor", "")
			require.True(t, denied.Code == 403 || denied.Code == 404, denied.Body.String())

			siblingID := uuid.Must(uuid.NewV7())
			siblingInput := input
			siblingInput.ScopeID = &roots[1]
			siblingInput.Reason = "Retain a different employer's review source"
			siblingRaw, err := json.Marshal(siblingInput)
			require.NoError(t, err)
			sibling := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+siblingID.String(), "admin", string(siblingRaw))
			require.Equal(t, 202, sibling.Code, sibling.Body.String())
			siblingApproval := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+siblingID.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", `{"expected_version":1,"decision":"approve","reason":"Independent owner confirms sibling employer"}`)
			require.Equal(t, 202, siblingApproval.Code, siblingApproval.Body.String())
			oldPath, oldMethod := links[0]+"/decision", http.MethodPost
			if kind != "hospital" {
				oldPath, oldMethod = links[0]+"/commands/"+uuid.Must(uuid.NewV7()).String(), http.MethodPut
			}
			prior := governanceRequest(router, oldMethod, oldPath, "admin", `{"expected_version":2,"decision":"revoke","reason":"Prepare a fresh identity review"}`)
			changeStatus := 200
			if kind != "hospital" {
				changeStatus = 201
			}
			require.Equal(t, changeStatus, prior.Code, prior.Body.String())
			links[0] = links[0][:len(links[0])-36] + uuid.Must(uuid.NewV7()).String()
			proposal := map[string]any{"subject": "user-reviewer", "reason": "Fresh pending account link"}
			if kind != "hospital" {
				proposal["expected_staff_version"] = 1
			}
			proposalRaw, err := json.Marshal(proposal)
			require.NoError(t, err)
			pending := governanceRequest(router, http.MethodPut, links[0], "admin", string(proposalRaw))
			expectedProposal := 200
			if kind != "hospital" {
				expectedProposal = 201
			}
			require.Equal(t, expectedProposal, pending.Code, pending.Body.String())
			var before int64
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("audit_events").Where("resource_type='iam_governance_grants' AND resource_id=? AND action='iam.governance.source.use'", id).Count(&before).Error
			}))
			hookResult := make(chan error, 1)
			cutoff := func(nativeCtx context.Context) error {
				if db.GuardedCommandActive(nativeCtx) {
					return fmt.Errorf("native target lookup retained a database guard")
				}
				var reads int64
				if err := db.WithTenant(nativeCtx, testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Table("audit_events").Where("resource_type='iam_governance_grants' AND resource_id=? AND action='iam.governance.source.use'", id).Count(&reads).Error
				}); err != nil {
					return err
				}
				if reads <= before {
					return fmt.Errorf("native lookup ran before the source preflight completed")
				}
				withdrawn := governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":2,"decision":"revoke","reason":"Withdraw source during native target observation"}`)
				if withdrawn.Code != 202 {
					return fmt.Errorf("source cutoff returned %d: %s", withdrawn.Code, withdrawn.Body.String())
				}
				return nil
			}
			identity.mu.Lock()
			identity.before = func(nativeCtx context.Context) error { err := cutoff(nativeCtx); hookResult <- err; return err }
			identity.mu.Unlock()
			lost := decide(0, "Target identity checked after preflight")
			select {
			case err := <-hookResult:
				require.NoError(t, err)
			default:
				t.Fatal("the native target observation did not run after preflight")
			}
			require.Equal(t, 403, lost.Code, lost.Body.String())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var routing int64
				require.NoError(t, tx.Table("iam_members").Where("tenant_id=? AND subject='user-auditor' AND role_key='governance_delegate'", testutil.TenantA).Count(&routing).Error)
				require.EqualValues(t, 1, routing, "a sibling grant preserves coarse routing while this employer's grant is revoked")
				require.NoError(t, tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_login_link_access','authorized',true)", roots[0].String()).Error)
				linkID := uuid.MustParse(links[0][len(links[0])-36:])
				var parent struct {
					Status  string
					Version int64
				}
				require.NoError(t, tx.Table("workforce_login_links").Select("status,version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", testutil.TenantA, roots[0], people[0], linkID).Take(&parent).Error)
				require.Equal(t, "pending", parent.Status)
				require.EqualValues(t, 1, parent.Version)
				var evidence int64
				require.NoError(t, tx.Table("workforce_login_link_authorities").Where("tenant_id=? AND employer_id=? AND link_id=?", testutil.TenantA, roots[0], linkID).Count(&evidence).Error)
				require.Zero(t, evidence, "stale source cannot append review evidence or activate the link")
				return nil
			}))

		})
	}
}
