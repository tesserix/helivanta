package pharmacy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplierReturnCreditAllocationFailurePreservesCreditAndBill(t *testing.T) {
	w, source := dispatchedCreditSource(t, "independent")
	credit := recordSupplierCreditFixture(t, w, source, "TEST-ALLOCATION-FAULT", 750)
	bill := futureSupplierCreditBill(t, w)
	allocation := credit + "/allocations/" + uuid.Must(uuid.NewV7()).String()
	body := creditJSON(t, allocationIntent(bill, 1, 750, 1, 1180, 500))
	reversal := allocation + "/reversals/" + uuid.Must(uuid.NewV7()).String()
	reverseBody := creditJSON(t, allocationReversalIntent(2, 250, 2, 680))
	apply := func(sql string) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		require.NoError(t, w.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_allocation_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	for _, command := range []struct {
		path, body, actor, table                   string
		version, available, allocated, outstanding int64
	}{
		{allocation, body, "editor", "pharmacy_supplier_credit_allocations", 1, 750, 0, 1180},
		{reversal, reverseBody, "writer", "pharmacy_supplier_credit_allocation_reversals", 2, 250, 500, 680},
	} {
		for _, fault := range []struct{ table, check string }{
			{"audit_events", "resource_type<>'" + command.table + "'"},
			{"audit_events", "resource_type<>'pharmacy_supplier_credit_positions'"},
			{"outbox_events", "subject<>'helivanta.in.pharmacy.supplier_credit_changed.v1'"},
		} {
			clear := func() {
				apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS synthetic_allocation_failure")
			}
			t.Cleanup(clear)
			apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT synthetic_allocation_failure CHECK(" + fault.check + ") NOT VALID")
			failed := w.request("PUT", command.path, command.actor, command.body)
			require.Equal(t, 503, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic_allocation_failure")
			require.Equal(t, 404, w.request("GET", command.path+"/recovery", command.actor, "").Code)
			clear()
			current := supplierCreditPosition(t, w, credit)
			require.Equal(t, command.version, current.Version)
			require.Equal(t, command.available, current.AvailableMinor)
			require.Equal(t, command.allocated, current.AllocatedMinor)
			balance := readSupplierBillBalances(t, w, bill)
			require.Equal(t, command.outstanding, balance.Outstanding)
			require.Equal(t, command.allocated, balance.Credit)
			require.Zero(t, balance.Paid)
		}
		posted := w.request("PUT", command.path, command.actor, command.body)
		require.Equal(t, 200, posted.Code, posted.Body.String())
	}
	apply("ALTER TABLE audit_events ADD CONSTRAINT synthetic_allocation_read_failure CHECK(resource_type NOT IN('pharmacy_supplier_credit_allocations','pharmacy_supplier_credit_allocation','pharmacy_supplier_credit_allocation_reversal')) NOT VALID")
	t.Cleanup(func() { apply("ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_allocation_read_failure") })
	for _, read := range []struct{ path, actor string }{
		{allocation, "editor"}, {allocation + "/recovery", "editor"}, {credit + "/allocations", "reader"},
		{allocation + "/reversal", "reader"}, {reversal + "/recovery", "writer"},
	} {
		failed := w.request("GET", read.path, read.actor, "")
		require.Equal(t, 503, failed.Code, failed.Body.String())
		require.NotContains(t, failed.Body.String(), "TEST allocate actual supplier credit")
		require.NotContains(t, failed.Body.String(), "TEST-ALLOCATION-FAULT")
		require.NotContains(t, failed.Body.String(), "TEST-FUTURE-BILL")
	}
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		for _, table := range []string{"pharmacy_supplier_credit_allocations", "pharmacy_supplier_credit_allocation_reversals"} {
			var n int64
			if err := tx.Table(table).Count(&n).Error; err != nil {
				return err
			}
			require.EqualValues(t, 1, n)
		}
		var n int64
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.pharmacy.supplier_credit_changed.v1").Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 3, n)
		return nil
	}))
}
