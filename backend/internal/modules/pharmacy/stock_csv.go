package pharmacy

import (
	"strconv"
	"time"

	"github.com/tesserix/helivanta/pkg/csvsafe"
)

const stockExportByteLimit = 8 * 1024 * 1024

func encodeStockCSV(rows []stockPosition, asOf time.Time) ([]byte, error) {
	w := csvsafe.NewWriter(stockExportByteLimit)
	if err := w.Write([]string{"snapshot_as_of_utc", "location_id", "location_code", "location_name", "zone_code", "aisle_code", "rack_code", "shelf_code", "product_id", "sku", "brand_name", "unit_of_issue", "batch_id", "lot_number", "expires_on", "expiry_time_zone", "expiry_local_date", "days_to_expiry", "expiry_band", "stock_status", "product_status", "regulatory_status", "on_hand_units", "receipt_units", "correction_units", "dispensable_units"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write([]string{asOf.UTC().Format(time.RFC3339Nano), row.LocationID.String(), csvsafe.Text(row.LocationCode), csvsafe.Text(row.LocationName), csvsafe.Text(row.StorageAddress.ZoneCode), csvsafe.Text(row.StorageAddress.AisleCode), csvsafe.Text(row.StorageAddress.RackCode), csvsafe.Text(row.StorageAddress.ShelfCode), row.ProductID.String(), csvsafe.Text(row.SKU), csvsafe.Text(row.BrandName), csvsafe.Text(row.UnitOfIssue), row.BatchID.String(), csvsafe.Text(row.LotNumber), row.ExpiresOn, row.ExpiryTimeZone, row.ExpiryLocalDate, strconv.Itoa(row.DaysToExpiry), row.ExpiryBand, row.StockStatus, row.ProductStatus, row.RegulatoryStatus, strconv.FormatInt(row.OnHandUnits, 10), row.ReceiptUnits, row.CorrectionUnits, strconv.FormatInt(row.DispensableUnits, 10)}); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
