package pharmacy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

func TestSupplierReturnCreditMatchesPartialCombinedDocumentWithoutSpendingPool(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, firstReturn := dispatchedCreditSource(t, ownership)
			secondReturn := uuid.Must(uuid.NewV7()).String()
			created := w.request("PUT", supplierReturnPath(secondReturn), "editor", w.body(4))
			require.Equal(t, 200, created.Code, created.Body.String())
			dispatched := w.request("PUT", supplierReturnPath(secondReturn)+"/dispatches/"+uuid.Must(uuid.NewV7()).String(), "writer", strings.ReplaceAll(dispatchInput(t, w), "TEST-OUT-1", "TEST-OUT-2"))
			require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
			credit := uuid.Must(uuid.NewV7()).String()
			path := creditPath(credit)
			created = w.request("PUT", path, "editor", creditJSON(t, actualCreditInput(firstReturn, "TEST-CN-COMBINED", 750, nil)))
			require.Equal(t, 200, created.Code, created.Body.String())
			for i, target := range []struct {
				id     string
				amount int64
			}{{firstReturn, 500}, {secondReturn, 250}} {
				command := uuid.Must(uuid.NewV7()).String()
				body := creditJSON(t, map[string]any{"return_id": target.id, "expected_version": i + 1, "expected_return_matched_minor": 0, "amount_minor": target.amount, "reason": "TEST supplier combined note attribution", "overage_reason": nil})
				matched := w.request("PUT", path+"/matches/"+command, "editor", body)
				require.Equal(t, 200, matched.Code, matched.Body.String())
				var receipt struct {
					AmountMinor        int64 `json:"amount_minor"`
					Version            int64 `json:"version"`
					ReturnMatchedMinor int64 `json:"return_matched_minor"`
				}
				require.NoError(t, json.Unmarshal(matched.Body.Bytes(), &receipt))
				require.Equal(t, target.amount, receipt.AmountMinor)
				require.EqualValues(t, i+2, receipt.Version)
				require.Equal(t, target.amount, receipt.ReturnMatchedMinor)
				replayed := w.request("PUT", path+"/matches/"+command, "editor", body)
				require.Equal(t, 200, replayed.Code, replayed.Body.String())
				require.JSONEq(t, matched.Body.String(), replayed.Body.String())
			}
			current := w.request("GET", path, "reader", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			var view pharmacy.SupplierCreditDocumentView
			require.NoError(t, json.Unmarshal(current.Body.Bytes(), &view))
			require.Equal(t, int64(3), view.Version)
			require.Equal(t, int64(750), view.MatchedMinor)
			require.Zero(t, view.AllocatedMinor)
			require.Equal(t, int64(750), view.AvailableMinor)
		})
	}
}
