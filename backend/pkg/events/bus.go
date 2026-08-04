package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/tenantdb"
)

const StreamName = "HMS"

// maxDeliver is the redelivery ceiling for durable consumers. Once a
// message has been delivered this many times without a successful Ack,
// handleMsg dead-letters it instead of Nak'ing it forever.
const maxDeliver = 5

// ackWait is the JetStream AckWait for durable pull consumers — how long
// the server waits for an Ack/Nak/Term before redelivering. It is a var
// (not const) so white-box tests in this package can shrink it to make
// the maxDeliver-exhaustion/DLQ path fast to exercise.
var ackWait = 30 * time.Second

// OutboxStore is the slice of tenantdb the bus needs (system tables only).
type OutboxStore interface {
	WithSystem(ctx context.Context, fn func(tx *gorm.DB) error) error
}

func Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_events_outbox",
		SQL: `
			CREATE TABLE outbox_events (
			  id uuid PRIMARY KEY,
			  subject text NOT NULL,
			  payload jsonb NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  published_at timestamptz
			);
			CREATE INDEX outbox_unpublished ON outbox_events (created_at) WHERE published_at IS NULL;
			CREATE TABLE processed_events (
			  consumer text NOT NULL,
			  event_id uuid NOT NULL,
			  processed_at timestamptz NOT NULL DEFAULT now(),
			  PRIMARY KEY (consumer, event_id)
			);`,
	}}
}

type Bus struct {
	nc *nats.Conn
	js nats.JetStreamContext

	// stopCtx/stop give Close() a way to unwind consumeLoop/RunDispatcher
	// goroutines even when the caller's ctx is long-lived (e.g. request
	// scoped or background.TODO()).
	stopCtx context.Context
	stop    context.CancelFunc
}

func NewBus(natsURL string) (*Bus, error) {
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{"hms.>"},
		Retention: nats.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	stopCtx, stop := context.WithCancel(context.Background())
	return &Bus{nc: nc, js: js, stopCtx: stopCtx, stop: stop}, nil
}

func (b *Bus) Ping(ctx context.Context) error {
	if !b.nc.IsConnected() {
		return errors.New("nats disconnected")
	}
	return nil
}

// Close stops all consumeLoop/RunDispatcher goroutines derived from this
// Bus (even if their caller ctx is still live) and drains the connection.
func (b *Bus) Close() {
	b.stop()
	b.nc.Drain()
}

// deriveCtx returns a ctx that is Done when either the caller's ctx ends
// or the Bus is Close()'d — whichever comes first.
func (b *Bus) deriveCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	derived, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-b.stopCtx.Done():
			cancel()
		case <-derived.Done():
		}
	}()
	return derived, cancel
}

type outboxRow struct {
	ID          uuid.UUID
	Subject     string
	Payload     []byte
	PublishedAt *time.Time
}

func (outboxRow) TableName() string { return "outbox_events" }

// Publish records the event in the outbox inside the caller's tx.
// Delivery happens asynchronously via RunDispatcher — at-least-once,
// never lost with the business write (issue #2).
func (b *Bus) Publish(tx *gorm.DB, subject string, evt Event) error {
	var id uuid.UUID
	if evt.ID == "" {
		id = uuid.New()
		evt.ID = id.String()
	} else {
		parsed, err := uuid.Parse(evt.ID)
		if err != nil {
			return fmt.Errorf("events: event id must be a uuid: %w", err)
		}
		id = parsed
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return tx.Create(&outboxRow{ID: id, Subject: subject, Payload: payload}).Error
}

// RunDispatcher drains the outbox into JetStream until ctx ends or the
// Bus is Close()'d.
func (b *Bus) RunDispatcher(ctx context.Context, db OutboxStore) {
	loopCtx, cancel := b.deriveCtx(ctx)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-loopCtx.Done():
			return
		case <-ticker.C:
			if err := b.drainOnce(loopCtx, db); err != nil {
				slog.Error("outbox dispatch", "err", err)
			}
		}
	}
}

func (b *Bus) drainOnce(ctx context.Context, db OutboxStore) error {
	return db.WithSystem(ctx, func(tx *gorm.DB) error {
		var rows []outboxRow
		if err := tx.Raw(`SELECT id, subject, payload FROM outbox_events
			WHERE published_at IS NULL ORDER BY created_at LIMIT 100
			FOR UPDATE SKIP LOCKED`).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			// MsgId gives JetStream server-side dedup on redelivery.
			if _, err := b.js.Publish(r.Subject, r.Payload, nats.MsgId(r.ID.String())); err != nil {
				// A single row's publish failure must not block the rest
				// of the batch (head-of-line blocking) — log and retry
				// this row on the next drain tick instead of aborting.
				slog.Error("outbox publish", "id", r.ID, "subject", r.Subject, "err", err)
				continue
			}
			if err := tx.Exec(`UPDATE outbox_events SET published_at = now() WHERE id = ?`, r.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// StartConsumers creates a durable pull subscription per consumer and
// processes messages with idempotency keyed on (consumer, event_id).
// Each consumer's loop stops when ctx ends or the Bus is Close()'d.
func (b *Bus) StartConsumers(ctx context.Context, db OutboxStore, consumers []Consumer) error {
	for _, c := range consumers {
		sub, err := b.js.PullSubscribe(c.Subject, c.Name, nats.AckExplicit(), nats.MaxDeliver(maxDeliver), nats.AckWait(ackWait))
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", c.Name, err)
		}
		loopCtx, cancel := b.deriveCtx(ctx)
		go func() {
			defer cancel()
			b.consumeLoop(loopCtx, db, c, sub)
		}()
	}
	return nil
}

// fetchRetryBackoff is how long consumeLoop pauses after a non-timeout
// Fetch error before retrying, so a persistently unhealthy NATS
// connection doesn't spin the loop hot.
const fetchRetryBackoff = 500 * time.Millisecond

func (b *Bus) consumeLoop(ctx context.Context, db OutboxStore, c Consumer, sub *nats.Subscription) {
	for ctx.Err() == nil {
		msgs, err := sub.Fetch(10, nats.Context(ctx))
		if err != nil {
			if !errors.Is(err, nats.ErrTimeout) &&
				!errors.Is(err, context.DeadlineExceeded) &&
				!errors.Is(err, context.Canceled) {
				time.Sleep(fetchRetryBackoff)
			}
			continue // timeout/ctx/backoff — poll again
		}
		for _, msg := range msgs {
			b.handleMsg(ctx, db, c, msg)
		}
	}
}

func (b *Bus) handleMsg(ctx context.Context, db OutboxStore, c Consumer, msg *nats.Msg) {
	var evt Event
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		slog.Error("consumer bad payload", "consumer", c.Name, "err", err)
		_ = msg.Term() // poison message — never parseable
		return
	}
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		// Scope the whole consumer tx (claim + handler) to the event's
		// tenant so handlers can write RLS-forced rows (phase 2 D4).
		// Invalid/empty tenant → GUC stays unset → tenant tables read
		// as empty and reject writes, same as before.
		if _, err := uuid.Parse(evt.TenantID); err == nil {
			if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, evt.TenantID).Error; err != nil {
				return err
			}
		}
		res := tx.Exec(`INSERT INTO processed_events (consumer, event_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			c.Name, evt.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // duplicate delivery — no-op (idempotency)
		}
		// Handler runs in the SAME tx as the idempotency claim — do not
		// open your own transaction; rollback of the claim implies
		// rollback of handler effects.
		return c.Handle(ctx, tx, evt)
	})
	if err != nil {
		slog.Error("consumer handle", "consumer", c.Name, "event", evt.ID, "err", err)
		if meta, mErr := msg.Metadata(); mErr == nil && meta.NumDelivered >= maxDeliver {
			dlqSubject := "hms.dlq." + c.Name
			if _, pErr := b.js.Publish(dlqSubject, msg.Data); pErr != nil {
				// DLQ publish failed — the event would vanish from both
				// the live stream and the DLQ if we Term()'d here, so
				// Nak instead and let the next redelivery retry the
				// dead-letter attempt.
				slog.Error("consumer dlq publish failed", "consumer", c.Name, "event", evt.ID,
					"dlq_subject", dlqSubject, "err", pErr)
				_ = msg.Nak()
				return
			}
			slog.Error("consumer dead-lettered event", "consumer", c.Name, "event", evt.ID,
				"num_delivered", meta.NumDelivered, "dlq_subject", dlqSubject)
			_ = msg.Term()
			return
		}
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}
