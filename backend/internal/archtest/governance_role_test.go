package archtest

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceRoutingRoleCannotConferOperatingAuthority(t *testing.T) {
	allowed := map[authz.Permission]bool{
		"iam.governance.read": true, "iam.governance.review": true,
		"workforce.login_links.review": true, "workforce.independent_login.review": true,
		"workforce.identity_checks.manage": true, "workforce.identity_checks.review": true,
		"workforce.employment.prepare": true, "workforce.employment.review": true, "workforce.employment.activate": true,
	}
	found := 0
	for _, module := range allModules() {
		for _, grant := range module.Permissions() {
			if !slices.Contains(grant.Roles, authz.RoleGovernanceDelegate) {
				continue
			}
			found++
			require.True(t, allowed[grant.Permission], "%s gives governance routing role undeclared business authority %s", module.Name(), grant.Permission)
		}
	}
	require.Positive(t, found, "the check must inspect actual governance role declarations")
}
