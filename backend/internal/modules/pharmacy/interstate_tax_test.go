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
