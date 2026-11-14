package workforce

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPurchaseOrderActorCannotManufactureEligibilityOrBorrowGovernancePurpose(t *testing.T) {
	guard := (&Module{}).PurchaseOrderActors(nil)
	_, governance := guard.(authz.CurrentStaffActorGuard)
	require.False(t, governance)
	called := false
	err := guard.WithCurrentPurchaseOrderActor(context.Background(), authz.StaffActorScope{}, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
}
