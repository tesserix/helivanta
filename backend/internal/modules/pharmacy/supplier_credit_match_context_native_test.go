package pharmacy_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestSupplierReturnCreditMatchContextIncludesOtherCreditsAndRetainedReversal(t *testing.T) {
	for _, ownership := range []string{"hospital_owned", "independent", "chain"} {
		t.Run(ownership, func(t *testing.T) {
			w, source := dispatchedCreditSource(t, ownership)
			credit := recordSupplierCreditFixture(t, w, source, "TEST-MATCH-CONTEXT", 750)
			other := recordSupplierCreditFixture(t, w, source, "TEST-OTHER-MATCH-CONTEXT", 100)
			original := credit + "/matches/" + uuid.Must(uuid.NewV7()).String()
			created := w.request("PUT", original, "editor", creditJSON(t, supplierCreditMatchInput(source, 1, 0, 500, nil)))
			require.Equal(t, 200, created.Code, created.Body.String())
			extra := w.request("PUT", other+"/matches/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchInput(source, 1, 500, 100, nil)))
			require.Equal(t, 200, extra.Code, extra.Body.String())
			read := func() pharmacy.SupplierCreditMatchContext {
				response := w.request("GET", original+"/context", "reader", "")
				require.Equal(t, 200, response.Code, response.Body.String())
				var observed pharmacy.SupplierCreditMatchContext
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &observed))
				require.Equal(t, source, observed.Returned.ID.String())
				require.Equal(t, observed.Match.OutletID, observed.Returned.OutletID)
				require.Equal(t, observed.Match.ReturnNumber, observed.Returned.Number)
				require.Equal(t, observed.Match.ExpectedCreditMinor, observed.Returned.ExpectedCreditMinor)
				require.Equal(t, "INR", observed.Returned.Currency)
				require.Equal(t, "TEST External Supplier", observed.Returned.PayeeName)
				require.NotEmpty(t, observed.Returned.OriginalBillReference)
				require.Equal(t, "user-editor", observed.Match.RecordedBy)
				require.Equal(t, "Asia/Kolkata", observed.Credit.Timezone)
				require.NotEmpty(t, observed.Credit.LocalDate)
				return observed
			}
			current := read()
			require.Equal(t, int64(2), current.Credit.Version)
			require.Equal(t, int64(500), current.Credit.MatchedMinor)
			require.Equal(t, int64(750), current.Credit.AvailableMinor)
			require.Equal(t, int64(500), current.Match.ReturnMatchedMinor)
			require.Equal(t, int64(600), current.Returned.MatchedMinor)
			require.Nil(t, current.Match.ReversalID)
			stale := w.request("PUT", original+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchReversalInput(2, 500)))
			require.Equal(t, 409, stale.Code, stale.Body.String())
			reversed := w.request("PUT", original+"/reversals/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, supplierCreditMatchReversalInput(current.Credit.Version, current.Returned.MatchedMinor)))
			require.Equal(t, 200, reversed.Code, reversed.Body.String())
			current = read()
			require.Equal(t, int64(3), current.Credit.Version)
			require.Zero(t, current.Credit.MatchedMinor)
			require.Equal(t, int64(100), current.Returned.MatchedMinor)
			require.Equal(t, int64(500), current.Match.ReturnMatchedMinor)
			require.NotNil(t, current.Match.ReversalID)
			voided := w.request("PUT", credit+"/voids/"+uuid.Must(uuid.NewV7()).String(), "editor", creditJSON(t, map[string]any{"expected_version": 3, "reason": "TEST void after matching reversal"}))
			require.Equal(t, 200, voided.Code, voided.Body.String())
			current = read()
			require.Equal(t, "voided", current.Credit.State)
			require.Equal(t, int64(4), current.Credit.Version)
			require.Zero(t, current.Credit.AvailableMinor)
			require.Equal(t, int64(100), current.Returned.MatchedMinor)
			for _, actor := range []string{"foreign", "sibling"} {
				denied := w.request("GET", original+"/context", actor, "")
				require.Equal(t, 404, denied.Code, denied.Body.String())
				require.NotContains(t, denied.Body.String(), "TEST-MATCH-CONTEXT")
			}
			require.Equal(t, 404, w.request("GET", other+"/matches/"+current.Match.ID.String()+"/context", "reader", "").Code)
			require.Equal(t, 400, w.request("GET", original+"/context?unknown=1", "reader", "").Code)
			apply := func(sql string) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_match_context_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
			}
			clear := func() {
				apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_credit_match_context_read")
			}
			t.Cleanup(clear)
			apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_credit_match_context_read CHECK(resource_type<>'pharmacy_supplier_credit_match_context') NOT VALID")
			failed := w.request("GET", original+"/context", "reader", "")
			require.Equal(t, 503, failed.Code, failed.Body.String())
			for _, private := range []string{"TEST-MATCH-CONTEXT", "TEST External Supplier", current.Returned.OriginalBillReference} {
				require.NotContains(t, failed.Body.String(), private)
			}
			clear()
			require.Equal(t, 200, w.request("GET", original+"/context", "reader", "").Code)
		})
	}
}
