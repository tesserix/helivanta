package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func futureSupplierCreditBill(t *testing.T, w supplierReturnWorld, references ...string) string {
	t.Helper()
	receipt, bill := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	received := do(w.r, "PUT", stockPath("movements/"+receipt), "editor", stockBody(w.location, w.batch, "receipt", "TEST-future-delivery-"+receipt, 20))
	require.Equal(t, 200, received.Code, received.Body.String())
	reference := "TEST-FUTURE-BILL"
	if len(references) > 0 {
		reference = references[0]
	}
	input := fmt.Sprintf(`{"payee_reference":"TEST-EXT","payee_name":"TEST External Supplier","document_reference":%q,"issued_on":"2026-11-17","due_on":"2026-11-30","currency":"INR","lines":[{"category":"inventory","receipt_id":%q,"description":"TEST future supplier cartons","net_minor":1000,"tax_minor":180,"recoverable_tax_minor":180,"landed_minor":0}]}`, reference, receipt)
	created := do(w.r, "PUT", payableRoot()+"/payables/"+bill, "editor", input)
	require.Equal(t, 200, created.Code, created.Body.String())
	return bill
}

func TestSupplierReturnCreditAllocatesPaidSourceCreditToFutureBillAndPreservesPaymentOnReversal(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, source := dispatchedCreditSource(t, ownership)
			paid := do(w.r, "PUT", payableRoot()+"/payables/"+w.bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":11800,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-PAID-ANCHOR","occurred_on":"2026-11-15","reason":"TEST original bill fully settled","reversal_of":null}`)
			require.Equal(t, 200, paid.Code, paid.Body.String())
			credit := recordSupplierCreditFixture(t, w, source, "TEST-CN-FUTURE", 750)
			bill := futureSupplierCreditBill(t, w)
			path := credit + "/allocations/" + uuid.Must(uuid.NewV7()).String()
			body := creditJSON(t, map[string]any{"payable_id": bill, "expected_version": 1, "expected_available_minor": 750, "expected_payable_version": 1, "expected_outstanding_minor": 1180, "amount_minor": 750, "occurred_on": "2026-11-18", "reason": "TEST apply retained supplier credit to future bill"})
			allocated := w.request("PUT", path, "editor", body)
			require.Equal(t, 200, allocated.Code, allocated.Body.String())
			view := supplierCreditPosition(t, w, credit)
			require.Equal(t, int64(2), view.Version)
			require.Equal(t, int64(750), view.AllocatedMinor)
			require.Zero(t, view.AvailableMinor)
			require.Zero(t, view.MatchedMinor)
			readBill := func(id string) (paid, credited, outstanding int64) {
				result := do(w.r, "GET", payableRoot()+"/payables/"+id, "editor", "")
				require.Equal(t, 200, result.Code, result.Body.String())
				var amounts struct {
					Paid        int64 `json:"paid_minor"`
					Credit      int64 `json:"credit_minor"`
					Outstanding int64 `json:"outstanding_minor"`
				}
				require.NoError(t, json.Unmarshal(result.Body.Bytes(), &amounts))
				return amounts.Paid, amounts.Credit, amounts.Outstanding
			}
			paidMinor, creditMinor, outstanding := readBill(bill)
			require.Zero(t, paidMinor)
			require.Equal(t, int64(750), creditMinor)
			require.Equal(t, int64(430), outstanding)
			paidMinor, creditMinor, outstanding = readBill(w.bill)
			require.Equal(t, int64(11800), paidMinor)
			require.Zero(t, creditMinor)
			require.Zero(t, outstanding)
			replayed := w.request("PUT", path, "editor", body)
			require.Equal(t, 200, replayed.Code, replayed.Body.String())
			require.JSONEq(t, allocated.Body.String(), replayed.Body.String())
			overpay := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":431,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-OVERPAY","occurred_on":"2026-11-19","reason":"TEST must include managed credit","reversal_of":null}`)
			require.Equal(t, 409, overpay.Code, overpay.Body.String())
			payment := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":430,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-REMAINING","occurred_on":"2026-11-19","reason":"TEST pay only outstanding liability","reversal_of":null}`)
			require.Equal(t, 200, payment.Code, payment.Body.String())
			reverse := path + "/reversals/" + uuid.Must(uuid.NewV7()).String()
			reverseBody := creditJSON(t, map[string]any{"expected_version": 2, "expected_available_minor": 0, "expected_payable_version": 3, "expected_outstanding_minor": 0, "occurred_on": "2026-11-20", "reason": "TEST reverse credit allocation without reversing payment"})
			reversed := w.request("PUT", reverse, "editor", reverseBody)
			require.Equal(t, 200, reversed.Code, reversed.Body.String())
			view = supplierCreditPosition(t, w, credit)
			require.Equal(t, int64(3), view.Version)
			require.Zero(t, view.AllocatedMinor)
			require.Equal(t, int64(750), view.AvailableMinor)
			paidMinor, creditMinor, outstanding = readBill(bill)
			require.Equal(t, int64(430), paidMinor)
			require.Zero(t, creditMinor)
			require.Equal(t, int64(750), outstanding)
			replayed = w.request("PUT", path, "editor", body)
			require.Equal(t, 200, replayed.Code, replayed.Body.String())
			require.JSONEq(t, allocated.Body.String(), replayed.Body.String())
		})
	}
}

func allocationIntent(bill string, version, available, billVersion, outstanding, amount int64) pharmacy.SupplierCreditAllocationInput {
	return pharmacy.SupplierCreditAllocationInput{PayableID: uuid.MustParse(bill), ExpectedVersion: &version, ExpectedAvailableMinor: &available, ExpectedPayableVersion: &billVersion, ExpectedOutstandingMinor: &outstanding, AmountMinor: &amount, OccurredOn: "2026-11-18", Reason: "TEST allocate actual supplier credit"}
}
func allocationReversalIntent(version, available, billVersion, outstanding int64) pharmacy.SupplierCreditAllocationReversalInput {
	return pharmacy.SupplierCreditAllocationReversalInput{ExpectedVersion: &version, ExpectedAvailableMinor: &available, ExpectedPayableVersion: &billVersion, ExpectedOutstandingMinor: &outstanding, OccurredOn: "2026-11-20", Reason: "TEST restore original supplier credit allocation"}
}

type supplierBillBalances struct {
	Paid        int64 `json:"paid_minor"`
	Credit      int64 `json:"credit_minor"`
	Outstanding int64 `json:"outstanding_minor"`
}

func readSupplierBillBalances(t *testing.T, w supplierReturnWorld, bill string) supplierBillBalances {
	t.Helper()
	result := do(w.r, "GET", payableRoot()+"/payables/"+bill, "editor", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	var values supplierBillBalances
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &values))
	return values
}
func TestSupplierReturnCreditAllocationsSerializeSharedCreditAndSharedBill(t *testing.T) {
	for _, sharedCredit := range []bool{true, false} {
		t.Run(fmt.Sprintf("shared_credit_%t", sharedCredit), func(t *testing.T) {
			w, source := dispatchedCreditSource(t, "independent")
			credits := []string{recordSupplierCreditFixture(t, w, source, "TEST-POOL-ONE", 1000)}
			bills := []string{futureSupplierCreditBill(t, w, "TEST-TARGET-ONE")}
			if sharedCredit {
				credits = append(credits, credits[0])
				bills = append(bills, futureSupplierCreditBill(t, w, "TEST-TARGET-TWO"))
			} else {
				credits = append(credits, recordSupplierCreditFixture(t, w, source, "TEST-POOL-TWO", 1000))
				bills = append(bills, bills[0])
			}
			results := make(chan int, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				path := credits[i] + "/allocations/" + uuid.Must(uuid.NewV7()).String()
				body := creditJSON(t, allocationIntent(bills[i], 1, 1000, 1, 1180, 750))
				wg.Go(func() { results <- w.request("PUT", path, "editor", body).Code })
			}
			wg.Wait()
			close(results)
			statuses := map[int]int{}
			for code := range results {
				statuses[code]++
			}
			require.Equal(t, 1, statuses[200])
			require.Equal(t, 1, statuses[409])
			first := supplierCreditPosition(t, w, credits[0])
			require.Equal(t, int64(1000), first.AllocatedMinor+first.AvailableMinor)
			if sharedCredit {
				require.Equal(t, int64(750), first.AllocatedMinor)
			} else {
				second := supplierCreditPosition(t, w, credits[1])
				require.Equal(t, int64(750), first.AllocatedMinor+second.AllocatedMinor)
			}
			for _, bill := range bills {
				balance := readSupplierBillBalances(t, w, bill)
				require.GreaterOrEqual(t, balance.Outstanding, int64(0))
				require.Equal(t, int64(1180), balance.Paid+balance.Credit+balance.Outstanding)
			}
		})
	}
}
func TestSupplierReturnCreditAllocationSharesLegacyPaymentBudget(t *testing.T) {
	w, source := dispatchedCreditSource(t, "hospital_owned")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-PAYMENT-RACE", 750)
	bill := futureSupplierCreditBill(t, w)
	path := credit + "/allocations/" + uuid.Must(uuid.NewV7()).String()
	body := creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 750))
	results := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Go(func() { results <- w.request("PUT", path, "editor", body).Code })
	wg.Go(func() {
		result := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"kind":"payment","amount_minor":500,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-RACING-PAYMENT","occurred_on":"2026-11-19","reason":"TEST same outstanding budget","reversal_of":null}`)
		results <- result.Code
	})
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for code := range results {
		statuses[code]++
	}
	require.Equal(t, 1, statuses[200])
	require.Equal(t, 1, statuses[409])
	balance := readSupplierBillBalances(t, w, bill)
	require.Equal(t, int64(1180), balance.Paid+balance.Credit+balance.Outstanding)
	require.GreaterOrEqual(t, balance.Outstanding, int64(0))
	current := supplierCreditPosition(t, w, credit)
	require.Equal(t, int64(750), current.AvailableMinor+current.AllocatedMinor)
	require.Equal(t, current.AllocatedMinor, balance.Credit)
}
func TestSupplierReturnCreditAllocationRecoveryScopeAndNetZeroPaymentVersion(t *testing.T) {
	w, source := dispatchedCreditSource(t, "chain")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-RECOVERY-ALLOC", 750)
	bill := futureSupplierCreditBill(t, w)
	path := credit + "/allocations/" + uuid.Must(uuid.NewV7()).String()
	body := creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 500))
	created := w.request("PUT", path, "editor", body)
	require.Equal(t, 200, created.Code, created.Body.String())
	recovered := w.request("GET", path+"/recovery", "editor", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, created.Body.String(), recovered.Body.String())
	require.Equal(t, 404, w.request("GET", path+"/recovery", "writer", "").Code)
	require.Equal(t, 409, w.request("PUT", path, "writer", body).Code)
	require.Equal(t, 403, w.request("PUT", path, "reader", body).Code)
	for _, actor := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, w.request("GET", path, actor, "").Code)
		require.Equal(t, 404, w.request("GET", credit+"/allocations", actor, "").Code)
		require.Equal(t, 404, w.request("PUT", path, actor, body).Code)
	}
	require.Equal(t, 200, w.request("GET", path, "reader", "").Code)
	payment := uuid.Must(uuid.NewV7()).String()
	paid := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/payments/"+payment, "editor", `{"kind":"payment","amount_minor":1,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-NET-ZERO-PAY","occurred_on":"2026-11-19","reason":"TEST changes the bill observation","reversal_of":null}`)
	require.Equal(t, 200, paid.Code, paid.Body.String())
	reversed := do(w.r, "PUT", payableRoot()+"/payables/"+bill+"/adjustments/"+uuid.Must(uuid.NewV7()).String(), "editor", fmt.Sprintf(`{"kind":"payment_reversal","amount_minor":1,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-NET-ZERO-REVERSE","occurred_on":"2026-11-20","reason":"TEST restores balance but not version","reversal_of":%q}`, payment))
	require.Equal(t, 200, reversed.Code, reversed.Body.String())
	stale := creditJSON(t, allocationIntent(bill, 2, 250, 2, 680, 250))
	require.Equal(t, 409, w.request("PUT", credit+"/allocations/"+uuid.Must(uuid.NewV7()).String(), "editor", stale).Code)
	fresh := w.request("PUT", credit+"/allocations/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, allocationIntent(bill, 2, 250, 4, 680, 250)))
	require.Equal(t, 200, fresh.Code, fresh.Body.String())
	reversePath := path + "/reversals/" + uuid.Must(uuid.NewV7()).String()
	reverseBody := creditJSON(t, allocationReversalIntent(3, 0, 5, 430))
	undo := w.request("PUT", reversePath, "writer", reverseBody)
	require.Equal(t, 200, undo.Code, undo.Body.String())
	recovered = w.request("GET", reversePath+"/recovery", "writer", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, undo.Body.String(), recovered.Body.String())
	require.Equal(t, 404, w.request("GET", reversePath+"/recovery", "editor", "").Code)
	require.Equal(t, 200, w.request("GET", path+"/reversal", "reader", "").Code)
	require.Equal(t, 409, w.request("PUT", path+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "writer", creditJSON(t, allocationReversalIntent(4, 500, 6, 930))).Code)
	current := supplierCreditPosition(t, w, credit)
	require.Equal(t, int64(250), current.AllocatedMinor)
	require.Equal(t, int64(500), current.AvailableMinor)
	require.Equal(t, int64(930), readSupplierBillBalances(t, w, bill).Outstanding)
}
