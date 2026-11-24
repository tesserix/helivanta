package pharmacy

import (
	"errors"

	"gorm.io/gorm"
)

func withNativeStocktakeSnapshot(tx *gorm.DB, action func() error) (err error) {
	var previous string
	if err = tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_stocktake_native_snapshot',true),'')`).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_native_snapshot',?,true)`, previous).Error)
	}()
	if err = tx.Exec(`SELECT set_config('app.pharmacy_stocktake_native_snapshot','1',true)`).Error; err != nil {
		return err
	}
	return action()
}
