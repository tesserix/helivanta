package drugmaster_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster" //nolint:depguard // own module under test
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"gorm.io/gorm"
)

func TestOutletCatalogueSearchReturnsPublishedPacksAndExactComposition(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("outlet-search", "2020-01-01", "H", "active"))
	source, ok := drugmaster.New().StockProducts(s.db).(stockproduct.CatalogueSource)
	require.True(t, ok)
	for _, query := range []string{"Testmox", "Amoxicillin", "8901234567890"} {
		rows, err := source.SearchCatalogue(t.Context(), query)
		require.NoError(t, err)
		require.Len(t, rows, 1, query)
		require.Equal(t, "30041010", rows[0].HsnCode)
		require.Equal(t, "H", rows[0].RegulatoryClass)
		require.Equal(t, "500", rows[0].Ingredients[0].StrengthValue)
		require.Equal(t, "outlet-search", rows[0].SourceVersion)
		called := false
		require.NoError(t, source.WithCatalogueProduct(t.Context(), rows[0].VersionID, func(_ context.Context, product stockproduct.CatalogueProduct) error {
			called = true
			require.Equal(t, rows[0], product)
			return nil
		}))
		require.True(t, called)
	}
	_, err := source.SearchCatalogue(t.Context(), "a")
	require.Error(t, err)
	require.ErrorIs(t, source.WithCatalogueProduct(t.Context(), uuid.Nil, func(context.Context, stockproduct.CatalogueProduct) error {
		t.Fatal("invalid product reached consumer")
		return nil
	}), stockproduct.ErrIneligible)
}

func TestScheduledReleaseReplayDoesNotCreateOrReopenReleases(t *testing.T) {
	s := newStack(t)
	file := raw(t, amoxicillin("scheduled-replay", "2020-01-01", "H", "active"))
	first, err := drugmaster.StageOnce(t.Context(), s.stager, file, "TEST scheduler")
	require.NoError(t, err)
	second, err := drugmaster.StageOnce(t.Context(), s.stager, file, "TEST scheduler")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, first, nil, "TEST reviewer", false, "TEST source rejected"))
	third, err := drugmaster.StageOnce(t.Context(), s.stager, file, "TEST scheduler")
	require.NoError(t, err)
	require.Equal(t, first, third)
	var status string
	require.NoError(t, s.approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT status FROM drugmaster_releases WHERE id=?`, first).Scan(&status).Error
	}))
	require.Equal(t, "rejected", status)
}

func TestOutletCatalogueKeepsConcentrationForEveryPackOfOneFormulation(t *testing.T) {
	s := newStack(t)
	file := amoxicillin("concentration-packs", "2020-01-01", "H", "active")
	five := json.Number("5")
	unit := "mL"
	file.Formulations[0].Ingredients[0].PerValue = &five
	file.Formulations[0].Ingredients[0].PerUnit = &unit
	second := file.Products[0]
	second.Code = "TEST-SECOND-PACK"
	second.GTIN = nil
	second.PackSize = 20
	file.Products = append(file.Products, second)
	s.publish(t, file)
	source := drugmaster.New().StockProducts(s.db).(stockproduct.CatalogueSource)
	rows, err := source.SearchCatalogue(t.Context(), "Testmox")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Len(t, row.Ingredients, 1)
		require.Equal(t, "500", row.Ingredients[0].StrengthValue)
		require.Equal(t, "5", *row.Ingredients[0].PerValue)
		require.Equal(t, "mL", *row.Ingredients[0].PerUnit)
	}
}
