package pharmacy

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func purchaseTaxFixture() (PurchaseTaxInput, payable) {
	gstin := "07ABCDE1234F1Z2"
	return PurchaseTaxInput{SupplierGstin: &gstin, SupplierState: "07", PlaceOfSupply: "07", Reason: "Supplier invoice evidence", Lines: []PurchaseTaxLineInput{{Position: 1, SupplyNature: "taxable", RatePpm: 120000, CgstMinor: 600, SgstMinor: 600, CgstRecoverableMinor: 300, SgstRecoverableMinor: 300}}}, payable{Fields: payableInput{Currency: "INR", Lines: []payableLineInput{{Category: "other", NetMinor: 10000, TaxMinor: 1200, RecoverableTaxMinor: 600}}}}
}

func TestPurchaseTaxEvidenceMustMatchPostedBill(t *testing.T) {
	t.Parallel()
	in, bill := purchaseTaxFixture()
	require.NoError(t, validatePurchaseTax(in, bill))
	for name, change := range map[string]func(*PurchaseTaxInput){
		"missing line":                  func(v *PurchaseTaxInput) { v.Lines = nil },
		"duplicate position":            func(v *PurchaseTaxInput) { v.Lines = append(v.Lines, v.Lines[0]) },
		"unknown line":                  func(v *PurchaseTaxInput) { v.Lines[0].Position = 2 },
		"different total tax":           func(v *PurchaseTaxInput) { v.Lines[0].CgstMinor++ },
		"different recoverable tax":     func(v *PurchaseTaxInput) { v.Lines[0].CgstRecoverableMinor++ },
		"recoverable exceeds component": func(v *PurchaseTaxInput) { v.Lines[0].CgstRecoverableMinor = 601; v.Lines[0].SgstRecoverableMinor = 0 },
		"negative amount":               func(v *PurchaseTaxInput) { v.Lines[0].IgstMinor = -1; v.Lines[0].CgstMinor++ },
		"mixed components":              func(v *PurchaseTaxInput) { v.Lines[0].IgstMinor = 1; v.Lines[0].CgstMinor-- },
		"wrong state":                   func(v *PurchaseTaxInput) { v.SupplierState = "25" },
		"wrong destination":             func(v *PurchaseTaxInput) { v.PlaceOfSupply = "XX" },
		"different Gstin state":         func(v *PurchaseTaxInput) { v.SupplierState = "27" },
		"unregistered positive tax":     func(v *PurchaseTaxInput) { v.SupplierGstin = nil },
		"exempt with tax":               func(v *PurchaseTaxInput) { v.Lines[0].SupplyNature = "exempt"; v.Lines[0].RatePpm = 0 },
		"negative rate":                 func(v *PurchaseTaxInput) { v.Lines[0].RatePpm = -1 },
		"overflowing rate":              func(v *PurchaseTaxInput) { v.Lines[0].RatePpm = 1000001 },
		"blank reason":                  func(v *PurchaseTaxInput) { v.Reason = " " },
		"nonrecord predecessor":         func(v *PurchaseTaxInput) { id := uuid.New(); v.Replaces = &id },
	} {
		t.Run(name, func(t *testing.T) {
			input, doc := purchaseTaxFixture()
			change(&input)
			require.Error(t, validatePurchaseTax(input, doc))
		})
	}
}

func TestPurchaseTaxPreservesInterstateAndTerritoryComponents(t *testing.T) {
	t.Parallel()
	input, bill := purchaseTaxFixture()
	input.PlaceOfSupply = "21"
	input.Lines[0] = PurchaseTaxLineInput{Position: 1, SupplyNature: "taxable", RatePpm: 120000, IgstMinor: 1200, IgstRecoverableMinor: 600}
	require.NoError(t, validatePurchaseTax(input, bill))
	input.PlaceOfSupply = "07"
	require.Error(t, validatePurchaseTax(input, bill))
	input.SupplierState = "04"
	gstin := "04ABCDE1234F1Z8"
	input.SupplierGstin = &gstin
	input.PlaceOfSupply = "04"
	input.Lines[0] = PurchaseTaxLineInput{Position: 1, SupplyNature: "taxable", RatePpm: 120000, CgstMinor: 600, UtgstMinor: 600, CgstRecoverableMinor: 300, UtgstRecoverableMinor: 300}
	require.NoError(t, validatePurchaseTax(input, bill))
	input.Lines[0].SgstMinor = 600
	input.Lines[0].UtgstMinor = 0
	require.Error(t, validatePurchaseTax(input, bill))
}

func TestPurchaseTaxAllowsExplicitOutsideGSTWithoutInventingInputCredit(t *testing.T) {
	t.Parallel()
	in, bill := purchaseTaxFixture()
	in.SupplierGstin = nil
	bill.Fields.Lines[0].TaxMinor = 0
	bill.Fields.Lines[0].RecoverableTaxMinor = 0
	in.Lines[0] = PurchaseTaxLineInput{Position: 1, SupplyNature: "outside_gst"}
	require.NoError(t, validatePurchaseTax(in, bill))
	in.Lines[0].CgstRecoverableMinor = 1
	require.Error(t, validatePurchaseTax(in, bill))
}
