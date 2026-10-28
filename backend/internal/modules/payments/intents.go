package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/transit"
)

// credentialKey is the Transit key sealing every tenant's provider
// secrets; each ciphertext is bound to its tenant and account by the
// derivation context, so it does not open under any other.
const credentialKey = "helivanta-payments-credentials" //nolint:gosec // Transit key name, not a credential value

// sealer is the part of OpenBao Transit payments uses.
type sealer interface {
	Encrypt(ctx context.Context, key string, derivation, plaintext []byte) (string, error)
	Decrypt(ctx context.Context, key string, derivation []byte, ciphertext string) ([]byte, error)
}

// service is the orchestration core: every way an intent opens, settles
// or closes runs through it, whoever triggered it.
type service struct {
	vault    sealer
	provider provider
	now      func() time.Time
}

func newService(vault *transit.Client, p provider) *service {
	s := &service{provider: p, now: func() time.Time { return time.Now().UTC() }}
	// A nil *transit.Client inside a non-nil interface would pass the nil
	// check and then panic; keep the interface nil instead.
	if vault != nil {
		s.vault = vault
	}
	return s
}

type intent struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Requester      string
	RequestID      uuid.UUID
	AmountMinor    int64
	Currency       string
	Status         string
	AccountID      *uuid.UUID
	ProviderRef    *string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	ChecksDone     int
	LateChecksLeft int
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

func (s *service) credentials(ctx context.Context, tx *gorm.DB, accountID uuid.UUID) (credentials, error) {
	var a account
	if err := tx.Raw(`SELECT * FROM payment_accounts WHERE id=?`, accountID).Scan(&a).Error; err != nil {
		return credentials{}, err
	}
	if a.ID == uuid.Nil {
		return credentials{}, fmt.Errorf("%w: account removed", errProviderUnavailable)
	}
	secret, err := s.unseal(ctx, a, a.KeySecretSealed)
	if err != nil {
		return credentials{}, err
	}
	return credentials{KeyID: a.KeyID, KeySecret: secret}, nil
}

// openRequest is what a requester's event asks for.
type openRequest struct {
	Requester        string
	RequestID        string
	AmountMinor      int64
	Currency         string
	ExpiresInSeconds int
}

// open answers one request exactly once: a redelivered request finds its
// intent and does nothing, so one request never makes a second QR here.
func (s *service) open(ctx context.Context, tx *gorm.DB, tenant string, r openRequest) error {
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
			VALUES (?,?,?,?,?,?,'failed',?,?)`, id, tenant, r.Requester, requestID, max(r.AmountMinor, 1), r.Currency, reason, now).Error; err != nil {
			return err
		}
		if err := transition(tx, tenant, id, "", "failed", "request"); err != nil {
			return err
		}
		return s.publish(tx, tenant, paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: ref, Reason: reason})
	}
	// Razorpay's UPI QR takes rupees only and closes between 2 minutes and
	// 2 hours; a request outside that cannot be served, and the requester
	// must hear so rather than wait.
	if r.AmountMinor < 1 || r.AmountMinor > 1000000000000 || r.Currency != "INR" || r.ExpiresInSeconds < 120 || r.ExpiresInSeconds > 7200 {
		return fail(paymentscontract.ReasonProviderRejected)
	}
	var a account
	if err := tx.Raw(`SELECT * FROM payment_accounts`).Scan(&a).Error; err != nil {
		return err
	}
	// No account is a refusal, never a fallback to some platform account.
	if a.ID == uuid.Nil {
		return fail(paymentscontract.ReasonProviderNotConfigured)
	}
	secret, err := s.unseal(ctx, a, a.KeySecretSealed)
	if err != nil {
		return fail(paymentscontract.ReasonProviderUnavailable)
	}
	expires := now.Add(time.Duration(r.ExpiresInSeconds) * time.Second).Truncate(time.Second)
	qrID, imageURL, err := s.provider.OpenQR(ctx, credentials{KeyID: a.KeyID, KeySecret: secret}, id.String(), r.AmountMinor, expires)
	switch {
	case errors.Is(err, errProviderRejected):
		return fail(paymentscontract.ReasonProviderRejected)
	case err != nil:
		return fail(paymentscontract.ReasonProviderUnavailable)
	}
	if err := tx.Exec(`INSERT INTO payment_intents (id,tenant_id,requester,request_id,amount_minor,currency,status,account_id,provider_ref,qr_image_url,expires_at,next_check_at,created_at)
		VALUES (?,?,?,?,?,?,'awaiting_payment',?,?,?,?,?,?)`, id, tenant, r.Requester, requestID, r.AmountMinor, r.Currency, a.ID, qrID, imageURL, expires,
		nextCheck(now, 0, expires), now).Error; err != nil {
		return err
	}
	if err := transition(tx, tenant, id, "", "awaiting_payment", "request"); err != nil {
		return err
	}
	return s.publish(tx, tenant, paymentscontract.SubjectIntentOpened, paymentscontract.IntentOpenedData{IntentRef: ref, QRImageURL: imageURL, ExpiresAt: expires})
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

// lockIntent takes the intent row for one check. A nudge (poll, webhook)
// skips a row another checker holds rather than queueing behind its
// provider call, and found is false then; a cancel must not be lost, so
// it waits and then reads the status the other checker left.
func lockIntent(tx *gorm.DB, wait bool, where string, args ...any) (intent, bool, error) {
	lock := " FOR UPDATE SKIP LOCKED"
	if wait {
		lock = " FOR UPDATE"
	}
	var in intent
	err := tx.Raw(`SELECT id,tenant_id,requester,request_id,amount_minor,currency,status,account_id,provider_ref,expires_at,created_at,checks_done,late_checks_left
		FROM payment_intents WHERE `+where+lock, args...).Scan(&in).Error
	return in, in.ID != uuid.Nil, err
}

// check asks the provider what was paid into the intent's QR and acts on
// the answer. It is the only place an intent becomes paid, expired or
// cancelled. closeAs, when set, is the state to end in if nothing was
// paid; the caller has already closed the QR, so nothing can land after
// this decision except the late payments the late checks look for.
func (s *service) check(ctx context.Context, tx *gorm.DB, in intent, cause, closeAs string) error {
	if in.ProviderRef == nil || in.AccountID == nil {
		return nil
	}
	c, err := s.credentials(ctx, tx, *in.AccountID)
	if err != nil {
		return err
	}
	if closeAs != "" {
		if err := s.provider.CloseQR(ctx, c, *in.ProviderRef); err != nil {
			return err
		}
	}
	found, err := s.provider.QRPayments(ctx, c, *in.ProviderRef)
	if err != nil {
		return err
	}
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
	ref := paymentscontract.IntentRef{Requester: in.Requester, RequestID: in.RequestID.String(), IntentID: in.ID.String()}
	now := s.now()
	if paid != nil {
		late := in.Status == "expired" || in.Status == "cancelled"
		if err := s.move(tx, in, "paid", cause, map[string]any{"paid_payment_id": paid.ID, "late": late, "next_check_at": nil}); err != nil {
			return err
		}
		return s.publish(tx, tenant, paymentscontract.SubjectIntentPaid, paymentscontract.IntentPaidData{IntentRef: ref,
			AmountMinor: paid.AmountMinor, Currency: paid.Currency, ProviderPaymentID: paid.ID, RRN: paid.RRN, PaidAt: paid.CreatedAt, Late: late})
	}
	switch {
	case in.Status == "awaiting_payment" && closeAs != "":
		if err := s.move(tx, in, closeAs, cause, map[string]any{"next_check_at": lateCheckAfter(now, 2)}); err != nil {
			return err
		}
		if closeAs == "cancelled" {
			return s.publish(tx, tenant, paymentscontract.SubjectIntentCancelled, paymentscontract.IntentCancelledData{IntentRef: ref})
		}
		return s.publish(tx, tenant, paymentscontract.SubjectIntentExpired, paymentscontract.IntentExpiredData{IntentRef: ref})
	case in.Status == "awaiting_payment":
		return tx.Exec(`UPDATE payment_intents SET checks_done=checks_done+1, next_check_at=? WHERE id=?`,
			nextCheck(in.CreatedAt, in.ChecksDone+1, in.ExpiresAt), in.ID).Error
	case in.Status == "expired" || in.Status == "cancelled":
		left := in.LateChecksLeft - 1
		return tx.Exec(`UPDATE payment_intents SET late_checks_left=?, next_check_at=? WHERE id=?`, max(left, 0), lateCheckAfter(now, left), in.ID).Error
	}
	return nil
}

// move is the guarded status update: it changes the row only from the
// status the caller read, and records the transition with its cause.
func (s *service) move(tx *gorm.DB, in intent, to, cause string, set map[string]any) error {
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

func (s *service) publish(tx *gorm.DB, tenant, subject string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: raw})
}

// cancel closes the QR of a request's intent and decides it, so the
// requester always hears paid or cancelled. A cancel that overtakes its
// request (the bus does not order subjects) is retried until the intent
// exists; an intent already decided answers nothing more.
func (s *service) cancel(ctx context.Context, tx *gorm.DB, requester, requestID string) error {
	var exists int64
	if err := tx.Raw(`SELECT count(*) FROM payment_intents WHERE requester=? AND request_id=?`, requester, requestID).Scan(&exists).Error; err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("payments: cancel for request %s arrived before its intent", requestID)
	}
	in, _, err := lockIntent(tx, true, "requester=? AND request_id=?", requester, requestID)
	if err != nil || in.Status != "awaiting_payment" {
		return err
	}
	return s.check(ctx, tx, in, "cancel", "cancelled")
}
