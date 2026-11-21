package pharmacy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSupplierReturnNotePrintsFrozenCommercialFactsAndEscapesText(t *testing.T) {
	note := supplierReturnNoteFixture()
	data, err := renderSupplierReturnNote(note)
	require.NoError(t, err)
	html := string(data)
	for _, expected := range []string{"Commercial supplier return note", "Return 42", "TEST Pharmacy", "TEST Main Outlet", "TEST-BILL-1", "TEST-OUT-1", "TEST Medicine", "TEST LOT", "2028-01-01", "tablet", "4", "Not a tax invoice", "default-src &#39;none&#39;"} {
		require.Contains(t, html, expected)
	}
	require.Contains(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;")
	require.NotContains(t, html, "<script>")
	require.NotContains(t, html, note.Lines[0].ID.String())
	require.NotContains(t, html, "<img")
}

func supplierReturnNoteFixture() supplierReturnNote {
	units := int64(4)
	id := uuid.Must(uuid.NewV7())
	return supplierReturnNote{
		PharmacyName: "TEST Pharmacy", OutletName: "TEST Main Outlet", OutletCode: "TEST-MAIN",
		SupplierName: "TEST Supplier <script>alert(1)</script>", OriginalBill: "TEST-BILL-1",
		Reference: "TEST-OUT-1", Recipient: "TEST Supplier receiver", HandoffUTC: "2026-11-21 09:00:00 UTC",
		Reason: "TEST short-date review", Number: 42,
		Lines: []SupplierReturnLine{{ID: id, ProductName: "TEST Medicine", GenericName: "TEST Generic", UnitCode: "tablet", LotNumber: "TEST LOT", ExpiresOn: "2028-01-01", SupplierReturnDraftLineInput: SupplierReturnDraftLineInput{QuantityUnits: &units}}},
	}
}

func TestSupplierReturnNoteRefusesIncompleteOrOversizeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*supplierReturnNote)
	}{
		{"missing retained outlet", func(n *supplierReturnNote) { n.OutletName = "" }},
		{"missing retained pharmacy", func(n *supplierReturnNote) { n.PharmacyName = "" }},
		{"no lines", func(n *supplierReturnNote) { n.Lines = nil }},
		{"too many lines", func(n *supplierReturnNote) {
			for range 100 {
				n.Lines = append(n.Lines, n.Lines[0])
			}
		}},
		{"missing units", func(n *supplierReturnNote) { n.Lines[0].QuantityUnits = nil }},
		{"zero units", func(n *supplierReturnNote) { *n.Lines[0].QuantityUnits = 0 }},
		{"missing batch", func(n *supplierReturnNote) { n.Lines[0].LotNumber = "" }},
		{"invalid expiry", func(n *supplierReturnNote) { n.Lines[0].ExpiresOn = "not-a-date" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := supplierReturnNoteFixture()
			tc.change(&note)
			data, err := renderSupplierReturnNote(note)
			require.Error(t, err)
			require.Empty(t, data)
		})
	}
}
