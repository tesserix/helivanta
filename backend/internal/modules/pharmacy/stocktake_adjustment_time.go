package pharmacy

import (
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

func withStocktakeAdjustmentTimestamp(tx *gorm.DB, p authn.Principal, outlet, take, command uuid.UUID, action func() error) (err error) {
	var previous string
	if err = tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_stocktake_command_time',true),'')`).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_command_time',?,true)`, previous).Error)
	}()
	result := tx.Exec(`SELECT set_config('app.pharmacy_stocktake_command_time',created_at::text,true)FROM pharmacy_stocktake_commands WHERE tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='approve'AND posted_xact=pg_current_xact_id()AND actor_subject=?`, p.TenantID, outlet, take, command, p.Subject)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return action()
}
