package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valkey-io/valkey-go"
)

const bucketScript = `
local rate, burst, period, now = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local state = redis.call('HMGET', KEYS[1], 'tokens', 'last')
local tokens, last = tonumber(state[1]) or burst, tonumber(state[2]) or now
now = math.max(now, last)
tokens = math.min(burst, tokens + (now - last) * rate / period)
local allowed, retry = 0, 0
if tokens >= 1 then allowed = 1; tokens = tokens - 1 else retry = math.ceil((1 - tokens) * period / rate) end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'last', now)
redis.call('PEXPIRE', KEYS[1], math.ceil(math.max(period, burst * period / rate)))
return {allowed, math.floor(tokens), retry, math.ceil((burst - tokens) * period / rate)}
`

// Valkey shares capacity budgets between replicas. During an outage it preserves
// local protection, reports degradation, and never changes authorization behavior.
type Valkey struct {
	options     valkey.ClientOption
	fallback    *Memory
	mu          sync.Mutex
	client      valkey.Client
	retryAfter  time.Time
	lastWarning time.Time
	closed      bool
	fallbacks   atomic.Uint64
}

func NewValkey(options valkey.ClientOption) *Valkey {
	options.DisableCache = true
	if options.Dialer.Timeout <= 0 {
		options.Dialer.Timeout = 100 * time.Millisecond
	}
	return &Valkey{options: options, fallback: NewMemory(10000)}
}

func (v *Valkey) FallbackCount() uint64 { return v.fallbacks.Load() }

func (v *Valkey) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	if v.client != nil {
		v.client.Close()
	}
}

func (v *Valkey) connection() valkey.Client {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil
	}
	if v.client != nil {
		return v.client
	}
	if time.Now().Before(v.retryAfter) {
		return nil
	}
	client, err := valkey.NewClient(v.options)
	if err != nil {
		v.retryAfter = time.Now().Add(time.Second)
		return nil
	}
	v.client = client
	return client
}

func (v *Valkey) degraded(key string, rule Rule, now time.Time) Decision {
	v.fallbacks.Add(1)
	v.mu.Lock()
	if time.Since(v.lastWarning) >= 10*time.Second {
		slog.Warn("Valkey rate limiter unavailable; using process-local protection", "component", "ratelimit")
		v.lastWarning = time.Now()
	}
	v.mu.Unlock()
	return v.fallback.Allow(key, rule, now)
}

func (v *Valkey) Allow(key string, rule Rule, now time.Time) Decision {
	client := v.connection()
	if client == nil {
		return v.degraded(key, rule, now)
	}
	// Hash all scope components: neither subjects nor source IPs enter cache keys.
	digest := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d:%d", key, rule.Rate, rule.Burst, rule.Per))
	bucket := fmt.Sprintf("helivanta:ratelimit:v1:%x", digest)
	ctx, cancel := context.WithTimeout(context.Background(), v.options.Dialer.Timeout)
	defer cancel()
	values, err := client.Do(ctx, client.B().Eval().Script(bucketScript).Numkeys(1).Key(bucket).Arg(strconv.Itoa(rule.Rate), strconv.Itoa(rule.Burst), strconv.FormatInt(max(1, rule.Per.Milliseconds()), 10), strconv.FormatInt(now.UnixMilli(), 10)).Build()).AsIntSlice()
	if err != nil || len(values) != 4 {
		return v.degraded(key, rule, now)
	}
	return Decision{Allowed: values[0] == 1, Limit: rule.Rate, Remaining: int(values[1]), RetryAfter: time.Duration(values[2]) * time.Millisecond, Reset: time.Duration(max(int64(0), values[3])) * time.Millisecond}
}
