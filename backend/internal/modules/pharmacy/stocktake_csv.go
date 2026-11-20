package pharmacy

import (
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/csvsafe"
)

type stocktakeRegisterRow struct {
	StocktakeLine
	CreatedAt              time.Time
	MovementID             *uuid.UUID
	AdjustmentLocationID   *uuid.UUID
	AdjustmentLocationCode *string
	AdjustmentUnits        *int64
	ApprovedBy             *string
	ApprovedAt             *time.Time
	ApprovalReason         *string
}

func (stocktakeRegisterRow) TableName() string { return "pharmacy_stocktake_lines" }

func stocktakeRegisterSection(line StocktakeLine) string {
	if line.NdpsStatus == "yes" {
		return "NDPS"
	}
	if line.RegulatoryClass != nil && (*line.RegulatoryClass == "H1" || *line.RegulatoryClass == "X") {
		return *line.RegulatoryClass
	}
	if line.RegulatoryClass == nil || line.NdpsStatus != "no" {
		return "unknown"
	}
	return "ordinary"
}

func stocktakeCSVText(value *string) string {
	if value == nil {
		return ""
	}
	return csvsafe.Text(*value)
}

func stocktakeCSVNumber(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func stocktakeCSVUUID(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func stocktakeCSVTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func encodeStocktakeCSV(header Stocktake, input []stocktakeRegisterRow) ([]byte, error) {
	rows := append([]stocktakeRegisterRow(nil), input...)
	order := map[string]int{"H1": 0, "X": 1, "NDPS": 2, "ordinary": 3, "unknown": 4}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := order[stocktakeRegisterSection(rows[i].StocktakeLine)], order[stocktakeRegisterSection(rows[j].StocktakeLine)]
		if a != b {
			return a < b
		}
		return rows[i].ID.String() < rows[j].ID.String()
	})
	w := csvsafe.NewWriter(stockExportByteLimit)
	if err := w.Write([]string{"register_section", "stocktake_number", "stocktake_state", "stocktake_version", "snapshot_at_utc", "stocktake_line_id", "product_id", "product_version", "sku", "brand_name", "unit_of_issue", "batch_id", "lot_number", "expires_on", "counted_location_id", "counted_location_code", "counted_location_name", "stock_status", "book_units", "counted_units", "difference_units", "counted_by_reference", "counted_at_utc", "count_reason", "regulatory_class", "ndps_status", "schedule_declaration_id", "adjustment_movement_id", "adjustment_location_id", "adjustment_location_code", "adjustment_units", "approved_by_reference", "approved_at_utc", "approval_reason"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write([]string{
			stocktakeRegisterSection(row.StocktakeLine), strconv.FormatInt(header.Number, 10), header.State, strconv.FormatInt(header.Version, 10), row.CreatedAt.UTC().Format(time.RFC3339Nano),
			row.ID.String(), row.ProductID.String(), strconv.FormatInt(row.ProductVersion, 10), csvsafe.Text(row.SKU), csvsafe.Text(row.BrandName), csvsafe.Text(row.UnitOfIssue),
			row.BatchID.String(), csvsafe.Text(row.LotNumber), row.ExpiresOn, row.LocationID.String(), csvsafe.Text(row.LocationCode), csvsafe.Text(row.LocationName), row.StockStatus,
			strconv.FormatInt(row.BookUnits, 10), stocktakeCSVNumber(row.CountedUnits), stocktakeCSVNumber(row.DifferenceUnits), stocktakeCSVText(row.CountedBy), stocktakeCSVText(row.CountedAt), stocktakeCSVText(row.CountReason),
			stocktakeCSVText(row.RegulatoryClass), row.NdpsStatus, stocktakeCSVUUID(row.ScheduleDeclarationID), stocktakeCSVUUID(row.MovementID), stocktakeCSVUUID(row.AdjustmentLocationID), stocktakeCSVText(row.AdjustmentLocationCode),
			stocktakeCSVNumber(row.AdjustmentUnits), stocktakeCSVText(row.ApprovedBy), stocktakeCSVTime(row.ApprovedAt), stocktakeCSVText(row.ApprovalReason),
		}); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
