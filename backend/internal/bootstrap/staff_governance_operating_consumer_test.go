package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func exerciseNativeOperatingGovernance(t *testing.T, router *gin.Engine, db *tenantdb.DB, ctx context.Context, kind string, employer, hiddenEmployer, staff, link uuid.UUID) {
	t.Helper()
	call := func(method, path, actor string, input any, status int) string {
		t.Helper()
		raw := ""
		if input != nil {
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			raw = string(encoded)
		}
		response := governanceRequest(router, method, path, actor, raw)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}

	createLocation := func(location uuid.UUID, code string) {
		t.Helper()
		if kind == "pharmacy" {
			call(http.MethodPut, "/v1/pharmacy/pharmacies/"+employer.String()+"/outlets/"+location.String(), "admin", map[string]any{"code": code, "name": "Synthetic native duty outlet", "country_code": "IN", "timezone": "Asia/Kolkata"}, http.StatusOK)
		} else {
			call(http.MethodPut, "/v1/supply/parties/"+employer.String()+"/premises/"+location.String(), "admin", map[string]any{"expected_version": 0, "code": code, "name": "Synthetic native duty warehouse", "kind": "warehouse", "line1": "Synthetic test building", "line2": "", "city": "Bhubaneswar", "region": "IN-OD", "postal_code": "751001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": "active"}, http.StatusOK)
		}
	}
	location, lateLocation := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	lateLocationVersion := int64(1)
	createLocation(location, "SYN-NATIVE-PRIMARY")
	createLocation(lateLocation, "SYN-NATIVE-FAULT")
	template := "pharmacy.inventory_receiver.v1"
	if kind == "supplier" {
		template = "supply.inventory_receiver.v1"
	}

	staffChoicePath := "/v1/workforce/operating-governance-employers/" + kind + "/" + employer.String() + "/staff-choices"
	call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusNotFound)
	locationChoicePath := "/v1/iam/operating-governance-employers/" + kind + "/" + employer.String() + "/location-choices"
	call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusNotFound)
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := iam.GovernanceGrantInput{Subject: "user-auditor", ExpectedMembershipEpoch: 1, TemplateKey: "iam.operating_access_reviewer.v1", ScopeKind: kind, ScopeID: &employer, Reason: "Synthetic independent duty reviewer", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	grantID := uuid.Must(uuid.NewV7())
	grantPath := "/v1/iam/governance-grants/" + grantID.String()
	call(http.MethodPut, grantPath, "admin", grant, http.StatusAccepted)
	call(http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.GovernanceGrantDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "Independent owner approves narrow operating source"}, http.StatusAccepted)
	directory := call(http.MethodGet, "/v1/workforce/operating-authority-scopes?limit=20", "auditor", nil, http.StatusOK)
	var employers struct {
		Data []workforcecontract.OperatingGovernanceEmployerScope `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(directory), &employers))
	require.Len(t, employers.Data, 1)
	require.Equal(t, employer, employers.Data[0].EmployerID)
	require.Equal(t, kind, employers.Data[0].EmployerKind)
	require.NotEmpty(t, employers.Data[0].Name)
	require.True(t, employers.Data[0].CanManage)
	require.True(t, employers.Data[0].CanReview)
	require.NotContains(t, directory, "source_grant_id")
	choices := call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
	require.JSONEq(t, choices, call(http.MethodGet, staffChoicePath+"?limit=20", "owner2", nil, http.StatusOK))
	var selected struct {
		Data []workforcecontract.OperatingGovernanceStaffChoice `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(choices), &selected))
	require.Len(t, selected.Data, 1)
	require.Equal(t, staff, selected.Data[0].StaffID)
	require.Equal(t, link, selected.Data[0].LinkID)
	require.Equal(t, "user-reviewer", selected.Data[0].Subject)
	require.EqualValues(t, 1, selected.Data[0].StaffVersion)
	require.EqualValues(t, 2, selected.Data[0].LinkVersion)
	require.NotEmpty(t, selected.Data[0].DisplayName)
	for _, private := range []string{"source_grant_id", "owner_membership_id", "employee_code", "email", "phone", "job_category"} {
		require.NotContains(t, choices, private)
	}
	call(http.MethodGet, staffChoicePath+"?subject=user-admin", "auditor", nil, http.StatusBadRequest)
	call(http.MethodGet, staffChoicePath+"?status=inactive", "auditor", nil, http.StatusBadRequest)
	call(http.MethodGet, "/v1/workforce/operating-governance-employers/"+kind+"/"+uuid.Must(uuid.NewV7()).String()+"/staff-choices?limit=20", "auditor", nil, http.StatusNotFound)
	call(http.MethodGet, "/v1/workforce/operating-governance-employers/"+kind+"/"+hiddenEmployer.String()+"/staff-choices?limit=20", "auditor", nil, http.StatusNotFound)
	for header, value := range map[string]string{"X-Helivanta-Expected-Tenant": testutil.TenantB, "X-Helivanta-Expected-Subject": "user-owner2"} {
		request := httptest.NewRequest(http.MethodGet, staffChoicePath+"?limit=20", nil)
		request.Header.Set("Authorization", "Bearer auditor")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-auditor")
		request.Header.Set(header, value)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), selected.Data[0].DisplayName)
	}
	auditCount := func() int64 {
		t.Helper()
		var count int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, employer.String()).Error; err != nil {
				return err
			}
			return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.operating_staff_choice.read'", testutil.TenantA, employer, staff).Count(&count).Error
		}))
		return count
	}
	t.Run("Staff choice audit failure rolls back disclosure", func(t *testing.T) {
		before := auditCount()
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_operating_staff_choice_audit", SQL: `CREATE FUNCTION test_operating_staff_choice_audit()RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='workforce.operating_staff_choice.read' THEN RAISE EXCEPTION 'synthetic read audit unavailable';END IF;RETURN NEW;END $$;CREATE TRIGGER test_operating_staff_choice_audit BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION test_operating_staff_choice_audit();`}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_operating_staff_choice_audit_restore", SQL: `DROP FUNCTION test_operating_staff_choice_audit()CASCADE;`}}))
		}()
		denied := call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusServiceUnavailable)
		require.NotContains(t, denied, selected.Data[0].DisplayName)
		require.Equal(t, before, auditCount())
	})
	t.Run("Staff choice purpose cannot modify identity", func(t *testing.T) {
		var proof struct{ Authority json.RawMessage }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, employer.String()).Error; err != nil {
				return err
			}
			return tx.Table("workforce_audit").Select("changes->'authority' AS authority").Where("actor_subject=?", "user-auditor").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.operating_staff_choice.read'", testutil.TenantA, employer, staff).Take(&proof).Error
		}))
		require.NotEmpty(t, proof.Authority)
		attempt := db.WithTenant(tenantdb.WithActor(ctx, "user-auditor"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-auditor',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_operating_selection',?,true)`, employer.String(), string(proof.Authority)).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_staff SET display_name='Synthetic forbidden selection write',version=version+1 WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, employer, staff).Error
		})
		var denied *pgconn.PgError
		require.ErrorAs(t, attempt, &denied)
		require.Contains(t, []string{"42501", "23514"}, denied.Code)
		unchanged := call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
		require.JSONEq(t, choices, unchanged)
	})
	t.Run("Staff choice support fails closed", func(t *testing.T) {
		var retained string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_get_functiondef('public.workforce_operating_selection_staff(uuid,uuid,text)'::regprocedure)`).Scan(&retained).Error
		}))
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_operating_staff_choice_support", SQL: `CREATE OR REPLACE FUNCTION workforce_operating_selection_staff(t uuid,e uuid,state text)RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;`}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_operating_staff_choice_support_restore", SQL: retained}}))
		}()
		call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusServiceUnavailable)
	})
	call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
	t.Run("Staff choice paging binds actor employer and kind", func(t *testing.T) {
		family := "pharmacies"
		if kind == "supplier" {
			family = "suppliers"
		}
		extraStaff, extraLink := uuid.New(), uuid.Must(uuid.NewV7())
		staffRoot := "/v1/workforce/" + family + "/" + employer.String() + "/staff/" + extraStaff.String()
		call(http.MethodPut, staffRoot+"?directory_context=v1", "admin", map[string]any{"employee_code": "SYN-NATIVE-PAGE", "display_name": "Synthetic paging account", "job_category": "pharmacist", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, http.StatusOK)
		linkPath := staffRoot + "/account-links/" + extraLink.String()
		call(http.MethodPut, linkPath, "admin", map[string]any{"expected_staff_version": 1, "subject": "user-auditor", "reason": "Synthetic second selectable Staff account"}, http.StatusCreated)
		call(http.MethodPut, linkPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner2", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent owner checks second account"}, http.StatusCreated)
		var first struct {
			Data []workforcecontract.OperatingGovernanceStaffChoice `json:"data"`
			Page struct {
				HasMore    bool    `json:"has_more"`
				NextCursor *string `json:"next_cursor"`
			} `json:"page"`
		}
		raw := call(http.MethodGet, staffChoicePath+"?status=active&limit=1", "auditor", nil, http.StatusOK)
		require.NoError(t, json.Unmarshal([]byte(raw), &first))
		require.Len(t, first.Data, 1)
		require.Equal(t, extraStaff, first.Data[0].StaffID)
		require.True(t, first.Page.HasMore)
		require.NotNil(t, first.Page.NextCursor)
		cursor := url.QueryEscape(*first.Page.NextCursor)
		next := call(http.MethodGet, staffChoicePath+"?status=active&limit=1&cursor="+cursor, "auditor", nil, http.StatusOK)
		var second struct {
			Data []workforcecontract.OperatingGovernanceStaffChoice `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(next), &second))
		require.Len(t, second.Data, 1)
		require.Equal(t, staff, second.Data[0].StaffID)
		call(http.MethodGet, staffChoicePath+"?status=active&limit=1&cursor="+cursor, "owner2", nil, http.StatusBadRequest)
		call(http.MethodGet, "/v1/workforce/operating-governance-employers/"+kind+"/"+uuid.Must(uuid.NewV7()).String()+"/staff-choices?status=active&limit=1&cursor="+cursor, "auditor", nil, http.StatusBadRequest)
		otherKind := "supplier"
		if kind == "supplier" {
			otherKind = "pharmacy"
		}
		call(http.MethodGet, "/v1/workforce/operating-governance-employers/"+otherKind+"/"+employer.String()+"/staff-choices?status=active&limit=1&cursor="+cursor, "auditor", nil, http.StatusBadRequest)
		for _, query := range []string{"?limit=21", "?limit=1&limit=20", "?status=active&status=active"} {
			call(http.MethodGet, staffChoicePath+query, "auditor", nil, http.StatusBadRequest)
		}
		call(http.MethodPut, staffRoot+"?directory_context=v1", "admin", map[string]any{"employee_code": "SYN-NATIVE-PAGE", "display_name": "Synthetic changed paging Staff", "job_category": "pharmacist", "employment_status": "active", "expected_version": 1, "directory_context_version": "v1"}, http.StatusOK)
		current := call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
		require.NotContains(t, current, extraStaff.String(), "a changed Staff revision cannot reuse its old reviewed link")
		require.JSONEq(t, choices, current)

	})
	t.Run("native location choices retain product ownership", func(t *testing.T) {
		rawChoices := call(http.MethodGet, locationChoicePath+"?country=IN&limit=20", "auditor", nil, http.StatusOK)
		require.JSONEq(t, rawChoices, call(http.MethodGet, locationChoicePath+"?country=IN&limit=20", "owner2", nil, http.StatusOK))
		var page struct {
			Data []iam.OperatingLocationChoice `json:"data"`
			Page struct {
				HasMore    bool    `json:"has_more"`
				NextCursor *string `json:"next_cursor"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal([]byte(rawChoices), &page))
		require.Len(t, page.Data, 2)
		require.Equal(t, lateLocation, page.Data[0].LocationID)
		require.Equal(t, location, page.Data[1].LocationID)
		for _, choice := range page.Data {
			require.Equal(t, testutil.TenantA, choice.OwnerTenantID.String())
			require.Equal(t, kind, choice.EmployerKind)
			require.Equal(t, employer, choice.EmployerID)
			require.Equal(t, "IN", choice.CountryCode)
			require.EqualValues(t, 1, choice.EmployerVersion)
			require.EqualValues(t, 1, choice.LocationVersion)
			require.NotEmpty(t, choice.Code)
			require.NotEmpty(t, choice.Name)
		}
		for _, private := range []string{"source_grant_id", "owner_membership_id", "line1", "postal_code", "stock", "licence", "license", "financial"} {
			require.NotContains(t, rawChoices, private)
		}
		first := call(http.MethodGet, locationChoicePath+"?country=IN&limit=1", "auditor", nil, http.StatusOK)
		var anchor struct {
			Data []iam.OperatingLocationChoice `json:"data"`
			Page struct {
				HasMore    bool    `json:"has_more"`
				NextCursor *string `json:"next_cursor"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal([]byte(first), &anchor))
		require.Len(t, anchor.Data, 1)
		require.Equal(t, lateLocation, anchor.Data[0].LocationID)
		require.True(t, anchor.Page.HasMore)
		require.NotNil(t, anchor.Page.NextCursor)
		cursor := url.QueryEscape(*anchor.Page.NextCursor)
		next := call(http.MethodGet, locationChoicePath+"?country=IN&limit=1&cursor="+cursor, "auditor", nil, http.StatusOK)
		var nextPage struct {
			Data []iam.OperatingLocationChoice `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(next), &nextPage))
		require.Len(t, nextPage.Data, 1)
		require.Equal(t, location, nextPage.Data[0].LocationID)
		call(http.MethodGet, locationChoicePath+"?country=IN&limit=1&cursor="+cursor, "owner2", nil, http.StatusBadRequest)
		call(http.MethodGet, "/v1/iam/operating-governance-employers/"+kind+"/"+hiddenEmployer.String()+"/location-choices?country=IN&limit=1&cursor="+cursor, "auditor", nil, http.StatusBadRequest)
		call(http.MethodGet, "/v1/iam/operating-governance-employers/"+kind+"/"+hiddenEmployer.String()+"/location-choices?limit=20", "auditor", nil, http.StatusNotFound)
		otherKind := "supplier"
		if kind == "supplier" {
			otherKind = "pharmacy"
		}
		call(http.MethodGet, "/v1/iam/operating-governance-employers/"+otherKind+"/"+employer.String()+"/location-choices?country=IN&limit=1&cursor="+cursor, "auditor", nil, http.StatusBadRequest)
		for _, query := range []string{"?limit=21", "?limit=1&limit=20", "?country=PK", "?country=IN&country=IN", "?subject=user-reviewer"} {
			call(http.MethodGet, locationChoicePath+query, "auditor", nil, http.StatusBadRequest)
		}
		module := "pharmacy"
		if kind == "supplier" {
			module = "supply"
		}
		t.Run("selection purpose closes inherited product writes", func(t *testing.T) {
			var owner struct{ ID uuid.UUID }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("iam_members").Select("id").Where("tenant_id=? AND subject='user-admin'AND role_key='tenant_admin'", testutil.TenantA).Take(&owner).Error
			}))
			now := time.Now()
			request := authz.GovernedIndependentEmployerRequest{Principal: authn.Principal{TenantID: testutil.TenantA, Subject: "user-admin", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}, Scope: authz.IndependentEmployerScope{TenantID: testutil.TenantA, EmployerKind: kind, EmployerID: employer}, Capability: authz.GovernanceOperatingManage, Source: authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: owner.ID, OwnerCount: 2, MembershipEpoch: 1, Scope: authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: kind, ID: &employer}, ExpiresAt: now.Add(time.Hour)}}
			proof, err := authz.OperatingLocationChoiceProof(request, now)
			require.NoError(t, err)
			raw, err := json.Marshal(proof)
			require.NoError(t, err)
			table := map[string]string{"pharmacy": "pharmacy_outlets", "supply": "supply_premises"}[module]
			roots := map[string]string{"pharmacy": "pharmacy_organizations", "supply": "supply_parties"}[module]
			attempt := db.WithTenant(tenantdb.WithActor(ctx, "user-admin"), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config(?,?,true)`, "app."+module+"_operating_choices", string(raw)).Error; err != nil {
					return err
				}
				var visible int64
				require.NoError(t, tx.Table(roots).Where("tenant_id=?", testutil.TenantA).Count(&visible).Error)
				require.EqualValues(t, 1, visible, "even a native owner cannot disclose sibling roots through this selected purpose")
				update := `UPDATE ` + table + ` SET name='Synthetic forbidden structural mutation'`
				if module == "supply" {
					update += `,version=version+1`
				}
				return tx.Exec(update+` WHERE tenant_id=? AND id=?`, testutil.TenantA, location).Error
			})
			var pg *pgconn.PgError
			require.ErrorAs(t, attempt, &pg)
			require.Contains(t, []string{"42501", "23514"}, pg.Code)
			require.JSONEq(t, rawChoices, call(http.MethodGet, locationChoicePath+"?country=IN&limit=20", "auditor", nil, http.StatusOK))
		})
		count := func() int64 {
			var total int64
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("audit_events").Where("tenant_id=? AND actor_subject='user-auditor'AND action=?", testutil.TenantA, module+".operating_location_choice.read").Count(&total).Error
			}))
			return total
		}
		before := count()
		require.Positive(t, before)
		apply := func(id, sql string) {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: id, SQL: sql}}))
		}
		t.Run("audit failure rolls back response", func(t *testing.T) {
			apply("test_location_choice_audit", `CREATE FUNCTION test_location_choice_audit()RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN('pharmacy.operating_location_choice.read','supply.operating_location_choice.read')THEN RAISE EXCEPTION 'synthetic location read audit unavailable';END IF;RETURN NEW;END $$;CREATE TRIGGER test_location_choice_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION test_location_choice_audit();`)
			defer apply("test_location_choice_audit_restore", `DROP FUNCTION test_location_choice_audit()CASCADE;`)
			denied := call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusServiceUnavailable)
			require.NotContains(t, denied, lateLocation.String())
			require.Equal(t, before, count())
		})
		t.Run("missing installed selection support", func(t *testing.T) {
			apply("test_location_choice_rls", `ALTER TABLE `+module+map[string]string{"pharmacy": "_outlets", "supply": "_premises"}[module]+` DISABLE ROW LEVEL SECURITY;`)
			defer apply("test_location_choice_rls_restore", `ALTER TABLE `+module+map[string]string{"pharmacy": "_outlets", "supply": "_premises"}[module]+` ENABLE ROW LEVEL SECURITY;`)
			call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusServiceUnavailable)
		})
		call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
		if kind == "supplier" {
			update := func(expected int64, status string) {
				call(http.MethodPut, "/v1/supply/parties/"+employer.String()+"/premises/"+lateLocation.String(), "admin", map[string]any{"expected_version": expected, "code": "SYN-NATIVE-FAULT", "name": "Synthetic native duty warehouse", "kind": "warehouse", "line1": "Synthetic test building", "line2": "", "city": "Bhubaneswar", "region": "IN-OD", "postal_code": "751001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": status}, http.StatusOK)
			}
			update(1, "inactive")
			inactive := call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
			require.NotContains(t, inactive, lateLocation.String())
			update(2, "active")
			lateLocationVersion = 3
			current := call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusOK)
			var refreshed struct {
				Data []iam.OperatingLocationChoice `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(current), &refreshed))
			require.Len(t, refreshed.Data, 2)
			require.Equal(t, lateLocation, refreshed.Data[0].LocationID)
			require.EqualValues(t, 3, refreshed.Data[0].LocationVersion)
		}
	})
	input := iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: kind, EmployerID: employer, LocationID: location, ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}, Subject: "user-reviewer", TemplateKey: template, Reason: "Synthetic native staff receiving duty", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	prefix := "/v1/iam/operating-governance-employers/" + kind + "/" + employer.String()
	duty := uuid.Must(uuid.NewV7())
	path := prefix + "/duties/" + duty.String()

	prepared := call(http.MethodPut, path, "owner2", input, http.StatusCreated)
	func() {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_native_operating_disable_origin_recorder", SQL: "ALTER TABLE iam_operating_duties DISABLE TRIGGER iam_operating_native_record"}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_native_operating_restore_origin_recorder", SQL: "ALTER TABLE iam_operating_duties ENABLE TRIGGER iam_operating_native_record"}}))
		}()
		failed := uuid.Must(uuid.NewV7())
		call(http.MethodPut, prefix+"/duties/"+failed.String(), "owner2", input, http.StatusServiceUnavailable)
		var original struct{ Authority json.RawMessage }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_operating_governance_origins").Select("authority").Where("tenant_id=? AND duty_id=?", testutil.TenantA, duty).Take(&original).Error
		}))
		lateID := uuid.Must(uuid.NewV7())
		lateInput := input
		lateInput.Scope.LocationID = lateLocation
		lateInput.Scope.ExpectedLocationVersion = lateLocationVersion
		body, err := json.Marshal(lateInput)
		require.NoError(t, err)
		lateErr := db.WithTenant(tenantdb.WithActor(ctx, "user-owner2"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_governance_access','authorized',true),set_config('app.iam_operating_command','prepare',true),set_config('app.iam_operating_native_authority',?,true),set_config('app.iam_operating_native_kind',?,true),set_config('app.iam_operating_native_employer',?,true),set_config('app.iam_operating_native_duty',?,true),set_config('app.iam_operating_native_target',?,true)`, string(original.Authority), kind, employer.String(), lateID.String(), input.Subject).Error; err != nil {
				return err
			}
			var lockedEpoch, lockedMembers int64
			if err := tx.Raw(`SELECT epoch,member_count FROM iam_subject_membership_epochs WHERE tenant_id=? AND subject=? AND member_count>0 FOR SHARE`, testutil.TenantA, input.Subject).Row().Scan(&lockedEpoch, &lockedMembers); err != nil {
				return err
			}
			require.EqualValues(t, 1, lockedEpoch)
			if err := tx.Exec(`SAVEPOINT native_epoch_read_only`).Error; err != nil {
				return err
			}
			forbidden := tx.Exec(`UPDATE iam_subject_membership_epochs SET member_count=member_count+1 WHERE tenant_id=? AND subject=?`, testutil.TenantA, input.Subject).Error
			var forbiddenPG *pgconn.PgError
			require.ErrorAs(t, forbidden, &forbiddenPG)
			require.Equal(t, "23514", forbiddenPG.Code)
			require.Equal(t, "membership epoch requires a membership transition", forbiddenPG.Message)
			if err := tx.Exec(`ROLLBACK TO SAVEPOINT native_epoch_read_only`).Error; err != nil {
				return err
			}
			var retainedEpoch, retainedMembers int64
			if err := tx.Raw(`SELECT epoch,member_count FROM iam_subject_membership_epochs WHERE tenant_id=? AND subject=? AND member_count>0 FOR SHARE`, testutil.TenantA, input.Subject).Row().Scan(&retainedEpoch, &retainedMembers); err != nil {
				return err
			}
			require.Equal(t, lockedEpoch, retainedEpoch)
			require.Equal(t, lockedMembers, retainedMembers)
			return tx.Exec(`INSERT INTO iam_operating_duties(tenant_id,id,employer_kind,employer_id,location_id,subject,template_key,membership_epoch,input,prepared_by,status,version,authority_epoch,actor_subject,reason,created_at,updated_at)VALUES(?,?,?,?,?,?,?,1,?::jsonb,?,'pending_review',1,1,?,?,clock_timestamp(),clock_timestamp())`, testutil.TenantA, lateID, kind, employer, lateLocation, input.Subject, input.TemplateKey, string(body), "user-owner2", "user-owner2", input.Reason).Error
		})
		var latePG *pgconn.PgError
		require.ErrorAs(t, lateErr, &latePG)
		require.Equal(t, "23514", latePG.Code)
		require.Equal(t, "iam_operating_native_completion", latePG.ConstraintName)

		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec("SELECT set_config('app.iam_operating_access','authorized',true)").Error)
			for _, rejected := range []uuid.UUID{failed, lateID} {
				for _, table := range []string{"iam_operating_duties", "iam_operating_duty_history", "iam_operating_governance_origins"} {
					key := "duty_id"
					if table == "iam_operating_duties" {
						key = "id"
					}
					var count int64
					require.NoError(t, tx.Table(table).Where("tenant_id=? AND "+key+"=?", testutil.TenantA, rejected).Count(&count).Error)
					require.Zero(t, count)
				}
			}
			return nil
		}))
	}()

	require.NotContains(t, prepared, "owner_membership_id")
	require.NotContains(t, prepared, "source_grant_id")
	require.JSONEq(t, prepared, call(http.MethodPut, path, "owner2", input, http.StatusOK))
	require.JSONEq(t, prepared, call(http.MethodGet, path+"/proposal-receipt", "owner2", nil, http.StatusOK))
	require.Contains(t, call(http.MethodGet, prefix+"/staff/"+staff.String()+"/duties", "auditor", nil, http.StatusOK), duty.String())
	approval := iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "Independent reviewer verifies current employer and Staff"}
	call(http.MethodPut, path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner2", approval, http.StatusConflict)
	command := uuid.Must(uuid.NewV7())
	commandPath := path + "/commands/" + command.String()

	func() {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_native_operating_disable_decision_recorder", SQL: "ALTER TABLE iam_operating_commands DISABLE TRIGGER iam_operating_native_record"}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: "test_native_operating_restore_decision_recorder", SQL: "ALTER TABLE iam_operating_commands ENABLE TRIGGER iam_operating_native_record"}}))
		}()
		call(http.MethodPut, commandPath, "auditor", approval, http.StatusServiceUnavailable)
	}()

	reviewed := call(http.MethodPut, commandPath, "auditor", approval, http.StatusCreated)
	require.Contains(t, reviewed, "approved_pending_activation")
	require.JSONEq(t, reviewed, call(http.MethodPut, commandPath, "auditor", approval, http.StatusOK))
	require.JSONEq(t, reviewed, call(http.MethodGet, commandPath, "auditor", nil, http.StatusOK))
	call(http.MethodGet, "/v1/iam/operating-duties/"+duty.String(), "admin", nil, http.StatusForbidden)
	legacyErr := db.WithTenant(tenantdb.WithActor(ctx, "user-admin"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_operating_command','revoke',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE iam_operating_duties SET status='revoked',version=version+1,authority_epoch=authority_epoch+1,actor_subject='user-admin',reason='A legacy command must not mutate a native duty'WHERE tenant_id=? AND id=?`, testutil.TenantA, duty).Error
	})
	var pg *pgconn.PgError
	require.ErrorAs(t, legacyErr, &pg)
	require.Equal(t, "42501", pg.Code)

	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("SELECT set_config('app.iam_operating_access','authorized',true)").Error)
		var origins, decisions, managers int64
		require.NoError(t, tx.Table("iam_operating_governance_origins").Where("tenant_id=? AND duty_id=?", testutil.TenantA, duty).Count(&origins).Error)
		require.EqualValues(t, 1, origins)
		require.NoError(t, tx.Table("iam_operating_governance_commands").Where("tenant_id=? AND command_id=?", testutil.TenantA, command).Count(&decisions).Error)
		require.EqualValues(t, 1, decisions)
		require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject IN('user-owner2','user-auditor')", testutil.TenantA).Count(&managers).Error)
		require.Zero(t, managers)
		return nil
	}))
	call(http.MethodPut, grantPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", iam.GovernanceGrantDecisionInput{ExpectedVersion: 2, Decision: "revoke", Reason: "Synthetic native source cutoff"}, http.StatusAccepted)
	call(http.MethodGet, locationChoicePath+"?limit=20", "auditor", nil, http.StatusNotFound)
	call(http.MethodGet, staffChoicePath+"?limit=20", "auditor", nil, http.StatusNotFound)
	cutoffDirectory := call(http.MethodGet, "/v1/workforce/operating-authority-scopes?limit=20", "auditor", nil, http.StatusOK)
	require.NotContains(t, cutoffDirectory, employer.String())
	require.Contains(t, cutoffDirectory, `"data":[]`)
	response := governanceRequest(router, http.MethodGet, commandPath, "auditor", "")
	require.True(t, response.Code == http.StatusNotFound || response.Code == http.StatusForbidden, response.Body.String())
	replacement := uuid.Must(uuid.NewV7())
	replacementPath := "/v1/iam/governance-grants/" + replacement.String()
	call(http.MethodPut, replacementPath, "admin", grant, http.StatusAccepted)
	call(http.MethodPut, replacementPath+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.GovernanceGrantDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "New source must not adopt former command receipts"}, http.StatusAccepted)
	call(http.MethodGet, commandPath, "auditor", nil, http.StatusNotFound)
	call(http.MethodPut, commandPath, "auditor", approval, http.StatusNotFound)
}
