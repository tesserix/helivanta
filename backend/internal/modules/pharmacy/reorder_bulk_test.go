package pharmacy_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

type reorderSellableBucket struct {
	ProductID  uuid.UUID
	LocationID uuid.UUID
	BatchID    uuid.UUID
	LotNumber  string
	ExpiresOn  string
	MrpMinor   int64
	OnHand     int64
}

func TestReorderBulkAvailabilityKeepsLotRulesAndProductIsolation(t *testing.T) {
	for _, ownership := range []string{"independent", "hospital_owned"} {
		t.Run(ownership, func(t *testing.T) {
			w := newReleaseWorldForOwnership(t, ownership)
			code, msg := w.release(w.batch, 30)
			require.Equal(t, 200, code, msg)
			shelf := func() string {
				batch := receive(t, w.r, w.product, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
				code, msg := w.release(batch, 10)
				require.Equal(t, 200, code, msg)
				return batch
			}
			expired, unpriced, held := shelf(), shelf(), shelf()
			w.rewriteBatch(t, expired, "expires_on='2026-11-01'")
			w.rewriteBatch(t, unpriced, "mrp_minor=NULL")
			w.incident(t, held, w.sellable)
			second := uuid.Must(uuid.NewV7()).String()
			created := do(w.r, "PUT", productPath(second), "editor", strings.Replace(productBody(0, "TEST Second product"), "TEST-AMOX-500", "TEST-SECOND-500", 1))
			require.Equal(t, 200, created.Code, created.Body.String())
			classify(t, w.r, second)
			approveClass(t, w.r, second, "H", false)
			batch2 := receive(t, w.r, second, w.quarantine, "2030-10-31", `,"mrp_minor":12500`)
			code, msg = w.release(batch2, 7)
			require.Equal(t, 200, code, msg)

			var all []reorderSellableBucket
			require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT product_id,location_id,batch_id,lot_number,expires_on::text,mrp_minor,on_hand
     FROM pharmacy_sellable_product_buckets(?,?,NULL,?::date)ORDER BY product_id,batch_id,location_id`, testutil.TenantA, pharmaOutlet, "2026-11-01").Scan(&all).Error
			}))
			require.Len(t, all, 2, "blocked lots and quarantine never become available")
			amounts := map[uuid.UUID]int64{}
			for _, row := range all {
				require.Equal(t, uuid.MustParse(w.sellable), row.LocationID)
				amounts[row.BatchID] = row.OnHand
			}
			require.Equal(t, map[uuid.UUID]int64{uuid.MustParse(w.batch): 30, uuid.MustParse(batch2): 7}, amounts)
			for _, product := range []string{w.product, second} {
				var single []reorderSellableBucket
				require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
					return tx.Raw(`SELECT ?::uuid product_id,location_id,batch_id,lot_number,expires_on::text,mrp_minor,on_hand
      FROM pharmacy_sellable_buckets(?,?,?,?::date)ORDER BY batch_id,location_id`, product, testutil.TenantA, pharmaOutlet, product, "2026-11-01").Scan(&single).Error
				}))
				var expected []reorderSellableBucket
				for _, row := range all {
					if row.ProductID == uuid.MustParse(product) {
						expected = append(expected, row)
					}
				}
				require.Equal(t, expected, single, "existing product-specific signature must not include another product")
			}
			for _, tc := range []struct {
				label, tenant, outlet string
				product               any
				legacy                bool
			}{
				{"foreign", testutil.TenantB, pharmaOutlet, nil, false},
				{"wrong-outlet", testutil.TenantA, uuid.Must(uuid.NewV7()).String(), nil, false},
				{"unstocked", testutil.TenantA, pharmaOutlet, uuid.Must(uuid.NewV7()).String(), false},
				{"legacy-null-product", testutil.TenantA, pharmaOutlet, nil, true},
			} {
				require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
					var n int64
					function := "pharmacy_sellable_product_buckets"
					if tc.legacy {
						function = "pharmacy_sellable_buckets"
					}
					// Function names above are fixed test constants, never request input.
					if err := tx.Raw(fmt.Sprintf("SELECT count(*) FROM %s(?,?,?::uuid,?::date)", function), tc.tenant, tc.outlet, tc.product, "2026-11-01").Scan(&n).Error; err != nil {
						return err
					}
					require.Zero(t, n, tc.label)
					return nil
				}))
			}
			for _, outlet := range []string{pharmaOutlet, uuid.Must(uuid.NewV7()).String()} {
				err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
					var n int64
					return tx.Raw("SELECT count(*) FROM pharmacy_sellable_product_buckets(?,?,NULL,NULL::date)", testutil.TenantA, outlet).Scan(&n).Error
				})
				require.Equal(t, "22004", sqlState(err), "a null day must not masquerade as zero availability")
			}
		})
	}
}
