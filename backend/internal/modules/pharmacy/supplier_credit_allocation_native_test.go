package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func futureSupplierCreditBill(t *testing.T, w supplierReturnWorld) string {
	t.Helper()
	receipt, bill := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	received := do(w.r, "PUT", stockPath("movements/"+receipt), "editor", stockBody(w.location, w.batch, "receipt", "TEST-future-supplier-delivery", 20))
	require.Equal(t, 200, received.Code, received.Body.String())
	input := fmt.Sprintf(`{"payee_reference":"TEST-EXT","payee_name":"TEST External Supplier","document_reference":"TEST-FUTURE-BILL","issued_on":"2026-11-17","due_on":"2026-11-30","currency":"INR","lines":[{"category":"inventory","receipt_id":%q,"description":"TEST future supplier cartons","net_minor":1000,"tax_minor":180,"recoverable_tax_minor":180,"landed_minor":0}]}`, receipt)
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
