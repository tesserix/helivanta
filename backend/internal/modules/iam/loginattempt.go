package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

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

var (
	errAttemptNotFound   = errors.New("login attempt not found")
	errAttemptsExhausted = errors.New("login attempt exhausted its factor attempts")
)

// loginAttempt holds the Zitadel session between the password step and
// the factor step. The browser is never given zitadelSessionToken (spec
// D2); it only ever sees authRequestID.
type loginAttempt struct {
	AuthRequestID  string
	SessionID      string
	SessionToken   string
	Subject        string
	FactorAttempts int
	ExpiresAt      time.Time
}

// loginAttemptRow is the GORM-mapped row for login_attempt. It is kept
// separate from loginAttempt so the token field's column name is
// explicit and the two never accidentally diverge in shape.
type loginAttemptRow struct {
	AuthRequestID  string    `gorm:"column:auth_request_id;primaryKey"`
	SessionID      string    `gorm:"column:zitadel_session_id"`
	SessionToken   string    `gorm:"column:zitadel_session_token"`
	Subject        string    `gorm:"column:subject"`
	FactorAttempts int       `gorm:"column:factor_attempts"`
	ExpiresAt      time.Time `gorm:"column:expires_at"`
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

// Put inserts a. Callers own authRequestID uniqueness; a collision (which
// should not happen — Zitadel mints authRequestID) surfaces as a plain
// error rather than being silently upserted.
func (s *loginAttemptStore) Put(ctx context.Context, a loginAttempt) error {
	row := loginAttemptRow(a)
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Create(&row).Error
	})
}

// Get returns the attempt for authRequestID, treating an expired row as
// not found and deleting it so expiry needs no separate cleanup job for
// correctness — a background sweep is still useful for table bloat, but
// nothing depends on it for behavior.
func (s *loginAttemptStore) Get(ctx context.Context, authRequestID string) (loginAttempt, error) {
	var row loginAttemptRow
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Where("auth_request_id = ?", authRequestID).First(&row).Error; err != nil {
			return err
		}
		if row.ExpiresAt.Before(time.Now()) {
			if err := tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error; err != nil {
				return err
			}
			return errAttemptNotFound
		}
		return nil
	})
	switch {
	case errors.Is(err, errAttemptNotFound):
		return loginAttempt{}, errAttemptNotFound
	case errors.Is(err, gorm.ErrRecordNotFound):
		return loginAttempt{}, errAttemptNotFound
	case err != nil:
		return loginAttempt{}, fmt.Errorf("get login attempt: %w", err)
	}
	return row.toDomain(), nil
}

// BumpAndGet increments factor_attempts and reads the new value in ONE
// statement (UPDATE ... RETURNING). Two statements — an UPDATE followed
// by a separate SELECT — would race under concurrent requests for the
// same auth_request_id, and the race runs in the attacker's favor: two
// concurrent guesses could both read the pre-increment count and both be
// admitted, granting extra tries against a six-digit code.
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
			WHERE auth_request_id = ? AND expires_at > now()
			RETURNING auth_request_id, zitadel_session_id, zitadel_session_token,
			          subject, factor_attempts, expires_at`,
			authRequestID).Scan(&row).Error
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

// Delete removes the attempt for authRequestID. Deleting an
// already-absent row is not an error: the caller's intent (this attempt
// must not exist) is already satisfied.
func (s *loginAttemptStore) Delete(ctx context.Context, authRequestID string) error {
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error
	})
}
