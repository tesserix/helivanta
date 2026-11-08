package workforce

import (
	"context"
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

// raceMembers stands in for IAM membership, so a proposal reaches the transaction under test.
type raceMembers struct{}

func (raceMembers) ListRoles(context.Context, string) ([]authz.RoleBinding, error) {
	return []authz.RoleBinding{{TenantID: establishmentRaceTenant, Role: authz.RoleStaffMember}}, nil
}

// A request admitted before a concurrent pharmacy registration reaches the handler without the admission's protection.
func TestHospitalCommandsRefuseAPharmacyRegisteredAfterAdmission(t *testing.T) {
	appDSN, ownerDSN, _ := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Migrate(t.Context(), slices.Concat(tenantdb.Migrations(), events.Migrations(), audit.Migrations(), New().Migrations())))
	pharmacy := uuid.Must(uuid.NewV7()).String()
	staffID := uuid.Must(uuid.NewV7()).String()
	linkID := uuid.Must(uuid.NewV7()).String()
	inPharmacy := func(fn func(*gorm.DB) error) error {
		return db.WithTenant(t.Context(), establishmentRaceTenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','manage',true)`, pharmacy).Error; err != nil {
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
			{`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason) VALUES(?,?,?,?,'user-candidate','pending',1,'user-admin','Synthetic race link','user-admin','Synthetic race link')`, []any{establishmentRaceTenant, pharmacy, staffID, linkID}},
		} {
			if err := tx.Exec(insert.sql, insert.args...).Error; err != nil {
				return err
			}
		}
		return nil
	}))

	gin.SetMode(gin.TestMode)
	h := &handlers{db: db, roles: raceMembers{}}
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "user-admin", TenantID: establishmentRaceTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(PermLoginLinkManage, PermLoginLinkReview))
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
	links := "/hospitals/:hospital/staff/:id/login-links"
	e.PUT(links+"/:link", h.proposeLoginLink(hospitalLinkEstablishment))
	e.POST(links+"/:link/decision", h.decideLoginLink(hospitalLinkEstablishment))
	e.GET(links, func(c *gin.Context) {
		if rows, err := h.listLoginLinks(hospitalLinkEstablishment)(c, pagination.Params{Limit: 10}); !c.IsAborted() {
			c.JSON(200, gin.H{"rows": len(rows), "failed": err != nil})
		}
	})
	e.GET(links+"/:link/history", func(c *gin.Context) {
		if rows, err := h.loginLinkHistory(hospitalLinkEstablishment)(c, pagination.Params{Limit: 10}); !c.IsAborted() {
			c.JSON(200, gin.H{"rows": len(rows), "failed": err != nil})
		}
	})
	staffLinks := "/hospitals/" + pharmacy + "/staff/" + staffID + "/login-links"
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
		{"POST", staffLinks + "/" + linkID + "/decision", `{"expected_version":1,"decision":"revoke","reason":"Synthetic race revoke"}`},
		{"PUT", staffLinks + "/" + uuid.Must(uuid.NewV7()).String(), `{"subject":"user-other","reason":"Synthetic race proposal"}`},
		{"GET", staffLinks, ""},
		{"GET", staffLinks + "/" + linkID + "/history", ""},
	} {
		t.Run(call.method+" "+call.path[len("/hospitals/")+len(pharmacy):]+" "+call.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(call.method, call.path, strings.NewReader(call.body)))
			require.Equal(t, 404, w.Code, w.Body.String())
		})
	}
	var registry struct{ Name, EstablishmentKind string }
	var access, staffRows, linkRows []string
	var linkEvents, linkReads int64
	require.NoError(t, inPharmacy(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT id::text||':'||status||':'||version FROM workforce_login_links WHERE tenant_id=? AND hospital_id=?`, establishmentRaceTenant, pharmacy).Scan(&linkRows).Error; err != nil {
			return err
		}
		if err := tx.Table("outbox_events").Where("subject IN ?", []string{LoginLinkChanged, PharmacyLoginLinkChanged}).Count(&linkEvents).Error; err != nil {
			return err
		}
		if err := tx.Table("workforce_audit").Where("action='workforce.login_link.read'").Count(&linkReads).Error; err != nil {
			return err
		}
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
	require.Equal(t, []string{linkID + ":pending:1"}, linkRows)
	require.Zero(t, linkEvents)
	require.Zero(t, linkReads)

	// The read statement's own filter is what keeps a registration landing after refuseOtherKind out of the hospital family.
	for kind, want := range map[string]int{hospitalEstablishmentKind: 0, pharmacyEstablishmentKind: 1} {
		var read []staff
		require.NoError(t, inPharmacy(func(tx *gorm.DB) error {
			return establishment{kind: kind, id: uuid.MustParse(pharmacy)}.staffRows(tx, establishmentRaceTenant).Select(staffColumns).Find(&read).Error
		}))
		require.Len(t, read, want, kind)
	}
}
