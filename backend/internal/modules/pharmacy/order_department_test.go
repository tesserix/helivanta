package pharmacy

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
)

func TestOrderDepartmentCheckNeedsConfiguredGuardedOwner(t *testing.T) {
	g := tradingEndpointGuard{}
	called := false
	err := g.WithHospitalDepartment(context.Background(), suppliertrade.Endpoint{}, uuid.Must(uuid.NewV7()), func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, suppliertrade.ErrUnavailable)
	require.False(t, called)
}
