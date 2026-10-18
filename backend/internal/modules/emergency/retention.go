package emergency

import (
	"context"
	"fmt"

	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// PurgeExpiredContacts clears expired caller envelopes without deleting case history.
func PurgeExpiredContacts(ctx context.Context, db *tenantdb.DB, limit int) (int, error) {
	if db == nil || limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("emergency retention requires a database and batch size 1..1000")
	}
	var count int
	err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('statement_timeout','30s',true),set_config('lock_timeout','3s',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`WITH due AS (
 SELECT tenant_id,base_id,id FROM emergency_cases
 WHERE contact_ciphertext IS NOT NULL AND contact_retain_until<=now()
 ORDER BY contact_retain_until,tenant_id,base_id,id LIMIT ? FOR UPDATE SKIP LOCKED
), purged AS (
 UPDATE emergency_cases c SET contact_ciphertext=NULL,contact_purged_at=now(),updated_at=now()
 FROM due d WHERE (c.tenant_id,c.base_id,c.id)=(d.tenant_id,d.base_id,d.id)
 RETURNING c.tenant_id,c.base_id,c.id
), audited AS (
 INSERT INTO emergency_audit(tenant_id,base_id,actor_subject,action,resource_id)
 SELECT tenant_id,base_id,'system:emergency-retention','case.contact_purged',id FROM purged
 RETURNING id
) SELECT count(*) FROM audited`, limit).Scan(&count).Error
	})
	if err != nil {
		return 0, fmt.Errorf("purge expired emergency contacts: %w", err)
	}
	return count, nil
}
