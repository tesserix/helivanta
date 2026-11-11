package workforce

import (
	"encoding/json"
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
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
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
	staffID := uuid.Must(uuid.NewV7()).String()
	inPharmacy := func(fn func(*gorm.DB) error) error {
		return db.WithTenant(t.Context(), establishmentRaceTenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, pharmacy).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	require.NoError(t, inPharmacy(func(tx *gorm.DB) error {
		for _, insert := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Race Pharmacy','pharmacy','independent')`, []any{establishmentRaceTenant, pharmacy}},
			{`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-admin','manager')`, []any{establishmentRaceTenant, pharmacy}},
			{`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-PHARMACY','Synthetic pharmacist','pharmacist','active')`, []any{establishmentRaceTenant, pharmacy, staffID}},
		} {
			if err := tx.Exec(insert.sql, insert.args...).Error; err != nil {
				return err
			}
		}
		return nil
	}))

	gin.SetMode(gin.TestMode)
	h := &handlers{db: db}
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant})
		c.Next()
	})
	e.PUT("/hospitals/:hospital", h.putHospital)
	e.PUT("/hospitals/:hospital/access/:subject", h.putAccess)
	e.DELETE("/hospitals/:hospital/access/:subject", h.deleteAccess)
	e.PUT("/hospitals/:hospital/staff/:id", h.putStaff)
	e.GET("/hospitals/:hospital/staff/:id", h.getStaff)
	e.GET("/hospitals/:hospital/staff", func(c *gin.Context) {
		if rows, err := h.listStaff(c, pagination.Params{Limit: 10}); !c.IsAborted() {
			c.JSON(200, gin.H{"rows": len(rows), "failed": err != nil})
		}
	})
	for _, call := range []struct{ method, path, body string }{
		{"PUT", "/hospitals/" + pharmacy, `{"name":"Synthetic Race Pharmacy"}`},
		{"PUT", "/hospitals/" + pharmacy, `{"name":"Synthetic Renamed Pharmacy"}`},
		{"DELETE", "/hospitals/" + pharmacy + "/access/user-admin", ""},
		{"PUT", "/hospitals/" + pharmacy + "/staff/" + uuid.Must(uuid.NewV7()).String(), `{"employee_code":"SYN-RACE","display_name":"Synthetic race","job_category":"pharmacist","employment_status":"active"}`},
		{"PUT", "/hospitals/" + pharmacy + "/staff/" + staffID, `{"employee_code":"SYN-PHARMACY","display_name":"Synthetic renamed","job_category":"pharmacist","employment_status":"inactive"}`},
		{"GET", "/hospitals/" + pharmacy + "/staff/" + staffID, ""},
		{"GET", "/hospitals/" + pharmacy + "/staff", ""},
		{"PUT", "/hospitals/" + pharmacy + "/access/user-admin", `{"level":"reader"}`},
		{"PUT", "/hospitals/" + pharmacy + "/access/user-other", `{"level":"manager"}`},
	} {
		t.Run(call.method+" "+call.path[len("/hospitals/")+len(pharmacy):]+" "+call.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(call.method, call.path, strings.NewReader(call.body)))
			require.Equal(t, 404, w.Code, w.Body.String())
		})
	}
	var registry struct{ Name, EstablishmentKind string }
	var access, staffRows []string
	require.NoError(t, inPharmacy(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT name,establishment_kind FROM workforce_hospitals WHERE tenant_id=? AND id=?`, establishmentRaceTenant, pharmacy).Scan(&registry).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT subject||':'||level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? ORDER BY subject`, establishmentRaceTenant, pharmacy).Scan(&access).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT id::text||':'||display_name||':'||employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=?`, establishmentRaceTenant, pharmacy).Scan(&staffRows).Error
	}))
	require.Equal(t, struct{ Name, EstablishmentKind string }{"Synthetic Race Pharmacy", "pharmacy"}, registry)
	require.Equal(t, []string{"user-admin:manager"}, access)
	require.Equal(t, []string{staffID + ":Synthetic pharmacist:active"}, staffRows)

	// The read statement's own filter is what keeps a registration landing after refuseOtherKind out of the hospital family.
	for kind, want := range map[string]int{hospitalEstablishmentKind: 0, pharmacyEstablishmentKind: 1} {
		var read []staff
		require.NoError(t, inPharmacy(func(tx *gorm.DB) error {
			return establishment{kind: kind, id: uuid.MustParse(pharmacy)}.staffRows(tx, establishmentRaceTenant).Select(staffColumns).Find(&read).Error
		}))
		require.Len(t, read, want, kind)
	}
}

// The enrollment core re-checks the admitted kind, so a hospital request reaching a pharmacy after admission writes nothing.
func TestEnrollmentCoreRefusesAPharmacyRegisteredAfterAdmission(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), slices.Concat(tenantdb.Migrations(), events.Migrations(), audit.Migrations(), New().Migrations())))
	pharmacy := uuid.Must(uuid.NewV7())
	staffID := uuid.Must(uuid.NewV7())
	p := authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant}
	h := &handlers{db: db}
	require.NoError(t, h.transaction(t.Context(), p, pharmacy, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Race Pharmacy','pharmacy','independent')`, establishmentRaceTenant, pharmacy).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-admin','manager')`, establishmentRaceTenant, pharmacy).Error
	}))
	authority := func(fn func(*gorm.DB, bool) error) error {
		return h.transaction(t.Context(), p, pharmacy, func(tx *gorm.DB) error {
			if err := enrollmentScope(tx, p, pharmacy); err != nil {
				return err
			}
			return fn(tx, false)
		})
	}
	input := func() *enrollmentInput {
		var in enrollmentInput
		require.NoError(t, json.Unmarshal([]byte(`{"expected_version":0,"staff":{"employee_code":"SYN-RACE","display_name":"Synthetic race","job_category":"pharmacist","employment_status":"active"},"profile":{"designation":"Synthetic pharmacist"}}`), &in))
		require.True(t, in.normalizeAndValidate(staffID))
		return &in
	}
	rows := func() []string {
		var found []string
		require.NoError(t, h.transaction(t.Context(), p, pharmacy, func(tx *gorm.DB) error {
			if err := enrollmentScope(tx, p, pharmacy); err != nil {
				return err
			}
			return tx.Raw(`SELECT 'staff' FROM workforce_staff WHERE tenant_id=? AND hospital_id=? UNION ALL SELECT 'profile' FROM workforce_enrollment_profile WHERE tenant_id=? AND hospital_id=?
			 UNION ALL SELECT action FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND action LIKE 'workforce.enrollment.%' ORDER BY 1`, establishmentRaceTenant, pharmacy, establishmentRaceTenant, pharmacy, establishmentRaceTenant, pharmacy).Scan(&found).Error
		}))
		return found
	}
	asHospital := establishment{kind: hospitalEstablishmentKind, id: pharmacy}
	_, err = h.writeEnrollmentFor(p, asHospital, staffID, input(), authority)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.Empty(t, rows())

	asPharmacy := establishment{kind: pharmacyEstablishmentKind, id: pharmacy}
	written, err := h.writeEnrollmentFor(p, asPharmacy, staffID, input(), authority)
	require.NoError(t, err, "the admitted kind writes, so the refusal above is the kind check")
	require.Equal(t, "Synthetic pharmacist", written.Profile.Designation)
	require.Equal(t, []string{"profile", "staff", "workforce.enrollment.changed"}, rows())

	_, err = h.readEnrollmentFor(p, asHospital, staffID, authority)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	read, err := h.readEnrollmentFor(p, asPharmacy, staffID, authority)
	require.NoError(t, err)
	require.Equal(t, written, read)
	require.Equal(t, []string{"profile", "staff", "workforce.enrollment.changed", "workforce.enrollment.read"}, rows())
}

// Every employment period and evidence command passes authorized, so a hospital route on an ID since registered as a pharmacy reads and writes nothing.
func TestEmploymentCoreRefusesAPharmacyRegisteredAfterAdmission(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), slices.Concat(tenantdb.Migrations(), events.Migrations(), audit.Migrations(), New().Migrations())))
	pharmacy := uuid.Must(uuid.NewV7())
	staffID := uuid.Must(uuid.NewV7())
	p := authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant}
	h := &handlers{db: db}
	scoped := func(fn func(*gorm.DB) error) error {
		return h.transaction(t.Context(), p, pharmacy, func(tx *gorm.DB) error {
			if err := enrollmentScope(tx, p, pharmacy); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	require.NoError(t, h.transaction(t.Context(), p, pharmacy, func(tx *gorm.DB) error {
		for _, insert := range []string{
			`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(@tenant,@pharmacy,'Synthetic Race Pharmacy','pharmacy','independent')`,
			`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(@tenant,@pharmacy,'user-admin','manager')`,
		} {
			if err := tx.Exec(insert, map[string]any{"tenant": establishmentRaceTenant, "pharmacy": pharmacy}).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, scoped(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-RACE','Synthetic pharmacist','pharmacist','active')`, establishmentRaceTenant, pharmacy, staffID).Error
	}))
	read := func(kind string) error {
		r := employmentRecord{p: p, owner: establishment{kind: kind, id: pharmacy}, staff: staffID, authority: func(_ []authz.Permission, fn func(*gorm.DB) error) error { return scoped(fn) }}
		return r.authorized([]authz.Permission{PermEnrollmentManage}, func(tx *gorm.DB, source EmploymentPeriodSource) error {
			return employmentPeriodAudit(tx, p, pharmacy, staffID, "workforce.employment_source.read", source.StaffVersion)
		})
	}
	audits := func() int64 {
		var found int64
		require.NoError(t, scoped(func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND action='workforce.employment_source.read'`, establishmentRaceTenant, pharmacy).Scan(&found).Error
		}))
		return found
	}
	require.ErrorIs(t, read(hospitalEstablishmentKind), gorm.ErrRecordNotFound)
	require.Zero(t, audits())
	require.NoError(t, read(pharmacyEstablishmentKind), "the admitted kind reads, so the refusal above is the kind check")
	require.EqualValues(t, 1, audits())
}

// Each document gate re-checks its own kind, so a file route admitted for one family never reaches the other's Staff.
func TestDocumentGatesRefuseTheOtherEstablishmentKind(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), slices.Concat(tenantdb.Migrations(), events.Migrations(), audit.Migrations(), New().Migrations())))
	p := authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant}
	h := &handlers{db: db}
	register := func(kind string) (uuid.UUID, uuid.UUID) {
		id, staffID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		require.NoError(t, h.transaction(t.Context(), p, id, func(tx *gorm.DB) error {
			ownership := map[string]any{hospitalEstablishmentKind: nil, pharmacyEstablishmentKind: "independent"}[kind]
			for _, insert := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Race Establishment',?,?)`, []any{establishmentRaceTenant, id, kind, ownership}},
				{`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-admin','manager')`, []any{establishmentRaceTenant, id}},
				{`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES(?,?,?,'SYN-RACE-DOC','Synthetic race Staff','pharmacist','active')`, []any{establishmentRaceTenant, id, staffID}},
			} {
				if err := tx.Exec(insert.sql, insert.args...).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		return id, staffID
	}
	hospital, hospitalStaff := register(hospitalEstablishmentKind)
	pharmacy, pharmacyStaff := register(pharmacyEstablishmentKind)
	authorize := func(gate documentGate, id, staffID uuid.UUID) error {
		return h.transaction(t.Context(), p, id, func(tx *gorm.DB) error { return gate.authorize(tx, p, id, staffID, true) })
	}
	require.ErrorIs(t, authorize(staffDocuments{}, pharmacy, pharmacyStaff), gorm.ErrRecordNotFound)
	require.ErrorIs(t, authorize(pharmacyStaffDocuments{}, hospital, hospitalStaff), gorm.ErrRecordNotFound)
	require.NoError(t, authorize(staffDocuments{}, hospital, hospitalStaff), "the hospital gate admits its own kind, so the refusal above is the kind check")
	require.NoError(t, authorize(pharmacyStaffDocuments{}, pharmacy, pharmacyStaff), "the pharmacy gate admits its own kind, so the refusal above is the kind check")
	require.False(t, intakeDocumentGate(pharmacyStaffDocuments{}), "a pharmacy file never runs in the HR intake transaction")
}
