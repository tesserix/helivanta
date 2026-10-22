// Package tax computes per-line tax from a country's tax scheme and a
// rate the tenant chose. It holds no rates of its own: the platform has no
// approved rates (docs/domain/india/billing-and-insurance.md IN-RC-10).
//
// Arithmetic is integer-only. Amounts are minor units; rates are parts per
// million, so half of any rate entered to 0.01% is still a whole number.
//
// Rounding: half up to the minor unit, once per component, per line. Within
// one jurisdiction the half-tax is computed once and used for both
// components, so they are always equal. For a tax-inclusive amount the
// taxable value is the amount less the tax, so taxable plus tax is exactly
// the amount and rounding can never push a price above its MRP.
package tax

import (
	"errors"
	"math/big"

	"github.com/tesserix/helivanta/pkg/countryprofile"
)

// Basis says whether a line amount already includes tax (as MRP does).
type Basis string

// Line amount bases.
const (
	Inclusive Basis = "inclusive"
	Exclusive Basis = "exclusive"
)

// Nature is how a supply is treated.
type Nature string

// Supply natures.
const (
	Taxable  Nature = "taxable"
	NilRated Nature = "nil_rated"
	Exempt   Nature = "exempt"
)

// MaxRatePPM is 100%.
const MaxRatePPM = 1_000_000

// RateStepPPM is the entry precision, 0.01%.
const RateStepPPM = 100

// Line is one priced line.
type Line struct {
	Amount  int64
	Basis   Basis
	RatePPM int64
	Nature  Nature
}

// Component is one tax on a line, such as CGST.
type Component struct {
	Name    string
	RatePPM int64
	Amount  int64
}

// Result is a line's taxable value and its components.
type Result struct {
	Taxable    int64
	Components []Component
	Nature     Nature
}

// Errors returned by Compute.
var (
	ErrUnknownJurisdiction = errors.New("tax: unknown jurisdiction")
	ErrInvalidRate         = errors.New("tax: invalid rate for supply nature")
	ErrInvalidBasis        = errors.New("tax: invalid amount basis")
	ErrNegativeAmount      = errors.New("tax: negative amount")
)

// ValidRate reports whether a rate is allowed for a nature.
func ValidRate(n Nature, ratePPM int64) bool {
	switch n {
	case Taxable:
		return ratePPM >= RateStepPPM && ratePPM <= MaxRatePPM && ratePPM%RateStepPPM == 0
	case NilRated, Exempt:
		return ratePPM == 0
	default:
		return false
	}
}

// Compute taxes one line supplied from supplier to placeOfSupply.
func Compute(s countryprofile.TaxScheme, supplier, placeOfSupply string, l Line) (Result, error) {
	from, ok := s.Jurisdiction(supplier)
	if !ok {
		return Result{}, ErrUnknownJurisdiction
	}
	if _, ok := s.Jurisdiction(placeOfSupply); !ok {
		return Result{}, ErrUnknownJurisdiction
	}
	if l.Amount < 0 {
		return Result{}, ErrNegativeAmount
	}
	if l.Basis != Inclusive && l.Basis != Exclusive {
		return Result{}, ErrInvalidBasis
	}
	if !ValidRate(l.Nature, l.RatePPM) {
		return Result{}, ErrInvalidRate
	}
	if l.Nature != Taxable {
		return Result{Taxable: l.Amount, Nature: l.Nature}, nil
	}
	if supplier != placeOfSupply {
		t := portion(l, 1)
		return Result{Taxable: taxableOf(l, t), Nature: Taxable, Components: []Component{{"IGST", l.RatePPM, t}}}, nil
	}
	half := portion(l, 2)
	second := "SGST"
	if from.Kind == "union_territory" {
		second = "UTGST"
	}
	return Result{Taxable: taxableOf(l, 2*half), Nature: Taxable, Components: []Component{
		{"CGST", l.RatePPM / 2, half}, {second, l.RatePPM / 2, half},
	}}, nil
}

// portion is round(A*r / (parts*(M+r))) inclusive, or round(V*r / (parts*M))
// exclusive, in big integers so no amount within int64 can overflow.
func portion(l Line, parts int64) int64 {
	n := new(big.Int).Mul(big.NewInt(l.Amount), big.NewInt(l.RatePPM))
	base := int64(MaxRatePPM)
	if l.Basis == Inclusive {
		base += l.RatePPM
	}
	d := new(big.Int).Mul(big.NewInt(parts), big.NewInt(base))
	// Half up for non-negative values: floor((2n + d) / 2d).
	n.Add(n.Lsh(n, 1), d)
	return n.Quo(n, d.Lsh(d, 1)).Int64()
}

func taxableOf(l Line, tax int64) int64 {
	if l.Basis == Inclusive {
		return l.Amount - tax
	}
	return l.Amount
}
