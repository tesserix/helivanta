package testinfra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestParseStartupTimeoutDefaultsWhenUnset(t *testing.T) {
	d, err := parseStartupTimeout("")
	require.NoError(t, err)
	require.Equal(t, defaultStartupTimeout, d)
}

func TestParseStartupTimeoutHonoursOverride(t *testing.T) {
	d, err := parseStartupTimeout("90s")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, d)
}

// A malformed or non-positive value must be an error rather than a quiet
// fall back to the default: someone who sets this env var is already
// chasing a timeout, and silently ignoring their value would send them
// looking in the wrong place.
func TestParseStartupTimeoutRejectsBadValues(t *testing.T) {
	for _, raw := range []string{"abc", "90", "0", "-30s"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseStartupTimeout(raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), raw)
		})
	}
}

// Guards the trap documented on startupTimeout: raising only the outer
// ForAll deadline leaves each child strategy on wait's own 60-second
// default, so the container still gives up after a minute regardless.
// That was the exact mistake behind the flaky coverage gate (#763), and
// it is invisible at the call site — the code reads as though the longer
// timeout applies.
//
// Only the inner timeout is asserted: MultiStrategy keeps its deadline in
// an unexported field and its Timeout() accessor reports a different one,
// so the outer deadline cannot be read back from here. The inner value is
// the one that silently defaults, which makes it the one worth pinning.
func TestWaitStrategyCarriesInnerStrategyTimeout(t *testing.T) {
	const timeout = 4 * time.Minute

	req := testcontainers.GenericContainerRequest{}
	opt := testcontainers.WithWaitStrategyAndDeadline(timeout,
		wait.ForListeningPort("4222/tcp").WithStartupTimeout(timeout))
	require.NoError(t, opt.Customize(&req))

	outer, ok := req.WaitingFor.(*wait.MultiStrategy)
	require.True(t, ok, "expected the customizer to wrap strategies in a MultiStrategy")
	require.Len(t, outer.Strategies, 1)

	inner, ok := outer.Strategies[0].(*wait.HostPortStrategy)
	require.True(t, ok)
	require.NotNil(t, inner.Timeout(), "inner strategy left on wait's 60s default")
	require.Equal(t, timeout, *inner.Timeout(), "inner strategy timeout not applied")

	// The hazard itself: without the explicit call the strategy reports no
	// timeout and wait falls back to its 60-second default. If a future
	// version of testcontainers propagates the outer deadline to children,
	// this assertion breaks and the comment above can be simplified.
	bare := wait.ForListeningPort("4222/tcp")
	require.Nil(t, bare.Timeout(), "a bare strategy is expected to carry no timeout of its own")
}
