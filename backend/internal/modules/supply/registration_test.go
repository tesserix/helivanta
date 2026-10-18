package supply_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"
)

func premiseBody(version int, country string) string {
	zone := "Asia/Kolkata"
	if country == "PK" {
		zone = "Asia/Karachi"
	}
	return fmt.Sprintf(`{"expected_version":%d,"code":" test-warehouse ","name":"TEST Warehouse","kind":"warehouse","line1":"TEST Building","line2":"","city":"TEST City","region":"TEST Region","postal_code":"TEST-001","country_code":%q,"time_zone":%q,"status":"active"}`, version, country, zone)
}
func licenseBody(version int, premise string) string {
	return fmt.Sprintf(`{"expected_version":%d,"kind":" wholesale ","number":"test-license-001","issuing_authority":"TEST Authority","jurisdiction":"TEST-MH","premise_id":%q,"issued_on":"2026-01-01","expires_on":"2030-12-31","product_categories":["prescription","otc"]}`, version, premise)
}
func TestPremisesAndLicenceDeclarationsArePrivateVersionedAndCountryBound(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST License Supplier")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	url := root + "/premises/" + premise
	w := testutil.Do(r, "PUT", url, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"code":"TEST-WAREHOUSE"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", url, "owner", premiseBody(0, "IN")).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", url, "owner", "").Body.String())
	for _, actor := range []string{"auditor", "manager", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", url, actor, "").Code)
	}
	bad := testutil.Do(r, "PUT", root+"/premises/"+uuid.Must(uuid.NewV7()).String(), "owner", premiseBody(0, "PK"))
	require.Equal(t, 400, bad.Code, bad.Body.String())
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(r, "PUT", licence, "owner", licenseBody(0, premise))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"number":"TEST-LICENSE-001"`)
	require.Contains(t, w.Body.String(), `"status":"declared"`)
	require.Contains(t, w.Body.String(), `"product_categories":["otc","prescription"]`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", licence, "owner", licenseBody(0, premise)).Body.String())
	read := testutil.Do(r, "GET", licence, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	var projected map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &projected))
	require.Contains(t, projected, "renewal")
	delete(projected, "renewal")
	unchanged, err := json.Marshal(projected)
	require.NoError(t, err)
	require.JSONEq(t, w.Body.String(), string(unchanged))
	for _, actor := range []string{"auditor", "manager", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", licence, actor, "").Code)
	}
	w = testutil.Do(r, "GET", root+"/licenses?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct{ Data []json.RawMessage }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
}

func TestRegistryUpdatesAreOptimisticAndRetainPagedPrivateHistory(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Registry Versions")).Code)
	id := uuid.Must(uuid.NewV7()).String()
	premise := root + "/premises/" + id
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	updated := strings.ReplaceAll(premiseBody(1, "IN"), "TEST Warehouse", "TEST Updated Warehouse")
	w := testutil.Do(r, "PUT", premise, "owner", updated)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", premise, "owner", updated).Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", premise, "owner", premiseBody(1, "IN")).Code)
	future := strings.Replace(updated, `"expected_version":1`, `"expected_version":99`, 1)
	require.Equal(t, 409, testutil.Do(r, "PUT", premise, "owner", future).Code)
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", licence, "owner", licenseBody(0, id)).Code)
	body := strings.ReplaceAll(licenseBody(1, id), "2030-12-31", "2031-12-31")
	w = testutil.Do(r, "PUT", licence, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", licence, "owner", body).Body.String())
	for _, url := range []string{premise, licence} {
		w = testutil.Do(r, "GET", url+"/revisions?limit=1", "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct {
			Data []struct{ Version int }
			Page struct {
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.Equal(t, 2, page.Data[0].Version)
		require.True(t, page.Page.HasMore)
		w = testutil.Do(r, "GET", url+"/revisions?limit=1&cursor="+page.Page.NextCursor, "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.Equal(t, 1, page.Data[0].Version)
		require.False(t, page.Page.HasMore)
		require.Equal(t, 404, testutil.Do(r, "GET", url+"/revisions", "manager", "").Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/licenses/"+uuid.Must(uuid.NewV7()).String()+"/revisions", "owner", "").Code)
}

func TestRegistryScopeValidationAndComplianceRoles(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Registry Roles")).Code)
	premiseID := uuid.Must(uuid.NewV7()).String()
	premise := root + "/premises/" + premiseID
	require.Equal(t, 200, testutil.Do(r, "PUT", premise, "owner", premiseBody(0, "IN")).Code)
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	grant := root + "/access/user-auditor/compliance"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", licence, "auditor", licenseBody(0, premiseID)).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", premise, "auditor", premiseBody(1, "IN")).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", root, "auditor", partyBody(1, "TEST Disallowed Edit")).Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", licence, "auditor", "").Code)
	for name, change := range map[string]func(map[string]any){
		"invalid category":    func(v map[string]any) { v["product_categories"] = []string{"unknown"} },
		"duplicate category":  func(v map[string]any) { v["product_categories"] = []string{"otc", "otc"} },
		"missing categories":  func(v map[string]any) { delete(v, "product_categories") },
		"invalid calendar":    func(v map[string]any) { v["issued_on"] = "2026-02-30" },
		"reversed validity":   func(v map[string]any) { v["expires_on"] = "2025-12-31" },
		"forged verification": func(v map[string]any) { v["status"] = "verified" },
		"forged tenant":       func(v map[string]any) { v["tenant_id"] = testutil.TenantB },
		"old UUID":            func(v map[string]any) { v["premise_id"] = uuid.NewString() },
	} {
		t.Run(name, func(t *testing.T) {
			body := changedPartyBody(t, licenseBody(0, premiseID), change)
			w := testutil.Do(r, "PUT", root+"/licenses/"+uuid.Must(uuid.NewV7()).String(), "owner", body)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	foreignPremise := uuid.Must(uuid.NewV7()).String()
	foreign := strings.ReplaceAll(root, strings.TrimPrefix(root, "/v1/supply/parties/"), uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 200, testutil.Do(r, "PUT", foreign, "foreign", partyBody(0, "TEST Foreign Supplier")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", foreign+"/premises/"+foreignPremise, "foreign", premiseBody(0, "IN")).Code)
	w := testutil.Do(r, "PUT", root+"/licenses/"+uuid.Must(uuid.NewV7()).String(), "owner", licenseBody(0, foreignPremise))
	require.Equal(t, 404, w.Code, w.Body.String())
	for _, prefix := range []string{"premises", "licenses"} {
		require.Equal(t, 404, testutil.Do(r, "GET", root+"/"+prefix, "foreign", "").Code)
	}
	invalidZone := strings.ReplaceAll(premiseBody(0, "IN"), "Asia/Kolkata", "Asia/Karachi")
	require.Equal(t, 400, testutil.Do(r, "PUT", root+"/premises/"+uuid.Must(uuid.NewV7()).String(), "owner", invalidZone).Code)
}

func TestRegistryOutboxFailureRollsBackHeadHistoryAndAudit(t *testing.T) {
	r, db := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Registry Rollback")).Code)
	premiseID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premiseID, "owner", premiseBody(0, "IN")).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_license_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_supply_license_failure CHECK(subject <> 'helivanta.in.supply.license_declared.v1') NOT VALID`}}))
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/licenses/"+id, "owner", licenseBody(0, premiseID))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NoError(t, partyTransaction(t, db, strings.TrimPrefix(root, "/v1/supply/parties/"), func(tx *gorm.DB) error {
		for _, table := range []string{"supply_licenses", "supply_license_revisions"} {
			var count int64
			if e := tx.Table(table).Count(&count).Error; e != nil {
				return e
			}
			require.Zero(t, count, table)
		}
		var count int64
		if e := tx.Table("audit_events").Where("resource_id=?", id).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestRegistryDuplicateLicenseReturnsActionableConflictAndRollsBack(t *testing.T) {
	r, db := partyHarness(t)
	party := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Unique Licence")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/licenses/"+uuid.Must(uuid.NewV7()).String(), "owner", licenseBody(0, premise)).Code)
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/licenses/"+id, "owner", licenseBody(0, premise))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "premise code or licence identity already exists")
	require.NoError(t, partyTransaction(t, db, party, func(tx *gorm.DB) error {
		for _, table := range []string{"supply_licenses", "supply_license_revisions"} {
			var count int64
			column := "id"
			if table == "supply_license_revisions" {
				column = "license_id"
			}
			if e := tx.Table(table).Where(column+"=?", id).Count(&count).Error; e != nil {
				return e
			}
			require.Zero(t, count, table)
		}
		return nil
	}))
}

func TestRegistryConcurrentRetriesCreateOneHeadRevisionAuditAndEvent(t *testing.T) {
	r, db := partyHarness(t)
	party := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Registry Concurrent")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	for _, entry := range []struct{ kind, id, body, subject string }{
		{"premises", premise, premiseBody(0, "IN"), "helivanta.in.supply.premise_updated.v1"},
		{"licenses", uuid.Must(uuid.NewV7()).String(), licenseBody(0, premise), "helivanta.in.supply.license_declared.v1"},
	} {
		t.Run(entry.kind, func(t *testing.T) {
			const requests = 6
			start := make(chan struct{})
			results := make(chan int, requests)
			var workers sync.WaitGroup
			for range requests {
				workers.Go(func() {
					<-start
					results <- testutil.Do(r, "PUT", root+"/"+entry.kind+"/"+entry.id, "owner", entry.body).Code
				})
			}
			close(start)
			workers.Wait()
			for range requests {
				require.Equal(t, 200, <-results)
			}
			require.NoError(t, partyTransaction(t, db, party, func(tx *gorm.DB) error {
				var count int64
				kind, column := "premise", "premise_id"
				if entry.kind == "licenses" {
					kind, column = "license", "license_id"
				}
				for _, q := range []*gorm.DB{
					tx.Table("supply_"+entry.kind).Where("id=?", entry.id),
					tx.Table("supply_"+kind+"_revisions").Where(column+"=?", entry.id),
					tx.Table("audit_events").Where("resource_type=? AND resource_id=?", "supply_"+entry.kind, entry.id),
					tx.Table("outbox_events").Where("subject=?", entry.subject),
				} {
					if e := q.Count(&count).Error; e != nil {
						return e
					}
					require.EqualValues(t, 1, count)
				}
				return nil
			}))
		})
	}
}

func TestRegistryAuditorReadOnlyAndStrictRLSWithWidenedGroupVisibility(t *testing.T) {
	r, db := partyHarness(t)
	party := uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Registry Isolation")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	licence := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/licenses/"+licence, "owner", licenseBody(0, premise)).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-auditor/auditor", "owner", "").Code)
	for _, resource := range []struct{ path, body string }{
		{root + "/premises/" + premise, premiseBody(1, "IN")},
		{root + "/licenses/" + licence, licenseBody(1, premise)},
	} {
		require.Equal(t, 200, testutil.Do(r, "GET", resource.path, "auditor", "").Code)
		require.Equal(t, 200, testutil.Do(r, "GET", resource.path+"/revisions", "auditor", "").Code)
		require.Equal(t, 404, testutil.Do(r, "PUT", resource.path, "auditor", resource.body).Code)
	}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registry_widened_visibility", SQL: `CREATE OR REPLACE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;`}}))
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if e := tx.Exec(`SELECT set_config('app.supply_subject','user-owner',true),set_config('app.supply_party',?,true)`, party).Error; e != nil {
				return e
			}
			for _, table := range []string{"supply_premises", "supply_licenses", "supply_premise_revisions", "supply_license_revisions"} {
				var count int64
				if e := tx.Table(table).Count(&count).Error; e != nil {
					return e
				}
				if tenant == testutil.TenantA {
					require.EqualValues(t, 1, count, table)
				} else {
					require.Zero(t, count, table)
				}
			}
			return nil
		}))
	}
	for _, sql := range []string{
		`UPDATE supply_premise_revisions SET version=99`, `DELETE FROM supply_license_revisions`,
		`DELETE FROM supply_premises`, `DELETE FROM supply_licenses`,
		`UPDATE supply_premises SET version=version+1,country_code='PK'`,
		`UPDATE supply_licenses SET version=version+2`,
	} {
		require.Error(t, partyTransaction(t, db, party, func(tx *gorm.DB) error { return tx.Exec(sql).Error }))
	}
}
