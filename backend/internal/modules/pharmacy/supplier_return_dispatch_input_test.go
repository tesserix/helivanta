package pharmacy

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSupplierReturnDispatchRequiresExplicitHandoffEvidence(t *testing.T) {
	version := int64(0)
	valid := SupplierReturnDispatchInput{ExpectedVersion: &version, LicenceID: uuid.MustParse("0199e000-0000-7000-8000-000000000010"), DocumentReference: " TEST-OUT-1 ", RecipientName: " TEST Supplier Driver ", HandedOverAt: "2026-11-21T13:30:00+05:30", PhysicalDisposition: "handed_to_supplier", Reason: " TEST actual handoff "}
	for _, tc := range []struct {
		name   string
		change func(*SupplierReturnDispatchInput)
		want   bool
	}{
		{"complete", func(*SupplierReturnDispatchInput) {}, true},
		{"missing version", func(i *SupplierReturnDispatchInput) { i.ExpectedVersion = nil }, false},
		{"stale version", func(i *SupplierReturnDispatchInput) { v := int64(1); i.ExpectedVersion = &v }, false},
		{"missing licence", func(i *SupplierReturnDispatchInput) { i.LicenceID = uuid.Nil }, false},
		{"missing reference", func(i *SupplierReturnDispatchInput) { i.DocumentReference = " " }, false},
		{"missing recipient", func(i *SupplierReturnDispatchInput) { i.RecipientName = " " }, false},
		{"control character", func(i *SupplierReturnDispatchInput) { i.RecipientName = "TEST\nforged" }, false},
		{"missing attestation", func(i *SupplierReturnDispatchInput) { i.PhysicalDisposition = "" }, false},
		{"invalid time", func(i *SupplierReturnDispatchInput) { i.HandedOverAt = "2026-02-30T00:00:00Z" }, false},
		{"excess precision", func(i *SupplierReturnDispatchInput) { i.HandedOverAt = "2026-11-21T08:00:00.0000001Z" }, false},
		{"missing reason", func(i *SupplierReturnDispatchInput) { i.Reason = "" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			require.Equal(t, tc.want, normalizeSupplierReturnDispatch(&in))
			if tc.want {
				require.Equal(t, "2026-11-21T08:00:00Z", in.HandedOverAt)
				require.Equal(t, "TEST-OUT-1", in.DocumentReference)
			}
		})
	}
}
