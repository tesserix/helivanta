package iam

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// accountChoice is one account whose password matched an email-plus-password
// submission, parked until the person picks it (#1751 spec D3). Like
// loginAttempt, the session token never reaches the browser.
type accountChoice struct {
	AuthRequestID string    `gorm:"column:auth_request_id;primaryKey"`
	TenantID      string    `gorm:"column:tenant_id;primaryKey"`
	SessionID     string    `gorm:"column:zitadel_session_id"`
	SessionToken  string    `gorm:"column:zitadel_session_token"`
	Subject       string    `gorm:"column:subject"`
	Name          string    `gorm:"column:name"`
	Account       string    `gorm:"column:account"`
	ExpiresAt     time.Time `gorm:"column:expires_at"`
}

func (accountChoice) TableName() string { return "login_account_choice" }

// PutChoices replaces every parked choice and any pending attempt for the
// auth request with choices, in one transaction: a fresh password step
// supersedes whatever an earlier one left, exactly as Put does.
func (s *loginAttemptStore) PutChoices(ctx context.Context, authRequestID string, choices []accountChoice) error {
	return s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Delete(&accountChoice{}, "auth_request_id = ?", authRequestID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&loginAttemptRow{}, "auth_request_id = ?", authRequestID).Error; err != nil {
			return err
		}
		return tx.Create(&choices).Error
	})
}

// TakeChoice removes every parked choice for the auth request and returns the
// chosen one with its siblings, in ONE statement, so two concurrent picks
// cannot both resume a session. It deletes nothing when tenantID is not a
// live choice: a wrong pick must not destroy the chooser the person is
// looking at. Expired choices are never returned.
func (s *loginAttemptStore) TakeChoice(ctx context.Context, authRequestID, tenantID string) (accountChoice, []accountChoice, error) {
	var taken []accountChoice
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`
			DELETE FROM login_account_choice
			WHERE auth_request_id = ?
			  AND EXISTS (SELECT 1 FROM login_account_choice
			              WHERE auth_request_id = ? AND tenant_id::text = ? AND expires_at > now())
			RETURNING auth_request_id, tenant_id, zitadel_session_id, zitadel_session_token,
			          subject, name, account, expires_at`,
			authRequestID, authRequestID, tenantID).Scan(&taken).Error
	})
	if err != nil {
		return accountChoice{}, nil, fmt.Errorf("take account choice: %w", err)
	}
	var chosen accountChoice
	var siblings []accountChoice
	for _, c := range taken {
		if c.TenantID == tenantID {
			chosen = c
		} else {
			siblings = append(siblings, c)
		}
	}
	if chosen.TenantID == "" || !chosen.ExpiresAt.After(time.Now()) {
		return accountChoice{}, siblings, errAttemptNotFound
	}
	return chosen, siblings, nil
}
