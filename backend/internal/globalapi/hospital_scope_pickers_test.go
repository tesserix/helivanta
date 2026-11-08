package globalapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// permissiveHospitalAuthority grants every scope so only workforce's own establishment filter can exclude a candidate.
type permissiveHospitalAuthority struct {
	db        *tenantdb.DB
	mu        sync.Mutex
	consulted []string
}

func (a *permissiveHospitalAuthority) AuthorizeScope(_ context.Context, r authz.ScopeRequest) (authz.ScopeGrant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.consulted = append(a.consulted, r.HospitalID)
	return authz.ScopeGrant{AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1, ScopeKind: "hospital"}, nil
}

func (a *permissiveHospitalAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, _ authz.ScopeGrant, _ time.Time, action func(context.Context) error) error {
	return a.db.WithGuardedTenantCommand(ctx, r.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, action)
}

func (a *permissiveHospitalAuthority) WithScopeGrants(ctx context.Context, checks []authz.ScopeCheck, _ time.Time, action func(context.Context) error) error {
	return a.db.WithGuardedTenantCommand(ctx, checks[0].Request.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, action)
}

func (a *permissiveHospitalAuthority) consultedFor(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, hospital := range a.consulted {
		if hospital == id {
			return true
		}
	}
	return false
}

func TestHospitalScopePickersIgnoreIndependentEmployerAccounts(t *testing.T) {
	permissions := []authz.Permission{supply.PermPartyWrite, supply.PermOrganizationManage, workforce.PermSupplierManage, workforce.PermIndependentLoginManage, workforce.PermIndependentLoginReview}
	authority := &permissiveHospitalAuthority{}
	backend, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Membership: &supplierIdentityMembership{},
		Tokens:     map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA, "operator": testutil.TenantA},
		Perms:      map[string][]authz.Permission{"owner": permissions, "reviewer": permissions, "operator": {}},
		Modules:    []platform.Module{supply.New(), workforce.New()},
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer {
			authority.db = db
			return authority
		},
	})
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"supply": server.URL, "workforce": server.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	must := func(method, path, token, body string, status int) string {
		code, out := call(method, path, token, body)
		require.Equal(t, status, code, out)
		return out
	}
	supplier := uuid.Must(uuid.NewV7()).String()
	must("PUT", "/v1/supply/parties/"+supplier, "owner", `{"expected_version":0,"code":"TEST-PICKER-SUPPLIER","record_kind":"owned_organisation","legal_name":"TEST Picker Supplier","trading_name":"TEST Picker Supplier","business_kind":"wholesaler","country_code":"IN","jurisdiction":"IN-OD","registration_number":"TEST-REG-PICKER","corporate_group":"","website":"","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, 200)
	base := "/v1/workforce/suppliers/" + supplier
	must("PUT", base, "owner", `{}`, 200)
	must("PUT", base+"/access/user-reviewer", "owner", `{"level":"manager"}`, 200)
	staff := uuid.Must(uuid.NewV7()).String()
	must("PUT", base+"/staff/"+staff+"?directory_context=v1", "owner", `{"employee_code":"TEST-PICKER-OP","display_name":"TEST Picker Operator","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`, 200)
	link := base + "/staff/" + staff + "/account-links/" + uuid.Must(uuid.NewV7()).String()
	must("PUT", link, "owner", `{"expected_staff_version":1,"subject":"user-operator","reason":"TEST checked account ownership"}`, 201)
	approved := must("PUT", link+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", `{"expected_version":1,"decision":"approve","reason":"TEST independently checked identity"}`, 201)
	require.Contains(t, approved, `"status":"active"`)
	for _, picker := range []string{"leave-review-scopes", "hr-intake-scopes", "employment-review-scopes", "person-review-scopes", "posting-scopes", "posting-review-scopes", "staff-actor-scopes"} {
		t.Run(picker, func(t *testing.T) {
			code, out := call("GET", "/v1/workforce/"+picker+"?limit=20", "operator", "")
			require.Equal(t, 200, code, out)
			var page struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &page))
			require.JSONEq(t, `[]`, string(page.Data), "a supplier employer is not a hospital scope")
			require.NotContains(t, out, supplier)
			require.False(t, authority.consultedFor(supplier), "hospital authority must not be asked about a supplier employer")
		})
	}
}
