package workforce

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const establishmentRaceTenant = "0199e000-0000-7000-8000-0000000000a1"

// A request admitted before a concurrent pharmacy registration reaches the handler without the admission's protection.
func TestHospitalCommandsRefuseAPharmacyRegisteredAfterAdmission(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), slices.Concat(tenantdb.Migrations(), events.Migrations(), audit.Migrations(), New().Migrations())))
	pharmacy := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, db.WithTenant(t.Context(), establishmentRaceTenant, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Race Pharmacy','pharmacy','independent')`, establishmentRaceTenant, pharmacy).Error
	}))

	gin.SetMode(gin.TestMode)
	h := &handlers{db: db}
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant})
		c.Next()
	})
	e.PUT("/hospitals/:hospital", h.putHospital)
	e.DELETE("/hospitals/:hospital/access/:subject", h.deleteAccess)
	for _, call := range []struct{ method, path, body string }{
		{"PUT", "/hospitals/" + pharmacy, `{"name":"Synthetic Race Pharmacy"}`},
		{"PUT", "/hospitals/" + pharmacy, `{"name":"Synthetic Renamed Pharmacy"}`},
		{"DELETE", "/hospitals/" + pharmacy + "/access/user-admin", ""},
	} {
		t.Run(call.method+" "+call.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(call.method, call.path, strings.NewReader(call.body)))
			require.Equal(t, 404, w.Code, w.Body.String())
		})
	}
	var row struct{ Name, EstablishmentKind string }
	require.NoError(t, db.WithTenant(t.Context(), establishmentRaceTenant, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT name,establishment_kind FROM workforce_hospitals WHERE tenant_id=? AND id=?`, establishmentRaceTenant, pharmacy).Scan(&row).Error
	}))
	require.Equal(t, struct{ Name, EstablishmentKind string }{"Synthetic Race Pharmacy", "pharmacy"}, row)
}
