package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/tenantdb"
)

const StreamName = "HMS"

// subjectRoot is the top of the subject space every HMS event lives under
// (`hms.<dir>.<module>.<event>.vN`), and therefore what StreamName claims.
const subjectRoot = "hms"

// namespaceUnsafe matches everything a NATS stream name and subject token
// may not contain. Dots and wildcards would silently change the shape of
// the subject space rather than fail, so they are replaced rather than
// rejected.
var namespaceUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

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
//
// WithAdmin is here deliberately, not as a convenience: it is what forces
// drainOnce to be able to compile only against a store that offers the
// admin-pool, RLS-bypassing path (see drainOnce's comment for why it needs
// it). Adding this method is itself part of Task 1 (#835) — a store that
// only implements WithSystem can no longer satisfy this interface, so the
// migration that puts RLS on outbox_events and the dispatcher's move off
// WithSystem cannot land apart.
type OutboxStore interface {
	WithSystem(ctx context.Context, fn func(tx *gorm.DB) error) error
	WithAdmin(ctx context.Context, fn func(tx *gorm.DB) error) error
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
	}, {
		// 0002_events_outbox_tenant brings the outbox inside the RLS
		// boundary (design spec D1/D1a, #835/#774). Append-only: 0001 is
		// never edited.
		//
		// tenant_id is NULLABLE — no SET NOT NULL — because
		// SubjectCredentialRevoked (iam/signout.go) publishes with no
		// TenantID at all: revocation is subject-scoped and ends every
		// session for a subject in every tenant, so there is genuinely no
		// tenant to name. The policy is asymmetric on purpose: USING calls
		// hms_tenant_visible(tenant_id), which evaluates NULL (not true)
		// for a tenant-less row — strictly the correct, strictest outcome,
		// since a platform-wide event is not any single tenant's to read.
		// WITH CHECK must NOT call hms_tenant_visible (LintRLS fails that
		// shape explicitly: reads may widen, writes stay pinned to one
		// tenant) and MUST permit NULL, or publishing a tenant-less event
		// from inside a tenant-scoped transaction — exactly what sign-out
		// does — would be rejected by the policy and sign-out would 500.
		ID: "0002_events_outbox_tenant",
		SQL: `
			ALTER TABLE outbox_events ADD COLUMN tenant_id uuid;

			-- Backfill from the envelope Publish has always written into
			-- payload. The regex guard matters: tenant-less events (e.g.
			-- CredentialRevoked) carry "" in payload->>'tenant_id', and a
			-- bare ::uuid cast would abort the whole migration on the
			-- first such row rather than leave it NULL.
			UPDATE outbox_events
			   SET tenant_id = (payload->>'tenant_id')::uuid
			 WHERE payload->>'tenant_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

			ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
			ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;

			CREATE POLICY tenant_isolation ON outbox_events
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id IS NULL
			              OR tenant_id = current_setting('app.tenant_id', true)::uuid);

			CREATE INDEX ON outbox_events (tenant_id);`,
	}, {
		// 0003 finishes the fix 0002_platform_rls started. That migration
		// wrapped current_setting in NULLIF inside hms_tenant_visible, so
		// USING stopped erroring on a pooled connection whose GUC had
		// reverted to '' — but 0002_events_outbox_tenant's WITH CHECK
		// calls current_setting DIRECTLY (it must not call
		// hms_tenant_visible: LintRLS requires writes to stay pinned to
		// one tenant), so it kept the bare cast and kept the bug.
		//
		// Verified against the dev database rather than reasoned about.
		// The SAME policy violation — inserting a row for a tenant other
		// than the transaction's — reports two different errors depending
		// on whether that pooled connection had previously served a
		// WithTenant transaction:
		//
		//   clean connection: ERROR: new row violates row-level security policy
		//   dirty connection: ERROR: invalid input syntax for type uuid: ""
		//
		// With a pool of 5 and no control over which connection serves a
		// request, which one a caller sees is effectively random. That is
		// nondeterminism in a security control: the second is a type
		// error, so any handler or test discriminating on "was this an RLS
		// rejection" sees a different answer run to run, and the operator
		// reading it has no reason to connect it to tenancy at all.
		//
		// The tenant-less INSERT path happens to survive today only
		// because the planner evaluates the cheap `tenant_id IS NULL`
		// before the function call. Postgres does not guarantee OR operand
		// order, so relying on that is relying on a cost estimate — this
		// makes the clause correct regardless of evaluation order.
		ID: "0003_events_outbox_tenant_check",
		SQL: `
			ALTER POLICY tenant_isolation ON outbox_events
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id IS NULL
			              OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);`,
	}}
}

type Bus struct {
	nc *nats.Conn
	js nats.JetStreamContext

	// ns namespaces this Bus's subject space. Empty in production, where
	// the stream is HMS over `hms.>` exactly as it has always been. Tests
	// set it so many of them can share one NATS server: JetStream refuses
	// two streams with overlapping subjects, so isolation has to come from
	// the subject space itself rather than from the stream name.
	//
	// It applies only at the JetStream boundary. Outbox rows still store
	// the module's own subject, so what a test writes to the database is
	// byte-identical to production.
	ns string

	// stopCtx/stop give Close() a way to unwind consumeLoop/RunDispatcher
	// goroutines even when the caller's ctx is long-lived (e.g. request
	// scoped or background.TODO()).
	stopCtx context.Context
	stop    context.CancelFunc
}

func NewBus(natsURL string) (*Bus, error) {
	return NewBusInNamespace(natsURL, "")
}

// NewBusInNamespace is NewBus with an isolated subject space.
//
// It exists for tests: many of them share one NATS server, and JetStream
// rejects a stream whose subjects overlap an existing one's, so each test
// needs its own subject space rather than merely its own stream name.
// Passing "" gives exactly what NewBus gives — this is not a production
// deployment knob, and nothing in cmd/ should call it.
func NewBusInNamespace(natsURL, ns string) (*Bus, error) {
	ns = sanitizeNamespace(ns)
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}
	b := &Bus{nc: nc, js: js, ns: ns}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      b.streamName(),
		Subjects:  []string{b.Subject(subjectRoot + ".>")},
		Retention: nats.LimitsPolicy,
		// streamMaxAge (retention.go) — was 7 days; see its comment for why
		// 24h is ample. The DLQ subject lives inside this same stream, so a
		// dead-lettered clinical event is bounded by the same window rather
		// than kept indefinitely.
		MaxAge: streamMaxAge,
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	b.stopCtx, b.stop = context.WithCancel(context.Background())
	return b, nil
}

// sanitizeNamespace makes an arbitrary label safe to embed in a stream
// name and a subject token. Go test names — the intended input — carry
// slashes for subtests, and a dot would silently split into two subject
// tokens rather than fail.
func sanitizeNamespace(ns string) string {
	return namespaceUnsafe.ReplaceAllString(ns, "_")
}

// streamName is StreamName in production, and one stream per namespace
// otherwise.
func (b *Bus) streamName() string {
	if b.ns == "" {
		return StreamName
	}
	return StreamName + "_" + b.ns
}

// Subject reports the subject this Bus actually uses on the wire for a
// logical subject — the module's own subject in production, and a
// namespaced one under test. Every subject crossing the JetStream
// boundary (published, subscribed or dead lettered) goes through here;
// one that does not is a silent leak between namespaces rather than a
// visible error, which is why tests that talk to NATS directly resolve
// their subjects through it too.
func (b *Bus) Subject(s string) string {
	if b.ns == "" {
		return s
	}
	return b.ns + "." + s
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
	if err := b.nc.Drain(); err != nil {
		slog.Error("nats drain", "err", err)
	}
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
	ID      uuid.UUID
	Subject string
	Payload []byte
	// TenantID is nullable — see 0002_events_outbox_tenant's comment.
	// Nil for events with no TenantID (e.g. CredentialRevoked), which the
	// tenant_isolation policy then makes readable by nobody but the
	// dispatcher's WithAdmin read.
	TenantID    *uuid.UUID
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
	row := outboxRow{ID: id, Subject: subject, Payload: payload}
	// tenant_id is derived from the envelope itself, never a separate
	// parameter, so the column and the payload can never disagree.
	// Absent or unparseable (empty string for a subject-scoped broadcast,
	// or any other non-UUID) leaves it NULL rather than erroring — the
	// same fail-closed-on-read, permissive-on-write shape the
	// tenant_isolation policy expects.
	if tenantID, err := uuid.Parse(evt.TenantID); err == nil {
		row.TenantID = &tenantID
	}
	return tx.Create(&row).Error
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
			b.drainSafely(loopCtx, db)
		}
	}
}

// drainSafely runs one drain pass, containing a panic to this tick. The
// dispatcher goroutine is started with a bare `go` in main, so a panic
// here would otherwise terminate the process.
func (b *Bus) drainSafely(ctx context.Context, db OutboxStore) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("outbox dispatch panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if err := b.drainOnce(ctx, db); err != nil {
		slog.Error("outbox dispatch", "err", err)
	}
}

func (b *Bus) drainOnce(ctx context.Context, db OutboxStore) error {
	// WithAdmin, not WithSystem (trap 1, design spec D1): 0002_events_outbox_tenant
	// put row-level security on outbox_events, and WithSystem sets no
	// tenant GUC. Every row's tenant_isolation policy would then evaluate
	// current_setting('app.tenant_id', true) as NULL, match nothing, and
	// this select would silently return zero rows on every tick — the
	// whole event bus stops platform-wide with no error anywhere.
	// WithAdmin runs on the admin pool, which bypasses RLS entirely, and
	// this is the one call site allowed to (see withAdminAllowlist in
	// internal/archtest/arch_test.go). TestDispatcherPublishesEveryTenant
	// in outbox_rls_test.go pins this: reverting this line to WithSystem
	// must make that test observe zero published events, not an error.
	return db.WithAdmin(ctx, func(tx *gorm.DB) error {
		var rows []outboxRow
		if err := tx.Raw(`SELECT id, subject, payload FROM outbox_events
			WHERE published_at IS NULL ORDER BY created_at LIMIT 100
			FOR UPDATE SKIP LOCKED`).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			// MsgId gives JetStream server-side dedup on redelivery.
			if _, err := b.js.Publish(b.Subject(r.Subject), r.Payload, nats.MsgId(r.ID.String())); err != nil {
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
		sub, err := b.js.PullSubscribe(b.Subject(c.Subject), c.Name, nats.AckExplicit(), nats.MaxDeliver(maxDeliver), nats.AckWait(ackWait))
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

// handleMsg is called directly from consumeLoop's `go`-free for-loop, so a
// panic here (e.g. from json.Unmarshal or future parsing added before
// runConsumerTx) has no recover between it and the process — runConsumerTx's
// recover only guards the tx it wraps. This is a backstop for exactly that
// gap: log and Nak so a redelivery gets another chance instead of the
// consumer goroutine, and therefore the process, dying.
func (b *Bus) handleMsg(ctx context.Context, db OutboxStore, c Consumer, msg *nats.Msg) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("consumer handleMsg panic", "consumer", c.Name,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			_ = msg.Nak()
		}
	}()
	var evt Event
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		slog.Error("consumer bad payload", "consumer", c.Name, "err", err)
		_ = msg.Term() // poison message — never parseable
		return
	}
	err := b.runConsumerTx(ctx, db, c, evt)
	if err != nil {
		slog.Error("consumer handle", "consumer", c.Name, "event_id", evt.ID, "tenant_id", evt.TenantID, "err", err)
		if meta, mErr := msg.Metadata(); mErr == nil && meta.NumDelivered >= maxDeliver {
			dlqSubject := b.Subject(subjectRoot + ".dlq." + c.Name)
			if _, pErr := b.js.Publish(dlqSubject, msg.Data); pErr != nil {
				// DLQ publish failed — the event would vanish from both
				// the live stream and the DLQ if we Term()'d here, so
				// Nak instead and let the next redelivery retry the
				// dead-letter attempt.
				slog.Error("consumer dlq publish failed", "consumer", c.Name, "event_id", evt.ID,
					"tenant_id", evt.TenantID, "dlq_subject", dlqSubject, "err", pErr)
				_ = msg.Nak()
				return
			}
			slog.Error("consumer dead-lettered event", "consumer", c.Name, "event_id", evt.ID,
				"tenant_id", evt.TenantID, "num_delivered", meta.NumDelivered, "dlq_subject", dlqSubject)
			_ = msg.Term()
			return
		}
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

// runConsumerTx runs the idempotency claim and the handler in one tx, and
// recovers a handler panic into a plain error. Without this, a single
// panicking event would unwind through GORM's Transaction (which recovers,
// rolls back and re-panics) and crash the consumer goroutine — and since
// consumers are started with a bare `go`, the whole process. Converting
// the panic to an error lets it flow through handleMsg's normal Nak /
// maxDeliver / dead-letter path exactly like a returned error, so a
// malformed-but-parseable payload costs one event instead of the API.
func (b *Bus) runConsumerTx(ctx context.Context, db OutboxStore, c Consumer, evt Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("consumer panic", "consumer", c.Name, "event_id", evt.ID,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("consumer panic: %v", r)
		}
	}()
	return db.WithSystem(ctx, func(tx *gorm.DB) error {
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
}
