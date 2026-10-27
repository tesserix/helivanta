package pharmacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	pharmacy "github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // self-import for external module acceptance tests
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPharmacyOperationalRolesHaveSeparateCapabilities(t *testing.T) {
	grants := pharmacy.New().Permissions()
	tests := []struct {
		role    authz.Role
		allowed []authz.Permission
	}{
		{authz.RolePharmacyOwner, []authz.Permission{pharmacy.PermAccountsRead, pharmacy.PermAccountsWrite, pharmacy.PermAccountsPayment, pharmacy.PermAccountsAdjust, pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermStockCorrect, pharmacy.PermStockQuarantine, pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermTaxManage, pharmacy.PermLicenceManage, pharmacy.PermLicenceReview, pharmacy.PermScheduleDeclare, pharmacy.PermScheduleReview, pharmacy.PermSaleRead}},
		{authz.RolePharmacyPurchasing, []authz.Permission{pharmacy.PermAccountsRead, pharmacy.PermAccountsWrite, pharmacy.PermStockRead, pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite}},
		{authz.RolePharmacyInventory, []authz.Permission{pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermStockCorrect, pharmacy.PermStockQuarantine, pharmacy.PermCatalogueRead}},
		{authz.RoleSupplyFinance, nil},
		{authz.RoleSupplyInventory, nil},
	}
	for _, test := range tests {
		t.Run(string(test.role), func(t *testing.T) {
			var actual []authz.Permission
			for _, grant := range grants {
				for _, role := range grant.Roles {
					if role == test.role {
						actual = append(actual, grant.Permission)
					}
				}
			}
			require.ElementsMatch(t, test.allowed, actual)
			require.NotContains(t, actual, pharmacy.PermDispenseRead)
			require.NotContains(t, actual, pharmacy.PermDispenseFulfil)
		})
	}
}
