package supply_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"
)

func territoryBody(version int, subjects string) string {
	return fmt.Sprintf(`{"expected_version":%d,"code":"TEST-TERRITORY","name":"TEST Territory","country_code":"IN","region":"TEST Maharashtra","city":"TEST Pune","description":"TEST Synthetic territory","status":"active","branch_premise_id":null,"assigned_sales_subjects":%s}`, version, subjects)
}

func TestTerritoryAssignmentsRejectMissingAuthorityAndDuplicateSubjects(t *testing.T) {
	r, _ := structureHarness(t)
	owner := structureParty(t, r, "TEST-ASSIGNMENTS", "owner")
	path := owner + "/territories/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", territoryBody(0, `["user-sales"]`)).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-sales/sales", "owner", "").Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", territoryBody(0, `["user-sales","user-sales"]`)).Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", territoryBody(0, `["invalid subject"]`)).Code)
	w := testutil.Do(r, "PUT", path, "owner", territoryBody(0, `["user-sales"]`))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path, "manager", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-manager/purchasing", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "manager", territoryBody(1, `[]`)).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/revisions", "sales", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", owner+"/access/user-sales/sales", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "sales", "").Code)
	w = testutil.Do(r, "PUT", path, "owner", territoryBody(1, `[]`))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"assigned_sales_subjects":[]`)
	require.Contains(t, w.Body.String(), `"version":2`)
	w = testutil.Do(r, "GET", path+"/revisions?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"has_more":true`)
}

func TestTerritoryCountryBranchAndImmutableDatabaseBoundaries(t *testing.T) {
	r, db := structureHarness(t)
	owner := structureParty(t, r, "TEST-TERRITORY-BOUNDARIES", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/corporate-structure", "owner", corporateBody(0, "")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-sales/sales", "owner", "").Code)
	branchID := uuid.Must(uuid.NewV7()).String()
	branch := owner + "/premises/" + branchID
	require.Equal(t, 200, testutil.Do(r, "PUT", branch, "owner", branchPremiseBody(0, "IN", "TEST-TERRITORY-BRANCH", "branch", "omitted")).Code)
	path := owner + "/territories/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(territoryBody(0, `["user-sales"]`), `"branch_premise_id":null`, `"branch_premise_id":"`+branchID+`"`, 1)
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", branch, "owner", branchPremiseBody(1, "IN", "TEST-TERRITORY-BRANCH", "warehouse", "omitted")).Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", owner+"/territories/"+uuid.Must(uuid.NewV7()).String(), "owner", strings.Replace(territoryBody(0, `[]`), `"IN"`, `"PK"`, 1)).Code)
	id := strings.TrimPrefix(owner, "/v1/supply/parties/")
	for _, query := range []string{`UPDATE supply_territory_revisions SET version=99`, `DELETE FROM supply_territories`, `DELETE FROM supply_territory_revisions`} {
		require.Error(t, partyTransaction(t, db, id, func(tx *gorm.DB) error { return tx.Exec(query).Error }))
	}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_structure_visible_tenant", SQL: `CREATE OR REPLACE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;`}}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true)`, id).Error; err != nil {
			return err
		}
		for _, table := range []string{"supply_territories", "supply_territory_revisions", "supply_territory_representatives", "supply_corporate_structures", "supply_corporate_revisions"} {
			var count int64
			if err := tx.Table(table).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, table)
		}
		return nil
	}))
}

func TestTerritoryListOnlyShowsAssignedActiveRecordsAndMaintainsHistory(t *testing.T) {
	r, _ := structureHarness(t)
	owner := structureParty(t, r, "TEST-TERRITORY-VIEWS", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-sales/sales", "owner", "").Code)
	first := owner + "/territories/" + uuid.Must(uuid.NewV7()).String()
	second := owner + "/territories/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", first, "owner", territoryBody(0, `["user-sales"]`)).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", second, "owner", strings.Replace(territoryBody(0, `[]`), "TEST-TERRITORY", "TEST-TERRITORY-SECOND", 1)).Code)
	w := testutil.Do(r, "GET", owner+"/territories", "sales", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var response struct{ Data []struct{ ID string } }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, strings.TrimPrefix(first, owner+"/territories/"), response.Data[0].ID)
	require.Equal(t, 404, testutil.Do(r, "GET", second, "sales", "").Code)
	w = testutil.Do(r, "PUT", first, "owner", strings.Replace(territoryBody(1, `["user-sales"]`), `"status":"active"`, `"status":"inactive"`, 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", first, "sales", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", first+"/revisions", "owner", "").Code)
}

func TestTerritoriesPersistUniqueSalesAssignmentsAndReplayOnce(t *testing.T) {
	r, _ := structureHarness(t)
	owner := structureParty(t, r, "TEST-TERRITORY-OWNER", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-sales/sales", "owner", "").Code)
	path := owner + "/territories/" + uuid.Must(uuid.NewV7()).String()
	body := territoryBody(0, `["user-sales"]`)
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"assigned_sales_subjects":["user-sales"]`)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", body).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	require.Equal(t, 200, testutil.Do(r, "GET", path, "sales", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", owner+"/territories", "sales", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
}

func TestTerritoryConcurrentRetryAndOutboxFailureKeepAssignmentsAtomic(t *testing.T) {
	r, db := structureHarness(t)
	owner := structureParty(t, r, "TEST-TERRITORY-ATOMIC", "owner")
	require.Equal(t, 200, testutil.Do(r, "PUT", owner+"/access/user-sales/sales", "owner", "").Code)
	territoryID := uuid.Must(uuid.NewV7()).String()
	path := owner + "/territories/" + territoryID
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan int, 4)
	for range 4 {
		go func() {
			start.Wait()
			results <- testutil.Do(r, "PUT", path, "owner", territoryBody(0, `["user-sales"]`)).Code
		}()
	}
	start.Done()
	for range 4 {
		require.Equal(t, 200, <-results)
	}
	id := strings.TrimPrefix(owner, "/v1/supply/parties/")
	var counts struct{ Revisions, Audits, Events, Assignments int }
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_territory_revisions WHERE territory_id=?) revisions,(SELECT count(*) FROM audit_events WHERE resource_type='supply_territories' AND resource_id=?) audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.territory_updated.v1' AND payload->'data'->>'territory_id'=?) events,(SELECT count(*) FROM supply_territory_representatives WHERE territory_id=?) assignments`, territoryID, territoryID, territoryID, territoryID).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Events)
	require.Equal(t, 1, counts.Assignments)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_territory_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_territory_outbox_failure CHECK(subject <> 'helivanta.in.supply.territory_updated.v1') NOT VALID`}}))
	w := testutil.Do(r, "PUT", path, "owner", territoryBody(1, `[]`))
	require.Equal(t, 500, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", path, "owner", "")
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"assigned_sales_subjects":["user-sales"]`)
}
