package archtest

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPurchaseOrderActorHasOnlyReviewedProcurementConsumers(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{method: "WithCurrentPurchaseOrderActor", allowlist: map[string]bool{"internal/modules/supply/order_authority.go": true, "internal/modules/workforce/purchase_order_actor_guard_test.go": true}, message: "The procurement actor purpose is separate from governance Staff actor admission and cannot confer order, stock, clinical or financial authority."})
}
func TestPurchaseOrderActorInterfaceStaysNarrow(t *testing.T) {
	guard := reflect.TypeFor[authz.CurrentPurchaseOrderActorGuard]()
	require.Equal(t, 1, guard.NumMethod())
	method, ok := guard.MethodByName("WithCurrentPurchaseOrderActor")
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[func(context.Context, authz.StaffActorScope, func(context.Context, authz.StaffActorReference) error) error](), method.Type)
}
