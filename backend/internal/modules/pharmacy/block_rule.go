package pharmacy

import (
	"database/sql"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lotBlockReason asks the one block rule (#1634) whether a lot may leave a sellable shelf on day; "" means it may.
func lotBlockReason(tx *gorm.DB, tenant string, outlet, batch uuid.UUID, day string) (string, error) {
	var reason sql.NullString
	if err := tx.Raw(`SELECT pharmacy_lot_block_reason(?,?,?,?::date)`, tenant, outlet, batch, day).Row().Scan(&reason); err != nil {
		return "", err
	}
	return reason.String, nil
}

// outletStockDay is the outlet's date on the database clock, the date the stock guards use.
func outletStockDay(tx *gorm.DB, tenant, outlet uuid.UUID) (string, error) {
	var day string
	if err := tx.Raw(`SELECT pharmacy_outlet_today(?,?)::text`, tenant, outlet).Row().Scan(&day); err != nil {
		return "", err
	}
	return day, nil
}
