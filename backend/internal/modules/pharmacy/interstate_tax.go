package pharmacy

import (
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"math/big"
)

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
		scheme, _ := countryprofile.Tax("IN")
		jurisdiction, ok := scheme.Jurisdiction(seller)
		if !ok {
			return errNotSellable{"choose a supported Indian supplier state"}
		}
		if jurisdiction.Kind == "union_territory" {
			for i := range lines {
				lines[i].UTGSTMinor, lines[i].SGSTMinor = lines[i].CGSTMinor, 0
			}
			for i := range charges {
				charges[i].UTGSTMinor, charges[i].SGSTMinor = charges[i].CGSTMinor, 0
			}
		}
		return nil
	}
	for _, ch := range charges {
		if ch.TaxTreatment == "own_service" {
			return errNotSellable{"an interstate sale cannot include an independently taxed service charge"}
		}
	}
	for i := range lines {
		l := &lines[i]
		if l.CeilingMinor != nil {
			list, capped := withinCeiling(l.ListAmountMinor, *l.CeilingMinor*l.QuantityUnits, l.DiscountBps, l.SupplyNature, l.RatePPM, true)
			l.ListAmountMinor = list
			l.DiscountMinor, l.AmountMinor = discountLine(list, l.DiscountBps)
			if l.AmountMinor < 1 {
				return errNotSellable{"the interstate ceiling leaves nothing to charge; sell more units"}
			}
			if capped {
				l.PriceLimit = "ceiling"
			}
		}
		l.TaxableMinor, l.IGSTMinor = integratedTax(l.AmountMinor, l.SupplyNature, l.RatePPM)
		l.CGSTMinor, l.SGSTMinor, l.UTGSTMinor = 0, 0, 0
	}
	for i := range charges {
		ch := &charges[i]
		ch.TaxableMinor, ch.IGSTMinor = integratedTax(ch.AmountMinor, ch.SupplyNature, ch.RatePPM)
		ch.CGSTMinor, ch.SGSTMinor, ch.UTGSTMinor = 0, 0, 0
	}
	return nil
}

func saleUTGST(lines []saleLine, charges []saleChargeLine) (utgst int64) {
	for _, line := range lines {
		utgst += line.UTGSTMinor
	}
	for _, charge := range charges {
		utgst += charge.UTGSTMinor
	}
	return utgst
}

func saleIGST(lines []saleLine, charges []saleChargeLine) (igst int64) {
	for _, l := range lines {
		igst += l.IGSTMinor
	}
	for _, ch := range charges {
		igst += ch.IGSTMinor
	}
	return igst
}

func saleDestination(seller, requested string) (string, error) {
	if requested == "" {
		return seller, nil
	}
	scheme, _ := countryprofile.Tax("IN")
	if _, ok := scheme.Jurisdiction(requested); !ok {
		return "", errNotSellable{"choose a supported Indian recipient state"}
	}
	return requested, nil
}

func creditTaxStep(taxable, cgst, sgst, igst, q, k, r int64) (amount, tx, cg, sg, ig int64) {
	step := func(v int64) int64 { return proRata(v, k+r, q) - proRata(v, k, q) }
	tx, cg, sg, ig = step(taxable), step(cgst), step(sgst), step(igst)
	return tx + cg + sg + ig, tx, cg, sg, ig
}
