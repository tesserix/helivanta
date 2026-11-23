package pharmacy_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStocktakeHospitalChoicePurposeIsReadOnly(t *testing.T) {
	router, db := stocktakeHarnessForOwnership(t, "hospital_owned")
	stockFixture(t, router)
	second := uuid.Must(uuid.NewV7()).String()
	response := do(router, "PUT", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+second, "editor", `{"code":"TEST-SECOND-CHOICE","name":"TEST unselected outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	hospital, department := uuid.New(), uuid.Must(uuid.NewV7())
	grant := authz.ScopeGrant{AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1, ScopeKind: "department", DepartmentID: department.String()}
	proof, err := json.Marshal(map[string]any{"purpose": "hospital_stocktake_outlet_choices", "permission": "pharmacy.stocktake.read", "tenant_id": testutil.TenantA, "hospital_id": hospital, "department_id": department, "actor_subject": "user-editor", "authenticated_at": time.Now().Add(-time.Minute), "idle_deadline": time.Now().Add(time.Minute), "actor": authz.StaffActorReference{StaffID: uuid.New(), LinkID: uuid.New(), GenerationID: uuid.New(), LinkVersion: 2, EmploymentEpoch: 1}, "operation": grant, "source": grant, "outlet_id": uuid.MustParse(pharmaOutlet), "pharmacy_id": uuid.MustParse(pharmaOrganization)})
	require.NoError(t, err)
	ctx := tenantdb.WithActor(context.Background(), "user-editor")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var before int64
		require.NoError(t, tx.Table("pharmacy_catalogue_products").Count(&before).Error)
		require.Positive(t, before)
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices',?,true)`, string(proof)).Error)
		var valid bool
		require.NoError(t, tx.Raw(`SELECT pharmacy_stocktake_hospital_choices_context(?,?,?)`, testutil.TenantA, hospital, department).Scan(&valid).Error)
		require.True(t, valid)
		var hidden int64
		require.NoError(t, tx.Table("pharmacy_catalogue_products").Count(&hidden).Error)
		require.Zero(t, hidden)
		var outlets []string
		require.NoError(t, tx.Table("pharmacy_outlets").Pluck("id", &outlets).Error)
		require.Equal(t, []string{pharmaOutlet}, outlets)
		err := tx.Transaction(func(inner *gorm.DB) error {
			return inner.Exec(`UPDATE pharmacy_organizations SET name='TEST forbidden choice mutation'WHERE tenant_id=? AND id=?`, testutil.TenantA, pharmaOrganization).Error
		})
		require.Error(t, err, "a projection must never update even its selected organization")
		var denied *pgconn.PgError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "42501", denied.Code)
		deleted := tx.Exec(`DELETE FROM pharmacy_outlets WHERE tenant_id=? AND id=?`, testutil.TenantA, pharmaOutlet)
		require.NoError(t, deleted.Error)
		require.Zero(t, deleted.RowsAffected)
		err = tx.Transaction(func(inner *gorm.DB) error {
			return inner.Exec(`INSERT INTO pharmacy_organizations(tenant_id,id,code,name,ownership_type)VALUES(?,?,'TEST-FORBIDDEN-CHOICE','TEST forbidden choice insert','hospital_owned')`, testutil.TenantA, uuid.Must(uuid.NewV7())).Error
		})
		require.Error(t, err, "projection authority cannot create an organization")
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "42501", denied.Code)
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices','',true)`).Error)
		var after int64
		require.NoError(t, tx.Table("pharmacy_catalogue_products").Count(&after).Error)
		require.Equal(t, before, after)
		var name string
		require.NoError(t, tx.Table("pharmacy_organizations").Select("name").Where("tenant_id=? AND id=?", testutil.TenantA, pharmaOrganization).Scan(&name).Error)
		require.Equal(t, "TEST Count Pharmacy", name)
		return nil
	}))
}
