package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
)

// credentialKey is the Transit key sealing every tenant's provider
// secrets; each ciphertext is bound to its tenant and account by the
// derivation context, so it does not open under any other.
const credentialKey = "helivanta-payments-credentials" //nolint:gosec // Transit key name, not a credential value

const (
	// checkLease covers one check's provider calls (two at 10 s each, plus
	// the vault). A checker that dies holding it is retried once it lapses.
	checkLease = 60 * time.Second
	// openDeadline is how long an intent may stay opening before the poller
	// fails it: the request consumer's QR call is 10 s inside a 20 s
	// after-commit budget, so a request older than this never got a QR.
	openDeadline = 45 * time.Second
	// lateCheckFailures bounds provider failures on a closed intent's late
	// checks; an open intent keeps retrying, because its decision is owed.
	lateCheckFailures = 10
)

// sealer is the part of OpenBao Transit payments uses.
type sealer interface {
	Encrypt(ctx context.Context, key string, derivation, plaintext []byte) (string, error)
	Decrypt(ctx context.Context, key string, derivation []byte, ciphertext string) ([]byte, error)
}

// service is the orchestration core: every way an intent opens, settles
// or closes runs through it, whoever triggered it. No database
// transaction is ever open across a provider or vault call: the app pool
// is small and shared by every module, so a slow provider must cost
// payments time, not the replica's connections.
type service struct {
	db       *tenantdb.DB
	vault    sealer
	provider provider
	now      func() time.Time
}

func newService(db *tenantdb.DB, vault *transit.Client, p provider) *service {
	s := &service{db: db, provider: p, now: func() time.Time { return time.Now().UTC() }}
	// A nil *transit.Client inside a non-nil interface would pass the nil
	// check and then panic; keep the interface nil instead.
	if vault != nil {
		s.vault = vault
	}
	return s
}

type intent struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Requester       string
	RequestID       uuid.UUID
	AmountMinor     int64
	Currency        string
	Status          string
	AccountID       *uuid.UUID
	KeyID           *string
	ProviderRef     *string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	ChecksDone      int
	LateChecksLeft  int
	CancelRequested bool
	CheckFailures   int
	LeaseUntil      *time.Time
}

func (in intent) ref() paymentscontract.IntentRef {
	return paymentscontract.IntentRef{Requester: in.Requester, RequestID: in.RequestID.String(), IntentID: in.ID.String()}
}

type account struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	Mode                string
	KeyID               string
	KeySecretSealed     string
	WebhookSecretSealed string
	UpdatedBy           string
	UpdatedAt           time.Time
}

func derivation(tenant, accountID uuid.UUID) []byte {
	return []byte(tenant.String() + "|" + accountID.String())
}

func (s *service) unseal(ctx context.Context, a account, sealed string) (string, error) {
	if s.vault == nil {
		return "", fmt.Errorf("%w: credential vault not configured", errProviderUnavailable)
	}
	plain, err := s.vault.Decrypt(ctx, credentialKey, derivation(a.TenantID, a.ID), sealed)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errProviderUnavailable, err)
	}
	return string(plain), nil
}

const intentColumns = `id,tenant_id,requester,request_id,amount_minor,currency,status,account_id,key_id,provider_ref,expires_at,created_at,
	checks_done,late_checks_left,cancel_requested,check_failures,lease_until`

// lockIntent takes the intent row. A nudge (poll, webhook) skips a row
// another transaction holds instead of queueing; found is false then. A
// consumer must not lose its event, so it waits.
func lockIntent(tx *gorm.DB, wait bool, where string, args ...any) (intent, bool, error) {
	lock := " FOR UPDATE SKIP LOCKED"
	if wait {
		lock = " FOR UPDATE"
	}
	var in intent
	err := tx.Raw(`SELECT `+intentColumns+` FROM payment_intents WHERE `+where+lock, args...).Scan(&in).Error
	return in, in.ID != uuid.Nil, err
}

func loadAccount(tx *gorm.DB) (account, error) {
	var a account
	err := tx.Raw(`SELECT * FROM payment_accounts`).Scan(&a).Error
	return a, err
}

// openRequest is what a requester's event asks for.
type openRequest struct {
	Requester        string
	RequestID        string
	AmountMinor      int64
	Currency         string
	ExpiresInSeconds int
}

// record answers one request exactly once, inside the consumer's
// transaction and without calling anyone: a redelivered request finds its
// intent (or the tombstone an earlier cancel left) and does nothing. The
// QR itself is made after commit, by open.
func (s *service) record(tx *gorm.DB, tenant string, r openRequest) error {
	requestID, err := uuid.Parse(r.RequestID)
	if err != nil {
		return fmt.Errorf("payments: request id %q is not a uuid: %w", r.RequestID, err)
	}
	var existing int64
	if err := tx.Raw(`SELECT count(*) FROM payment_intents WHERE requester=? AND request_id=?`, r.Requester, requestID).Scan(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	now := s.now()
	ref := paymentscontract.IntentRef{Requester: r.Requester, RequestID: requestID.String(), IntentID: id.String()}
	fail := func(reason string) error {
		if err := tx.Exec(`INSERT INTO payment_intents (id,tenant_id,requester,request_id,amount_minor,currency,status,failure_reason,expires_at)
			VALUES (?,?,?,?,?,?,'failed',?,?)`, id, tenant, r.Requester, requestID, min(max(r.AmountMinor, 1), 1000000000000), r.Currency, reason, now).Error; err != nil {
			return err
		}
		if err := transition(tx, tenant, id, "", "failed", "request"); err != nil {
			return err
		}
		return publish(tx, tenant, paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: ref, Reason: reason})
	}
	// Razorpay's UPI QR takes rupees only and closes between 2 minutes and
	// 2 hours; a request outside that cannot be served, and the requester
	// must hear so rather than wait.
	if r.AmountMinor < 1 || r.AmountMinor > 1000000000000 || r.Currency != "INR" || r.ExpiresInSeconds < 120 || r.ExpiresInSeconds > 7200 {
		return fail(paymentscontract.ReasonProviderRejected)
	}
	// FOR SHARE: a key change (account.put) waits for this request to be
	// recorded, and then sees it open and refuses.
	var a account
	if err := tx.Raw(`SELECT * FROM payment_accounts FOR SHARE`).Scan(&a).Error; err != nil {
		return err
	}
	// No account is a refusal, never a fallback to some platform account.
	if a.ID == uuid.Nil {
		return fail(paymentscontract.ReasonProviderNotConfigured)
	}
	expires := now.Add(time.Duration(r.ExpiresInSeconds) * time.Second).Truncate(time.Second)
	if err := tx.Exec(`INSERT INTO payment_intents (id,tenant_id,requester,request_id,amount_minor,currency,status,account_id,key_id,expires_at,next_check_at,created_at)
		VALUES (?,?,?,?,?,?,'opening',?,?,?,?,?)`, id, tenant, r.Requester, requestID, r.AmountMinor, r.Currency, a.ID, a.KeyID, expires,
		now.Add(openDeadline), now).Error; err != nil {
		return err
	}
	return transition(tx, tenant, id, "", "opening", "request")
}

// open makes the QR for an opening intent, after the consumer committed.
// It runs on every delivery, so it acts only while the intent is opening.
func (s *service) open(ctx context.Context, tenant, requester, requestID string) error {
	var in intent
	var a account
	if err := s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT `+intentColumns+` FROM payment_intents WHERE requester=? AND request_id=?`, requester, requestID).Scan(&in).Error; err != nil {
			return err
		}
		var err error
		a, err = loadAccount(tx)
		return err
	}); err != nil || in.Status != "opening" {
		return err
	}
	qrID, imageURL, err := "", "", errProviderUnavailable
	if a.ID == *in.AccountID && a.KeyID == *in.KeyID {
		var secret string
		if secret, err = s.unseal(ctx, a, a.KeySecretSealed); err == nil {
			qrID, imageURL, err = s.provider.OpenQR(ctx, credentials{KeyID: a.KeyID, KeySecret: secret}, in.ID.String(), in.AmountMinor, in.ExpiresAt)
		}
	}
	nudge := false
	orphan := false
	if txErr := s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		cur, _, err2 := lockIntent(tx, true, "id=?", in.ID)
		if err2 != nil {
			return err2
		}
		if cur.Status != "opening" {
			// The poller failed it at the deadline (or a cancel tombstoned
			// it) while the QR was being made: nobody will show this QR.
			orphan = err == nil
			return nil
		}
		if err != nil {
			reason := paymentscontract.ReasonProviderUnavailable
			if errors.Is(err, errProviderRejected) {
				reason = paymentscontract.ReasonProviderRejected
			}
			if err := move(tx, cur, "failed", "provider", map[string]any{"failure_reason": reason, "next_check_at": nil}); err != nil {
				return err
			}
			return publish(tx, tenant, paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: cur.ref(), Reason: reason})
		}
		next := nextCheck(cur.CreatedAt, 0, cur.ExpiresAt)
		if cur.CancelRequested {
			next, nudge = s.now(), true
		}
		if err := move(tx, cur, "awaiting_payment", "provider", map[string]any{"provider_ref": qrID, "qr_image_url": imageURL, "next_check_at": next}); err != nil {
			return err
		}
		return publish(tx, tenant, paymentscontract.SubjectIntentOpened, paymentscontract.IntentOpenedData{IntentRef: cur.ref(), QRImageURL: imageURL, ExpiresAt: cur.ExpiresAt})
	}); txErr != nil {
		return txErr
	}
	if orphan {
		s.closeOrphan(ctx, a, qrID)
	}
	if nudge {
		return s.check(ctx, tenant, in.ID, "cancel", true)
	}
	return nil
}

// closeOrphan closes a QR nobody holds; failing that, it closes itself at
// close_by, so the failure is logged rather than retried.
func (s *service) closeOrphan(ctx context.Context, a account, qrID string) {
	secret, err := s.unseal(ctx, a, a.KeySecretSealed)
	if err == nil {
		err = s.provider.CloseQR(ctx, credentials{KeyID: a.KeyID, KeySecret: secret}, qrID)
	}
	if err != nil {
		slog.WarnContext(ctx, "payments could not close an unused QR", "tenant_id", a.TenantID, "error", err)
	}
}

// requestCancel records a cancel inside the consumer's transaction. A
// cancel that overtakes its request (the bus does not order subjects)
// leaves a cancelled tombstone the request then finds, so the request
// never opens a QR; the requester hears cancelled at once.
func (s *service) requestCancel(tx *gorm.DB, tenant string, r openRequest) error {
	requestID, err := uuid.Parse(r.RequestID)
	if err != nil {
		return fmt.Errorf("payments: request id %q is not a uuid: %w", r.RequestID, err)
	}
	in, found, err := lockIntent(tx, true, "requester=? AND request_id=?", r.Requester, requestID)
	if err != nil {
		return err
	}
	if !found {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO payment_intents (id,tenant_id,requester,request_id,amount_minor,currency,status,expires_at)
			VALUES (?,?,?,?,?,?,'cancelled',?)`, id, tenant, r.Requester, requestID, min(max(r.AmountMinor, 1), 1000000000000), r.Currency, s.now()).Error; err != nil {
			return err
		}
		if err := transition(tx, tenant, id, "", "cancelled", "cancel"); err != nil {
			return err
		}
		return publish(tx, tenant, paymentscontract.SubjectIntentCancelled, paymentscontract.IntentCancelledData{IntentRef: paymentscontract.IntentRef{
			Requester: r.Requester, RequestID: requestID.String(), IntentID: id.String()}})
	}
	if in.Status != "opening" && in.Status != "awaiting_payment" {
		return nil
	}
	// The poller honours cancel_requested too, so the cancel holds even if
	// the after-commit check below never runs.
	return tx.Exec(`UPDATE payment_intents SET cancel_requested=true, next_check_at=CASE WHEN status='awaiting_payment' THEN now() ELSE next_check_at END WHERE id=?`, in.ID).Error
}

// cancelNow closes and decides a cancelled intent right away; if it cannot
// (provider down, lease held), cancel_requested keeps the poller on it.
func (s *service) cancelNow(ctx context.Context, tenant, requester, requestID string) error {
	var in intent
	if err := s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT `+intentColumns+` FROM payment_intents WHERE requester=? AND request_id=?`, requester, requestID).Scan(&in).Error
	}); err != nil || in.Status != "awaiting_payment" || !in.CancelRequested {
		return err
	}
	if err := s.check(ctx, tenant, in.ID, "cancel", true); err != nil {
		slog.WarnContext(ctx, "payments cancel deferred to the poller", "tenant_id", tenant, "intent_id", in.ID, "error", err)
	}
	return nil
}

// pollOffsets are the checks after an intent opens; after the last one
// it is checked every 30 s until it expires.
var pollOffsets = []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}

// nextCheck never schedules past expiry: the expiry check is the one that
// closes the QR and decides, so it must run on time.
func nextCheck(opened time.Time, checksDone int, expires time.Time) time.Time {
	var at time.Time
	if checksDone < len(pollOffsets) {
		at = opened.Add(pollOffsets[checksDone])
	} else {
		at = opened.Add(pollOffsets[len(pollOffsets)-1] + time.Duration(checksDone-len(pollOffsets)+1)*30*time.Second)
	}
	if at.After(expires) {
		return expires
	}
	return at
}

// lateCheckAfter is when a closed intent is looked at again for a payment
// that landed as it closed: +2 min, then +15 min, then never.
func lateCheckAfter(closedAt time.Time, left int) *time.Time {
	var at time.Time
	switch left {
	case 2:
		at = closedAt.Add(2 * time.Minute)
	case 1:
		at = closedAt.Add(13 * time.Minute)
	default:
		return nil
	}
	return &at
}

// check asks the provider what was paid into an intent's QR and acts on
// the answer; it is the only place an intent becomes paid, expired or
// cancelled. It claims the intent with a lease in one short transaction,
// calls the provider with none open, and decides in a second, so two
// checkers never call for the same intent at once and no connection waits
// on the provider. An awaiting intent past expiry, or with a cancel
// requested, is closed before its payments are fetched: nothing can land
// after the decision except what the late checks look for.
func (s *service) check(ctx context.Context, tenant string, id uuid.UUID, cause string, wait bool) error {
	var in intent
	var a account
	claimed := false
	if err := s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		var found bool
		var err error
		if in, found, err = lockIntent(tx, wait, "id=?", id); err != nil || !found {
			return err
		}
		if in.Status == "opening" {
			return s.expireOpening(tx, in)
		}
		if in.ProviderRef == nil {
			return nil
		}
		if in.LeaseUntil != nil && in.LeaseUntil.After(s.now()) {
			return nil
		}
		if a, err = loadAccount(tx); err != nil {
			return err
		}
		// Keys cannot change while an intent is open (account.put), so a
		// mismatch is a closed intent opened under old keys: its late checks
		// cannot be answered by this account, and stop.
		if a.ID != *in.AccountID || a.KeyID != *in.KeyID {
			if in.Status == "expired" || in.Status == "cancelled" {
				slog.WarnContext(ctx, "payments late check stopped: account keys changed", "tenant_id", tenant, "intent_id", id)
				return tx.Exec(`UPDATE payment_intents SET next_check_at=NULL WHERE id=?`, id).Error
			}
			return nil
		}
		claimed = true
		return tx.Exec(`UPDATE payment_intents SET lease_until=? WHERE id=?`, s.now().Add(checkLease), id).Error
	}); err != nil || !claimed {
		return err
	}
	closeAs := ""
	if in.Status == "awaiting_payment" {
		switch {
		case in.CancelRequested:
			closeAs = "cancelled"
		case !s.now().Before(in.ExpiresAt):
			closeAs = "expired"
		}
	}
	found, err := s.fetch(ctx, a, *in.ProviderRef, closeAs != "")
	if err != nil {
		if txErr := s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			stop := (in.Status == "expired" || in.Status == "cancelled") && in.CheckFailures+1 >= lateCheckFailures
			next := any(s.now().Add(30 * time.Second))
			if stop {
				next = nil
			}
			return tx.Exec(`UPDATE payment_intents SET lease_until=NULL, check_failures=check_failures+1, next_check_at=CASE WHEN next_check_at IS NULL THEN NULL ELSE ?::timestamptz END WHERE id=?`, next, id).Error
		}); txErr != nil {
			return txErr
		}
		return err
	}
	return s.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		cur, _, err := lockIntent(tx, true, "id=?", id)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE payment_intents SET lease_until=NULL, check_failures=0 WHERE id=?`, id).Error; err != nil {
			return err
		}
		return s.decide(tx, cur, found, cause, closeAs)
	})
}

func (s *service) fetch(ctx context.Context, a account, qrID string, closeFirst bool) ([]providerPayment, error) {
	secret, err := s.unseal(ctx, a, a.KeySecretSealed)
	if err != nil {
		return nil, err
	}
	c := credentials{KeyID: a.KeyID, KeySecret: secret}
	if closeFirst {
		if err := s.provider.CloseQR(ctx, c, qrID); err != nil {
			return nil, err
		}
	}
	return s.provider.QRPayments(ctx, c, qrID)
}

// expireOpening fails an intent whose QR was never made in time, so its
// requester hears an answer instead of waiting on a lost delivery.
func (s *service) expireOpening(tx *gorm.DB, in intent) error {
	if s.now().Before(in.CreatedAt.Add(openDeadline)) {
		return nil
	}
	reason := paymentscontract.ReasonProviderUnavailable
	if err := move(tx, in, "failed", "poll", map[string]any{"failure_reason": reason, "next_check_at": nil}); err != nil {
		return err
	}
	return publish(tx, in.TenantID.String(), paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: in.ref(), Reason: reason})
}

// decide records what the provider reported and moves the intent.
func (s *service) decide(tx *gorm.DB, in intent, found []providerPayment, cause, closeAs string) error {
	tenant := in.TenantID.String()
	var paid *providerPayment
	for i := range found {
		p := found[i]
		if p.Status != "captured" {
			continue
		}
		var known int64
		if err := tx.Raw(`SELECT count(*) FROM payment_provider_payments WHERE provider_payment_id=?`, p.ID).Scan(&known).Error; err != nil {
			return err
		}
		if known > 0 {
			continue
		}
		outcome := "mismatch"
		if p.AmountMinor == in.AmountMinor && p.Currency == in.Currency {
			outcome = "excess"
			if in.Status != "paid" && paid == nil {
				outcome, paid = "paid", &found[i]
			}
		}
		if err := tx.Exec(`INSERT INTO payment_provider_payments (tenant_id,intent_id,provider_payment_id,amount_minor,currency,rrn,outcome,provider_created_at)
			VALUES (?,?,?,?,?,?,?,?)`, tenant, in.ID, p.ID, p.AmountMinor, p.Currency, p.RRN, outcome, p.CreatedAt).Error; err != nil {
			return err
		}
	}
	now := s.now()
	if paid != nil {
		late := in.Status == "expired" || in.Status == "cancelled"
		if err := move(tx, in, "paid", cause, map[string]any{"paid_payment_id": paid.ID, "late": late, "next_check_at": nil}); err != nil {
			return err
		}
		return publish(tx, tenant, paymentscontract.SubjectIntentPaid, paymentscontract.IntentPaidData{IntentRef: in.ref(),
			AmountMinor: paid.AmountMinor, Currency: paid.Currency, ProviderPaymentID: paid.ID, RRN: paid.RRN, PaidAt: paid.CreatedAt, Late: late})
	}
	switch {
	case in.Status == "awaiting_payment" && closeAs != "":
		if err := move(tx, in, closeAs, cause, map[string]any{"next_check_at": lateCheckAfter(now, 2)}); err != nil {
			return err
		}
		if closeAs == "cancelled" {
			return publish(tx, tenant, paymentscontract.SubjectIntentCancelled, paymentscontract.IntentCancelledData{IntentRef: in.ref()})
		}
		return publish(tx, tenant, paymentscontract.SubjectIntentExpired, paymentscontract.IntentExpiredData{IntentRef: in.ref()})
	case in.Status == "awaiting_payment" && cause == "poll":
		return tx.Exec(`UPDATE payment_intents SET checks_done=checks_done+1, next_check_at=? WHERE id=?`,
			nextCheck(in.CreatedAt, in.ChecksDone+1, in.ExpiresAt), in.ID).Error
	case (in.Status == "expired" || in.Status == "cancelled") && cause == "poll":
		// Only the poller spends a late check: the webhook Razorpay sends
		// for our own close must not use up the +2 min look.
		left := in.LateChecksLeft - 1
		return tx.Exec(`UPDATE payment_intents SET late_checks_left=?, next_check_at=? WHERE id=?`, max(left, 0), lateCheckAfter(now, left), in.ID).Error
	}
	return nil
}

// move is the guarded status update: it changes the row only from the
// status the caller read, and records the transition with its cause.
func move(tx *gorm.DB, in intent, to, cause string, set map[string]any) error {
	set["status"] = to
	res := tx.Table("payment_intents").Where("id = ? AND status = ?", in.ID, in.Status).Updates(set)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("payments: intent %s left %s before it could move to %s", in.ID, in.Status, to)
	}
	return transition(tx, in.TenantID.String(), in.ID, in.Status, to, cause)
}

func transition(tx *gorm.DB, tenant string, id uuid.UUID, from, to, cause string) error {
	var fromStatus *string
	if from != "" {
		fromStatus = &from
	}
	return tx.Exec(`INSERT INTO payment_intent_transitions (tenant_id,intent_id,from_status,to_status,cause) VALUES (?,?,?,?,?)`,
		tenant, id, fromStatus, to, cause).Error
}

func publish(tx *gorm.DB, tenant, subject string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: raw})
}
