package iam

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceOperatingSourcesRequireAnIndependentEmployerAndTheirOwnTemplate(t *testing.T) {
	for _, capability := range []authz.GovernanceCapability{"iam.governance.operating.manage", "iam.governance.operating.review"} {
		for _, kind := range []string{"pharmacy", "supplier"} {
			require.Equal(t, "iam.operating_access_reviewer.v1", governanceSourceTemplate(capability, kind))
		}
		for _, kind := range []string{"", "tenant", "hospital", "department", "outlet", "premise"} {
			require.Empty(t, governanceSourceTemplate(capability, kind))
		}
	}
	require.Empty(t, governanceSourceTemplate("iam.operating.review", "pharmacy"))
	require.Empty(t, governanceSourceTemplate("iam.governance.operating.*", "supplier"))
}

func TestGovernanceOperatingTemplateCannotGrantStockOrBroadenItsEmployer(t *testing.T) {
	var definition *GovernanceTemplateDefinition
	for index := range governanceTemplates {
		if governanceTemplates[index].Key == "iam.operating_access_reviewer.v1" {
			definition = &governanceTemplates[index]
		}
	}
	require.NotNil(t, definition)
	require.ElementsMatch(t, []string{"pharmacy", "supplier"}, definition.ScopeKinds)
	require.ElementsMatch(t, []string{"iam.governance.operating.manage", "iam.governance.operating.review"}, definition.Permissions)
	id := uuid.Must(uuid.NewV7())
	start := time.Now().UTC().Truncate(time.Microsecond)
	input := GovernanceGrantInput{Subject: "synthetic-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: definition.Key, ScopeID: &id, Reason: "Independent employer duty administration", ValidFrom: start, ExpiresAt: start.Add(time.Hour)}
	for _, kind := range []string{"pharmacy", "supplier"} {
		input.ScopeKind = kind
		require.NoError(t, input.Validate())
	}
	for _, kind := range []string{"hospital", "department", "outlet", "premise", "tenant"} {
		input.ScopeKind = kind
		require.Error(t, input.Validate())
	}
	input.ScopeKind = "pharmacy"
	input.ScopeID = nil
	require.Error(t, input.Validate())
}
