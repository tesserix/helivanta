package iam

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// securityKeys is the iam_security_keys registry (#1898): the keys Zitadel
// verified for a person during Helivanta's own sign-in. Like login_attempt it
// is global identity data, not tenant data, so every query runs through
// WithSystem. subject is always the Zitadel user id read from the verified
// session, never anything the browser sent.
type securityKeys struct {
	db *tenantdb.DB
}

type securityKeyRow struct {
	Subject string `gorm:"column:subject;primaryKey"`
	KeyID   string `gorm:"column:key_id;primaryKey"`
	RPID    string `gorm:"column:rp_id"`
}

func (securityKeyRow) TableName() string { return "iam_security_keys" }

// Add records a verified key. A second Add of the same (subject, key_id) is a
// no-op, so a retried enrolment cannot fail on its own earlier success.
func (r *securityKeys) Add(ctx context.Context, subject, keyID, rpID string) error {
	row := securityKeyRow{Subject: subject, KeyID: keyID, RPID: rpID}
	if err := r.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	}); err != nil {
		return fmt.Errorf("record security key: %w", err)
	}
	return nil
}

// Contains reports whether every id in keyIDs is registered to subject. An
// empty list is false: "no keys to check" must never read as "all checked".
func (r *securityKeys) Contains(ctx context.Context, subject string, keyIDs []string) (bool, error) {
	unique := map[string]struct{}{}
	for _, id := range keyIDs {
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return false, nil
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	var n int64
	if err := r.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Model(&securityKeyRow{}).Where("subject = ? AND key_id IN ?", subject, ids).Count(&n).Error
	}); err != nil {
		return false, fmt.Errorf("read security keys: %w", err)
	}
	return n == int64(len(ids)), nil
}
