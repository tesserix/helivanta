package supply_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func branchPremiseBody(version int, country, code, kind string, branch any) string {
	var body map[string]any
	if err := json.Unmarshal([]byte(premiseBody(version, country)), &body); err != nil {
		panic(err)
	}
	body["code"], body["kind"] = code, kind
	if branch != "omitted" {
		body["branch_premise_id"] = branch
	}
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestWarehouseBranchAssignmentRoundTripsAndRetainsHistory(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", body).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted"))
	require.Equal(t, 200, w.Code, w.Body.String())
	path := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	linked := branchPremiseBody(0, "IN", "TEST-WAREHOUSE", "warehouse", branch)
	w = testutil.Do(r, "PUT", path, "owner", linked)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"branch_premise_id":"`+branch+`"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", linked).Body.String())
}

func TestBranchOmissionPreservesAssignmentAndNullUnlinksWithPagedHistory(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
	path := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch)).Code)
	omitted := strings.Replace(branchPremiseBody(1, "IN", "TEST-WH", "warehouse", "omitted"), "TEST Warehouse", "TEST Changed", 1)
	w := testutil.Do(r, "PUT", path, "owner", omitted)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"branch_premise_id":"`+branch+`"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", omitted).Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(1, "IN", "TEST-WH", "warehouse", nil)).Code)
	w = testutil.Do(r, "PUT", path, "owner", branchPremiseBody(2, "IN", "TEST-WH", "warehouse", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"branch_premise_id":null`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", branchPremiseBody(2, "IN", "TEST-WH", "warehouse", nil)).Body.String())
	pagePath := path + "/revisions?limit=1"
	for _, version := range []int{3, 2, 1} {
		w = testutil.Do(r, "GET", pagePath, "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct {
				Version  int
				Snapshot struct {
					Branch *string `json:"branch_premise_id"`
				}
			}
			Page struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.Equal(t, version, page.Data[0].Version)
		if version == 3 {
			require.Nil(t, page.Data[0].Snapshot.Branch)
		} else {
			require.NotNil(t, page.Data[0].Snapshot.Branch)
			require.Equal(t, branch, *page.Data[0].Snapshot.Branch)
		}
		require.Equal(t, version > 1, page.Page.HasMore)
		pagePath = path + "/revisions?limit=1&cursor=" + page.Page.NextCursor
	}
}

func TestBranchAssignmentsDenyForeignOwnersAndDoNotInheritRoles(t *testing.T) {
	r, _ := partyHarness(t)
	roots := []string{}
	branches := []string{}
	for i, actor := range []string{"owner", "owner", "foreign"} {
		root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
		body := strings.ReplaceAll(strings.ReplaceAll(strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1), "TEST-DISTRIBUTOR", fmt.Sprintf("TEST-OWNER-%d", i)), "TEST-REG-001", fmt.Sprintf("TEST-REG-%d", i))
		require.Equal(t, 200, testutil.Do(r, "PUT", root, actor, body).Code)
		branch := uuid.Must(uuid.NewV7()).String()
		require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, actor, branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
		roots = append(roots, root)
		branches = append(branches, branch)
	}
	path := roots[0] + "/premises/" + uuid.Must(uuid.NewV7()).String()
	for _, branch := range []string{branches[1], branches[2], uuid.Must(uuid.NewV7()).String()} {
		w := testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch))
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	body := branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branches[0])
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, actor := range []string{"manager", "foreign", "auditor"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path, actor, "").Code)
		require.Equal(t, 404, testutil.Do(r, "PUT", path, actor, body).Code)
	}
	grant := roots[0] + "/access/user-auditor/auditor"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "auditor", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "auditor", body).Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "auditor", "").Code)
	for _, role := range []string{"finance", "warehouse", "sales", "driver"} {
		grant = roots[0] + "/access/user-manager/" + role
		require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
		require.Equal(t, 404, testutil.Do(r, "PUT", path, "manager", body).Code)
		require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	}
}

func TestBranchKindsRetirementCountryAndDatabaseConstraints(t *testing.T) {
	for _, country := range []string{"IN"} {
		t.Run(country, func(t *testing.T) {
			r, db := partyHarness(t)
			party := uuid.Must(uuid.NewV7()).String()
			root := "/v1/supply/parties/" + party
			body := strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)
			require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", body).Code)
			branch := uuid.Must(uuid.NewV7()).String()
			branchPath := root + "/premises/" + branch
			require.Equal(t, 200, testutil.Do(r, "PUT", branchPath, "owner", branchPremiseBody(0, country, "TEST-BRANCH", "branch", "omitted")).Code)
			child := uuid.Must(uuid.NewV7()).String()
			path := root + "/premises/" + child
			for _, kind := range []string{"registered", "branch", "pharmacy"} {
				require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, country, "TEST-WH", kind, branch)).Code)
			}
			require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, country, "TEST-WH", "warehouse", uuid.NewString())).Code)
			require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, country, "TEST-WH", "warehouse", child)).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, country, "TEST-WH", "depot", branch)).Code)
			require.Equal(t, 404, testutil.Do(r, "PUT", root+"/premises/"+uuid.Must(uuid.NewV7()).String(), "owner", branchPremiseBody(0, country, "TEST-WH2", "warehouse", child)).Code)
			w := testutil.Do(r, "PUT", branchPath, "owner", branchPremiseBody(1, country, "TEST-BRANCH", "warehouse", "omitted"))
			require.Equal(t, 409, w.Code, w.Body.String())
			retired := strings.Replace(branchPremiseBody(1, country, "TEST-BRANCH", "branch", "omitted"), `"status":"active"`, `"status":"inactive"`, 1)
			require.Equal(t, 200, testutil.Do(r, "PUT", branchPath, "owner", retired).Code)
			require.Equal(t, 404, testutil.Do(r, "PUT", root+"/premises/"+uuid.Must(uuid.NewV7()).String(), "owner", branchPremiseBody(0, country, "TEST-NEW", "warehouse", branch)).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(1, country, "TEST-WH-CHANGED", "depot", branch)).Code)
			for _, query := range []string{
				`UPDATE supply_premises SET branch_premise_id=id,version=version+1 WHERE id=?`,
				`UPDATE supply_premises SET branch_premise_id='00000000-0000-0000-0000-000000000000',version=version+1 WHERE id=?`,
				`UPDATE supply_premises SET branch_premise_id='0199e000-0000-7000-8000-000000000099',version=version+1 WHERE id=?`,
				`UPDATE supply_premises SET kind='branch',version=version+1 WHERE id=?`,
			} {
				require.Error(t, partyTransaction(t, db, party, func(tx *gorm.DB) error { return tx.Exec(query, child).Error }))
			}
			require.Error(t, partyTransaction(t, db, party, func(tx *gorm.DB) error {
				return tx.Exec(`UPDATE supply_premises SET kind='warehouse',version=version+1 WHERE id=?`, branch).Error
			}))
		})
	}
}

func TestBranchAssignmentConcurrentRetriesPublishOnceAndRollBackOnOutboxFailure(t *testing.T) {
	r, db, bus, ctx := partySetup(t)
	party := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
	child := uuid.Must(uuid.NewV7()).String()
	path := root + "/premises/" + child
	delivered := make(chan events.Event, 20)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-branch-observer", Subject: "helivanta.in.supply.premise_updated.v1", Handle: func(_ context.Context, _ *gorm.DB, e events.Event) error { delivered <- e; return nil }}}))
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan int, 4)
	for range 4 {
		go func() {
			start.Wait()
			results <- testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch)).Code
		}()
	}
	start.Done()
	for range 4 {
		require.Equal(t, 200, <-results)
	}
	for {
		select {
		case event := <-delivered:
			var data map[string]any
			require.NoError(t, json.Unmarshal(event.Data, &data))
			if data["premise_id"] != child {
				continue
			}
			require.Equal(t, map[string]any{"party_id": party, "premise_id": child, "version": float64(1)}, data)
			goto received
		case <-ctx.Done():
			t.Fatal("premise event not delivered")
		}
	}
received:
	var counts struct{ Revisions, Audits, Events int }
	require.NoError(t, partyTransaction(t, db, party, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_premise_revisions WHERE premise_id=?) revisions,(SELECT count(*) FROM audit_events WHERE resource_type='supply_premises' AND resource_id=?) audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.premise_updated.v1' AND payload->'data'->>'premise_id'=?) events`, child, child, child).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Events)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_branch_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_branch_outbox_failure CHECK(subject <> 'helivanta.in.supply.premise_updated.v1') NOT VALID`}}))
	w := testutil.Do(r, "PUT", path, "owner", branchPremiseBody(1, "IN", "TEST-WH", "warehouse", nil))
	require.Equal(t, 500, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", path, "owner", "")
	require.Contains(t, w.Body.String(), `"branch_premise_id":"`+branch+`"`)
	require.Contains(t, w.Body.String(), `"version":1`)
}

func TestBranchAssignmentAndParentReclassificationCannotBothCommit(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	branchPath := root + "/premises/" + branch
	require.Equal(t, 200, testutil.Do(r, "PUT", branchPath, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
	path := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	var start sync.WaitGroup
	start.Add(1)
	linked, retyped := make(chan int, 1), make(chan int, 1)
	go func() {
		start.Wait()
		linked <- testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch)).Code
	}()
	go func() {
		start.Wait()
		retyped <- testutil.Do(r, "PUT", branchPath, "owner", branchPremiseBody(1, "IN", "TEST-BRANCH", "warehouse", "omitted")).Code
	}()
	start.Done()
	a, b := <-linked, <-retyped
	require.True(t, (a == 200 && b == 409) || (a == 404 && b == 200), "assignment=%d reclassification=%d", a, b)
}

func TestCounterpartyBranchAssignmentsAndUnknownInputsAreRejected(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Counterparty")).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
	path := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch)).Code)
	valid := branchPremiseBody(0, "IN", "TEST-WH", "warehouse", nil)
	for _, body := range []string{strings.TrimSuffix(valid, "}") + `,"tenant_id":"` + testutil.TenantB + `"}`, valid + `{}`, `null`, `{"branch_premise_id":{}}`} {
		w := testutil.Do(r, "PUT", path, "owner", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}

func TestBranchDatabaseGuardReportsScopeAndKindConstraints(t *testing.T) {
	r, db := partyHarness(t)
	party := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Branch Owner"), "counterparty", "owned_organisation", 1)).Code)
	branch := uuid.Must(uuid.NewV7()).String()
	child := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+branch, "owner", branchPremiseBody(0, "IN", "TEST-BRANCH", "branch", "omitted")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+child, "owner", branchPremiseBody(0, "IN", "TEST-WH", "warehouse", branch)).Code)
	for _, test := range []struct{ sql, id, constraint string }{
		{`UPDATE supply_premises SET kind='warehouse',version=version+1 WHERE id=?`, branch, "supply_premise_branch_kind"},
		{`UPDATE supply_premises SET kind='branch',version=version+1 WHERE id=?`, child, "supply_premise_branch_reference"},
		{`UPDATE supply_premises SET branch_premise_id='0199e000-0000-7000-8000-000000000099',version=version+1 WHERE id=?`, child, "supply_premise_branch_parent"},
	} {
		err := partyTransaction(t, db, party, func(tx *gorm.DB) error { return tx.Exec(test.sql, test.id).Error })
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg)
		require.Equal(t, test.constraint, pg.ConstraintName)
	}
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			var n int64
			if e := tx.Table("supply_premises").Count(&n).Error; e != nil {
				return e
			}
			require.Zero(t, n)
			return nil
		}))
	}
}
