package globalapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStocktakeReviewedReadsPageCurrentDutiesAcrossIndependentPharmacyChain(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.input.Reason = "TEST reviewed chain location coverage"
	type expectedLocation struct{ pharmacy, name, code, duty string }
	expected := map[string]expectedLocation{}
	employer := f.pharmacy
	employerName := "TEST Independent Pharmacy"
	for i := 0; i < 23; i++ {
		if i == 8 || i == 16 {
			employer = v7()
			employerName = fmt.Sprintf("TEST Chain Pharmacy %d", i/8+1)
			root := "/v1/pharmacy/pharmacies/" + employer
			f.call("PUT", root, "owner", map[string]any{"code": fmt.Sprintf("TEST-CHAIN-%d", i/8), "name": employerName, "ownership_type": "independent"}, 200)
			workforce := "/v1/workforce/pharmacies/" + employer
			f.call("PUT", workforce, "owner", map[string]any{}, 200)
			for _, subject := range []string{"user-owner", "user-reviewer"} {
				f.call("PUT", workforce+"/access/"+subject, "owner", map[string]any{"level": "manager"}, 200)
			}
			staff, link := v7(), v7()
			staffRoot := workforce + "/staff/" + staff
			f.call("PUT", staffRoot+"?directory_context=v1", "owner", map[string]any{"employee_code": fmt.Sprintf("TEST-CHAIN-STAFF-%d", i/8), "display_name": "TEST Multi-employer Pharmacist", "job_category": "pharmacist", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, 200)
			linkRoot := staffRoot + "/account-links/" + link
			f.call("PUT", linkRoot, "owner", map[string]any{"subject": "user-receiver", "expected_staff_version": 1, "reason": "TEST independently reviewed employer account"}, 201)
			f.call("PUT", linkRoot+"/commands/"+v7(), "reviewer", map[string]any{"decision": "approve", "expected_version": 1, "reason": "TEST verify chain Staff identity"}, 201)
			f.input.Identity.StaffID = uuid.MustParse(staff)
			f.input.Identity.LinkID = uuid.MustParse(link)
		}
		outlet := f.outlet
		code := "TEST-RX-MAIN"
		if i > 0 {
			outlet = v7()
			code = fmt.Sprintf("TEST-CHAIN-OUTLET-%03d", i)
			f.call("PUT", "/v1/pharmacy/pharmacies/"+employer+"/outlets/"+outlet, "owner", map[string]any{"code": code, "name": "TEST Working Outlet " + code, "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
		}
		f.input.Scope.EmployerID = uuid.MustParse(employer)
		f.input.Scope.LocationID = uuid.MustParse(outlet)
		f.duty = "/v1/iam/operating-duties/" + v7()
		f.activate(t)
		expected[outlet] = expectedLocation{employer, employerName, code, strings.TrimPrefix(f.duty, "/v1/iam/operating-duties/")}
	}
	type location struct {
		Tenant      string   `json:"owner_tenant_id"`
		Employer    string   `json:"employer_id"`
		ID          string   `json:"location_id"`
		Name        string   `json:"employer_name"`
		Code        string   `json:"location_code"`
		Duty        string   `json:"duty_id"`
		TimeZone    string   `json:"time_zone"`
		Permissions []string `json:"assigned_permissions"`
	}
	type page struct {
		Data []location `json:"data"`
		Page struct {
			HasMore bool   `json:"has_more"`
			Next    string `json:"next_cursor"`
		} `json:"page"`
	}
	root := "/v1/iam/me/pharmacy-stocktake-locations?limit=20"
	read := func(path string) page {
		t.Helper()
		started := time.Now()
		body := f.call("GET", path, "receiver", nil, 200)
		elapsed := time.Since(started)
		require.Less(t, elapsed, 4*time.Second, "current chain pages must respect the existing shared directory deadline")
		var result page
		require.NoError(t, json.Unmarshal([]byte(body), &result))
		t.Logf("reviewed chain page: rows=%d elapsed=%s", len(result.Data), elapsed)
		return result
	}
	first := read(root)
	require.Len(t, first.Data, 20)
	require.True(t, first.Page.HasMore)
	require.NotEmpty(t, first.Page.Next)
	second := read(root + "&cursor=" + url.QueryEscape(first.Page.Next))
	require.Len(t, second.Data, 3)
	require.False(t, second.Page.HasMore)
	require.Empty(t, second.Page.Next)
	seen := map[string]bool{}
	for _, row := range append(first.Data, second.Data...) {
		require.False(t, seen[row.ID], "a page boundary must not duplicate a current duty")
		seen[row.ID] = true
		ref, ok := expected[row.ID]
		require.True(t, ok)
		require.Equal(t, testutil.TenantA, row.Tenant)
		require.Equal(t, ref.pharmacy, row.Employer)
		require.Equal(t, ref.name, row.Name)
		require.Equal(t, ref.code, row.Code)
		require.Equal(t, ref.duty, row.Duty)
		require.Equal(t, "Asia/Kolkata", row.TimeZone)
		require.ElementsMatch(t, []string{"pharmacy.stocktake.read", "pharmacy.stocktake.count"}, row.Permissions)
	}
	require.Len(t, seen, 23)
}
