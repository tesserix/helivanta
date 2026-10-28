package drugmaster_test

import (
	"crypto/md5"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/drugmaster" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #476's target: typeahead p95 under 120 ms across at least 60,000
// products (spec D8). This measures the handler against a 60,000-product
// synthetic catalogue on the test database; it does not claim the
// 100-requests-per-second production figure.
func TestSearchStaysUnder120msAcross60000Products(t *testing.T) {
	// Run alone (CI's "Drug master search latency" step): beside every
	// other package under -race, the clock measures the machine.
	if os.Getenv("HELIVANTA_LATENCY_CHECK") != "1" {
		t.Skip("set HELIVANTA_LATENCY_CHECK=1 to measure search latency in isolation")
	}
	s := newStack(t)
	// Through the real staging and decision path: a licensed catalogue is
	// this size, so staging and verification must cope with it too.
	f := drugmaster.ReleaseFile{Country: "IN", Source: drugmaster.ReleaseSource{Name: "synthetic-test-fixture", LicenceRef: "TEST-LICENCE", Version: "latency"}}
	for i := range 2000 {
		f.Molecules = append(f.Molecules, drugmaster.MoleculeIn{INNName: fmt.Sprintf("Molecule %x", md5.Sum([]byte(fmt.Sprint(i))))})
	}
	for i := range 6000 {
		f.Formulations = append(f.Formulations, drugmaster.FormulationIn{Code: fmt.Sprintf("TEST-F-%d", i), ValidFrom: "2020-01-01",
			Label: fmt.Sprintf("Formulation %d", i), DoseForm: "tablet", Route: "oral", RegulatoryClass: "H", NDPS: ptr(false), TeleconsultPrescribable: ptr(true),
			Ingredients: []drugmaster.IngredientIn{{Molecule: f.Molecules[i%2000].INNName, StrengthValue: "500", StrengthUnit: "mg"}}})
	}
	for i := range 60000 {
		f.Products = append(f.Products, drugmaster.ProductIn{Code: fmt.Sprintf("TEST-P-%d", i), Formulation: fmt.Sprintf("TEST-F-%d", i%6000),
			ValidFrom: "2020-01-01", BrandName: fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprint("brand", i)))), Manufacturer: "TEST Pharma",
			PackSize: 10, UnitOfIssue: "tablet", HSNCode: "30049099", Status: "active"})
	}
	file := raw(t, f)
	start := time.Now()
	release, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)
	staged := time.Since(start)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, release, file, "Ravi Approver", true, "latency fixture"))
	t.Logf("staged 60,000 products in %s, verified and published in %s", staged, time.Since(start)-staged)

	rng := rand.New(rand.NewPCG(476, 476))
	const hex = "0123456789abcdef"
	var took []time.Duration
	for i := range 200 {
		q := string([]byte{hex[rng.IntN(16)], hex[rng.IntN(16)], hex[rng.IntN(16)]})
		if i%4 == 0 {
			q = "molecule" + q[:2]
		}
		start := time.Now()
		res := testutil.Do(s.r, http.MethodGet, "/v1/drugmaster/search?q="+q, "doctor", "")
		took = append(took, time.Since(start))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	slices.Sort(took)
	p95 := took[len(took)*95/100]
	t.Logf("search p95 over 200 queries, 60,000 products: %s (max %s)", p95, took[len(took)-1])
	require.Less(t, p95, 120*time.Millisecond)
}
