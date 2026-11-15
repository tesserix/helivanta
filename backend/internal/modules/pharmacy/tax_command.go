package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// taxAbsent stands for an optional field that was not sent, so it never
// hashes the same as any value a client can send.
const taxAbsent = "\x00none"

// taxIntent is the hash of a tax command's normalised fields (#1715).
func taxIntent(fields ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func optionalField(v *string) string {
	if v == nil {
		return taxAbsent
	}
	return *v
}

// priorTaxCommand reports whether id already recorded this exact command
// by this person. The lookup is by tenant and id alone: the same id in
// another outlet or product is still a reused id. Anything that cannot be
// proven the same command, including a row without a hash, fails closed as
// errStockIdentity rather than being recorded again or replayed.
func priorTaxCommand(tx *gorm.DB, table, tenant string, id uuid.UUID, intent, subject string) (bool, error) {
	if err := stockLock(tx, tenant+":tax-command:"+id.String()); err != nil {
		return false, err
	}
	var prior []struct {
		IntentSHA256 *string `gorm:"column:intent_sha256"`
		RecordedBy   string
	}
	if err := tx.Table(table).Select("intent_sha256, recorded_by").Where("tenant_id=? AND id=?", tenant, id).Find(&prior).Error; err != nil {
		return false, err
	}
	if len(prior) == 0 {
		return false, nil
	}
	if prior[0].IntentSHA256 == nil || *prior[0].IntentSHA256 != intent || prior[0].RecordedBy != subject {
		return false, errStockIdentity
	}
	return true, nil
}
