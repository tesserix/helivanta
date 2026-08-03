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
	return &Bus{nc: nc, js: js}, nil
}

func (b *Bus) Ping(ctx context.Context) error {
	if !b.nc.IsConnected() {
		return errors.New("nats disconnected")
	}
	return nil
}

func (b *Bus) Close() { b.nc.Drain() }

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
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return tx.Create(&outboxRow{ID: uuid.MustParse(evt.ID), Subject: subject, Payload: payload}).Error
}

// RunDispatcher drains the outbox into JetStream until ctx ends.
func (b *Bus) RunDispatcher(ctx context.Context, db OutboxStore) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.drainOnce(ctx, db); err != nil {
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
				return err
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
func (b *Bus) StartConsumers(ctx context.Context, db OutboxStore, consumers []Consumer) error {
	for _, c := range consumers {
		sub, err := b.js.PullSubscribe(c.Subject, c.Name, nats.AckExplicit(), nats.MaxDeliver(5))
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", c.Name, err)
		}
		go b.consumeLoop(ctx, db, c, sub)
	}
	return nil
}

func (b *Bus) consumeLoop(ctx context.Context, db OutboxStore, c Consumer, sub *nats.Subscription) {
	for ctx.Err() == nil {
		msgs, err := sub.Fetch(10, nats.Context(ctx))
		if err != nil {
			continue // timeout/ctx — poll again
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
		res := tx.Exec(`INSERT INTO processed_events (consumer, event_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			c.Name, evt.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // duplicate delivery — no-op (idempotency)
		}
		// Handler runs in the same tx as the idempotency claim, so a
		// failed handler rolls the claim back and redelivery retries.
		return c.Handle(ctx, evt)
	})
	if err != nil {
		slog.Error("consumer handle", "consumer", c.Name, "event", evt.ID, "err", err)
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}
