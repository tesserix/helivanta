package supply_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // self-import of the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func partyBody(version int, name string) string {
	return fmt.Sprintf(`{"expected_version":%d,"code":"TEST-DISTRIBUTOR","record_kind":"counterparty","legal_name":%q,"trading_name":"TEST Trade","business_kind":"distributor","country_code":"IN","jurisdiction":"TEST-MH","registration_number":"TEST-REG-001","corporate_group":"TEST Group","website":"https://supplier.example.test","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, version, name)
}

func partyHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	r, db, _, _ := partySetup(t)
	return r, db
}

func partySetup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	admin := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.party.access.manage", "supply.organization.manage"}
	return testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"owner": testutil.TenantA, "auditor": testutil.TenantA, "manager": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"owner": admin, "manager": admin, "foreign": admin, "auditor": {"supply.party.read", "supply.party.write", "supply.directory.read"}},
		Modules: []platform.Module{supply.New()},
	})
}

func TestPartyCreationIsPrivateDraftAndExactRetry(t *testing.T) {
	r, _ := partyHarness(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	body := partyBody(0, "TEST Synthetic Distributor")
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"onboarding_status":"draft"`)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"legal_name":"TEST Synthetic Distributor"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", body).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
}

func TestPartyAuditorRequiresExplicitGrantAndRevocationIsImmediate(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := partyBody(0, "TEST Private Supplier")
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", body).Code)
	for _, token := range []string{"auditor", "manager", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path, token, "").Code)
	}
	grant := path + "/access/user-auditor/auditor"
	w := testutil.Do(r, "PUT", grant, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", grant, "owner", "").Body.String())
	require.Equal(t, 200, testutil.Do(r, "GET", path, "auditor", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "auditor", partyBody(1, "TEST Unauthorized Edit")).Code)
	require.Equal(t, 403, testutil.Do(r, "PUT", grant, "auditor", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "auditor", "").Code)
}

func TestPartyFutureVersionCannotMasqueradeAsAnIdenticalRetry(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", partyBody(0, "TEST Versioned Supplier")).Code)
	w := testutil.Do(r, "PUT", path, "owner", partyBody(99, "TEST Versioned Supplier"))
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestSupplierDirectoryOmitsPrivateFieldsAndFiltersByCurrentGrants(t *testing.T) {
	r, _ := partyHarness(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", partyBody(0, "TEST Private Supplier")).Code)
	other := strings.ReplaceAll(partyBody(0, "TEST Another Supplier"), "TEST-DISTRIBUTOR", "TEST-OTHER")
	other = strings.ReplaceAll(other, "TEST-REG-001", "TEST-REG-002")
	require.Equal(t, 200, testutil.Do(r, "PUT", "/v1/supply/parties/"+uuid.Must(uuid.NewV7()).String(), "owner", other).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path+"/access/user-auditor/auditor", "owner", "").Code)
	w := testutil.Do(r, "GET", "/v1/supply/parties?limit=1", "manager", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "registration_number")
	require.NotContains(t, w.Body.String(), "default_credit_days")
	var page struct {
		Data []struct{ ID string }
		Page struct {
			HasMore bool `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.True(t, page.Page.HasMore)
	w = testutil.Do(r, "GET", "/v1/supply/parties", "auditor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, id, page.Data[0].ID)
	w = testutil.Do(r, "GET", "/v1/supply/parties", "foreign", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"data":[]`)
}

func TestPartyStructuredContactAddressAndTaxRecordsRoundTripPrivately(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := structuredPartyBody(0, "TEST Structured Supplier")
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"email":"purchasing@example.test"`)
	require.Contains(t, w.Body.String(), `"line1":"TEST Registered Office"`)
	require.Contains(t, w.Body.String(), `"number":"TEST-TAX-001"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", body).Body.String())
	directory := testutil.Do(r, "GET", "/v1/supply/parties", "manager", "")
	require.NotContains(t, directory.Body.String(), "purchasing@example.test")
	require.NotContains(t, directory.Body.String(), "TEST-TAX-001")
}

func structuredPartyBody(version int, name string) string {
	contact, address, tax := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	return strings.TrimSuffix(strings.Replace(partyBody(version, name), `,"contacts":[],"addresses":[],"tax_registrations":[]`, "", 1), "}") + fmt.Sprintf(`,"contacts":[{"id":%q,"name":"TEST Purchasing Contact","role":"purchasing","email":"purchasing@example.test","phone":"+12025550101","alternate_phone":""}],"addresses":[{"id":%q,"kind":"registered","line1":"TEST Registered Office","line2":"","city":"TEST City","region":"TEST Region","postal_code":"TEST-001","country_code":"IN"}],"tax_registrations":[{"id":%q,"kind":"gstin","number":"TEST-TAX-001","jurisdiction":"TEST-MH"}]}`, contact, address, tax)
}

func TestPartyHeaderUpdateDoesNotRewriteUnchangedContactRecords(t *testing.T) {
	r, db := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := structuredPartyBody(0, "TEST Stable Contacts")
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", body).Code)
	updated := strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), "TEST Stable Contacts", "TEST New Legal Name", 1)
	w := testutil.Do(r, "PUT", path, "owner", updated)
	require.Equal(t, 200, w.Code, w.Body.String())
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("resource_type=?", "supply_party_contacts").Count(&count).Error
	}))
	require.EqualValues(t, 1, count, "unchanged contacts must not be deleted and recreated when only a profile header changes")
}

func TestPartyMissingOrForeignUpdateDoesNotRevealExistence(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "owner", partyBody(1, "TEST Missing Supplier")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "foreign", partyBody(0, "TEST Foreign Supplier")).Code)
	w := testutil.Do(r, "PUT", path, "owner", partyBody(1, "TEST Unauthorized Update"))
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Contains(t, testutil.Do(r, "GET", path, "foreign", "").Body.String(), `"legal_name":"TEST Foreign Supplier"`)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
}

func TestPartyRejectsOversizedBodyWithPayloadTooLarge(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", path, "owner", partyBody(0, strings.Repeat("x", 32768)))
	require.Equal(t, 413, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
}

func TestPartyRevisionHistoryIsPagedPrivateAndRevocationAware(t *testing.T) {
	r, _ := partyHarness(t)
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", partyBody(0, "TEST Original Supplier")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", partyBody(1, "TEST Revised Supplier")).Code)
	w := testutil.Do(r, "GET", path+"/revisions?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct {
			Version      int
			ActorSubject string `json:"actor_subject"`
			Snapshot     struct {
				LegalName string `json:"legal_name"`
			}
		}
		Page struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 2, page.Data[0].Version)
	require.Equal(t, "user-owner", page.Data[0].ActorSubject)
	require.Equal(t, "TEST Revised Supplier", page.Data[0].Snapshot.LegalName)
	require.True(t, page.Page.HasMore)
	w = testutil.Do(r, "GET", path+"/revisions?limit=1&cursor="+page.Page.NextCursor, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 1, page.Data[0].Version)
	require.Equal(t, "TEST Original Supplier", page.Data[0].Snapshot.LegalName)
	require.False(t, page.Page.HasMore)
	for _, token := range []string{"auditor", "manager", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path+"/revisions", token, "").Code)
	}
	grant := path + "/access/user-auditor/auditor"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path+"/revisions", "auditor", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/revisions", "auditor", "").Code)
}

func TestSupplierDirectorySearchesAllPagesAndTreatsWildcardsLiterally(t *testing.T) {
	r, _ := partyHarness(t)
	for i, name := range []string{"TEST Alpha Supplier", "TEST Beta Supplier", "TEST Percent% Supplier"} {
		body := strings.ReplaceAll(partyBody(0, name), "TEST-DISTRIBUTOR", fmt.Sprintf("TEST-SUPPLIER-%d", i))
		body = strings.ReplaceAll(body, "TEST-REG-001", fmt.Sprintf("TEST-REG-%d", i))
		require.Equal(t, 200, testutil.Do(r, "PUT", "/v1/supply/parties/"+uuid.Must(uuid.NewV7()).String(), "owner", body).Code)
	}
	for _, q := range []string{"test alpha", "TEST-SUPPLIER-0", "test percent%"} {
		w := testutil.Do(r, "GET", "/v1/supply/parties?limit=1&q="+url.QueryEscape(q), "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct {
				LegalName string `json:"legal_name"`
			}
			Page struct {
				HasMore bool `json:"has_more"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.False(t, page.Page.HasMore)
		if strings.Contains(q, "alpha") || strings.Contains(q, "-0") {
			require.Equal(t, "TEST Alpha Supplier", page.Data[0].LegalName)
		}
	}
	w := testutil.Do(r, "GET", "/v1/supply/parties?q=%25", "owner", "")
	require.Contains(t, w.Body.String(), `"data":[]`)
	require.Equal(t, 400, testutil.Do(r, "GET", "/v1/supply/parties?q="+strings.Repeat("x", 201), "owner", "").Code)
}
