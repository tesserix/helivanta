package pharmacy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTallyHeadsFollowTheClosedGrammar(t *testing.T) {
	t.Parallel()
	for head, label := range map[string]string{
		"voucher_type:sale":                      "Voucher type for sales",
		"voucher_type:supplier_payment_reversal": "Voucher type for reversed supplier payments",
		"tender:upi_verified":                    "Counter verified UPI",
		"tender:admission_account":               "Admission accounts",
		"sales_goods:taxable:120000":             "Sales of goods taxable at 12%",
		"sales_goods:taxable:25000":              "Sales of goods taxable at 2.5%",
		"sales_goods:taxable:0":                  "Sales of goods taxable at 0%",
		"sales_service:taxable:180000":           "Sales of own services taxable at 18%",
		"sales_goods:nil_rated:0":                "Sales of goods, nil rated",
		"sales_service:exempt:0":                 "Sales of own services, exempt",
		"cgst:50000":                             "Output CGST on 5% supplies",
		"sgst:1000000":                           "Output SGST on 100% supplies",
		"payment_method:bank":                    "Supplier payments by bank transfer",
		"supplier:TEST-ACME/01":                  "Supplier TEST-ACME/01",
		"supplier:" + strings.Repeat("Ä", 100):   "Supplier " + strings.Repeat("Ä", 100),
	} {
		got, ok := tallyHeadLabel(head)
		require.True(t, ok, head)
		require.Equal(t, label, got, head)
	}
}

func TestTallyHeadsRefuseAnythingOutsideTheGrammar(t *testing.T) {
	t.Parallel()
	for _, head := range []string{
		"", "sale", "voucher_type:", "voucher_type:purchase", "tender:cheque", "tender:CASH",
		"sales_goods:taxable", "sales_goods:taxable:012", "sales_goods:taxable:+5", "sales_goods:taxable:1000001",
		"sales_goods:nil_rated:50000", "sales_service:exempt:1", "sales_goods:zero:0", "sales:taxable:0",
		"cgst:", "cgst:-1", "igst:120000", "cess:10000", "payment_method:cheque",
		"supplier:", "supplier: ACME", "supplier:ACME ", "supplier:acme", "supplier:" + strings.Repeat("A", 101), "supplier:\xff", "supplier:AC\x01ME", "supplier:AC\tME", "supplier:AC\u0085ME",
	} {
		_, ok := tallyHeadLabel(head)
		require.False(t, ok, head)
	}
}

func TestTallyHeadsAreBuiltFromRecordedRates(t *testing.T) {
	t.Parallel()
	require.Equal(t, "sales_goods:taxable:120000", tallySalesHead("principal_supply", "taxable", 120000))
	require.Equal(t, "sales_goods:exempt:0", tallySalesHead("goods", "exempt", 0))
	require.Equal(t, "sales_service:taxable:180000", tallySalesHead("own_service", "taxable", 180000))
	require.Equal(t, "cgst:120000", tallyTaxHead("cgst", 120000))
}
