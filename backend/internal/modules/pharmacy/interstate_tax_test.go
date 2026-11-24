package pharmacy

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInterstateTaxIsOneRoundedComponent(t *testing.T) {
	lines := []saleLine{{AmountMinor: 12500, TaxableMinor: 11160, CGSTMinor: 670, SGSTMinor: 670, SupplyNature: "taxable", RatePPM: 120000}}
	require.NoError(t, applySaleDestination("27", "21", lines, nil))
	require.EqualValues(t, 11161, lines[0].TaxableMinor)
	require.EqualValues(t, 1339, lines[0].IGSTMinor)
	require.Zero(t, lines[0].CGSTMinor)
	require.Zero(t, lines[0].SGSTMinor)
}

func TestInterstateOwnServiceFailsWithoutChangingLines(t *testing.T) {
	lines := []saleLine{{AmountMinor: 12500, TaxableMinor: 11160, CGSTMinor: 670, SGSTMinor: 670, SupplyNature: "taxable", RatePPM: 120000}}
	err := applySaleDestination("27", "21", lines, []saleChargeLine{{TaxTreatment: "own_service"}})
	require.Error(t, err)
	require.EqualValues(t, 670, lines[0].CGSTMinor)
}

func TestInterstateCeilingUsesIntegratedTaxRounding(t *testing.T) {
	ceiling := int64(11160)
	lines := []saleLine{{AmountMinor: 12500, ListAmountMinor: 12500, QuantityUnits: 1, CeilingMinor: &ceiling, TaxableMinor: 11160, CGSTMinor: 670, SGSTMinor: 670, SupplyNature: "taxable", RatePPM: 120000}}
	require.NoError(t, applySaleDestination("27", "21", lines, nil))
	require.EqualValues(t, 12499, lines[0].AmountMinor)
	require.EqualValues(t, 11160, lines[0].TaxableMinor)
	require.Equal(t, "ceiling", lines[0].PriceLimit)
}

func TestInterstateReturnsReverseEveryOriginalPaisa(t *testing.T) {
	var amount, taxable, igst int64
	for k := int64(0); k < 7; k++ {
		a, tx, cg, sg, ig := creditTaxStep(11161, 0, 0, 1339, 7, k, 1)
		require.Zero(t, cg)
		require.Zero(t, sg)
		require.Equal(t, a, tx+ig)
		amount += a
		taxable += tx
		igst += ig
	}
	require.EqualValues(t, 12500, amount)
	require.EqualValues(t, 11161, taxable)
	require.EqualValues(t, 1339, igst)
}

func TestInterstateTaxBoundariesAndSupplyNatures(t *testing.T) {
	for _, tc := range []struct {
		gross, rate, taxable, tax int64
		nature                    string
	}{
		{10000, 50000, 9524, 476, "taxable"}, {1, 1000000, 0, 1, "taxable"}, {1, 120000, 1, 0, "taxable"},
		{1_000_000_000_000, 1000000, 500_000_000_000, 500_000_000_000, "taxable"},
		{10000, 0, 10000, 0, "nil_rated"}, {10000, 0, 10000, 0, "exempt"},
	} {
		taxable, tax := integratedTax(tc.gross, tc.nature, tc.rate)
		require.Equal(t, tc.taxable, taxable)
		require.Equal(t, tc.tax, tax)
	}
}

func TestInterstateDestinationDefaultsAndValidates(t *testing.T) {
	for _, tc := range []struct{ seller, requested, want string }{{"27", "", "27"}, {"27", "27", "27"}, {"27", "21", "21"}, {"21", "27", "27"}} {
		got, err := saleDestination(tc.seller, tc.requested)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	for _, bad := range []string{"99", "2", " 21", "OR", "21\n"} {
		_, err := saleDestination("27", bad)
		require.Error(t, err)
	}
}

func TestInterstateConversionPreservesIntrastateAndPrincipalSupply(t *testing.T) {
	original := saleLine{AmountMinor: 12500, TaxableMinor: 11160, CGSTMinor: 670, SGSTMinor: 670, SupplyNature: "taxable", RatePPM: 120000}
	for _, destination := range []string{"", "27"} {
		lines := []saleLine{original}
		require.NoError(t, applySaleDestination("27", destination, lines, nil))
		require.Equal(t, original, lines[0])
	}
	charges := []saleChargeLine{{AmountMinor: 100, TaxableMinor: 90, CGSTMinor: 5, SGSTMinor: 5, SupplyNature: "taxable", RatePPM: 120000, TaxTreatment: "principal_supply"}}
	require.NoError(t, applySaleDestination("27", "21", nil, charges))
	require.EqualValues(t, 89, charges[0].TaxableMinor)
	require.EqualValues(t, 11, charges[0].IGSTMinor)
	require.Zero(t, charges[0].CGSTMinor)
	require.Zero(t, charges[0].SGSTMinor)
}
