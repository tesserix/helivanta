package pharmacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // External test verifies the existing platform review boundary.
)

func TestHospitalStocktakeChoicesPreservePlatformScheduleReview(t *testing.T) {
	_, db, _, lower := waitingLowering(t)
	approver := approverDB(t, db)
	waiting, err := pharmacy.ListScheduleLowerings(t.Context(), approver)
	require.NoError(t, err)
	require.Len(t, waiting, 1, "the existing platform role must still see its countersigned request")
	require.Equal(t, lower, waiting[0].DeclarationID.String())

	require.NoError(t, approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		tables := []string{"pharmacy_product_schedule_declarations", "pharmacy_product_schedule_countersigns", "pharmacy_product_schedule_reviews", "pharmacy_catalogue_products"}
		for _, table := range tables {
			var visible int64
			require.NoError(t, tx.Table(table).Count(&visible).Error)
			require.Positive(t, visible, table)
		}
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices','{"purpose":"hospital_stocktake_outlet_choices"}',true)`).Error)
		for _, table := range tables {
			var visible int64
			require.NoError(t, tx.Table(table).Count(&visible).Error)
			require.Zero(t, visible, "a hospital directory purpose cannot borrow platform review visibility: %s", table)
		}
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices','',true)`).Error)
		var restored int64
		require.NoError(t, tx.Table("pharmacy_product_schedule_countersigns").Count(&restored).Error)
		require.EqualValues(t, 1, restored)
		return nil
	}))
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approver, waiting[0].DeclarationID, "TEST platform reviewer", true, "TEST countersigned review"))
	waiting, err = pharmacy.ListScheduleLowerings(t.Context(), approver)
	require.NoError(t, err)
	require.Empty(t, waiting)
}
