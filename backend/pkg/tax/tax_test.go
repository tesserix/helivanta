package tax_test

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/tax"
)

func india(t *testing.T) countryprofile.TaxScheme {
	t.Helper()
	s, ok := countryprofile.Tax("IN")
	require.True(t, ok)
	return s
}

type golden struct {
	Name       string  `json:"name"`
	Supplier   string  `json:"supplier"`
	POS        string  `json:"pos"`
	Amount     int64   `json:"amount"`
	Basis      string  `json:"basis"`
	RatePPM    int64   `json:"rate_ppm"`
	Nature     string  `json:"nature"`
	Taxable    int64   `json:"taxable"`
	Components [][]any `json:"components"`
}

func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/gst.json")
	require.NoError(t, err)
	var doc struct{ Cases []golden }
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Cases)
	s := india(t)
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			r, err := tax.Compute(s, c.Supplier, c.POS, tax.Line{Amount: c.Amount, Basis: tax.Basis(c.Basis), RatePPM: c.RatePPM, Nature: tax.Nature(c.Nature)})
			require.NoError(t, err)
			require.Equal(t, c.Taxable, r.Taxable)
			require.Equal(t, tax.Nature(c.Nature), r.Nature)
			want := []tax.Component{}
			for _, comp := range c.Components {
				want = append(want, tax.Component{Name: comp[0].(string), RatePPM: int64(comp[1].(float64)), Amount: int64(comp[2].(float64))})
			}
			got := r.Components
			if got == nil {
				got = []tax.Component{}
			}
			require.Equal(t, want, got)
		})
	}
}

// An inclusive amount is split exactly: no paisa is created or lost, and
// the two intra-jurisdiction halves are always equal.
func TestInclusiveConservesTheAmount(t *testing.T) {
	s := india(t)
	rng := rand.New(rand.NewSource(1283)) //nolint:gosec // deterministic test data
	for range 10000 {
		amount := rng.Int63n(1_000_000_000_000) + 1
		rate := (rng.Int63n(10000) + 1) * 100
		pos := "27"
		if rng.Intn(2) == 0 {
			pos = "29"
		}
		r, err := tax.Compute(s, "27", pos, tax.Line{Amount: amount, Basis: tax.Inclusive, RatePPM: rate, Nature: tax.Taxable})
		require.NoError(t, err)
		sum := r.Taxable
		for _, c := range r.Components {
			sum += c.Amount
		}
		require.Equal(t, amount, sum, "amount %d rate %d", amount, rate)
		if pos == "27" {
			require.Len(t, r.Components, 2)
			require.Equal(t, r.Components[0].Amount, r.Components[1].Amount)
		}
	}
}

func TestComputeRejects(t *testing.T) {
	s := india(t)
	for name, tc := range map[string]struct {
		supplier, pos string
		line          tax.Line
		err           error
	}{
		"rate not in 0.01% steps": {"27", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 50050, Nature: tax.Taxable}, tax.ErrInvalidRate},
		"rate above 100%":         {"27", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 1000100, Nature: tax.Taxable}, tax.ErrInvalidRate},
		"taxable at zero":         {"27", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 0, Nature: tax.Taxable}, tax.ErrInvalidRate},
		"nil-rated with a rate":   {"27", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 50000, Nature: tax.NilRated}, tax.ErrInvalidRate},
		"unknown nature":          {"27", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 0, Nature: "zero_rated"}, tax.ErrInvalidRate},
		"unknown basis":           {"27", "27", tax.Line{Amount: 100, Basis: "gross", RatePPM: 50000, Nature: tax.Taxable}, tax.ErrInvalidBasis},
		"negative amount":         {"27", "27", tax.Line{Amount: -1, Basis: tax.Inclusive, RatePPM: 50000, Nature: tax.Taxable}, tax.ErrNegativeAmount},
		"unknown supplier":        {"25", "27", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 50000, Nature: tax.Taxable}, tax.ErrUnknownJurisdiction},
		"unknown place of supply": {"27", "99", tax.Line{Amount: 100, Basis: tax.Inclusive, RatePPM: 50000, Nature: tax.Taxable}, tax.ErrUnknownJurisdiction},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tax.Compute(s, tc.supplier, tc.pos, tc.line)
			require.ErrorIs(t, err, tc.err)
		})
	}
}
