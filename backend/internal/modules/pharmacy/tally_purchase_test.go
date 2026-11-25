package pharmacy

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestTallyPurchasePostsExactSourceTaxAndCosts(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	rows := []tallyRow{
		{Rank: 4, ID: id, TenantScope: "11111111-1111-4111-8111-111111111111", OutletScope: "0199e000-0000-7000-8000-000000000001", Day: "2026-11-01", Number: "TEST-PURCHASE", Currency: "INR", Supplier: "TEST-SUPPLIER", Part: "purchase_cost", Treatment: "inventory", Nature: "taxable", RatePPM: 120000, TaxableMinor: 10200},
		{Part: "purchase_tax", Kind: "cgst", RatePPM: 120000, TaxableMinor: 600, Amount: 300},
		{Part: "purchase_tax", Kind: "sgst", RatePPM: 120000, TaxableMinor: 600, Amount: 300},
		{Part: "purchase_supplier", Amount: 11400},
	}
	v, err := tallyVoucherOf(rows)
	require.NoError(t, err)
	require.Equal(t, "voucher_type:supplier_purchase", v.Type)
	require.NotEqual(t, "helivanta-"+id.String(), v.RemoteID)
	again, err := tallyVoucherOf(rows)
	require.NoError(t, err)
	require.Equal(t, v.RemoteID, again.RemoteID)
	require.Equal(t, "Purchase bill TEST-PURCHASE", v.Narration)
	entries, err := mergedTallyEntries(v.Entries)
	require.NoError(t, err)
	require.Equal(t, []tallyEntry{
		{Ledger: "input_cgst:120000", Debit: true, Minor: 300},
		{Ledger: "input_sgst:120000", Debit: true, Minor: 300},
		{Ledger: "nonrecoverable_cgst:120000", Debit: true, Minor: 300},
		{Ledger: "nonrecoverable_sgst:120000", Debit: true, Minor: 300},
		{Ledger: "purchases:inventory:taxable:120000", Debit: true, Minor: 10200},
		{Ledger: "supplier:TEST-SUPPLIER", Party: true, Minor: 11400},
	}, entries)
}
func TestTallyPurchaseRefusesMissingSourceEvidence(t *testing.T) {
	t.Parallel()
	_, err := tallyVoucherOf([]tallyRow{{Rank: 4, ID: uuid.Must(uuid.NewV7()), Part: "purchase_missing"}})
	require.ErrorIs(t, err, errTallyPurchaseEvidence)
}

func TestTallyPurchaseOptionIsExplicitAndUnambiguous(t *testing.T) {
	for _, test := range []struct {
		query    string
		want, ok bool
	}{
		{"", false, true}, {"?purchase_bills=false", false, true}, {"?purchase_bills=true", true, true},
		{"?purchase_bills=1", false, false}, {"?purchase_bills=", false, false}, {"?purchase_bills=true&purchase_bills=true", false, false},
	} {
		t.Run(test.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/"+test.query, nil)
			want, ok := tallyPurchaseOption(c)
			require.Equal(t, test.want, want)
			require.Equal(t, test.ok, ok)
			if !test.ok {
				require.Equal(t, 400, response.Code)
			}
		})
	}
}

func TestTallyPurchaseIdentityIsStableAndScoped(t *testing.T) {
	t.Parallel()
	bill := uuid.MustParse("0199e000-0000-7000-8000-000000000002")
	tenant := "11111111-1111-4111-8111-111111111111"
	outlet := "0199e000-0000-7000-8000-000000000001"
	got, err := tallyPurchaseIdentity(tenant, outlet, bill)
	require.NoError(t, err)
	require.Equal(t, "helivanta-d17896b2-c7e0-5b85-a016-0813656df50d", got)
	other, err := tallyPurchaseIdentity("22222222-2222-4222-8222-222222222222", outlet, bill)
	require.NoError(t, err)
	require.NotEqual(t, got, other)
	other, err = tallyPurchaseIdentity(tenant, bill.String(), bill)
	require.NoError(t, err)
	require.NotEqual(t, got, other)
	_, err = tallyPurchaseIdentity("", outlet, bill)
	require.ErrorIs(t, err, errTallyVoucher)
}
