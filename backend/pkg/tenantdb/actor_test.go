package tenantdb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestActorRoundTripsThroughContext(t *testing.T) {
	require.Empty(t, tenantdb.ActorFrom(context.Background()))
	require.Equal(t, "user-1", tenantdb.ActorFrom(tenantdb.WithActor(context.Background(), "user-1")))
}
