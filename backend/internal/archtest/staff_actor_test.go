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
			"internal/modules/organization/posting_choices.go":       true,
			"internal/modules/workforce/posting_authority.go":        true,
			"internal/modules/workforce/staff_actor_guard_test.go":   true,
			"internal/bootstrap/staff_actor_test.go":                 true,
			"internal/globalapi/workforce_person_review_test.go":     true,
			"internal/globalapi/workforce_posting_authority_test.go": true,
		},
		message: "ADR-0052 permits only the reviewed posting and department-choice authorization consumers, not general cross-owner business calls.",
	})
}

func TestStaffActorProviderIsResolvedOnlyAtWiringBoundaries(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{
		method: "StaffActors",
		allowlist: map[string]bool{
			"internal/platform/staff_actors.go":                      true,
			"internal/modules/organization/module.go":                true,
			"internal/modules/workforce/module.go":                   true,
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
