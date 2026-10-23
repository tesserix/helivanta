package supply_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func binBody(version int, code string) string {
	return fmt.Sprintf(`{"expected_version":%d,"code":%q,"name":"TEST Bin","aisle":"A","rack":"R1","shelf":"S1","storage_class":"ambient","status":"active"}`, version, code)
}

func TestOwnedWarehouseBinsAreVersionedAndRetrySafe(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(partyBody(0, "TEST Owned Distributor"), `"counterparty"`, `"owned_organisation"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", body).Code)
	premise := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	path := premise + "/bins/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-BIN-1"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"country_code":"IN"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-BIN-1")).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	changed := strings.Replace(binBody(1, "TEST-BIN-1"), `"S1"`, `"S2"`, 1)
	w = testutil.Do(r, "PUT", path, "owner", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", changed).Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", binBody(1, "TEST-BIN-1")).Code)
	w = testutil.Do(r, "GET", path+"/revisions?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct{ Version int }
		Page struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 2, page.Data[0].Version)
	require.True(t, page.Page.HasMore)
	w = testutil.Do(r, "GET", path+"/revisions?limit=1&cursor="+page.Page.NextCursor, "owner", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 1, page.Data[0].Version)
	require.False(t, page.Page.HasMore)
}

func TestBinsRequireOwnedStoragePremisesAndCurrentExplicitRoles(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Counterparty")).Code)
	premise := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", premise+"/bins/"+uuid.Must(uuid.NewV7()).String(), "owner", binBody(0, "TEST-1")).Code)
	owned := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(partyBody(0, "TEST Owned"), "TEST-DISTRIBUTOR", "TEST-OWNED"), "TEST-REG-001", "TEST-REG-002"), "counterparty", "owned_organisation")
	root = "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", owned).Code)
	premise = root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	path := premise + "/bins/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-1")).Code)
	for _, token := range []string{"foreign", "manager", "auditor"} {
		for _, url := range []string{path, premise + "/bins", path + "/revisions"} {
			w := testutil.Do(r, "GET", url, token, "")
			require.Equal(t, 404, w.Code, w.Body.String())
		}
		require.Equal(t, 404, testutil.Do(r, "PUT", path, token, binBody(1, "TEST-1")).Code)
	}
	require.Equal(t, 403, testutil.Do(r, "GET", path, "denied", "").Code)
	grant := root + "/access/user-auditor/auditor"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	for _, url := range []string{path, premise + "/bins", path + "/revisions"} {
		require.Equal(t, 200, testutil.Do(r, "GET", url, "auditor", "").Code)
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "auditor", binBody(1, "TEST-1")).Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "auditor", "").Code)
	for _, role := range []string{"finance", "warehouse", "sales", "driver"} {
		grant := root + "/access/user-manager/" + role
		require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
		require.Equal(t, 404, testutil.Do(r, "GET", path, "manager", "").Code)
		require.Equal(t, 404, testutil.Do(r, "PUT", path, "manager", binBody(1, "TEST-1")).Code)
		require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	}
	grant = root + "/access/user-manager/purchasing"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "manager", binBody(1, "TEST-1")).Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "manager", binBody(1, "TEST-1")).Code)
}

func TestBinInputCountryAndPremiseBoundaries(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	owned := strings.Replace(partyBody(0, "TEST Owned Distributor"), "counterparty", "owned_organisation", 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", owned).Code)
	first := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	second := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", first, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", second, "owner", strings.Replace(strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-DEPOT", 1), `"kind":"warehouse"`, `"kind":"depot"`, 1)).Code)
	branch := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	branchBody := strings.Replace(strings.Replace(premiseBody(0, "IN"), "test-warehouse", "TEST-BRANCH", 1), `"kind":"warehouse"`, `"kind":"branch"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", branch, "owner", branchBody).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", branch+"/bins", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", branch+"/bins/"+uuid.Must(uuid.NewV7()).String(), "owner", binBody(0, "TEST-BIN")).Code)
	path := first + "/bins/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-BIN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"country_code":"IN"`)
	for name, change := range map[string]func(map[string]any){
		"tenant spoof":     func(v map[string]any) { v["tenant_id"] = testutil.TenantB },
		"country spoof":    func(v map[string]any) { v["country_code"] = "PK" },
		"premise spoof":    func(v map[string]any) { v["premise_id"] = strings.TrimPrefix(second, root+"/premises/") },
		"role spoof":       func(v map[string]any) { v["role"] = "administrator" },
		"missing version":  func(v map[string]any) { delete(v, "expected_version") },
		"negative version": func(v map[string]any) { v["expected_version"] = -1 },
		"empty code":       func(v map[string]any) { v["code"] = " " },
		"invalid code":     func(v map[string]any) { v["code"] = "A/B" },
		"control name":     func(v map[string]any) { v["name"] = "TEST\nName" },
		"long rack":        func(v map[string]any) { v["rack"] = strings.Repeat("x", 61) },
		"invalid status":   func(v map[string]any) { v["status"] = "verified" },
		"invalid storage":  func(v map[string]any) { v["storage_class"] = "unrestricted" },
	} {
		t.Run(name, func(t *testing.T) {
			w := testutil.Do(r, "PUT", path, "owner", changedPartyBody(t, binBody(1, "TEST-BIN"), change))
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", binBody(99, "TEST-BIN")).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", first+"/bins/"+uuid.Must(uuid.NewV7()).String(), "owner", binBody(0, "TEST-BIN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", second+"/bins/"+uuid.Must(uuid.NewV7()).String(), "owner", binBody(0, "TEST-BIN")).Code)
	id := strings.TrimPrefix(path, first+"/bins/")
	for _, suffix := range []string{"", "/revisions"} {
		require.Equal(t, 404, testutil.Do(r, "GET", second+"/bins/"+id+suffix, "owner", "").Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/premises/"+uuid.Must(uuid.NewV7()).String()+"/bins", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", first+"/bins/"+uuid.NewString(), "owner", "").Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "owner", binBody(1, "TEST-BIN")+`{}`).Code)
}

func TestPremiseWithRetainedBinsCannotBecomeANonStorageAddress(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Owned"), "counterparty", "owned_organisation", 1)).Code)
	premise := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	path := premise + "/bins/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-BIN")).Code)
	w := testutil.Do(r, "PUT", premise, "owner", strings.Replace(premiseBody(1, "IN"), `"kind":"warehouse"`, `"kind":"registered"`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", strings.Replace(premiseBody(1, "IN"), `"status":"active"`, `"status":"inactive"`, 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", strings.Replace(binBody(1, "TEST-BIN"), `"S1"`, `"S2"`, 1)).Code)
	w = testutil.Do(r, "PUT", path, "owner", strings.Replace(binBody(1, "TEST-BIN"), `"status":"active"`, `"status":"inactive"`, 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"inactive"`)
	require.Equal(t, 200, testutil.Do(r, "GET", path+"/revisions", "owner", "").Code)
}

func TestConcurrentBinRetriesProduceOneAuditedRevisionAndJetStreamEvent(t *testing.T) {
	r, db, bus, ctx := partySetup(t)
	partyID := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + partyID
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Owned"), "counterparty", "owned_organisation", 1)).Code)
	premiseID := uuid.Must(uuid.NewV7()).String()
	premise := root + "/premises/" + premiseID
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	binID := uuid.Must(uuid.NewV7()).String()
	path := premise + "/bins/" + binID
	delivered := make(chan events.Event, 2)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-bin-observer", Subject: "helivanta.in.supply.bin_updated.v1", Handle: func(_ context.Context, _ *gorm.DB, e events.Event) error { delivered <- e; return nil }}}))
	const retries = 6
	results := make(chan int, retries)
	var start sync.WaitGroup
	start.Add(1)
	for range retries {
		go func() { start.Wait(); results <- testutil.Do(r, "PUT", path, "owner", binBody(0, "TEST-BIN")).Code }()
	}
	start.Done()
	for range retries {
		require.Equal(t, 200, <-results)
	}
	select {
	case event := <-delivered:
		require.Equal(t, testutil.TenantA, event.TenantID)
		require.Equal(t, "helivanta.in.supply.bin_updated.v1", event.Type)
		var data map[string]any
		require.NoError(t, json.Unmarshal(event.Data, &data))
		require.Equal(t, map[string]any{"party_id": partyID, "premise_id": premiseID, "bin_id": binID, "version": float64(1)}, data)
	case <-ctx.Done():
		t.Fatal("bin event did not reach durable JetStream consumer")
	}
	var counts struct{ Heads, Revisions, Audits, Events int }
	require.NoError(t, partyTransaction(t, db, partyID, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_bins WHERE id=?) heads,(SELECT count(*) FROM supply_bin_revisions WHERE bin_id=?) revisions,(SELECT count(*) FROM audit_events WHERE resource_type='supply_bins' AND resource_id=?) audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.bin_updated.v1') events`, binID, binID, binID).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Heads)
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Events)
}

func TestBinOutboxFailureRollsBackHeadRevisionAndAudit(t *testing.T) {
	r, db := partyHarness(t)
	partyID := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + partyID
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Owned"), "counterparty", "owned_organisation", 1)).Code)
	premise := root + "/premises/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_bin_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_bin_outbox_failure CHECK(subject <> 'helivanta.in.supply.bin_updated.v1') NOT VALID`}}))
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", premise+"/bins/"+id, "owner", binBody(0, "TEST-BIN"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", premise+"/bins/"+id, "owner", "").Code)
	var total int64
	require.NoError(t, partyTransaction(t, db, partyID, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_bins WHERE id=?) + (SELECT count(*) FROM supply_bin_revisions WHERE bin_id=?) + (SELECT count(*) FROM audit_events WHERE resource_type='supply_bins' AND resource_id=?)`, id, id, id).Scan(&total).Error
	}))
	require.Zero(t, total)
}

func TestBinDatabaseGuardsRetainHistoryAndDenyUnscopedReads(t *testing.T) {
	r, db := partyHarness(t)
	partyID := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + partyID
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", strings.Replace(partyBody(0, "TEST Owned"), "counterparty", "owned_organisation", 1)).Code)
	premiseID := uuid.Must(uuid.NewV7()).String()
	premise := root + "/premises/" + premiseID
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", premise+"/bins/"+id, "owner", binBody(0, "TEST-BIN")).Code)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			for _, table := range []string{"supply_bins", "supply_bin_revisions"} {
				var count int64
				require.NoError(t, tx.Table(table).Count(&count).Error)
				require.Zero(t, count)
			}
			return nil
		}))
	}
	for name, sql := range map[string]string{
		"delete bin":         `DELETE FROM supply_bins WHERE id=?`,
		"delete history":     `DELETE FROM supply_bin_revisions WHERE bin_id=?`,
		"mutate history":     `UPDATE supply_bin_revisions SET actor_subject='spoof' WHERE bin_id=?`,
		"unversioned update": `UPDATE supply_bins SET name='TEST Tamper' WHERE id=?`,
		"tenant relocation":  `UPDATE supply_bins SET tenant_id='22222222-2222-2222-2222-222222222222',version=version+1 WHERE id=?`,
		"premise relocation": `UPDATE supply_bins SET premise_id='0199e000-0000-7000-8000-000000000009',version=version+1 WHERE id=?`,
		"unaudited update":   `UPDATE supply_bins SET name='TEST Tamper',version=version+1 WHERE id=?`,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, partyTransaction(t, db, partyID, func(tx *gorm.DB) error { return tx.Exec(sql, id).Error }))
		})
	}
	for i := range 2 {
		require.Equal(t, 200, testutil.Do(r, "PUT", premise+"/bins/"+uuid.Must(uuid.NewV7()).String(), "owner", binBody(0, fmt.Sprintf("TEST-%d", i))).Code)
	}
	seen := map[string]bool{}
	path := premise + "/bins?limit=1"
	for {
		w := testutil.Do(r, "GET", path, "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct{ ID string }
			Page struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.False(t, seen[page.Data[0].ID])
		seen[page.Data[0].ID] = true
		if !page.Page.HasMore {
			break
		}
		path = premise + "/bins?limit=1&cursor=" + page.Page.NextCursor
	}
	require.Len(t, seen, 3)
}
