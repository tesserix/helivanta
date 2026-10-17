package supply_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's public test interface
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type operatingMembership struct{}

func (operatingMembership) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	if strings.HasPrefix(subject, "user-") {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleSupplyAdmin}}, nil
	}
	return nil, nil
}

func operatingHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	admin := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.organization.manage", "supply.party.access.manage", "supply.premise.read", "supply.premise.write", "supply.bin.read", "supply.bin.write", "supply.operations.read", "supply.operations.write", "supply.delivery.read", "supply.delivery.write", "supply.delivery.assigned.read", "supply.delivery.assigned.action"}
	tokens := map[string]string{}
	perms := map[string][]authz.Permission{}
	for _, token := range []string{"owner", "inventory", "picker", "packer", "dispatcher", "driver", "other-driver", "regulator", "finance", "foreign"} {
		tokens[token] = testutil.TenantA
		perms[token] = admin
	}
	tokens["foreign"] = testutil.TenantB
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: tokens, Perms: perms, Roles: operatingMembership{}, Modules: []platform.Module{supply.New()}})
	return r, db
}

func operatingGrant(t *testing.T, r *gin.Engine, party, premise, token, role string) string {
	t.Helper()
	path := party + "/grants/" + uuid.Must(uuid.NewV7()).String()
	start := time.Now().Add(-time.Hour)
	w := testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-"+token, role, premise, start, start.Add(24*time.Hour), "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	return path
}

func revokeOperatingGrant(t *testing.T, r *gin.Engine, grant string) {
	t.Helper()
	w := testutil.Do(r, "GET", grant, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	payload["expected_version"] = 1
	payload["status"] = "revoked"
	for _, field := range []string{"id", "party_id", "version", "created_at", "updated_at", "effective_status"} {
		delete(payload, field)
	}
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	w = testutil.Do(r, "PUT", grant, "owner", string(encoded))
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestOperatingLocationsUseRealBinsWithoutPrivateProfileAuthority(t *testing.T) {
	r, _ := operatingHarness(t)
	party := structureParty(t, r, "TEST-OPERATING-OWNER", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	operatingGrant(t, r, party, premise, "inventory", "inventory")
	w := testutil.Do(r, "GET", party+"/operating-locations", "inventory", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), premise)
	require.NotContains(t, w.Body.String(), "line1")
	require.NotContains(t, w.Body.String(), "registration_number")
	require.Equal(t, 404, testutil.Do(r, "GET", party, "inventory", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", party+"/premises/"+premise, "inventory", "").Code)
}

func TestOperatingInventoryEditsCanonicalBinsWithScopedReadOnlySeparation(t *testing.T) {
	r, db := operatingHarness(t)
	party := structureParty(t, r, "TEST-OPERATING-BINS", "owner")
	premiseID := uuid.Must(uuid.NewV7()).String()
	otherID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premiseID, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+otherID, "owner", strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-OTHER-WAREHOUSE", 1)).Code)
	for _, role := range []string{"inventory", "picker", "packer", "dispatcher", "driver", "regulator"} {
		operatingGrant(t, r, party, premiseID, role, role)
	}
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-finance", "finance", nil, from, from.Add(24*time.Hour), "active")).Code)
	binID := uuid.Must(uuid.NewV7()).String()
	path := party + "/operating-locations/" + premiseID + "/bins/" + binID
	body := binBody(0, "TEST-OP-BIN")
	w := testutil.Do(r, "PUT", path, "inventory", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "inventory", body).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", party+"/premises/"+premiseID+"/bins/"+binID, "owner", "").Body.String())
	for _, token := range []string{"picker", "packer", "dispatcher", "regulator"} {
		require.Equal(t, 200, testutil.Do(r, "GET", path, token, "").Code, token)
		require.Equal(t, 200, testutil.Do(r, "GET", path+"/revisions", token, "").Code, token)
		require.Equal(t, 404, testutil.Do(r, "PUT", path, token, binBody(1, "TEST-OP-BIN")).Code, token)
	}
	for _, token := range []string{"finance", "driver", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path, token, "").Code, token)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", party+"/operating-locations/"+otherID+"/bins", "inventory", "").Code)
	partyID := strings.TrimPrefix(party, "/v1/supply/parties/")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-finance',true)`, partyID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("supply_bins").Where("id=?", binID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestOperatingScopeRevocationAndMetadataContextAreImmediatelyEnforced(t *testing.T) {
	r, db := operatingHarness(t)
	party := structureParty(t, r, "TEST-OPERATING-REVOKE", "owner")
	premiseID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premiseID, "owner", premiseBody(0, "IN")).Code)
	grant := operatingGrant(t, r, party, premiseID, "inventory", "inventory")
	path := party + "/operating-locations/" + premiseID
	require.Equal(t, 200, testutil.Do(r, "GET", path, "inventory", "").Code)
	revokeOperatingGrant(t, r, grant)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "inventory", "").Code)
	partyID := strings.TrimPrefix(party, "/v1/supply/parties/")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.supply_directory','off',true),set_config('app.supply_manager','off',true)`, partyID).Error; err != nil {
			return err
		}
		var allowed bool
		if err := tx.Raw(`SELECT supply_operating_owned(?,?,?,ARRAY['administrator'])`, testutil.TenantA, partyID, premiseID).Scan(&allowed).Error; err != nil {
			return err
		}
		require.True(t, allowed)
		var flags struct{ Directory, Manager string }
		if err := tx.Raw(`SELECT current_setting('app.supply_directory',true) AS directory,current_setting('app.supply_manager',true) AS manager`).Scan(&flags).Error; err != nil {
			return err
		}
		require.Equal(t, "off", flags.Directory)
		require.Equal(t, "off", flags.Manager)
		return nil
	}))
}
