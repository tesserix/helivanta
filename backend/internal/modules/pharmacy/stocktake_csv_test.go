package pharmacy

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/csvsafe"
)

func TestStocktakeCSVRetainsControlledSectionsZerosAndCorrections(t *testing.T) {
	zero, shortage, correction := int64(0), int64(-10), int64(-10)
	h1 := "H1"
	reason := "+SUM(1,2)"
	base := stocktakeRegisterRow{StocktakeLine: StocktakeLine{
		ID: uuid.Must(uuid.NewV7()), StocktakeID: uuid.Must(uuid.NewV7()), ProductID: uuid.Must(uuid.NewV7()), ProductVersion: 1,
		SKU: "=HYPERLINK()", BrandName: "TEST Medicine", UnitOfIssue: "tablet", BatchID: uuid.Must(uuid.NewV7()),
		LocationID: uuid.Must(uuid.NewV7()), LocationCode: "TEST-Q", LocationName: "TEST Quarantine", BookUnits: 10,
		LotNumber: "TEST-LOT", ExpiresOn: "2030-12-31", StockStatus: "quarantine", CountedUnits: &zero,
		DifferenceUnits: &shortage, RegulatoryClass: &h1, NdpsStatus: "no", CountReason: &reason,
	}, CreatedAt: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC), AdjustmentUnits: &correction}
	ndps := base
	ndps.ID = uuid.Must(uuid.NewV7())
	ndps.NdpsStatus = "yes"
	unknown := base
	unknown.ID = uuid.Must(uuid.NewV7())
	unknown.RegulatoryClass = nil
	unknown.NdpsStatus = "unknown"
	unknown.CountedUnits, unknown.DifferenceUnits, unknown.AdjustmentUnits = nil, nil, nil
	rows := []stocktakeRegisterRow{unknown, ndps, base}
	data, err := encodeStocktakeCSV(Stocktake{StocktakeSummary: StocktakeSummary{Number: 3, State: "posted", Version: 3}}, rows)
	require.NoError(t, err)
	parsed, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	require.NoError(t, err)
	require.Len(t, parsed, 4)
	entry := func(i int) map[string]string {
		result := map[string]string{}
		for j, name := range parsed[0] {
			result[name] = parsed[i][j]
		}
		return result
	}
	first := entry(1)
	require.Equal(t, "H1", first["register_section"])
	require.Equal(t, "0", first["counted_units"])
	require.Equal(t, "-10", first["difference_units"])
	require.Equal(t, "-10", first["adjustment_units"])
	require.Equal(t, "'=HYPERLINK()", first["sku"])
	require.Equal(t, "'+SUM(1,2)", first["count_reason"])
	require.Equal(t, "2026-11-20T08:00:00Z", first["snapshot_at_utc"])
	require.Equal(t, "NDPS", entry(2)["register_section"])
	require.Equal(t, "unknown", entry(3)["register_section"])
	require.Empty(t, entry(3)["counted_units"])
	require.Empty(t, entry(3)["difference_units"])
	require.Empty(t, entry(3)["adjustment_units"])
	require.Equal(t, "unknown", rows[0].NdpsStatus, "export ordering must not mutate caller input")
}

func TestStocktakeCSVRefusesOversizeWithoutPartialBytes(t *testing.T) {
	reason := strings.Repeat("r", 500)
	rows := make([]stocktakeRegisterRow, 10000)
	for i := range rows {
		rows[i].StocktakeLine = StocktakeLine{SKU: "TEST", BrandName: strings.Repeat("m", 200), CountReason: &reason, NdpsStatus: "unknown"}
		rows[i].ApprovalReason = &reason
	}
	data, err := encodeStocktakeCSV(Stocktake{}, rows)
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestStocktakeRegisterSectionsRetainDeclaredClasses(t *testing.T) {
	for _, tt := range []struct{ class, ndps, section string }{
		{"H1", "no", "H1"}, {"X", "no", "X"}, {"X", "yes", "NDPS"},
		{"H", "no", "ordinary"}, {"none", "no", "ordinary"},
		{"H", "unknown", "unknown"}, {"H1", "unknown", "H1"},
	} {
		t.Run(tt.class+"/"+tt.ndps, func(t *testing.T) {
			line := StocktakeLine{RegulatoryClass: &tt.class, NdpsStatus: tt.ndps}
			require.Equal(t, tt.section, stocktakeRegisterSection(line))
			require.Equal(t, tt.class, *line.RegulatoryClass)
		})
	}
}
