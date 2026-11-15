package workforce

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperatingSelectionPlanRetainsTheEntireAuthorizedPredicateAndIntegrityCoverage(t *testing.T) {
	const start = "CREATE FUNCTION workforce_operating_selection_link(t uuid,e uuid,s uuid,state text,generation uuid,basis bigint,revision bigint)RETURNS boolean LANGUAGE sql STABLE AS $$"
	_, original, ok := strings.Cut(operatingStaffChoicesSchema, start)
	require.True(t, ok)
	predicate, _, ok := strings.Cut(original, "$$;")
	require.True(t, ok)
	_, replacement, ok := strings.Cut(operatingSelectionPlansSchema, "AS $link$")
	require.True(t, ok)
	body, _, ok := strings.Cut(replacement, "$link$;")
	require.True(t, ok)
	require.Equal(t, "\nBEGIN\n IF NULLIF(current_setting('app.workforce_operating_selection',true),'')IS NULL THEN\n  RETURN false;\n END IF;\n RETURN ("+predicate+");\nEND\n", body)
	baseline := operatingStaffChoicesSchema[strings.Index(operatingStaffChoicesSchema, "DO $integrity$"):]
	baseline, _, ok = strings.Cut(baseline, "END $integrity$;")
	require.True(t, ok)
	baseline = strings.Replace(baseline+"END $integrity$;", "CREATE FUNCTION workforce_operating_selection_integrity()", "CREATE OR REPLACE FUNCTION workforce_operating_selection_integrity()", 1)
	require.Contains(t, operatingSelectionPlansSchema, baseline)
	precheck := strings.Index(operatingSelectionPlansSchema, "IF workforce_operating_selection_integrity()IS DISTINCT FROM true THEN")
	require.GreaterOrEqual(t, precheck, 0, "missing, false or NULL prior integrity must abort before replacing a guard")
	require.Less(t, precheck, strings.Index(operatingSelectionPlansSchema, "CREATE OR REPLACE FUNCTION workforce_operating_selection_link"))
	var predecessor, replacementIndex = -1, -1
	for index, migration := range New().Migrations() {
		if migration.ID == "0116_workforce_operating_staff_choices" {
			predecessor = index
		}
		if migration.ID == "0118_workforce_operating_selection_plans" {
			replacementIndex = index
		}
	}
	require.GreaterOrEqual(t, predecessor, 0)
	require.Greater(t, replacementIndex, predecessor)
}
