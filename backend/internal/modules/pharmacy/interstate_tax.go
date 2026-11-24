package pharmacy

import "math/big"

// IGST is rounded once, rather than doubling a rounded central-tax half.
func integratedTax(amount int64, nature string, rate int64) (taxable, igst int64) {
	if nature == "taxable" {
		den := big.NewInt(1_000_000 + rate)
		num := new(big.Int).Mul(big.NewInt(amount), big.NewInt(rate))
		num.Add(num, new(big.Int).Quo(new(big.Int).Set(den), big.NewInt(2)))
		igst = num.Quo(num, den).Int64()
	}
	return amount - igst, igst
}

func applySaleDestination(seller, destination string, lines []saleLine, charges []saleChargeLine) error {
	if destination == "" || seller == destination {
		return nil
	}
	for _, ch := range charges {
		if ch.TaxTreatment == "own_service" {
			return errNotSellable{"an interstate sale cannot include an independently taxed service charge"}
		}
	}
	for i := range lines {
		l := &lines[i]
		l.TaxableMinor, l.IGSTMinor = integratedTax(l.AmountMinor, l.SupplyNature, l.RatePPM)
		l.CGSTMinor, l.SGSTMinor = 0, 0
	}
	return nil
}
