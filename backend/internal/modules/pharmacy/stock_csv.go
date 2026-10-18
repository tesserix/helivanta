package pharmacy

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var errStockCSVTooLarge = errors.New("stock CSV exceeds the byte limit")

const stockExportByteLimit = 8 * 1024 * 1024

type stockCSVBuffer struct{ bytes.Buffer }

func (b *stockCSVBuffer) Write(data []byte) (int, error) {
	if len(data) > stockExportByteLimit-b.Len() {
		return 0, errStockCSVTooLarge
	}
	return b.Buffer.Write(data)
}

func stockCSVText(value string) string {
	start := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) })
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || (start != "" && strings.ContainsRune("=+-@", rune(start[0]))) {
		return "'" + value
	}
	return value
}

func encodeStockCSV(rows []stockPosition, asOf time.Time) ([]byte, error) {
	var buffer stockCSVBuffer
	w := csv.NewWriter(&buffer)
	w.UseCRLF = true
	if err := w.Write([]string{"snapshot_as_of_utc", "location_id", "location_code", "location_name", "zone_code", "aisle_code", "rack_code", "shelf_code", "product_id", "sku", "brand_name", "unit_of_issue", "batch_id", "lot_number", "expires_on", "expiry_time_zone", "expiry_local_date", "days_to_expiry", "expiry_band", "stock_status", "product_status", "regulatory_status", "on_hand_units", "receipt_units", "correction_units", "dispensable_units"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write([]string{asOf.UTC().Format(time.RFC3339Nano), row.LocationID.String(), stockCSVText(row.LocationCode), stockCSVText(row.LocationName), stockCSVText(row.StorageAddress.ZoneCode), stockCSVText(row.StorageAddress.AisleCode), stockCSVText(row.StorageAddress.RackCode), stockCSVText(row.StorageAddress.ShelfCode), row.ProductID.String(), stockCSVText(row.SKU), stockCSVText(row.BrandName), stockCSVText(row.UnitOfIssue), row.BatchID.String(), stockCSVText(row.LotNumber), row.ExpiresOn, row.ExpiryTimeZone, row.ExpiryLocalDate, strconv.Itoa(row.DaysToExpiry), row.ExpiryBand, row.StockStatus, row.ProductStatus, row.RegulatoryStatus, strconv.FormatInt(row.OnHandUnits, 10), row.ReceiptUnits, row.CorrectionUnits, strconv.FormatInt(row.DispensableUnits, 10)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
