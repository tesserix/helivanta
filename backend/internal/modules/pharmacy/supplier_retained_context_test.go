package pharmacy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

type supplierSafetyDepth struct{}

type retainedContextSafety struct {
	remaining []f.Shipment
	refuse    int
}

func (s *retainedContextSafety) WithSafeShipment(ctx context.Context, shipment f.Shipment, next func(context.Context) error) error {
	depth, _ := ctx.Value(supplierSafetyDepth{}).(int)
	if depth >= len(s.remaining) || s.remaining[depth].ID != shipment.ID {
		return errors.New("source guard continuation lost its retained context")
	}
	if depth == s.refuse {
		return f.ErrConflict
	}
	return next(context.WithValue(ctx, supplierSafetyDepth{}, depth+1))
}

func TestSupplierRetainedSafetyPropagatesEverySourceFrameAndLateDenial(t *testing.T) {
	shipments := make([]f.Shipment, 8)
	for i := range shipments {
		shipments[i].ID = uuid.Must(uuid.NewV7())
	}
	for _, refuse := range []int{-1, 0, 7} {
		safety := &retainedContextSafety{remaining: shipments, refuse: refuse}
		called := 0
		err := withSupplierShipmentSafety(t.Context(), safety, shipments, func() error { called++; return nil })
		if refuse == -1 {
			require.NoError(t, err)
			require.Equal(t, 1, called)
		} else {
			require.ErrorIs(t, err, f.ErrConflict)
			require.Zero(t, called)
		}
	}
}
