package globalapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostingUpdatePlanMetricsExcludePrivatePlanContent(t *testing.T) {
	private := "SYNTHETIC-PRIVATE-SOURCE-PROOF"
	metrics, ok := postingUpdatePlanMetrics([]byte(`[{"Plan":{"Node Type":"ModifyTable","Total Cost":123456.75,"Plan Rows":1,"Filter":"` + private + `"},"JIT":{"Functions":87,"Options":{"Inlining":true,"Optimization":true,"Expressions":true,"Deforming":true}},"Query Text":"` + private + `"}]`))
	require.True(t, ok)
	require.Equal(t, float64(123456.75), metrics.TotalCost)
	require.Equal(t, int64(87), metrics.JITFunctions)
	require.True(t, metrics.JITExpressions)
	require.NotContains(t, metrics.String(), private)
	for _, raw := range []string{"", `{}`, `[]`, `[{"Plan":{"Total Cost":-1}}]`, `[{"Plan":{"Total Cost":"` + private + `"}}]`} {
		_, ok := postingUpdatePlanMetrics([]byte(raw))
		require.False(t, ok)
	}
}

func TestPostingExplainTemplatePreservesOrderedBindings(t *testing.T) {
	original := "UPDATE workforce_posting_proposals SET status=$1, version=$2 WHERE tenant_id=$3 AND id=$4"
	out, ok := postingUpdateExplainTemplate(original, 4)
	require.True(t, ok)
	require.Equal(t, "EXPLAIN (FORMAT JSON) UPDATE workforce_posting_proposals SET status=?, version=? WHERE tenant_id=? AND id=?", out)
	for _, input := range []string{
		"UPDATE workforce_posting_proposals SET status=$2 WHERE id=$1",
		"UPDATE workforce_posting_proposals SET status=$1 WHERE id=$1",
		"UPDATE workforce_posting_proposals SET status=$1",
		"UPDATE unrelated SET status=$1 WHERE id=$2",
		"SELECT * FROM workforce_posting_proposals WHERE id=$1",
		"UPDATE workforce_posting_proposals SET status=$1 WHERE id=$2; SELECT $3, $4",
	} {
		_, ok := postingUpdateExplainTemplate(input, 4)
		require.False(t, ok)
	}
}
