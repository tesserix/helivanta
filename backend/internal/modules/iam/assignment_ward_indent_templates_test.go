package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHospitalWardIndentTemplatesNeverIssueOrDispense(t *testing.T) {
	expected := map[string][]string{
		"pharmacy.department_ward_indent_reader.v1":    {"pharmacy.indent.read", "workforce.ward_indent_actor.use"},
		"pharmacy.department_ward_indent_requester.v1": {"pharmacy.indent.read", "pharmacy.indent.raise", "workforce.ward_indent_actor.use"},
	}
	found := 0
	for _, template := range staffAccessTemplates() {
		if !strings.HasPrefix(template.Key, "pharmacy.department_ward_indent_") {
			require.NotContains(t, template.Permissions, "workforce.ward_indent_actor.use", "%s borrows the ward indent actor purpose", template.Key)
			continue
		}
		permissions, ok := expected[template.Key]
		require.True(t, ok, "only the two reviewed ward indent roles are declared")
		require.Equal(t, "pharmacy_ward_indent", template.Family)
		require.Equal(t, permissions, template.Permissions)
		require.Equal(t, []string{"department"}, template.ScopeKinds)
		require.Equal(t, []string{"hospital"}, template.EstablishmentKinds)
		found++
	}
	require.Equal(t, len(expected), found)
}
