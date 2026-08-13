package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/nats-io/nats.go"
)

// Broadcast is a fanout subscription: EVERY replica running one
// receives every message, unlike Consumer, where replicas sharing a
// durable name compete and exactly one wins.
//
// Handle returns nothing and gets no transaction. A broadcast is a hint
// — the durable truth is in Postgres — so there is nothing to ack
// meaningfully, nothing to claim for idempotency, and nothing a handler
// could usefully fail at. A dropped broadcast degrades to the
// consumer's own read-through and TTL, never to incorrectness.
type Broadcast struct {
	Subject string
	Handle  func(ctx context.Context, evt Event)
}

// StartBroadcasts subscribes to each broadcast with an ephemeral,
// unnamed JetStream consumer delivering only new messages.
//
// Ephemeral and unnamed is the point: a durable name would be shared
// across replicas (competing delivery) or would have to be made unique
// per replica and then leak a consumer per pod restart. An ephemeral
// consumer vanishes with the connection.
//
// DeliverNew, not DeliverAll: a replica starting up has an empty cache
// and reads through to Postgres for everything, so replaying historical
// invalidations would be pure noise.
func (b *Bus) StartBroadcasts(ctx context.Context, bs []Broadcast) error {
	for _, bc := range bs {
		sub, err := b.js.Subscribe(b.Subject(bc.Subject), func(msg *nats.Msg) {
			b.handleBroadcastMsg(ctx, bc, msg)
		}, nats.DeliverNew())
		if err != nil {
			return fmt.Errorf("broadcast subscribe %s: %w", bc.Subject, err)
		}
		go func() {
			<-ctx.Done()
			_ = sub.Unsubscribe()
		}()
	}
	return nil
}

// handleBroadcastMsg decodes and dispatches one message, containing a
// panic from either json.Unmarshal or the handler itself — this runs on
// the NATS client library's own callback goroutine, so an unrecovered
// panic here would take the whole subscription down with it, exactly as
// handleMsg guards the consumer path.
func (b *Bus) handleBroadcastMsg(ctx context.Context, bc Broadcast, msg *nats.Msg) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "broadcast handler panicked", "panic", fmt.Sprint(rec),
				"subject", bc.Subject, "stack", string(debug.Stack()))
		}
	}()
	var evt Event
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		slog.ErrorContext(ctx, "broadcast: undecodable message", "err", err, "subject", bc.Subject)
		return
	}
	bc.Handle(ctx, evt)
}
