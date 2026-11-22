package pharmacy_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestOperatingFulfilmentAuditRequiresExactDutyAndAction(t *testing.T) {
	r, db := catalogueHarness(t)
	operatingStockFixture(t, r)
	order, duty, staff, link := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	insert := func(tx *gorm.DB, action, kind, permission, actor string, resource *uuid.UUID) error {
		return tx.Exec(`INSERT INTO pharmacy_operating_stock_audit(tenant_id,id,pharmacy_id,outlet_id,action,resource_kind,resource_id,permission,duty_id,snapshot_version,authority_epoch,staff_id,staff_version,link_id,link_version,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,3,2,?,1,?,2,?)`, testutil.TenantA, uuid.Must(uuid.NewV7()), pharmaOrganization, pharmaOutlet, action, kind, resource, permission, duty, staff, link, actor).Error
	}
	fulfil := receiverSettings("pharmacy.stock.fulfil")
	require.NoError(t, operatingStockTx(t, db, fulfil, func(tx *gorm.DB) error {
		return insert(tx, "supplier_fulfilment_access", "supplier_order", fulfil.permission, receiverSubject, &order)
	}))
	var retained struct {
		DutyID, StaffID, LinkID uuid.UUID
		SnapshotVersion         int64
		AuthorityEpoch          int64
		Permission              string
	}
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_operating_stock_audit").Select("duty_id,staff_id,link_id,snapshot_version,authority_epoch,permission").Where("resource_id=?", order).Take(&retained).Error
	}))
	require.Equal(t, duty, retained.DutyID)
	require.Equal(t, staff, retained.StaffID)
	require.Equal(t, link, retained.LinkID)
	require.EqualValues(t, 3, retained.SnapshotVersion)
	require.EqualValues(t, 2, retained.AuthorityEpoch)
	require.Equal(t, fulfil.permission, retained.Permission)
	for _, tc := range []struct {
		name, action, kind, permission string
		resource                       *uuid.UUID
	}{
		{"unknown action", "invented_fulfilment", "supplier_order", fulfil.permission, &order},
		{"wrong resource", "supplier_fulfilment_access", "product", fulfil.permission, &order},
		{"missing order", "supplier_fulfilment_access", "supplier_order", fulfil.permission, nil},
		{"read duty cannot fulfil", "supplier_fulfilment_access", "supplier_order", "pharmacy.stock.read", &order},
		{"receiver cannot fulfil", "supplier_fulfilment_access", "supplier_order", "pharmacy.stock.receive", &order},
		{"fulfiller cannot record receipt", "pharmacy.operating_stock.receipt_recorded", "receipt", fulfil.permission, &order},
		{"read cannot claim supplier order", "pharmacy.operating_stock.read", "supplier_order", "pharmacy.stock.read", &order},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireSQLState(t, operatingStockTx(t, db, receiverSettings(tc.permission), func(tx *gorm.DB) error {
				return insert(tx, tc.action, tc.kind, tc.permission, receiverSubject, tc.resource)
			}), "23514")
		})
	}
	for _, scope := range []operatingStockSettings{{}, {subject: "other", access: fulfil.access, pharmacy: fulfil.pharmacy, outlet: fulfil.outlet, permission: fulfil.permission}, {subject: fulfil.subject, access: fulfil.access, pharmacy: uuid.Must(uuid.NewV7()).String(), outlet: fulfil.outlet, permission: fulfil.permission}} {
		requireSQLState(t, operatingStockTx(t, db, scope, func(tx *gorm.DB) error {
			return insert(tx, "supplier_fulfilment_access", "supplier_order", fulfil.permission, receiverSubject, &order)
		}), "42501")
	}
}
