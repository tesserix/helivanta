package pharmacy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSupplierCreditDocumentInputPreservesExplicitCommercialIntent(t *testing.T) {
	amount := int64(1500)
	input := SupplierCreditDocumentInput{ReturnID: uuid.Must(uuid.NewV7()), DocumentReference: " test-cn-1 ", IssuedOn: "2026-11-15", AmountMinor: &amount, Currency: "INR", Reason: " TEST actual supplier note "}
	require.True(t, normalizeSupplierCreditDocument(&input))
	require.Equal(t, "TEST-CN-1", input.DocumentReference)
	require.Equal(t, "TEST actual supplier note", input.Reason)
	for _, tc := range []struct {
		name   string
		change func(*SupplierCreditDocumentInput)
	}{
		{"missing amount", func(i *SupplierCreditDocumentInput) { i.AmountMinor = nil }},
		{"zero amount", func(i *SupplierCreditDocumentInput) { *i.AmountMinor = 0 }},
		{"oversize amount", func(i *SupplierCreditDocumentInput) { *i.AmountMinor = 1000000000001 }},
		{"missing return", func(i *SupplierCreditDocumentInput) { i.ReturnID = uuid.Nil }},
		{"old identity", func(i *SupplierCreditDocumentInput) { i.ReturnID = uuid.New() }},
		{"invalid date", func(i *SupplierCreditDocumentInput) { i.IssuedOn = "2026-02-30" }},
		{"unsupported currency", func(i *SupplierCreditDocumentInput) { i.Currency = "USD" }},
		{"blank reference", func(i *SupplierCreditDocumentInput) { i.DocumentReference = " " }},
		{"blank reason", func(i *SupplierCreditDocumentInput) { i.Reason = " " }},
		{"bad correction", func(i *SupplierCreditDocumentInput) { id := uuid.Nil; i.CorrectionOf = &id }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			value := int64(1500)
			in.AmountMinor = &value
			tc.change(&in)
			require.False(t, normalizeSupplierCreditDocument(&in))
		})
	}
}

func TestSupplierCreditVoidRequiresExplicitVersionAndReason(t *testing.T) {
	v := int64(1)
	in := SupplierCreditVoidInput{ExpectedVersion: &v, Reason: " TEST correction needed "}
	require.True(t, normalizeSupplierCreditVoid(&in))
	require.Equal(t, "TEST correction needed", in.Reason)
	in.ExpectedVersion = nil
	require.False(t, normalizeSupplierCreditVoid(&in))
	v = 0
	in.ExpectedVersion = &v
	require.False(t, normalizeSupplierCreditVoid(&in))
	v = 1
	in.Reason = " "
	require.False(t, normalizeSupplierCreditVoid(&in))
}
