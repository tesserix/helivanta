package archtest

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestStaffActorAuthorizationHasOnlyReviewedConsumers(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{
		method: "WithCurrentStaffActor",
		allowlist: map[string]bool{
			"internal/modules/organization/posting_choices.go":                true,
			"internal/modules/workforce/posting_authority.go":                 true,
			"internal/modules/workforce/partner_member_guard.go":              true, // Reviewed membership source admission; no operating authority.
			"internal/modules/workforce/posting_source_guard.go":              true,
			"internal/modules/workforce/staff_actor_scopes.go":                true,
			"internal/modules/workforce/leave_department_review.go":           true,
			"internal/modules/pharmacy/staff_department_binding_authority.go": true,
			"internal/modules/workforce/staff_actor_guard_test.go":            true,
			"internal/bootstrap/staff_actor_test.go":                          true,
			"internal/globalapi/workforce_person_review_test.go":              true,
			"internal/globalapi/workforce_posting_authority_test.go":          true,
		},
		message: "ADRs0052/0060 permit only reviewed posting/source-use and department-choice authorization consumers, not general product business calls.",
	})
}

func TestStaffActorProviderIsResolvedOnlyAtWiringBoundaries(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{
		method: "StaffActors",
		allowlist: map[string]bool{
			"internal/platform/staff_actors.go":                      true,
			"internal/platform/staff_posting_sources.go":             true,
			"internal/modules/organization/module.go":                true,
			"internal/modules/workforce/module.go":                   true,
			"internal/modules/pharmacy/module.go":                    true,
			"cmd/api/main.go":                                        true,
			"internal/testutil/harness.go":                           true,
			"internal/modules/workforce/staff_actor_guard_test.go":   true,
			"internal/bootstrap/staff_actor_test.go":                 true,
			"internal/globalapi/workforce_person_review_test.go":     true,
			"internal/globalapi/workforce_posting_authority_test.go": true,
		},
		message: "Resolve the Workforce-owned source provider at application wiring; product modules may consume only the injected authorization interface.",
	})
}

func TestStaffActorAuthorizationInterfaceStaysNarrow(t *testing.T) {
	guard := reflect.TypeFor[authz.CurrentStaffActorGuard]()
	require.Equal(t, 1, guard.NumMethod(), "business methods require a separately reviewed integration contract")
	method, ok := guard.MethodByName("WithCurrentStaffActor")
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[func(context.Context, authz.StaffActorScope, func(context.Context, authz.StaffActorReference) error) error](), method.Type,
		"the provider must not expose repositories, transactions or callable business APIs")
}

func TestPostingSourceAuthorizationInterfaceStaysNarrow(t *testing.T) {
	guard := reflect.TypeFor[authz.CurrentStaffPostingSourceGuard]()
	require.Equal(t, 1, guard.NumMethod(), "source consumers must not receive repositories or business services")
	method, ok := guard.MethodByName("WithCurrentStaffPosting")
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[func(context.Context, authz.StaffPostingSourceScope, func(context.Context, authz.StaffPostingSourceReference) error) error](), method.Type)
}
