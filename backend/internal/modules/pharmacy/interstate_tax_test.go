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
