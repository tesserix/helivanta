package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// maxFactorAttempts bounds how many wrong TOTP codes a single login
// attempt may submit before the server drops the Zitadel session
// outright. Five, not three: mistyping a rolling six-digit code is
// ordinary, and the server should tolerate an honest slip. Five, not
// fifty: Zitadel applies a delay rather than a lockout (spec D6), so the
// server's own cap is the only thing standing between a guesser and the
// 10^6 code space — a limit that loose lets that space erode.
const maxFactorAttempts = 5

// attemptStage is which step a login_attempt row is waiting for (#856,
// 0007_iam). Each endpoint reads only rows at its own stage
// (loginAttemptStore.Get), so a row can never be used for a step it was not
// written for.
type attemptStage string

const (
	// stageFactor means the password was right and a TOTP code is due:
	// for an enrolled TOTP (POST /v1/auth/login/factor) or, when Enrolling
	// is set, for one just registered (POST /v1/auth/login/enroll, #948).
	// Written by Password.
	stageFactor attemptStage = "factor"
	// stagePasswordChange means every factor is proven and the password must
	// change (POST /v1/auth/login/password-change). Written by Password or
	// Factor, and ONLY on loginclient.OutcomePasswordChangeRequired — which
	// a full sufficiency decision alone produces.
	stagePasswordChange attemptStage = "password_change"
)

var (
	errAttemptNotFound   = errors.New("login attempt not found")
	errAttemptsExhausted = errors.New("login attempt exhausted its factor attempts")
)

// loginAttempt holds the Zitadel session between the password step and
// the step after it — a factor or a password change (Stage). The browser is
// never given zitadelSessionToken (spec D2); it only ever sees
// authRequestID.
type loginAttempt struct {
	AuthRequestID  string
	SessionID      string
	SessionToken   string
	Subject        string
	FactorAttempts int
	ExpiresAt      time.Time
	// Enrolling is true when the password step registered a fresh TOTP
	// for this attempt and POST /v1/auth/login/enroll must confirm it
	// (#948, spec D4); false when an already-enrolled factor is awaited
	// via POST /v1/auth/login/factor. Each handler refuses the other's
	// row. Set by Put; AdvanceToPasswordChange clears it.
	Enrolling bool
	Stage     attemptStage
}

// loginAttemptRow is the GORM-mapped row for login_attempt. It is kept
// separate from loginAttempt so the token field's column name is
// explicit and the two never accidentally diverge in shape.
type loginAttemptRow struct {
	AuthRequestID  string       `gorm:"column:auth_request_id;primaryKey"`
	SessionID      string       `gorm:"column:zitadel_session_id"`
	SessionToken   string       `gorm:"column:zitadel_session_token"`
	Subject        string       `gorm:"column:subject"`
	FactorAttempts int          `gorm:"column:factor_attempts"`
	ExpiresAt      time.Time    `gorm:"column:expires_at"`
	Enrolling      bool         `gorm:"column:enrolling"`
	Stage          attemptStage `gorm:"column:stage"`
}

func (loginAttemptRow) TableName() string { return "login_attempt" }

func (r loginAttemptRow) toDomain() loginAttempt { return loginAttempt(r) }

// loginAttemptStore is the Postgres-backed store for login_attempt. It is
// not tenant-scoped (see the 0004_iam migration comment in module.go), so
// every query runs through WithSystem, never WithTenant.
type loginAttemptStore struct {
	db *tenantdb.DB
}

func newLoginAttemptStore(db *tenantdb.DB) *loginAttemptStore {
	return &loginAttemptStore{db: db}
}

// Put upserts a, replacing any existing row for the same AuthRequestID
// and resetting FactorAttempts to a's value (ordinarily 0). This is a
// deliberate choice, not an oversight: a fresh password step submitted
// for an auth_request_id that already has a pending attempt (a retried
// or resumed login) legitimately supersedes it — the old Zitadel session
// is being replaced by a new one, so the old row's guess count has
// nothing left to protect. Zitadel mints auth_request_id, so a collision
// against an UNRELATED login is not a realistic case this needs to
// defend against; an INSERT-only Put would instead turn the ordinary
// retried-login case into an opaque primary-key-violation error at the
// handler (Task 3).
func (s *loginAttemptStore) Put(ctx context.Context, a loginAttempt) error {
	row := loginAttemptRow(a)
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "auth_request_id"}},
			UpdateAll: true,
		}).Create(&row).Error
	})
}

// Get returns the attempt for authRequestID at stage, treating an expired row as
// not found and deleting it so expiry needs no separate cleanup job for
// correctness (spec D6). A row nobody reads again is deleted by
// SweepExpired instead (#869, loginattempt_sweep.go), which bounds how long
// its live Zitadel session token stays at rest — but no READ depends on the
// sweep having run: an expired row is refused here regardless.
//
// A row at a DIFFERENT stage is errAttemptNotFound too, exactly like a
// missing one (#856): the stage is part of what the caller asked for, and an
// endpoint must not be able to resume a session written for another step —
// above all, password-change must never resume a session whose TOTP is still
// unproven. Taking the stage as a parameter, rather than leaving each handler
// to compare it, makes forgetting the check impossible to express.
func (s *loginAttemptStore) Get(ctx context.Context, authRequestID string, stage attemptStage) (loginAttempt, error) {
	var row loginAttemptRow
	var expired bool
	// Same shape as BumpAndGet's exhaustion path: the closure must return
	// nil on the expired branch too, or WithSystem's wrapping
	// Transaction(fn) rolls the just-issued DELETE back along with
	// everything else, and the row survives despite Get reporting
	// errAttemptNotFound to the caller. Signal expiry via the `expired`
	// flag, examined only after the transaction has committed.
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Where("auth_request_id = ?", authRequestID).First(&row).Error; err != nil {
			return err
		}
		if row.ExpiresAt.Before(time.Now()) {
			if err := tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error; err != nil {
				return err
			}
			expired = true
		}
		return nil
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return loginAttempt{}, errAttemptNotFound
	case err != nil:
		return loginAttempt{}, fmt.Errorf("get login attempt: %w", err)
	case expired:
		return loginAttempt{}, errAttemptNotFound
	case row.Stage != stage:
		return loginAttempt{}, errAttemptNotFound
	}
	return row.toDomain(), nil
}

// BumpAndGet increments factor_attempts and reads the new value in ONE
// statement (UPDATE ... RETURNING). It counts only stageFactor rows (#856): a
// password-change row is not a TOTP guessing budget, and reads as not found. Two statements — an UPDATE followed
// by a separate SELECT — would race under concurrent requests for the
// same auth_request_id, and the race runs in the attacker's favor: two
// concurrent guesses could both read the pre-increment count and both be
// admitted, granting extra tries against a six-digit code.
//
// Call this ONLY after Zitadel has rejected a submitted code — it counts
// wrong codes (spec D6), not attempts. A caller that bumps before
// verifying (e.g. to reserve a slot, or bumps unconditionally regardless
// of Zitadel's answer) silently shrinks the real budget from five to
// four, because the eventual correct code still consumes one of the five
// bumps this method hands out.
//
// When the incremented value reaches maxFactorAttempts, the row is
// deleted in the same statement's transaction and errAttemptsExhausted is
// returned, so the Zitadel session cannot be reused for a further guess.
func (s *loginAttemptStore) BumpAndGet(ctx context.Context, authRequestID string) (loginAttempt, error) {
	var row loginAttemptRow
	var exhausted bool
	// The exhaustion delete must COMMIT, not roll back with the sentinel
	// error: WithSystem wraps this closure in a transaction and rolls it
	// back on any non-nil return, which would undo the delete along with
	// the increment. So the closure always returns nil on success (delete
	// included) and reports exhaustion via the exhausted flag instead,
	// examined only after the transaction has committed.
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		err := tx.Raw(`
			UPDATE login_attempt
			SET factor_attempts = factor_attempts + 1
			WHERE auth_request_id = ? AND expires_at > now() AND stage = ?
			RETURNING auth_request_id, zitadel_session_id, zitadel_session_token,
			          subject, factor_attempts, expires_at, enrolling, stage`,
			authRequestID, stageFactor).Scan(&row).Error
		if err != nil {
			return err
		}
		if row.AuthRequestID == "" {
			return errAttemptNotFound
		}
		if row.FactorAttempts >= maxFactorAttempts {
			if err := tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error; err != nil {
				return err
			}
			exhausted = true
		}
		return nil
	})
	// If the transaction itself fails to commit (infra failure, not a
	// business outcome), `exhausted` and the incremented count are both
	// discarded along with the rest of the transaction — the guess is
	// not counted. This errs toward the attacker (an infra blip can hand
	// back a free retry) rather than toward the legitimate user (who
	// would otherwise lose a guess to a failure that was never their
	// fault). Considered and accepted: an infra failure on this path is
	// already an incident, and the alternative — persisting the count
	// outside the guess's own transaction — would let a lost commit here
	// count a guess that was never actually evaluated.
	switch {
	case errors.Is(err, errAttemptNotFound):
		return loginAttempt{}, errAttemptNotFound
	case err != nil:
		return loginAttempt{}, fmt.Errorf("bump login attempt: %w", err)
	case exhausted:
		return loginAttempt{}, errAttemptsExhausted
	}
	return row.toDomain(), nil
}

// UpdateToken replaces the stored Zitadel session token for
// authRequestID — Zitadel rotates the token on each factor submission
// (spec D3), and the old token must stop being usable the moment a new
// one is issued.
func (s *loginAttemptStore) UpdateToken(ctx context.Context, authRequestID, token string) error {
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&loginAttemptRow{}).
			Where("auth_request_id = ?", authRequestID).
			Update("zitadel_session_token", token)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errAttemptNotFound
		}
		return nil
	})
}

// AdvanceToPasswordChange moves authRequestID's row from stageFactor to
// stagePasswordChange (#856): Factor and Enroll call it when
// CompleteAfterFactor answers OutcomePasswordChangeRequired after a verified
// TOTP. It also clears enrolling (#948): the enrolment is confirmed, and a
// password-change row is no factor step of either kind. Guarded on the
// current stage so only a factor row advances; anything else is
// errAttemptNotFound.
func (s *loginAttemptStore) AdvanceToPasswordChange(ctx context.Context, authRequestID string) error {
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&loginAttemptRow{}).
			Where("auth_request_id = ? AND stage = ?", authRequestID, stageFactor).
			Updates(map[string]any{"stage": stagePasswordChange, "enrolling": false})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errAttemptNotFound
		}
		return nil
	})
}

// Delete removes the attempt for authRequestID. Deleting an
// already-absent row is not an error: the caller's intent (this attempt
// must not exist) is already satisfied.
func (s *loginAttemptStore) Delete(ctx context.Context, authRequestID string) error {
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error
	})
}
