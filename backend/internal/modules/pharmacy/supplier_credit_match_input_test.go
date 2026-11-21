package pharmacy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSupplierCreditMatchInputRequiresExplicitCurrentObservations(t *testing.T) {
	version, observed, amount := int64(1), int64(0), int64(500)
	input := SupplierCreditMatchInput{ReturnID: uuid.Must(uuid.NewV7()), ExpectedVersion: &version, ExpectedReturnMatchedMinor: &observed, AmountMinor: &amount, Reason: " TEST partial attribution "}
	require.True(t, normalizeSupplierCreditMatch(&input))
	require.Equal(t, "TEST partial attribution", input.Reason)
	for _, tc := range []struct {
		name   string
		change func(*SupplierCreditMatchInput)
	}{
		{"missing version", func(i *SupplierCreditMatchInput) { i.ExpectedVersion = nil }},
		{"zero version", func(i *SupplierCreditMatchInput) { v := int64(0); i.ExpectedVersion = &v }},
		{"overflow version", func(i *SupplierCreditMatchInput) { v := int64(1000000000000); i.ExpectedVersion = &v }},
		{"missing observed total", func(i *SupplierCreditMatchInput) { i.ExpectedReturnMatchedMinor = nil }},
		{"negative observed total", func(i *SupplierCreditMatchInput) { v := int64(-1); i.ExpectedReturnMatchedMinor = &v }},
		{"missing amount", func(i *SupplierCreditMatchInput) { i.AmountMinor = nil }},
		{"zero amount", func(i *SupplierCreditMatchInput) { v := int64(0); i.AmountMinor = &v }},
		{"overflow total", func(i *SupplierCreditMatchInput) { v := int64(1000000000000); i.ExpectedReturnMatchedMinor = &v }},
		{"missing return", func(i *SupplierCreditMatchInput) { i.ReturnID = uuid.Nil }},
		{"legacy identity", func(i *SupplierCreditMatchInput) { i.ReturnID = uuid.New() }},
		{"blank reason", func(i *SupplierCreditMatchInput) { i.Reason = " " }},
		{"empty overage reason", func(i *SupplierCreditMatchInput) { v := " "; i.OverageReason = &v }},
	} {
		t.Run(tc.name, func(t *testing.T) { in := input; tc.change(&in); require.False(t, normalizeSupplierCreditMatch(&in)) })
	}
}

func TestSupplierCreditMatchReversalRequiresCurrentVersionTotalAndReason(t *testing.T) {
	version, observed := int64(2), int64(500)
	input := SupplierCreditMatchReversalInput{ExpectedVersion: &version, ExpectedReturnMatchedMinor: &observed, Reason: " TEST full commercial correction "}
	require.True(t, normalizeSupplierCreditMatchReversal(&input))
	require.Equal(t, "TEST full commercial correction", input.Reason)
	input.ExpectedReturnMatchedMinor = nil
	require.False(t, normalizeSupplierCreditMatchReversal(&input))
	input.ExpectedReturnMatchedMinor = &observed
	input.ExpectedVersion = nil
	require.False(t, normalizeSupplierCreditMatchReversal(&input))
	input.ExpectedVersion = &version
	input.Reason = " "
	require.False(t, normalizeSupplierCreditMatchReversal(&input))
}
