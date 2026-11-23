package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHospitalStocktakeTemplatesKeepCountingAndReviewSeparate(t *testing.T) {
	expected := map[string][]string{
		"pharmacy.department_stocktake_reader.v1":   {"pharmacy.stocktake.read", "workforce.stocktake_actor.use"},
		"pharmacy.department_stocktake_counter.v1":  {"pharmacy.stocktake.read", "pharmacy.stocktake.count", "workforce.stocktake_actor.use"},
		"pharmacy.department_stocktake_reviewer.v1": {"pharmacy.stocktake.read", "pharmacy.stocktake.review", "workforce.stocktake_actor.use"},
	}
	found := 0
	for _, template := range staffAccessTemplates() {
		if !strings.HasPrefix(template.Key, "pharmacy.department_stocktake_") {
			continue
		}
		permissions, ok := expected[template.Key]
		require.True(t, ok, "only the three reviewed hospital stocktake roles are declared")
		require.Equal(t, "pharmacy_stocktake", template.Family)
		require.Equal(t, permissions, template.Permissions)
		require.Equal(t, []string{"department"}, template.ScopeKinds)
		require.Equal(t, []string{"hospital"}, template.EstablishmentKinds)
		found++
	}
	require.Equal(t, len(expected), found)
}
