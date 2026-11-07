package drugmaster_test

import (
	"context"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster" //nolint:depguard // own module under test
	"github.com/tesserix/helivanta/pkg/stockproduct"
)

func TestStockProductReferenceRequiresCurrentPublishedActiveVersion(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("stock-reference-1", "2020-01-01", "H", "active"))
	status, product := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, 200, status)
	version := uuid.MustParse(product["version_id"].(string))
	status, stock := s.get(t, "/products/by-gtin/8901234567890/stock-reference")
	require.Equal(t, 200, status)
	require.Equal(t, version.String(), stock["version_id"])
	require.Equal(t, "Testmox", stock["brand_name"])
	guard := drugmaster.New().StockProducts(s.db)
	called := false
	err := guard.WithCurrentStockProduct(t.Context(), version, func(_ context.Context, ref stockproduct.Reference) error {
		called = true
		require.Equal(t, version, ref.VersionID)
		require.EqualValues(t, 10, ref.PackSize)
		require.Equal(t, "capsule", ref.UnitOfIssue)
		require.Equal(t, "H", ref.RegulatoryClass)
		require.False(t, ref.Ndps)
		return nil
	})
	require.NoError(t, err)
	require.True(t, called)
	s.publish(t, amoxicillin("stock-reference-recall", todayIST(t), "H", "recalled"))
	called = false
	err = guard.WithCurrentStockProduct(t.Context(), version, func(context.Context, stockproduct.Reference) error { called = true; return nil })
	require.ErrorIs(t, err, stockproduct.ErrIneligible)
	require.False(t, called, "a stale product version must never admit a stock operation after recall")
}

func TestStockProductCommandReferenceHoldsPublicationUntilConsumerCommit(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("stock-command-reference", "2020-01-01", "H", "active"))
	status, product := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, 200, status)
	version := uuid.MustParse(product["version_id"].(string))
	command, ok := drugmaster.New().StockProducts(s.db).(stockproduct.CommandGuard)
	require.True(t, ok, "a stock commitment needs the command-bound source guard")
	tenant := "11111111-1111-1111-1111-111111111111"
	require.ErrorIs(t, command.WithCurrentStockProductInCommand(t.Context(), tenant, version, func(context.Context, stockproduct.Reference) error { return nil }), stockproduct.ErrUnavailable)
	canPublish := func() bool {
		var acquired bool
		// Same serialization key as the owner's publisher, not a consumer's local mutex.
		require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_try_advisory_xact_lock(476001)`).Scan(&acquired).Error
		}))
		return acquired
	}
	require.NoError(t, s.db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "TEST-stock-owner"), tenant, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		require.NoError(t, command.WithCurrentStockProductInCommand(ctx, tenant, version, func(_ context.Context, ref stockproduct.Reference) error {
			require.Equal(t, version, ref.VersionID)
			return nil
		}))
		require.False(t, canPublish(), "the source must remain locked after its callback until the consumer transaction commits")
		return nil
	}))
	require.True(t, canPublish())
}
