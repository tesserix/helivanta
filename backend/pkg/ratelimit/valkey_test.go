package ratelimit

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/valkey-io/valkey-go"
)

func TestValkeyLimiterSharesAtomicBudgetAcrossReplicas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := testcontainers.Run(ctx, "valkey/valkey:9.1.1-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	option := valkey.ClientOption{InitAddress: []string{endpoint}, DisableCache: true, Dialer: net.Dialer{Timeout: time.Second}}
	admin, err := valkey.NewClient(option)
	require.NoError(t, err)
	defer admin.Close()
	require.NoError(t, admin.Do(ctx, admin.B().Arbitrary("ACL", "SETUSER", "helivanta", "on", "nopass", "~helivanta:*", "+@all", "-@dangerous").Build()).Error())
	option.Username = "helivanta"
	option.Password = "nopass"

	first, second := NewValkey(option), NewValkey(option)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	now := time.Now().UTC()
	rule := Rule{Rate: 60, Burst: 2, Per: time.Minute}
	require.True(t, first.Allow("same-principal", rule, now).Allowed)
	require.True(t, second.Allow("same-principal", rule, now).Allowed)
	require.False(t, first.Allow("same-principal", rule, now).Allowed)
	require.True(t, second.Allow("same-principal", rule, now.Add(time.Second)).Allowed)
	require.False(t, first.Allow("same-principal", rule, now.Add(time.Second)).Allowed)
	require.True(t, first.Allow("another-tenant", rule, now).Allowed)
	var allowed atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			limiter := first
			if i%2 == 0 {
				limiter = second
			}
			if limiter.Allow("concurrent", Rule{Rate: 60, Burst: 5, Per: time.Minute}, now).Allowed {
				allowed.Add(1)
			}
		})
	}
	group.Wait()
	require.Equal(t, int32(5), allowed.Load())
	require.Zero(t, first.FallbackCount())
	require.Zero(t, second.FallbackCount())
	option.Username = ""
	option.Password = ""
	client, err := valkey.NewClient(option)
	require.NoError(t, err)
	defer client.Close()
	keys, err := client.Do(ctx, client.B().Keys().Pattern("helivanta:ratelimit:*").Build()).AsStrSlice()
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	for _, key := range keys {
		require.NotContains(t, key, "principal")
		ttl, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		require.Positive(t, ttl)
	}
}

func TestValkeyOutageUsesBoundedLocalFallback(t *testing.T) {
	limiter := NewValkey(valkey.ClientOption{InitAddress: []string{"127.0.0.1:1"}, DisableCache: true, Dialer: net.Dialer{Timeout: 50 * time.Millisecond}})
	defer limiter.Close()
	now := time.Now()
	rule := Rule{Rate: 10, Burst: 1, Per: time.Minute}
	require.True(t, limiter.Allow("fallback", rule, now).Allowed)
	require.False(t, limiter.Allow("fallback", rule, now).Allowed)
	require.Equal(t, uint64(2), limiter.FallbackCount())
}
