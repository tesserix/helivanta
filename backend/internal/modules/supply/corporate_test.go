package supply_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func structureHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	admin := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.organization.manage", "supply.party.access.manage", "supply.premise.read", "supply.premise.write", "supply.territory.read", "supply.territory.write"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Roles: accessMembership{},
		Tokens:  map[string]string{"owner": testutil.TenantA, "manager": testutil.TenantA, "sales": testutil.TenantA, "auditor": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"owner": admin, "manager": admin, "foreign": admin, "sales": {"supply.directory.read", "supply.territory.read"}, "auditor": {"supply.party.read", "supply.territory.read"}},
		Modules: []platform.Module{supply.New()},
	})
	return r, db
}

func structureParty(t *testing.T, r *gin.Engine, code, token string) string {
	t.Helper()
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.ReplaceAll(partyBody(0, "TEST "+code), "counterparty", "owned_organisation")
	body = strings.ReplaceAll(strings.ReplaceAll(body, "TEST-DISTRIBUTOR", code), "TEST-REG-001", "TEST-REG-"+code)
	w := testutil.Do(r, "PUT", path, token, body)
	require.Equal(t, 200, w.Code, w.Body.String())
	return path
}

func corporateBody(version int, parent string) string {
	var id any
	if parent != "" {
		id = strings.TrimPrefix(parent, "/v1/supply/parties/")
	}
	value, err := json.Marshal(id)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"expected_version":%d,"parent_party_id":%s}`, version, value)
}

func TestCorporateRootAndSubsidiaryAreVersionedAndRetrySafe(t *testing.T) {
	r, _ := structureHarness(t)
	parent := structureParty(t, r, "TEST-CORPORATE-PARENT", "owner")
	child := structureParty(t, r, "TEST-CORPORATE-CHILD", "owner")
	path := child + "/corporate-structure"
	w := testutil.Do(r, "GET", path, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":0`)
	require.Contains(t, w.Body.String(), `"created_at":null`)
	require.Equal(t, 200, testutil.Do(r, "PUT", parent+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	w = testutil.Do(r, "PUT", path, "owner", corporateBody(0, parent))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":1`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", corporateBody(0, parent)).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	w = testutil.Do(r, "PUT", path, "owner", corporateBody(1, ""))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"parent_party_id":null`)
	require.Contains(t, w.Body.String(), `"version":2`)
	w = testutil.Do(r, "GET", path+"/revisions", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"version":2`)
}

func TestCorporateRelationshipsRequireBothOwnersAndNeverInheritAccess(t *testing.T) {
	r, _ := structureHarness(t)
	parent := structureParty(t, r, "TEST-PARENT-ACCESS", "manager")
	child := structureParty(t, r, "TEST-CHILD-ACCESS", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", parent+"/corporate-structure", "manager", corporateBody(0, "")).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", child+"/corporate-structure", "owner", corporateBody(0, parent)).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", parent+"/access/user-owner/administrator", "manager", "").Code)
	w := testutil.Do(r, "PUT", child+"/corporate-structure", "owner", corporateBody(0, parent))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", child, "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", child+"/corporate-structure", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", child+"/corporate-structure", "foreign", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", parent+"/access/user-owner/administrator", "manager", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", child+"/corporate-structure", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", parent+"/corporate-structure", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", child+"/corporate-structure", "owner", corporateBody(1, "")).Code)
}

func TestCorporateTwoLevelForeignKeysPreventCyclesAndCounterpartyParents(t *testing.T) {
	r, db := structureHarness(t)
	a := structureParty(t, r, "TEST-GROUP-A", "owner")
	b := structureParty(t, r, "TEST-GROUP-B", "owner")
	c := structureParty(t, r, "TEST-GROUP-C", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", a+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", b+"/corporate-structure", "owner", corporateBody(0, a)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", c+"/corporate-structure", "owner", corporateBody(0, b)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", a+"/corporate-structure", "owner", corporateBody(1, b)).Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", a+"/corporate-structure", "owner", corporateBody(1, a)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", b+"/corporate-structure", "owner", corporateBody(99, a)).Code)
	id := strings.TrimPrefix(a, "/v1/supply/parties/")
	parent := strings.TrimPrefix(b, "/v1/supply/parties/")
	err := partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE supply_corporate_structures SET parent_party_id=?,version=version+1 WHERE party_id=?`, parent, id).Error
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "supply_corporate_root_reference")
	for _, query := range []string{`UPDATE supply_corporate_revisions SET version=99`, `DELETE FROM supply_corporate_structures`, `DELETE FROM supply_corporate_revisions`} {
		require.Error(t, partyTransaction(t, db, id, func(tx *gorm.DB) error { return tx.Exec(query).Error }))
	}
	other := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", other, "owner", strings.ReplaceAll(strings.ReplaceAll(partyBody(0, "TEST Counterparty"), "TEST-DISTRIBUTOR", "TEST-COUNTERPARTY"), "TEST-REG-001", "TEST-COUNTERPARTY")).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", other+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", c+"/corporate-structure", "owner", corporateBody(0, other)).Code)
}

func TestCorporateConcurrentReverseLinksCannotBothCommit(t *testing.T) {
	r, _ := structureHarness(t)
	a := structureParty(t, r, "TEST-RACE-A", "owner")
	b := structureParty(t, r, "TEST-RACE-B", "owner")
	for _, path := range []string{a, b} {
		require.Equal(t, 200, testutil.Do(r, "PUT", path+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	}
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan int, 2)
	go func() {
		start.Wait()
		results <- testutil.Do(r, "PUT", a+"/corporate-structure", "owner", corporateBody(1, b)).Code
	}()
	go func() {
		start.Wait()
		results <- testutil.Do(r, "PUT", b+"/corporate-structure", "owner", corporateBody(1, a)).Code
	}()
	start.Done()
	first, second := <-results, <-results
	require.ElementsMatch(t, []int{200, 409}, []int{first, second})
}

func TestCorporateConcurrentReplayAndOutboxFailurePreserveExactlyOneRevision(t *testing.T) {
	r, db := structureHarness(t)
	owner := structureParty(t, r, "TEST-CORPORATE-ATOMIC", "owner")
	path := owner + "/corporate-structure"
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan int, 4)
	for range 4 {
		go func() { start.Wait(); results <- testutil.Do(r, "PUT", path, "owner", corporateBody(0, "")).Code }()
	}
	start.Done()
	for range 4 {
		require.Equal(t, 200, <-results)
	}
	id := strings.TrimPrefix(owner, "/v1/supply/parties/")
	var counts struct{ Revisions, Audits, Events int }
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_corporate_revisions WHERE party_id=?) revisions,(SELECT count(*) FROM audit_events WHERE resource_type='supply_corporate_structures' AND resource_id=?) audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.corporate_updated.v1' AND payload->'data'->>'party_id'=?) events`, id, id, id).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Events)
	parent := structureParty(t, r, "TEST-CORPORATE-ROLLBACK-PARENT", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", parent+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_corporate_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_corporate_outbox_failure CHECK(subject <> 'helivanta.in.supply.corporate_updated.v1') NOT VALID`}}))
	w := testutil.Do(r, "PUT", path, "owner", corporateBody(1, parent))
	require.Equal(t, 500, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", path, "owner", "")
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"parent_party_id":null`)
}
