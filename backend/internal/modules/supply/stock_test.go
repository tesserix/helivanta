package supply_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestWholesaleReceiptIsQuarantinedScopedAndRecoverable(t *testing.T) {
	r, _ := operatingHarness(t)
	party := structureParty(t, r, "TEST-STOCK-OWNER", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", party+"/premises/"+premise, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	operatingGrant(t, r, party, premise, "inventory", "inventory")
	w = testutil.Do(r, "GET", party+"/operating-locations", "inventory", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"read_stock"`)
	require.Contains(t, w.Body.String(), `"receive_stock"`)
	path := party + "/operating-locations/" + premise + "/stock/receipts/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"product_version_id": uuid.Must(uuid.NewV7()).String(), "lot_number": "TEST-LOT-001", "expires_on": "2030-12-31", "unit_code": "pack", "quantity_units": 100, "source_reference": "TEST-GRN-001:1", "reason": "TEST physical receipt counted"}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	w = testutil.Do(r, "PUT", path, "inventory", string(raw))
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"stock_status":"quarantine"`)
	require.Contains(t, w.Body.String(), `"available_units":0`)
	require.Contains(t, w.Body.String(), `"actor_subject":"user-inventory"`)
	first := w.Body.String()
	w = testutil.Do(r, "PUT", path, "inventory", string(raw))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, first, w.Body.String())
	w = testutil.Do(r, "GET", path, "inventory", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, first, w.Body.String())
	for _, token := range []string{"foreign", "driver", "finance"} {
		w = testutil.Do(r, "GET", path, token, "")
		require.Equal(t, 404, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "TEST-LOT-001")
	}
	w = testutil.Do(r, "PUT", path, "inventory", strings.Replace(string(raw), `"quantity_units":100`, `"quantity_units":101`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestWholesaleReceiptConcurrentReplayPostsOneCount(t *testing.T) {
	r, db := operatingHarness(t)
	root := structureParty(t, r, "TEST-CONCURRENT-STOCK", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := root + "/operating-locations/" + premise + "/stock/receipts/" + id
	body := `{"product_version_id":"abcdefab-cdef-4abc-8abc-abcdefabcdef","lot_number":"TEST-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":100,"source_reference":"TEST-GRN:1","reason":"TEST physical receipt"}`
	type response struct {
		status int
		body   string
	}
	results := make(chan response, 8)
	var start sync.WaitGroup
	start.Add(1)
	for range 8 {
		go func() {
			start.Wait()
			w := testutil.Do(r, "PUT", path, "owner", body)
			results <- response{w.Code, w.Body.String()}
		}()
	}
	start.Done()
	created := 0
	var first string
	for range 8 {
		result := <-results
		require.Contains(t, []int{200, 201}, result.status, result.body)
		if result.status == 201 {
			created++
		}
		if first == "" {
			first = result.body
		} else {
			require.JSONEq(t, first, result.body)
		}
	}
	require.Equal(t, 1, created)
	var count int
	require.NoError(t, partyTransaction(t, db, strings.TrimPrefix(root, "/v1/supply/parties/"), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.stock_receipt_recorded.v1' AND payload->'data'->>'receipt_id'=?`, id).Scan(&count).Error
	}))
	require.Equal(t, 1, count)
	w := testutil.Do(r, "PUT", root+"/operating-locations/"+premise+"/stock/receipts/"+uuid.Must(uuid.NewV7()).String(), "owner", body)
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestWholesaleReceiptAuditAndOutboxFailuresDoNotRecordStock(t *testing.T) {
	for _, failure := range []struct{ name, sql string }{
		{"audit", `ALTER TABLE audit_events ADD CONSTRAINT test_stock_failure CHECK(resource_type<>'supply_stock_receipts') NOT VALID`},
		{"outbox", `ALTER TABLE outbox_events ADD CONSTRAINT test_stock_failure CHECK(subject<>'helivanta.in.supply.stock_receipt_recorded.v1') NOT VALID`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			r, db := operatingHarness(t)
			root := structureParty(t, r, "TEST-ROLLBACK-STOCK", "owner")
			premise := uuid.Must(uuid.NewV7()).String()
			require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stock_failure", SQL: failure.sql}}))
			path := root + "/operating-locations/" + premise + "/stock/receipts/" + uuid.Must(uuid.NewV7()).String()
			w := testutil.Do(r, "PUT", path, "owner", `{"product_version_id":"abcdefab-cdef-4abc-8abc-abcdefabcdef","lot_number":"TEST-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":100,"source_reference":"TEST-GRN:1","reason":"TEST physical count"}`)
			require.Equal(t, 503, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "test_stock_failure")
			var counts struct{ Receipts, Audits, Events int }
			require.NoError(t, partyTransaction(t, db, strings.TrimPrefix(root, "/v1/supply/parties/"), func(tx *gorm.DB) error {
				return tx.Raw(`SELECT (SELECT count(*) FROM supply_stock_receipts) receipts,(SELECT count(*) FROM audit_events WHERE resource_type='supply_stock_receipts') audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.stock_receipt_recorded.v1') events`).Scan(&counts).Error
			}))
			require.Zero(t, counts)
			require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
		})
	}
}

func TestWholesaleReceiptAccessCutoffAlsoCutsOffRecovery(t *testing.T) {
	r, _ := operatingHarness(t)
	root := structureParty(t, r, "TEST-CUTOFF-STOCK", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	grant := operatingGrant(t, r, root, premise, "inventory", "inventory")
	path := root + "/operating-locations/" + premise + "/stock/receipts/" + uuid.Must(uuid.NewV7()).String()
	body := `{"product_version_id":"abcdefab-cdef-4abc-8abc-abcdefabcdef","lot_number":"TEST-PRIVATE-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":100,"source_reference":"TEST-GRN:1","reason":"TEST physical count"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "inventory", body).Code)
	revokeOperatingGrant(t, r, grant)
	for _, method := range []string{"GET", "PUT"} {
		w := testutil.Do(r, method, path, "inventory", body)
		require.Equal(t, 404, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "TEST-PRIVATE-LOT")
	}
}

func TestWholesaleReceiptRefusesChangedSignedInAccount(t *testing.T) {
	r, _ := operatingHarness(t)
	root := structureParty(t, r, "TEST-ACCOUNT-STOCK", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	body := `{"product_version_id":"abcdefab-cdef-4abc-8abc-abcdefabcdef","lot_number":"TEST-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":100,"source_reference":"TEST-GRN-001:1","reason":"TEST physical receipt counted"}`
	req := httptest.NewRequest("PUT", root+"/operating-locations/"+premise+"/stock/receipts/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Subject", "user-inventory")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"session_changed"`)
}

func TestWholesaleReceiptRejectsInactivePremiseAtDatabaseBoundary(t *testing.T) {
	r, db := operatingHarness(t)
	root := structureParty(t, r, "TEST-INACTIVE-STOCK", "owner")
	premise := uuid.Must(uuid.NewV7()).String()
	path := root + "/premises/" + premise
	w := testutil.Do(r, "PUT", path, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "PUT", path, "owner", strings.Replace(premiseBody(1, "IN"), `"active"`, `"inactive"`, 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	party := strings.TrimPrefix(root, "/v1/supply/parties/")
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.actor_subject','user-owner',true)`, party).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO supply_stock_receipts(tenant_id,party_id,premise_id,id,product_version_id,lot_number,expires_on,unit_code,quantity_units,source_reference,reason,actor_subject)VALUES(?,?,?,?,?,'TEST-LOT','2030-01-01','pack',1,'TEST-LINE','TEST receipt','user-owner')`, testutil.TenantA, party, premise, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())).Error
	})
	require.Error(t, err, "even the non-bypass application pool must reject inactive physical stock destinations")
}
