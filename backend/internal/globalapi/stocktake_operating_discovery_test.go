package globalapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestStocktakeReviewedReadsDiscoverCurrentDutyLocations(t *testing.T) {
	policy, err := release.New(release.Config{URL: "https://stocktake-directory.test", ClientKey: "sdk-test"})
	require.NoError(t, err)
	var withdrawn atomic.Bool
	f := newPharmacyReceiverFixtureWithAdmission(t, func(method, path string) gin.HandlerFunc {
		admit := policy.Admission(method, path)
		return func(c *gin.Context) {
			if withdrawn.Load() {
				admit(c)
				return
			}
			c.Next()
		}
	})
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	counter := f.duty[len(f.duty)-36:]
	f.activate(t)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_reader.v1"
	reader := f.duty[len(f.duty)-36:]
	f.activate(t)
	withdrawn.Store(true)
	const path = "/v1/iam/me/pharmacy-stocktake-locations"
	type choice struct {
		OwnerTenantID       string   `json:"owner_tenant_id"`
		DutyID              string   `json:"duty_id"`
		EmployerID          string   `json:"employer_id"`
		LocationID          string   `json:"location_id"`
		EmployerName        string   `json:"employer_name"`
		LocationName        string   `json:"location_name"`
		LocationCode        string   `json:"location_code"`
		CountryCode         string   `json:"country_code"`
		TimeZone            string   `json:"time_zone"`
		TemplateKey         string   `json:"template_key"`
		TemplateLabel       string   `json:"template_label"`
		AssignedPermissions []string `json:"assigned_permissions"`
	}
	type page struct {
		Data []choice `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	var first page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"?limit=1", "receiver", nil, 200)), &first))
	require.Len(t, first.Data, 1)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	require.Equal(t, counter, first.Data[0].DutyID)
	require.Equal(t, "pharmacy.stocktake_counter.v1", first.Data[0].TemplateKey)
	require.Equal(t, "Outlet stocktake counter", first.Data[0].TemplateLabel)
	require.ElementsMatch(t, []string{"pharmacy.stocktake.read", "pharmacy.stocktake.count"}, first.Data[0].AssignedPermissions)
	for _, actor := range []string{"owner", "foreign"} {
		f.call("GET", path+"?limit=1&cursor="+url.QueryEscape(*first.Page.NextCursor), actor, nil, 400)
	}
	var second page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"?limit=1&cursor="+url.QueryEscape(*first.Page.NextCursor), "receiver", nil, 200)), &second))
	require.Len(t, second.Data, 1)
	require.False(t, second.Page.HasMore)
	require.Nil(t, second.Page.NextCursor)
	require.Equal(t, reader, second.Data[0].DutyID)
	require.Equal(t, "pharmacy.stocktake_reader.v1", second.Data[0].TemplateKey)
	require.Equal(t, "Outlet stocktake reader", second.Data[0].TemplateLabel)
	require.Equal(t, []string{"pharmacy.stocktake.read"}, second.Data[0].AssignedPermissions)
	for _, row := range []choice{first.Data[0], second.Data[0]} {
		require.Equal(t, testutil.TenantA, row.OwnerTenantID)
		require.Equal(t, f.pharmacy, row.EmployerID)
		require.Equal(t, f.outlet, row.LocationID)
		require.Equal(t, "TEST Independent Pharmacy", row.EmployerName)
		require.Equal(t, "TEST TEST-RX-MAIN", row.LocationName)
		require.Equal(t, "TEST-RX-MAIN", row.LocationCode)
		require.Equal(t, "IN", row.CountryCode)
		require.Equal(t, "Asia/Kolkata", row.TimeZone)
	}
	for _, actor := range []string{"owner", "foreign"} {
		var empty page
		require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, actor, nil, 200)), &empty))
		require.Empty(t, empty.Data)
		require.False(t, empty.Page.HasMore)
		require.Nil(t, empty.Page.NextCursor)
	}
	for _, query := range []string{"?limit=21", "?limit=1&limit=2", "?employer=" + f.pharmacy, "?", "?cursor="} {
		f.call("GET", path+query, "receiver", nil, 400)
	}
	f.checks.down.Store(true)
	unavailable := f.call("GET", path, "receiver", nil, 503)
	require.NotContains(t, unavailable, "TEST Independent Pharmacy")
	require.NotContains(t, unavailable, "TEST TEST-RX-MAIN")
}

func TestStocktakeReviewedReadsPinAdmissionToTheExactDuty(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	counter := uuid.MustParse(f.duty[len(f.duty)-36:])
	f.activate(t)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_reader.v1"
	reader := uuid.MustParse(f.duty[len(f.duty)-36:])
	f.activate(t)
	request := authz.OperatingScopeRequest{Principal: f.principal, EmployerKind: "pharmacy", EmployerID: uuid.MustParse(f.pharmacy), LocationID: uuid.MustParse(f.outlet), Permission: "pharmacy.stocktake.read", DutyID: reader}
	uses := 0
	use := func(r authz.OperatingScopeRequest) error {
		return f.authority.WithOperatingScope(tenantdb.WithActor(f.ctx, f.principal.Subject), r, func(_ context.Context, grant authz.OperatingScopeGrant) error {
			uses++
			if r.DutyID != uuid.Nil {
				require.Equal(t, r.DutyID, grant.DutyID)
			}
			require.Equal(t, r.Permission, grant.Permission)
			return nil
		})
	}
	require.NoError(t, use(request))
	request.Permission = "pharmacy.stocktake.count"
	require.ErrorIs(t, use(request), authz.ErrScopedCommandDenied)
	request.DutyID = counter
	require.NoError(t, use(request))
	request.DutyID = uuid.Nil
	require.NoError(t, use(request))
	request.DutyID = uuid.MustParse(v7())
	require.ErrorIs(t, use(request), authz.ErrScopedCommandDenied)
	request.DutyID = uuid.MustParse("0199e000-0000-4000-8000-000000000001")
	require.ErrorIs(t, use(request), authz.ErrInvalidScopedAssignment)
	request.DutyID = counter
	request.LocationID = uuid.MustParse(f.sibling)
	require.ErrorIs(t, use(request), authz.ErrScopedCommandDenied)
	require.Equal(t, 3, uses)
}

func TestStocktakeReviewedReadsWithholdLocationNamesWhenAuditFails(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	duty := f.duty[len(f.duty)-36:]
	f.activate(t)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stocktake_location_audit_failure", SQL: `CREATE FUNCTION test_stocktake_location_audit_failure()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='iam.operating.location_read'THEN RAISE EXCEPTION 'TEST blocked discovery audit';END IF;RETURN NEW;END$$;CREATE TRIGGER test_stocktake_location_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION test_stocktake_location_audit_failure();`}}))
	const path = "/v1/iam/me/pharmacy-stocktake-locations"
	failed := f.call("GET", path, "receiver", nil, 503)
	require.NotContains(t, failed, "TEST Independent Pharmacy")
	require.NotContains(t, failed, "TEST TEST-RX-MAIN")
	audits := func() int64 {
		var n int64
		require.NoError(t, f.db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM audit_events WHERE tenant_id=? AND actor_subject='user-receiver' AND action='iam.operating.location_read' AND resource_id=?`, testutil.TenantA, duty).Scan(&n).Error
		}))
		return n
	}
	require.Zero(t, audits())
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stocktake_location_audit_restored", SQL: `DROP TRIGGER test_stocktake_location_audit_failure ON audit_events;DROP FUNCTION test_stocktake_location_audit_failure();`}}))
	var success struct {
		Data []struct {
			EmployerName string `json:"employer_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, "receiver", nil, 200)), &success))
	require.Len(t, success.Data, 1)
	require.Equal(t, "TEST Independent Pharmacy", success.Data[0].EmployerName)
	require.EqualValues(t, 1, audits())
}

func TestStocktakeReviewedReadsContinuePastChangedStaffAndLinks(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_reader.v1"
	f.activate(t)
	staff := "/v1/workforce/pharmacies/" + f.pharmacy + "/staff/" + f.staff
	f.call("PUT", staff+"?directory_context=v1", "owner", map[string]any{"employee_code": "TEST-PH-RX", "display_name": "TEST Updated Worker", "job_category": "pharmacist", "employment_status": "active", "expected_version": 1, "directory_context_version": "v1"}, 200)
	oldLink := staff + "/account-links/" + f.input.Identity.LinkID.String()
	f.call("PUT", oldLink+"/commands/"+v7(), "owner", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "TEST retire changed worker basis"}, 201)
	link := v7()
	newLink := staff + "/account-links/" + link
	f.call("PUT", newLink, "owner", map[string]any{"expected_staff_version": 2, "subject": "user-receiver", "reason": "TEST confirm updated worker account"}, 201)
	f.call("PUT", newLink+"/commands/"+v7(), "reviewer", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independently reviewed changed identity"}, 201)
	f.input.Identity.StaffVersion, f.input.Identity.LinkID, f.input.Identity.LinkVersion = 2, uuid.MustParse(link), 2
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_reviewer.v1"
	f.activate(t)
	const path = "/v1/iam/me/pharmacy-stocktake-locations"
	type page struct {
		Data []struct {
			DutyID string `json:"duty_id"`
		} `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	query := path + "?limit=1"
	for i := range 2 {
		var empty page
		require.NoError(t, json.Unmarshal([]byte(f.call("GET", query, "receiver", nil, 200)), &empty))
		require.Empty(t, empty.Data, "changed basis must not be an accessible duty")
		require.True(t, empty.Page.HasMore, "empty candidate page %d must continue", i)
		require.NotNil(t, empty.Page.NextCursor)
		query = path + "?limit=1&cursor=" + url.QueryEscape(*empty.Page.NextCursor)
	}
	var current page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", query, "receiver", nil, 200)), &current))
	require.Len(t, current.Data, 1)
	require.Equal(t, f.duty[len(f.duty)-36:], current.Data[0].DutyID)
	require.False(t, current.Page.HasMore)
	require.Nil(t, current.Page.NextCursor)
	f.call("PUT", f.duty+"/commands/"+v7(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST remove remaining current duty"}, 201)
	var revoked page
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path, "receiver", nil, 200)), &revoked))
	require.Empty(t, revoked.Data)
	require.False(t, revoked.Page.HasMore)
	require.Nil(t, revoked.Page.NextCursor)
}

func TestStocktakeReviewedReadsRequireOneExactAccountHeader(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	for _, tc := range []struct {
		name              string
		tenants, subjects []string
	}{
		{name: "missing"},
		{name: "other tenant", tenants: []string{testutil.TenantB}, subjects: []string{"user-receiver"}},
		{name: "other actor", tenants: []string{testutil.TenantA}, subjects: []string{"user-owner"}},
		{name: "duplicate tenant", tenants: []string{testutil.TenantA, testutil.TenantA}, subjects: []string{"user-receiver"}},
		{name: "duplicate actor", tenants: []string{testutil.TenantA}, subjects: []string{"user-receiver", "user-receiver"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/v1/iam/me/pharmacy-stocktake-locations", nil)
			request.Header.Set("Authorization", "Bearer receiver")
			for _, value := range tc.tenants {
				request.Header.Add("X-Helivanta-Expected-Tenant", value)
			}
			for _, value := range tc.subjects {
				request.Header.Add("X-Helivanta-Expected-Subject", value)
			}
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, request)
			require.Equal(t, 409, response.Code)
			require.NotContains(t, response.Body.String(), "TEST Independent Pharmacy")
		})
	}
}
