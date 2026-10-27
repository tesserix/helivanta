package drugmaster_test

import (
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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
	release := uuid.Must(uuid.NewV7())
	require.NoError(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		// Postgres 16 has no uuidv7(); the ids only need the v7 version nibble.
		for _, q := range []string{
			`INSERT INTO drugmaster_releases (id,country,source_name,licence_ref,source_version,file_sha256,staged_by) VALUES (?,'IN','synthetic-test-fixture','TEST-LICENCE','latency',md5('latency')||md5('latency'),'Asha Stager')`,
			`INSERT INTO drugmaster_molecules (id,release_id,inn_name) SELECT v7(),?,'Molecule '||md5(i::text) FROM generate_series(1,2000) i`,
			`INSERT INTO drugmaster_formulations (id,release_id,code) SELECT v7(),?,'TEST-F-'||i FROM generate_series(1,6000) i`,
			`INSERT INTO drugmaster_formulation_versions (id,formulation_id,release_id,valid_from,label,dose_form,route,regulatory_class,ndps,teleconsult_prescribable) SELECT v7(),f.id,?,'2020-01-01','Formulation '||f.code,'tablet','oral','H',false,true FROM drugmaster_formulations f`,
			`INSERT INTO drugmaster_formulation_ingredients (version_id,position,molecule_id,strength_value,strength_unit) SELECT v.id,1,m.id,500,'mg' FROM (SELECT id, row_number() OVER (ORDER BY id) n FROM drugmaster_formulation_versions) v JOIN (SELECT id, row_number() OVER (ORDER BY id) n FROM drugmaster_molecules) m ON m.n=(v.n%2000)+1`,
			`INSERT INTO drugmaster_products (id,release_id,code,formulation_id) SELECT v7(),?,'TEST-P-'||i,(SELECT id FROM drugmaster_formulations WHERE code='TEST-F-'||((i%6000)+1)) FROM generate_series(1,60000) i`,
			`INSERT INTO drugmaster_product_versions (id,product_id,release_id,valid_from,brand_name,manufacturer,pack_size,unit_of_issue,hsn_code,status) SELECT v7(),p.id,?,'2020-01-01',md5(p.code),'TEST Pharma',10,'tablet','30049099','active' FROM drugmaster_products p`,
		} {
			q = strings.ReplaceAll(q, "v7()", "overlay(gen_random_uuid()::text placing '7' from 15 for 1)::uuid")
			args := make([]any, strings.Count(q, "?"))
			for i := range args {
				args[i] = release
			}
			if err := tx.Exec(q, args...).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, release, "Ravi Approver", true, "latency fixture"))

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
