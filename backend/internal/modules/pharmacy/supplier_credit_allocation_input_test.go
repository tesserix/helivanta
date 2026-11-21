package pharmacy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSupplierCreditAllocationRequiresExplicitConservedObservedBalances(t *testing.T) {
	version, available, billVersion, outstanding, amount := int64(1), int64(750), int64(1), int64(1180), int64(750)
	input := SupplierCreditAllocationInput{PayableID: uuid.Must(uuid.NewV7()), ExpectedVersion: &version, ExpectedAvailableMinor: &available, ExpectedPayableVersion: &billVersion, ExpectedOutstandingMinor: &outstanding, AmountMinor: &amount, OccurredOn: "2026-11-18", Reason: " TEST apply received supplier credit "}
	require.True(t, normalizeSupplierCreditAllocation(&input))
	require.Equal(t, "TEST apply received supplier credit", input.Reason)
	for _, tc := range []struct {
		name   string
		change func(*SupplierCreditAllocationInput)
	}{
		{"missing credit version", func(i *SupplierCreditAllocationInput) { i.ExpectedVersion = nil }},
		{"missing bill version", func(i *SupplierCreditAllocationInput) { i.ExpectedPayableVersion = nil }},
		{"missing available amount", func(i *SupplierCreditAllocationInput) { i.ExpectedAvailableMinor = nil }},
		{"missing outstanding", func(i *SupplierCreditAllocationInput) { i.ExpectedOutstandingMinor = nil }},
		{"missing actual amount", func(i *SupplierCreditAllocationInput) { i.AmountMinor = nil }},
		{"exceeds declared pool", func(i *SupplierCreditAllocationInput) { v := int64(751); i.AmountMinor = &v }},
		{"exceeds declared liability", func(i *SupplierCreditAllocationInput) { v := int64(749); i.ExpectedOutstandingMinor = &v }},
		{"zero credit version", func(i *SupplierCreditAllocationInput) { v := int64(0); i.ExpectedVersion = &v }},
		{"overflow bill version", func(i *SupplierCreditAllocationInput) { v := int64(1000000000000); i.ExpectedPayableVersion = &v }},
		{"negative pool", func(i *SupplierCreditAllocationInput) { v := int64(-1); i.ExpectedAvailableMinor = &v }},
		{"missing bill", func(i *SupplierCreditAllocationInput) { i.PayableID = uuid.Nil }},
		{"legacy bill key", func(i *SupplierCreditAllocationInput) { i.PayableID = uuid.New() }},
		{"invalid date", func(i *SupplierCreditAllocationInput) { i.OccurredOn = "2026-02-30" }},
		{"unsupported date", func(i *SupplierCreditAllocationInput) { i.OccurredOn = "1969-12-31" }},
		{"blank reason", func(i *SupplierCreditAllocationInput) { i.Reason = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			tc.change(&in)
			require.False(t, normalizeSupplierCreditAllocation(&in))
		})
	}
}

func TestSupplierCreditAllocationReversalPreservesExplicitZeroPoolAndLiability(t *testing.T) {
	version, pool, billVersion, outstanding := int64(2), int64(0), int64(3), int64(0)
	in := SupplierCreditAllocationReversalInput{ExpectedVersion: &version, ExpectedAvailableMinor: &pool, ExpectedPayableVersion: &billVersion, ExpectedOutstandingMinor: &outstanding, OccurredOn: "2026-11-20", Reason: " TEST restore allocation, keep payment "}
	require.True(t, normalizeSupplierCreditAllocationReversal(&in))
	require.Equal(t, "TEST restore allocation, keep payment", in.Reason)
	in.ExpectedAvailableMinor = nil
	require.False(t, normalizeSupplierCreditAllocationReversal(&in))
	in.ExpectedAvailableMinor = &pool
	in.ExpectedOutstandingMinor = nil
	require.False(t, normalizeSupplierCreditAllocationReversal(&in))
	in.ExpectedOutstandingMinor = &outstanding
	in.OccurredOn = "2026-02-30"
	require.False(t, normalizeSupplierCreditAllocationReversal(&in))
}
