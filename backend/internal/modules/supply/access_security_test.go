package supply_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's public test interface
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestAccessManagerRequiresCurrentAdministratorOnTargetParty(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Private Target")).Code)
	grant := root + "/access/user-manager/administrator"
	denied := testutil.Do(r, "PUT", grant, "manager", "")
	require.Equal(t, 404, denied.Code, denied.Body.String())
	require.Equal(t, 404, testutil.Do(r, "DELETE", root+"/access/user-owner/administrator", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "manager", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", root, "owner", "").Code)
}

func TestAccessCannotRemoveLastAdministrator(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Retained Admin")).Code)
	denied := testutil.Do(r, "DELETE", root+"/access/user-owner/administrator", "owner", "")
	require.Equal(t, 409, denied.Code, denied.Body.String())
	require.Equal(t, 200, testutil.Do(r, "GET", root, "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-manager/administrator", "owner", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", root+"/access/user-owner/administrator", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", root+"/access/user-auditor/auditor", "owner", "").Code)
}

type accessMembership struct{}

func (accessMembership) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	if subject == "user-owner" || subject == "user-manager" || subject == "user-auditor" || subject == "user-sales" {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleSupplyAdmin}}, nil
	}
	return nil, nil
}
func accessHarnessWithRoles(t *testing.T, roles platform.RoleLister) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	permissions := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.party.access.manage", "supply.organization.manage", "supply.premise.read", "supply.premise.write", "supply.license.read", "supply.license.write", "supply.bin.read", "supply.bin.write"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "manager": testutil.TenantA, "auditor": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": permissions, "manager": permissions, "auditor": permissions, "foreign": permissions}, Roles: roles, Modules: []platform.Module{supply.New()}})
	return r, db
}
func ownedAccessParty(t *testing.T, r *gin.Engine) string {
	t.Helper()
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(partyBody(0, "TEST Explicit Access"), "counterparty", "owned_organisation", 1)
	w := testutil.Do(r, "PUT", root, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	return root
}
func TestLegacyAccessRequiresVerifiedNewTargetMembership(t *testing.T) {
	r, _ := accessHarnessWithRoles(t, accessMembership{})
	root := ownedAccessParty(t, r)
	denied := testutil.Do(r, "PUT", root+"/access/not-a-tenant-member/auditor", "owner", "")
	require.Equal(t, 404, denied.Code, denied.Body.String())
}

type legacyMembershipProbe struct {
	db          *tenantdb.DB
	party       string
	calls       int
	unavailable bool
}

func (p *legacyMembershipProbe) ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error) {
	p.calls++
	if p.unavailable {
		return nil, errors.New("TEST identity unavailable")
	}
	var free bool
	if e := p.db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0))`, testutil.TenantA+":supply-access-admin:"+p.party).Scan(&free).Error
	}); e != nil {
		return nil, e
	}
	if !free {
		return nil, errors.New("membership lookup held a business lock")
	}
	return accessMembership{}.ListRoles(ctx, subject)
}
func TestLegacyGrantReplayAndRevocationDoNotRequireTargetLookup(t *testing.T) {
	probe := &legacyMembershipProbe{}
	r, db := accessHarnessWithRoles(t, probe)
	probe.db = db
	root := ownedAccessParty(t, r)
	probe.party = strings.TrimPrefix(root, "/v1/supply/parties/")
	path := root + "/access/user-auditor/auditor"
	w := testutil.Do(r, "PUT", path, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, probe.calls, "new target verified without a business lock")
	probe.unavailable = true
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", "").Body.String())
	require.Equal(t, 1, probe.calls)
	require.Equal(t, 204, testutil.Do(r, "DELETE", path, "owner", "").Code)
	require.Equal(t, 1, probe.calls)
	failed := testutil.Do(r, "PUT", path, "owner", "")
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.Equal(t, 2, probe.calls)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
}
