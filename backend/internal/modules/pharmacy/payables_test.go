package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func payableRoot() string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/accounts"
}

func expenseBody(reference string) string {
	return fmt.Sprintf(`{"payee_reference":"TEST-SUPPLIER","payee_name":"TEST Supplier","document_reference":%q,"issued_on":"2026-10-01","due_on":"2026-10-31","currency":"INR","lines":[{"category":"utilities","description":"TEST electricity","net_minor":10000,"tax_minor":1800,"recoverable_tax_minor":1800,"landed_minor":0}]}`, reference)
}

func TestPayableRecordsExpenseAndOutstandingWithoutDuplicatingRetry(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	body := expenseBody("TEST-BILL-001")
	first := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, first.Code, first.Body.String())
	var row struct {
		ID               string
		TotalMinor       int64  `json:"total_minor"`
		OutstandingMinor int64  `json:"outstanding_minor"`
		ActorSubject     string `json:"actor_subject"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &row))
	require.Equal(t, id, row.ID)
	require.EqualValues(t, 11800, row.TotalMinor)
	require.EqualValues(t, 11800, row.OutstandingMinor)
	require.Equal(t, "user-editor", row.ActorSubject)
	retry := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, retry.Code, retry.Body.String())
	require.JSONEq(t, first.Body.String(), retry.Body.String())
	page := do(r, "GET", payableRoot()+"/payables", "editor", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	var list struct{ Data []json.RawMessage }
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, 409, do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", body).Code)
}

func settlementBody(kind, reference string, amount int, reversal *string) string {
	reverse := "null"
	if reversal != nil {
		reverse = fmt.Sprintf("%q", *reversal)
	}
	return fmt.Sprintf(`{"kind":%q,"amount_minor":%d,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":%q,"occurred_on":"2026-10-15","reason":"TEST settlement evidence","reversal_of":%s}`, kind, amount, reference, reverse)
}
func TestPartialPaymentsCreditsAndReversalPreservePayableHistory(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	require.Equal(t, 200, do(r, "PUT", path, "editor", expenseBody("TEST-PARTIAL")).Code)
	payment := uuid.Must(uuid.NewV7()).String()
	body := settlementBody("payment", "TEST-BANK-001", 3000, nil)
	result := do(r, "PUT", path+"/payments/"+payment, "editor", body)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Equal(t, 200, do(r, "PUT", path+"/payments/"+payment, "editor", body).Code)
	require.Equal(t, 409, do(r, "PUT", path+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", body).Code)
	assertBalance := func(paid, credit, left int64) {
		t.Helper()
		w := do(r, "GET", path, "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var row struct {
			Paid   int64 `json:"paid_minor"`
			Credit int64 `json:"credit_minor"`
			Left   int64 `json:"outstanding_minor"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
		require.Equal(t, paid, row.Paid)
		require.Equal(t, credit, row.Credit)
		require.Equal(t, left, row.Left)
	}
	assertBalance(3000, 0, 8800)
	require.Equal(t, 409, do(r, "PUT", path+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("payment", "TEST-OVERPAY", 9000, nil)).Code)
	credit := do(r, "PUT", path+"/adjustments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("credit", "TEST-CREDIT", 800, nil))
	require.Equal(t, 200, credit.Code, credit.Body.String())
	assertBalance(3000, 800, 8000)
	reversal := do(r, "PUT", path+"/adjustments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("payment_reversal", "TEST-REVERSAL", 3000, &payment))
	require.Equal(t, 200, reversal.Code, reversal.Body.String())
	assertBalance(0, 800, 11000)
	require.Equal(t, 409, do(r, "PUT", path+"/adjustments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("payment_reversal", "TEST-REVERSE-TWICE", 3000, &payment)).Code)
	assertBalance(0, 800, 11000)
	history := do(r, "GET", path+"/settlements", "editor", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var list struct {
		Data []struct {
			ActorSubject string `json:"actor_subject"`
			PaidBy       string `json:"paid_by"`
		}
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &list))
	require.Len(t, list.Data, 3)
	require.Equal(t, "user-editor", list.Data[0].ActorSubject)
	require.Equal(t, "TEST Finance operator", list.Data[0].PaidBy)
}

func TestInventoryAuditLinksAcquisitionCostAndFlagsUnvaluedStock(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	stock := strings.TrimSuffix(strings.Split(productPath(""), "/catalogue")[0], "/") + "/stock"
	receipt := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", stock+"/movements/"+receipt, "editor", stockBody(location, batch, "receipt", "TEST-COST-RECEIPT", 25))
	require.Equal(t, 200, w.Code, w.Body.String())
	audit := func(costed bool, qty int64, status string) {
		t.Helper()
		w := do(r, "GET", payableRoot()+"/inventory-audit", "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct {
				ID      string
				OnHand  int64   `json:"on_hand_units"`
				Missing int64   `json:"uncosted_receipt_units"`
				Cost    string  `json:"acquisition_cost_minor"`
				Value   *string `json:"valuation_minor"`
				Status  string  `json:"valuation_status"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		row := page.Data[0]
		require.Equal(t, batch, row.ID)
		require.Equal(t, qty, row.OnHand)
		require.Equal(t, status, row.Status)
		if costed {
			require.Equal(t, "25000", row.Cost)
			require.Zero(t, row.Missing)
		} else {
			require.EqualValues(t, 25, row.Missing)
			require.Nil(t, row.Value)
		}
		if status == "complete" {
			require.NotNil(t, row.Value)
			require.Equal(t, "25000", *row.Value)
		} else {
			require.Nil(t, row.Value)
		}
	}
	audit(false, 25, "cost_missing")
	body := strings.Replace(expenseBody("TEST-ACQUISITION"), `"category":"utilities"`, `"receipt_id":"`+receipt+`","category":"inventory"`, 1)
	body = strings.ReplaceAll(body, "10000", "25000")
	body = strings.ReplaceAll(body, "1800", "2500")
	w = do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	audit(true, 25, "complete")
	require.Equal(t, 409, do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(body, "TEST-ACQUISITION", "TEST-DOUBLE-COST", 1)).Code)
	w = do(r, "PUT", stock+"/corrections/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "correction", "TEST-STOCK-DAMAGE", -5))
	require.Equal(t, 200, w.Code, w.Body.String())
	audit(true, 20, "valuation_review")
}

// A stocked product's currency cannot relabel its acquisition costs. With
// India the only supported country (#1287) there is no other currency to
// relabel to: any other is refused as unsupported before the stocked-product
// guard, which stays for when a second country returns.
func TestStockedProductCurrencyCannotRelabelAcquisitionCosts(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, _, _ := stockFixture(t, r)
	body := strings.Replace(productBody(1, "TEST Amoxicillin 500"), `"INR"`, `"USD"`, 1)
	w := do(r, "PUT", productPath(product), "editor", body)
	require.Equal(t, 400, w.Code, w.Body.String())
}

func TestPayableScopeValidationAndImmutableDatabaseControls(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	id := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	body := expenseBody("TEST-GUARDS")
	require.Equal(t, 200, do(r, "PUT", path, "editor", body).Code)
	for _, token := range []string{"reader", "denied"} {
		require.Equal(t, 403, do(r, "GET", path, token, "").Code)
		require.Equal(t, 403, do(r, "PUT", path, token, body).Code)
	}
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "GET", path, token, "").Code)
		require.Equal(t, 404, do(r, "PUT", path, token, body).Code)
		require.Equal(t, 404, do(r, "GET", path+"/settlements", token, "").Code)
		require.Equal(t, 404, do(r, "GET", payableRoot()+"/inventory-audit", token, "").Code)
	}
	for _, bad := range []struct{ from, to string }{{pharmaOutlet, "0199e000-0000-7000-8000-000000000002"}, {pharmaOrganization, "0199e000-0000-7000-8000-000000000011"}} {
		w := do(r, "GET", strings.Replace(path, bad.from, bad.to, 1), "editor", "")
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	require.Equal(t, 409, do(r, "PUT", path, "editor", strings.Replace(body, "TEST Supplier", "TEST Changed", 1)).Code)
	for name, invalid := range map[string]string{
		"bad date":           strings.Replace(body, "2026-10-01", "2026-02-30", 1),
		"due before invoice": strings.Replace(body, "2026-10-31", "2026-09-30", 1),
		"negative":           strings.Replace(body, "10000", "-1", 1),
		"fractional":         strings.Replace(body, "10000", "1.2", 1),
		"over recoverable":   strings.Replace(body, `"recoverable_tax_minor":1800`, `"recoverable_tax_minor":1801`, 1),
		"unknown identity":   strings.Replace(body, `"currency":`, `"tenant_id":"TEST","currency":`, 1),
		"missing receipt":    strings.Replace(body, `"utilities"`, `"inventory"`, 1),
		"trailing":           body + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", invalid)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	for _, table := range []string{"pharmacy_payables", "pharmacy_payable_lines", "pharmacy_settlements"} {
		for _, action := range []string{"DELETE FROM ", "TRUNCATE "} {
			require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(action + table).Error }))
		}
		for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
			require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
				var count int64
				err := tx.Table(table).Count(&count).Error
				require.Zero(t, count)
				return err
			}))
		}
	}
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(`UPDATE pharmacy_payables SET total_minor=1`).Error }))
	require.NoError(t, db.Migrate(t.Context(), pharmacy.New().Migrations()))
}

func TestConcurrentPaymentsCannotOverAllocateAndFailuresDoNotPublish(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	require.Equal(t, 200, do(r, "PUT", path, "editor", expenseBody("TEST-CONCURRENT")).Code)
	const workers = 6
	start := make(chan struct{})
	codes := make(chan int, workers)
	for i := range workers {
		go func() {
			<-start
			codes <- do(r, "PUT", path+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("payment", fmt.Sprintf("TEST-PARALLEL-%d", i), 6000, nil)).Code
		}()
	}
	close(start)
	winners := 0
	for range workers {
		code := <-codes
		if code == 200 {
			winners++
		} else {
			require.Equal(t, 409, code)
		}
	}
	require.Equal(t, 1, winners)
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_settlements (id,tenant_id,outlet_id,payable_id,kind,amount_minor,currency,paid_by,method,source_reference,occurred_on,reason,actor_subject) VALUES (?,?,?,?,'payment',6000,'INR','TEST','bank','TEST-DIRECT','2026-10-15','TEST','user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, id).Error
	}))
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.pharmacy.settlement_recorded.v1").Count(&count).Error
	}))
	require.EqualValues(t, 1, count)
}

func TestFinancialOutboxDeliversOnlyOpaqueDocumentAndEntryReferences(t *testing.T) {
	r, db, bus, ctx := catalogueSetup(t)
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	delivered := make(chan events.Event, 4)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{Name: "TEST-pharmacy-financial-observer", Subject: "helivanta.in.pharmacy.>", Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error { delivered <- event; return nil }}}))
	id := uuid.Must(uuid.NewV7()).String()
	entry := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	require.Equal(t, 200, do(r, "PUT", path, "editor", expenseBody("TEST-PRIVATE-SUPPLIER-DOCUMENT")).Code)
	require.Equal(t, 200, do(r, "PUT", path+"/payments/"+entry, "editor", settlementBody("payment", "TEST-PRIVATE-PAYMENT", 3000, nil)).Code)
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case event := <-delivered:
			require.Equal(t, testutil.TenantA, event.TenantID)
			require.NotContains(t, string(event.Data), "TEST")
			switch event.Type {
			case "PayableRecorded":
				require.JSONEq(t, fmt.Sprintf(`{"pharmacy_id":%q,"outlet_id":%q,"payable_id":%q}`, pharmaOrganization, pharmaOutlet, id), string(event.Data))
			case "SettlementRecorded":
				require.JSONEq(t, fmt.Sprintf(`{"pharmacy_id":%q,"outlet_id":%q,"payable_id":%q,"settlement_id":%q}`, pharmaOrganization, pharmaOutlet, id, entry), string(event.Data))
			default:
				t.Fatalf("unexpected event type %q", event.Type)
			}
			seen[event.Type] = true
		case <-ctx.Done():
			t.Fatal("financial outbox events did not reach JetStream")
		}
	}
}

func TestMultipleExpenseLinesKeepOrderAndInvalidReceiptRollsBackDocument(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(expenseBody("TEST-MULTILINE")), &body))
	body["lines"] = append(body["lines"].([]any), map[string]any{"category": "rent", "description": "TEST second line", "net_minor": 20000, "tax_minor": 0, "recoverable_tax_minor": 0, "landed_minor": 0})
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7()).String()
	path := payableRoot() + "/payables/" + id
	first := do(r, "PUT", path, "editor", string(encoded))
	require.Equal(t, 200, first.Code, first.Body.String())
	var result struct {
		Total int64 `json:"total_minor"`
		Lines []struct {
			Description string `json:"description"`
		}
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &result))
	require.EqualValues(t, 31800, result.Total)
	require.Len(t, result.Lines, 2)
	require.Equal(t, "TEST electricity", result.Lines[0].Description)
	require.Equal(t, "TEST second line", result.Lines[1].Description)
	require.Equal(t, 200, do(r, "PUT", path, "editor", string(encoded)).Code)
	body["document_reference"] = "TEST-INVALID-RECEIPT"
	body["lines"] = append(body["lines"].([]any), map[string]any{"category": "inventory", "receipt_id": uuid.Must(uuid.NewV7()).String(), "description": "TEST unknown receipt", "net_minor": 10000, "tax_minor": 0, "recoverable_tax_minor": 0, "landed_minor": 0})
	invalid, err := json.Marshal(body)
	require.NoError(t, err)
	badID := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", payableRoot()+"/payables/"+badID, "editor", string(invalid))
	require.Equal(t, 400, w.Code, w.Body.String())
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("pharmacy_payables").Where("id=?", badID).Count(&count).Error
		require.Zero(t, count)
		return err
	}))
}

// #576 D4: the overview's bill counts open a list filtered the same way.
// Today is the outlet's own date (Asia/Kolkata): 2026-10-20 at 23:00 UTC is
// already 2026-10-21 there.
func TestPayablesDueFilter(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC) })
	bill := func(ref, due string) string {
		id := uuid.Must(uuid.NewV7()).String()
		body := strings.Replace(strings.Replace(expenseBody(ref), `"due_on":"2026-10-31"`, `"due_on":"`+due+`"`, 1), `"issued_on":"2026-10-01"`, `"issued_on":"2026-10-01"`, 1)
		w := do(r, "PUT", payableRoot()+"/payables/"+id, "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		return id
	}
	bill("TEST-YESTERDAY", "2026-10-20")
	bill("TEST-TODAY", "2026-10-21")
	bill("TEST-WEEK-END", "2026-10-28")
	bill("TEST-LATER", "2026-10-29")
	paid := bill("TEST-PAID", "2026-10-20")
	w := do(r, "PUT", payableRoot()+"/payables/"+paid+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", settlementBody("payment", "TEST-PAID-IN-FULL", 11800, nil))
	require.Equal(t, 200, w.Code, w.Body.String())

	refs := func(query string) []string {
		w := do(r, "GET", payableRoot()+"/payables"+query, "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct {
				Ref string `json:"document_reference"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		out := []string{}
		for _, row := range page.Data {
			out = append(out, row.Ref)
		}
		return out
	}
	require.ElementsMatch(t, []string{"TEST-YESTERDAY"}, refs("?due=overdue"))
	require.ElementsMatch(t, []string{"TEST-TODAY", "TEST-WEEK-END"}, refs("?due=week"))
	require.Len(t, refs(""), 5)
	require.Equal(t, 400, do(r, "GET", payableRoot()+"/payables?due=soon", "editor", "").Code)
	require.Equal(t, 400, do(r, "GET", payableRoot()+"/payables?due=week&due=overdue", "editor", "").Code)
}
