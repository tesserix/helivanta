package supply_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's public API under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func operationalEntryHarness(t *testing.T) *gin.Engine {
	t.Helper()
	admin := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.organization.manage", "supply.party.access.manage", "supply.premise.read", "supply.premise.write", "supply.bin.read", "supply.bin.write", "supply.operations.read", "supply.operations.write", "supply.support.read", "supply.delivery.read", "supply.delivery.write"}
	reader := []authz.Permission{"supply.party.read", "supply.directory.read", "supply.premise.read", "supply.operations.read", "supply.operations.write", "supply.support.read", "supply.delivery.read", "supply.delivery.write", "supply.delivery.assigned.read", "supply.delivery.assigned.action"}
	tokens := map[string]string{"owner": testutil.TenantA}
	permissions := map[string][]authz.Permission{"owner": admin}
	for _, token := range []string{"inventory", "auditor", "driver", "regulator", "outsider", "foreign"} {
		tokens[token] = testutil.TenantA
		permissions[token] = reader
	}
	tokens["foreign"] = testutil.TenantB
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: tokens, Perms: permissions, Roles: operatingMembership{}, Modules: []platform.Module{supply.New()}})
	return r
}

func TestOperatingAccessEntrypointSeparatesScopedRolesFromPrivateAuthority(t *testing.T) {
	r := operationalEntryHarness(t)
	root := structureParty(t, r, "TEST-OPERATING-ENTRY", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	other := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+other, "owner", strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-ENTRY-OTHER", 1)).Code)
	for _, role := range []string{"inventory", "auditor", "driver", "regulator"} {
		operatingGrant(t, r, root, premise, role, role)
		w := testutil.Do(r, "GET", root+"/access", role, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var access struct {
			Roles          []string `json:"roles"`
			OperatingRoles []string `json:"operating_roles"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &access))
		require.Empty(t, access.Roles)
		require.Equal(t, []string{role}, access.OperatingRoles)
		w = testutil.Do(r, "GET", "/v1/supply/parties", role, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"access_roles":[]`)
		require.Contains(t, w.Body.String(), `"operating_roles":["`+role+`"]`)
		require.NotContains(t, w.Body.String(), "registration_number")
		require.Equal(t, 404, testutil.Do(r, "GET", root, role, "").Code)
		require.Equal(t, 404, testutil.Do(r, "GET", root+"/premises/"+premise, role, "").Code)
		require.Equal(t, 200, testutil.Do(r, "GET", root+"/operating-locations/"+premise, role, "").Code)
		require.Equal(t, 404, testutil.Do(r, "GET", root+"/operating-locations/"+other, role, "").Code)
	}
	for _, token := range []string{"outsider", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", root+"/access", token, "").Code)
		require.NotContains(t, testutil.Do(r, "GET", "/v1/supply/parties", token, "").Body.String(), "TEST-OPERATING-ENTRY")
	}
}

func TestOperatingAccessAndMutationsStopWhenPremiseBecomesInactive(t *testing.T) {
	r := operationalEntryHarness(t)
	root := structureParty(t, r, "TEST-OPERATING-INACTIVE", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	operatingGrant(t, r, root, premise, "inventory", "inventory")
	bin := root + "/operating-locations/" + premise + "/bins/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", bin, "inventory", binBody(0, "TEST-INACTIVE-BIN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	deactivate := strings.Replace(premiseBody(1, "IN"), `"status":"active"`, `"status":"inactive"`, 1)
	w = testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", deactivate)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", root+"/operating-locations/"+premise, "inventory", "")
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", bin, "inventory", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", bin, "inventory", binBody(1, "TEST-INACTIVE-BIN")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/access", "inventory", "").Code)
	w = testutil.Do(r, "GET", root+"/operating-locations", "inventory", "")
	require.NotContains(t, w.Body.String(), premise)
	w = testutil.Do(r, "GET", "/v1/supply/parties", "inventory", "")
	require.NotContains(t, w.Body.String(), "TEST-OPERATING-INACTIVE")
	require.Equal(t, 200, testutil.Do(r, "GET", root+"/premises/"+premise, "owner", "").Code)
}

func TestDeliveryHistoryRetainsGlobalReadAfterWarehouseDeactivation(t *testing.T) {
	r := operationalEntryHarness(t)
	root := structureParty(t, r, "TEST-DELIVERY-ARCHIVE", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	for _, role := range []string{"inventory", "auditor", "driver", "regulator"} {
		operatingGrant(t, r, root, premise, role, role)
	}
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-outsider", "auditor", nil, from, from.Add(time.Hour), "active")).Code)
	base := root + "/premises/" + premise
	path := base + "/delivery-tasks/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", deliveryBody(0, "", "draft")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", deliveryBody(1, "", "cancelled")).Code)
	assignedID := uuid.Must(uuid.NewV7()).String()
	assigned := strings.ReplaceAll(strings.ReplaceAll(deliveryBody(0, "user-driver", "assigned"), "TEST-DELIVERY", "TEST-ACTIVE-ARCHIVE"), "TEST-MANUAL-LOGISTICS-REF", "TEST-ACTIVE-ARCHIVE-REF")
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/delivery-tasks/"+assignedID, "owner", assigned).Code)
	deactivate := strings.Replace(premiseBody(1, "IN"), `"status":"active"`, `"status":"inactive"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", deactivate).Code)
	for _, token := range []string{"owner", "outsider"} {
		w := testutil.Do(r, "GET", root+"/operating-locations/"+premise, token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"inactive"`)
		require.Contains(t, w.Body.String(), `"allowed_actions":["read_deliveries"]`)
		w = testutil.Do(r, "GET", root+"/operating-locations", token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), premise)
		require.Contains(t, w.Body.String(), `"allowed_actions":["read_deliveries"]`)
		w = testutil.Do(r, "GET", path, token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"allowed_actions":[]`)
		require.Contains(t, w.Body.String(), `"status":"cancelled"`)
		w = testutil.Do(r, "GET", base+"/delivery-tasks", token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), `"allowed_actions":["`)
		w = testutil.Do(r, "GET", path+"/revisions", token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"version":1`)
		require.Contains(t, w.Body.String(), `"version":2`)
	}
	for _, token := range []string{"inventory", "auditor", "driver", "regulator", "foreign"} {
		for _, endpoint := range []string{path, base + "/delivery-tasks", path + "/revisions"} {
			require.Equal(t, 404, testutil.Do(r, "GET", endpoint, token, "").Code)
		}
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", deliveryBody(1, "", "cancelled")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", base+"/delivery-drivers", "owner", "").Code)
	driverPath := base + "/my-deliveries/" + assignedID
	require.Equal(t, 404, testutil.Do(r, "GET", driverPath, "driver", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", driverPath+"/actions/"+uuid.Must(uuid.NewV7()).String(), "driver", driverActionBody(1, 1, "start")).Code)
}

func TestDeliveryOriginGuardRejectsStaleMutationAfterDeactivation(t *testing.T) {
	r, db, party, premise, path, _ := deliveryFixture(t)
	deactivate := strings.Replace(premiseBody(1, "IN"), `"status":"active"`, `"status":"inactive"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", party+"/premises/"+premise, "owner", deactivate).Code)
	partyID := stringsLast(party)
	err := partyTransaction(t, db, partyID, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_subject','user-dispatcher',true),set_config('app.actor_subject','user-dispatcher',true)`).Error; e != nil {
			return e
		}
		if e := tx.Exec(`UPDATE supply_delivery_tasks SET status='cancelled',driver_subject='',assignment_expires_at=NULL,assignment_generation=assignment_generation+1,version=version+1 WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=?`, testutil.TenantA, partyID, premise, stringsLast(path)).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO supply_delivery_revisions(id,tenant_id,party_id,premise_id,delivery_id,version,snapshot,actor_subject,reason) SELECT ?,tenant_id,party_id,premise_id,id,version,to_jsonb(supply_delivery_tasks),'user-dispatcher','TEST stale origin mutation' FROM supply_delivery_tasks WHERE tenant_id=? AND party_id=? AND premise_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, partyID, premise, stringsLast(path)).Error
	})
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg)
	require.Equal(t, "supply_delivery_origin", pg.ConstraintName)
	w := testutil.Do(r, "GET", path, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"assigned"`)
	require.Contains(t, w.Body.String(), `"version":1`)
}
