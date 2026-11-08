package pharmacy

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
)

func TestOrderProductsRequireAConfiguredGuardedOwner(t *testing.T) {
	called := false
	err := (orderProductsGuard{}).WithCurrentOrderProducts(context.Background(), suppliertrade.Endpoint{}, []supplierorder.SupplierOrderLine{}, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, supplierorder.ErrUnavailable)
	require.False(t, called)
}
