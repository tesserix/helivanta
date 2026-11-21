package pharmacy_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func dispatchInput(t *testing.T, w supplierReturnWorld) string {
	t.Helper()
	var licence string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error { return tx.Table("pharmacy_outlet_licences").Select("id").Scan(&licence).Error }))
	return fmt.Sprintf(`{"expected_version":0,"licence_id":%q,"document_reference":"TEST-OUT-1","recipient_name":"TEST Supplier Driver","handed_over_at":%q,"physical_disposition":"handed_to_supplier","reason":"TEST four units physically handed to supplier"}`, licence, time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
}
func TestSupplierReturnDispatchRemovesQuarantineExactlyOnce(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w := supplierReturnWorldWithDispatch(t, ownership, true)
			parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
			created := w.request("PUT", parent, "editor", w.body(4))
			require.Equal(t, 200, created.Code, created.Body.String())
			path := parent + "/dispatches/" + uuid.Must(uuid.NewV7()).String()
			input := dispatchInput(t, w)
			dispatched := w.request("PUT", path, "writer", input)
			require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
			var receipt pharmacy.SupplierReturnDispatchReceipt
			require.NoError(t, json.Unmarshal(dispatched.Body.Bytes(), &receipt))
			require.Equal(t, "dispatched", receipt.State)
			require.Len(t, receipt.Lines, 1)
			require.Equal(t, "user-writer", receipt.DispatchedBy)
			replay := w.request("PUT", path, "writer", input)
			require.Equal(t, 200, replay.Code, replay.Body.String())
			require.JSONEq(t, dispatched.Body.String(), replay.Body.String())
			recovered := w.request("GET", path, "writer", "")
			require.Equal(t, 200, recovered.Code, recovered.Body.String())
			require.JSONEq(t, dispatched.Body.String(), recovered.Body.String())
			require.Equal(t, 404, w.request("GET", path, "editor", "").Code)
			require.Equal(t, 409, w.request("PUT", path, "editor", input).Code)
			require.Equal(t, 409, w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", input).Code)
			require.Equal(t, 409, w.request("PUT", parent+"/cancellations/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"expected_version":0,"reason":"TEST too late"}`).Code)
			view := w.request("GET", parent, "reader", "")
			require.Equal(t, 200, view.Code, view.Body.String())
			require.Contains(t, view.Body.String(), `"state":"dispatched"`)
			replay = w.request("PUT", parent, "editor", w.body(4))
			require.Equal(t, 200, replay.Code, replay.Body.String())
			require.JSONEq(t, created.Body.String(), replay.Body.String())
			balance := do(w.r, "GET", stockPath("balance?location_id="+w.location+"&batch_id="+w.batch), "editor", "")
			require.Equal(t, 200, balance.Code, balance.Body.String())
			require.Contains(t, balance.Body.String(), `"on_hand_units":76`)
			snapshot := time.Now().UTC().Format(time.RFC3339Nano)
			reconciliation := do(w.r, "GET", stockPath("reconciliation?location_id="+w.location+"&batch_id="+w.batch+"&from=1970-01-01T00:00:00Z&as_of="+snapshot), "editor", "")
			require.Equal(t, 200, reconciliation.Code, reconciliation.Body.String())
			require.Contains(t, reconciliation.Body.String(), `"supplier_return_units":"-4"`)
			require.Contains(t, reconciliation.Body.String(), `"closing_units":76`)
			exported := do(w.r, "GET", stockPath("positions.csv?as_of="+snapshot), "editor", "")
			require.Equal(t, 200, exported.Code, exported.Body.String())
			csvRows, err := csv.NewReader(strings.NewReader(exported.Body.String())).ReadAll()
			require.NoError(t, err)
			require.Len(t, csvRows, 2)
			index := slices.Index(csvRows[0], "supplier_return_units")
			require.GreaterOrEqual(t, index, 0)
			require.Equal(t, "-4", csvRows[1][index])

			for _, actor := range []string{"sibling", "foreign"} {
				require.Equal(t, 404, w.request("PUT", path, actor, input).Code)
			}
			require.Equal(t, 403, w.request("PUT", path, "reader", input).Code)
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				var n int64
				if err := tx.Table("pharmacy_stock_movements").Where("kind='supplier_return'").Count(&n).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, n)
				var healthy bool
				if err := tx.Raw("SELECT pharmacy_operating_choice_integrity() AND pharmacy_operating_choice_support()").Scan(&healthy).Error; err != nil {
					return err
				}
				require.True(t, healthy)
				return nil
			}))
		})
	}
}

func TestSupplierReturnDispatchCapsOriginalReceiptAcrossDraftsAndPreservesFinancials(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	first, second := supplierReturnPath(uuid.Must(uuid.NewV7()).String()), supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	for path, quantity := range map[string]int{first: 48, second: 40} {
		created := w.request("PUT", path, "editor", w.body(quantity))
		require.Equal(t, 200, created.Code, created.Body.String())
	}
	input := dispatchInput(t, w)
	posted := w.request("PUT", first+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", input)
	require.Equal(t, 200, posted.Code, posted.Body.String())
	// More stock in the same batch does not increase the original receipt's return allowance.
	added := do(w.r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(w.location, w.batch, "receipt", "TEST-different-original-delivery", 40))
	require.Equal(t, 200, added.Code, added.Body.String())
	path := second + "/dispatches/" + uuid.Must(uuid.NewV7()).String()
	refused := w.request("PUT", path, "writer", input)
	require.Equal(t, 409, refused.Code, refused.Body.String())
	require.Equal(t, 404, w.request("GET", path, "writer", "").Code)
	bill := do(w.r, "GET", payableRoot()+"/payables/"+w.bill, "editor", "")
	require.Equal(t, 200, bill.Code, bill.Body.String())
	require.Contains(t, bill.Body.String(), `"outstanding_minor":11800`)
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var n int64
		for _, table := range []string{"pharmacy_supplier_return_dispatches", "pharmacy_supplier_return_dispatch_lines"} {
			if err := tx.Table(table).Count(&n).Error; err != nil {
				return err
			}
			require.EqualValues(t, 1, n)
		}
		return nil
	}))
}

func TestSupplierReturnDispatchRejectsFutureHandoffAndUnsupportedSchedule(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "hospital_owned", true)
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	input := dispatchInput(t, w)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(input), &decoded))
	for _, at := range []string{time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano), "1970-01-01T00:00:00Z"} {
		decoded["handed_over_at"] = at
		body, err := json.Marshal(decoded)
		require.NoError(t, err)
		failed := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", string(body))
		require.Equal(t, 409, failed.Code, failed.Body.String())
	}
	var product string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_stock_batches").Select("product_id").Where("id=?", w.batch).Scan(&product).Error
	}))
	declaration := declare(t, w.r, product, "X", "editor")
	failed := w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", input)
	require.Equal(t, 409, failed.Code, failed.Body.String())
	require.Equal(t, 200, review(w.r, product, declaration, "writer", "approved"))
	failed = w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", input)
	require.Equal(t, 409, failed.Code, failed.Body.String())
	generic := do(w.r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(w.location, w.batch, "supplier_return", "TEST-forged-command", -4))
	require.Equal(t, 400, generic.Code, generic.Body.String())
}

func TestSupplierReturnDispatchAndCancellationRaceHasOnlyOneTerminalFact(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "chain", true)
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	input := dispatchInput(t, w)
	start := make(chan struct{})
	result := make(chan int, 2)
	go func() {
		<-start
		result <- w.request("PUT", parent+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", input).Code
	}()
	go func() {
		<-start
		result <- w.request("PUT", parent+"/cancellations/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"expected_version":0,"reason":"TEST cancel race"}`).Code
	}()
	close(start)
	codes := []int{<-result, <-result}
	require.ElementsMatch(t, []int{200, 409}, codes)
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw("SELECT(SELECT COUNT(*)FROM pharmacy_supplier_return_dispatches)+(SELECT COUNT(*)FROM pharmacy_supplier_return_cancellations)").Scan(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestSupplierReturnDispatchEvidenceFailureRollsBackStockAndRecovery(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	input := dispatchInput(t, w)
	path := parent + "/dispatches/" + uuid.Must(uuid.NewV7()).String()
	for _, fault := range []struct{ table, check string }{
		{"audit_events", "resource_type<>'pharmacy_supplier_return_dispatches'"},
		{"audit_events", "resource_type<>'pharmacy_supplier_return_dispatch_lines'"},
		{"outbox_events", "subject<>'helivanta.in.pharmacy.stock_movement_recorded.v1'"},
	} {
		apply := func(sql string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_dispatch_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() {
			apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_dispatch_evidence_failure")
		}
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_dispatch_evidence_failure CHECK(" + fault.check + ") NOT VALID")
		failed := w.request("PUT", path, "writer", input)
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "synthetic_dispatch_evidence_failure")
		require.Equal(t, 404, w.request("GET", path, "writer", "").Code)
		require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
			var n int64
			if err := tx.Table("pharmacy_stock_movements").Where("kind='supplier_return'").Count(&n).Error; err != nil {
				return err
			}
			require.Zero(t, n)
			return nil
		}))
		clear()
	}
	posted := w.request("PUT", path, "writer", input)
	require.Equal(t, 200, posted.Code, posted.Body.String())
}

func TestSupplierReturnDispatchThroughGlobalAPIPreservesActorFences(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	upstream := httptest.NewServer(w.r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 15 * time.Second, Upstreams: map[string]string{"pharmacy": upstream.URL}})
	require.NoError(t, err)
	request := func(method, path, actor, body, expectedSubject, expectedTenant string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		if expectedSubject != "" {
			req.Header.Set("X-Helivanta-Expected-Subject", expectedSubject)
		}
		if expectedTenant != "" {
			req.Header.Set("X-Helivanta-Expected-Tenant", expectedTenant)
		}
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		return response
	}
	parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
	created := request("PUT", parent, "editor", w.body(4), "user-editor", testutil.TenantA)
	require.Equal(t, 200, created.Code, created.Body.String())
	path := parent + "/dispatches/" + uuid.Must(uuid.NewV7()).String()
	input := dispatchInput(t, w)
	for _, fence := range []struct{ subject, tenant string }{
		{"", testutil.TenantA}, {"user-other", testutil.TenantA}, {"user-writer", testutil.TenantB},
	} {
		failed := request("PUT", path, "writer", input, fence.subject, fence.tenant)
		require.Equal(t, 409, failed.Code, failed.Body.String())
	}
	posted := request("PUT", path, "writer", input, "user-writer", testutil.TenantA)
	require.Equal(t, 200, posted.Code, posted.Body.String())
	require.Contains(t, posted.Header().Get("Via"), "helivanta-global-api")
	recovered := request("GET", path, "writer", "", "user-writer", testutil.TenantA)
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.Equal(t, "private, no-store", recovered.Header().Get("Cache-Control"))
	require.JSONEq(t, posted.Body.String(), recovered.Body.String())
	require.Equal(t, 404, request("GET", path, "editor", "", "user-editor", testutil.TenantA).Code)
	revoked := do(w.r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-writer", "editor", "")
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Equal(t, 404, request("GET", path, "writer", "", "user-writer", testutil.TenantA).Code)
	require.Equal(t, 404, request("PUT", path, "writer", input, "user-writer", testutil.TenantA).Code)
}

func TestSupplierReturnDispatchDatabaseRequiresCompleteExactMovementProof(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	parentID := uuid.Must(uuid.NewV7()).String()
	parent := supplierReturnPath(parentID)
	created := w.request("PUT", parent, "editor", w.body(4))
	require.Equal(t, 200, created.Code, created.Body.String())
	command := uuid.Must(uuid.NewV7()).String()
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_supplier_return_dispatches(id,tenant_id,outlet_id,return_id,licence_id,licence_review_id,document_reference,recipient_name,handed_over_at,physical_disposition,reason,intent_sha256,actor_subject)
  SELECT ?,current_setting('app.tenant_id')::uuid,?,?,o.id,l.review_id,'TEST-OUT','TEST supplier',clock_timestamp(),'handed_to_supplier','TEST no proof',?,'user-editor'
  FROM pharmacy_outlet_licences o CROSS JOIN LATERAL pharmacy_trading_licence_source(o.tenant_id,o.outlet_id,o.id)l`, command, pharmaOutlet, parentID, strings.Repeat("a", 64)).Error
	})
	require.ErrorContains(t, err, "dispatch requires every original line and movement")
	require.Equal(t, 404, w.request("GET", parent+"/dispatches/"+command, "editor", "").Code)
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
   VALUES(?,current_setting('app.tenant_id')::uuid,?,?,?,'supplier_return',-4,'TEST forged proof',?,'user-editor')`, uuid.Must(uuid.NewV7()), pharmaOutlet, w.location, w.batch, "supplier-return:"+command+":1").Error
	})
	require.ErrorContains(t, err, "supplier return movements require immutable dispatch proof")
}

func TestSupplierReturnDispatchReceiptCapSerializesDifferentReturns(t *testing.T) {
	w := supplierReturnWorldWithDispatch(t, "independent", true)
	paths := make([]string, 2)
	for i := range paths {
		parent := supplierReturnPath(uuid.Must(uuid.NewV7()).String())
		created := w.request("PUT", parent, "editor", w.body(50))
		require.Equal(t, 200, created.Code, created.Body.String())
		paths[i] = parent + "/dispatches/" + uuid.Must(uuid.NewV7()).String()
	}
	received := do(w.r, "PUT", stockPath("movements/"+uuid.Must(uuid.NewV7()).String()), "editor", stockBody(w.location, w.batch, "receipt", "TEST-separate-original-receipt", 40))
	require.Equal(t, 200, received.Code, received.Body.String())
	input := dispatchInput(t, w)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, path := range paths {
		go func() { <-start; results <- w.request("PUT", path, "writer", input).Code }()
	}
	close(start)
	require.ElementsMatch(t, []int{200, 409}, []int{<-results, <-results})
	balance := do(w.r, "GET", stockPath("balance?location_id="+w.location+"&batch_id="+w.batch), "editor", "")
	require.Equal(t, 200, balance.Code, balance.Body.String())
	require.Contains(t, balance.Body.String(), `"on_hand_units":70`)
}
