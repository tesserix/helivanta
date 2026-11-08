package supply

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"testing"
)

func TestFulfilmentPublicationCannotMintAuthorityWithoutAnOwnerCommand(t *testing.T) {
	scope := f.SourceScope{TenantID: uuid.New(), OrderID: uuid.Must(uuid.NewV7()), OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), Product: "supply"}
	guard := New().FulfilmentOrders(platform.Deps{})
	_, err := guard.VerifyOrder(tenantdb.WithActor(t.Context(), "TEST-frozen-source-actor"), scope, f.NewReservation)
	require.ErrorIs(t, err, f.ErrUnavailable)
	scope.Product = "hospital"
	_, err = guard.VerifyOrder(t.Context(), scope, f.NewReservation)
	require.ErrorIs(t, err, f.ErrInvalid)
	called := false
	p := &fulfilmentOrderProof{owner: &fulfilmentOrderOwner{}}
	err = p.WithCurrent(t.Context(), func(context.Context, supplierorder.SupplierOrder) error { called = true; return nil })
	require.ErrorIs(t, err, f.ErrUnavailable)
	require.False(t, called)
}
