package supply_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // External tests exercise this module's own storage purpose.
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// This fixture tests only the owner's storage purpose after admission. It is not
// evidence of real IAM/Staff/graph admission or an authenticated browser journey.
func verifyPartnerShipmentProjection(t *testing.T, db *tenantdb.DB, order supplierorder.SupplierOrder, shipment f.Shipment) {
	body, err := json.Marshal(shipment)
	require.NoError(t, err)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(context.Background(), "system:supplier-shipments"), shipment.SourceTenantID.String(), func(tx *gorm.DB) error { return supply.TestRetainSourceShipment(tx, shipment, body) }))
	grant := authz.PartnerScopeGrant{GrantID: uuid.Must(uuid.NewV7()), RecipientTenantID: shipment.BuyerTenantID.String(), HospitalID: shipment.HospitalID, SourceTenantID: shipment.SourceTenantID.String(), Product: shipment.Product.Owner, SourceOrganizationID: shipment.SourceOrganizationID, SourceLocationID: shipment.SourceLocationID, RelationshipID: order.Input.RelationshipID, BuyerOrganizationID: order.Terms.Buyer.OrganizationID, BuyerLocationID: shipment.BuyerLocationID, TermsSha256: order.Input.TermsSha256, Permission: authz.PartnerShipmentRead, ProjectionHash: strings.Repeat("a", 64)}
	read := func(current authz.PartnerScopeGrant, actor string, expected int64) {
		t.Helper()
		current.ValidUntil = time.Now().Add(30 * time.Second)
		require.NoError(t, db.WithTenant(tenantdb.WithActor(context.Background(), actor), grant.SourceTenantID, func(tx *gorm.DB) error {
			return supply.TestPartnerShipmentFrame(tx, current, "TEST-partner-reader", func() error {
				for _, table := range []string{"supply_purchase_orders", "supply_supplier_dispatched_shipments"} {
					var count int64
					require.NoError(t, tx.Table(table).Count(&count).Error)
					require.Equal(t, expected, count, table)
				}
				for _, table := range []string{"supply_purchase_order_history", "supply_purchase_order_commands"} {
					var count int64
					require.NoError(t, tx.Table(table).Count(&count).Error)
					require.Zero(t, count, table)
				}
				return nil
			})
		}))
	}
	read(grant, "TEST-partner-reader", 1)
	for _, field := range []string{"hospital", "relationship", "terms", "source_organization", "source_location", "source_product", "buyer_organization", "buyer_location", "buyer_tenant"} {
		t.Run("partner projection refuses "+field, func(t *testing.T) {
			changed := grant
			switch field {
			case "hospital":
				changed.HospitalID = uuid.New()
			case "relationship":
				changed.RelationshipID = uuid.Must(uuid.NewV7())
			case "terms":
				changed.TermsSha256 = strings.Repeat("b", 64)
			case "source_organization":
				changed.SourceOrganizationID = uuid.Must(uuid.NewV7())
			case "source_location":
				changed.SourceLocationID = uuid.Must(uuid.NewV7())
			case "source_product":
				changed.Product = "pharmacy"
			case "buyer_organization":
				changed.BuyerOrganizationID = uuid.Must(uuid.NewV7())
			case "buyer_location":
				changed.BuyerLocationID = uuid.Must(uuid.NewV7())
			case "buyer_tenant":
				changed.RecipientTenantID = uuid.NewString()
			}
			read(changed, "TEST-partner-reader", 0)
		})
	}
	read(grant, "TEST-outsider", 0)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(context.Background(), "TEST-partner-reader"), grant.SourceTenantID, func(tx *gorm.DB) error {
		current := grant
		current.ValidUntil = time.Now().Add(30 * time.Second)
		if err := supply.TestPartnerShipmentFrame(tx, current, "TEST-partner-reader", func() error {
			for _, statement := range []string{"UPDATE supply_purchase_orders SET version=version+1 WHERE id=?", "UPDATE supply_supplier_dispatched_shipments SET created_at=created_at WHERE id=?"} {
				target := order.ID
				if strings.Contains(statement, "dispatched_shipments") {
					target = shipment.ID
				}
				require.NoError(t, tx.SavePoint("partner_write_denial").Error)
				result := tx.Exec(statement, target)
				if result.Error != nil {
					var database *pgconn.PgError
					require.ErrorAs(t, result.Error, &database)
					require.Contains(t, []string{"42501", "23514", "P0001"}, database.Code)
					require.NoError(t, tx.RollbackTo("partner_write_denial").Error)
				} else {
					require.Zero(t, result.RowsAffected)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		var restored string
		if err := tx.Raw("SELECT current_setting('app.supply_partner_shipments',true)").Scan(&restored).Error; err != nil {
			return err
		}
		require.Empty(t, restored)
		var hidden int64
		require.NoError(t, tx.Table("supply_purchase_orders").Count(&hidden).Error)
		require.Zero(t, hidden)
		return nil
	}))
}
