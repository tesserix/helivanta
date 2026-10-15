package supply_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func partyTransaction(t *testing.T, db *tenantdb.DB, id string, fn func(*gorm.DB) error) error {
	t.Helper()
	return db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_subject','user-owner',true),set_config('app.supply_party',?,true),set_config('app.supply_directory','on',true),set_config('app.supply_manager','on',true)`, id).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func changedPartyBody(t *testing.T, body string, change func(map[string]any)) string {
	t.Helper()
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &fields))
	change(fields)
	data, err := json.Marshal(fields)
	require.NoError(t, err)
	return string(data)
}

func TestPartyRejectsInvalidPrivateFieldsWithoutCreatingARecord(t *testing.T) {
	r, _ := partyHarness(t)
	body := structuredPartyBody(0, "TEST Invalid Profile")
	cases := map[string]func(map[string]any){
		"unknown field":        func(v map[string]any) { v["tenant_id"] = testutil.TenantB },
		"missing version":      func(v map[string]any) { delete(v, "expected_version") },
		"missing children":     func(v map[string]any) { delete(v, "contacts") },
		"null children":        func(v map[string]any) { v["contacts"] = nil },
		"unsupported country":  func(v map[string]any) { v["country_code"] = "US" },
		"invalid code":         func(v map[string]any) { v["code"] = "TEST CODE" },
		"negative credit":      func(v map[string]any) { v["default_credit_days"] = -1 },
		"unsafe website":       func(v map[string]any) { v["website"] = "http://supplier.example.test" },
		"website credentials":  func(v map[string]any) { v["website"] = "https://user:secret@ supplier.example.test" },
		"blank contact":        func(v map[string]any) { v["contacts"].([]any)[0].(map[string]any)["name"] = "  " },
		"invalid phone":        func(v map[string]any) { v["contacts"].([]any)[0].(map[string]any)["phone"] = "not-a-phone" },
		"invalid email":        func(v map[string]any) { v["contacts"].([]any)[0].(map[string]any)["email"] = "not-an-email" },
		"old UUID":             func(v map[string]any) { v["contacts"].([]any)[0].(map[string]any)["id"] = uuid.NewString() },
		"duplicate contact ID": func(v map[string]any) { v["contacts"] = append(v["contacts"].([]any), v["contacts"].([]any)[0]) },
		"too many contacts": func(v map[string]any) {
			contacts := make([]any, 21)
			for i := range contacts {
				contacts[i] = v["contacts"].([]any)[0]
			}
			v["contacts"] = contacts
		},
		"foreign registered address": func(v map[string]any) { v["addresses"].([]any)[0].(map[string]any)["country_code"] = "PK" },
		"foreign tax kind":           func(v map[string]any) { v["tax_registrations"].([]any)[0].(map[string]any)["kind"] = "ntn" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
			w := testutil.Do(r, "PUT", path, "owner", changedPartyBody(t, body, change))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
		})
	}
	path := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	for _, invalid := range []string{`{`, body + ` {}`, body + strings.Repeat(" ", 32768)} {
		w := testutil.Do(r, "PUT", path, "owner", invalid)
		require.Contains(t, []int{400, 413}, w.Code, w.Body.String())
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", "/v1/supply/parties/"+uuid.NewString(), "owner", body).Code)
	require.Equal(t, 403, testutil.Do(r, "PUT", path, "denied", body).Code)
	require.Equal(t, 401, testutil.Do(r, "PUT", path, "unknown", body).Code)
}

func TestPartyConcurrentRetriesCommitOneRevisionAndEvent(t *testing.T) {
	r, db := partyHarness(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	body := structuredPartyBody(0, "TEST Concurrent Supplier")
	const count = 6
	results := make(chan int, count)
	var start sync.WaitGroup
	start.Add(1)
	for range count {
		go func() { start.Wait(); results <- testutil.Do(r, "PUT", path, "owner", body).Code }()
	}
	start.Done()
	for range count {
		require.Equal(t, 200, <-results)
	}
	var counts struct{ Revisions, Events, Audits, Contacts, Grants int }
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_party_revisions WHERE party_id=?) AS revisions,
   (SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.party_updated.v1') AS events,
   (SELECT count(*) FROM audit_events WHERE resource_type='supply_parties' AND resource_id=?) AS audits,
   (SELECT count(*) FROM supply_party_contacts WHERE party_id=?) AS contacts,
   (SELECT count(*) FROM supply_party_access WHERE party_id=?) AS grants`, id, id, id, id).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Events)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Contacts)
	require.Equal(t, 1, counts.Grants)
	first := testutil.Do(r, "PUT", path, "owner", partyBody(1, "TEST Winning Revision"))
	require.Equal(t, 200, first.Code, first.Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", partyBody(1, "TEST Losing Revision")).Code)
}

func TestPartyDuplicateIdentityRollsBackEveryRelatedRow(t *testing.T) {
	r, db := partyHarness(t)
	body := structuredPartyBody(0, "TEST Unique Supplier")
	require.Equal(t, 200, testutil.Do(r, "PUT", "/v1/supply/parties/"+uuid.Must(uuid.NewV7()).String(), "owner", body).Code)
	for _, field := range []string{"code", "registration_number", "tax"} {
		t.Run(field, func(t *testing.T) {
			id := uuid.Must(uuid.NewV7()).String()
			duplicate := changedPartyBody(t, structuredPartyBody(0, "TEST Duplicate"), func(v map[string]any) {
				if field != "code" {
					v["code"] = "TEST-" + strings.ToUpper(field)
				}
				if field != "registration_number" {
					v["registration_number"] = "TEST-" + strings.ToUpper(field)
				}
				if field != "tax" {
					v["tax_registrations"].([]any)[0].(map[string]any)["number"] = "TEST-TAX-" + field
				}
			})
			w := testutil.Do(r, "PUT", "/v1/supply/parties/"+id, "owner", duplicate)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
				var total int
				err := tx.Raw(`SELECT (SELECT count(*) FROM supply_parties WHERE id=?) +
    (SELECT count(*) FROM supply_party_access WHERE party_id=?) +
    (SELECT count(*) FROM supply_party_contacts WHERE party_id=?) +
    (SELECT count(*) FROM supply_party_addresses WHERE party_id=?) +
    (SELECT count(*) FROM supply_party_tax_registrations WHERE party_id=?) +
    (SELECT count(*) FROM supply_party_revisions WHERE party_id=?) +
    (SELECT count(*) FROM audit_events WHERE resource_id=?)`, id, id, id, id, id, id, id).Row().Scan(&total)
				require.Zero(t, total)
				return err
			}))
		})
	}
}

func TestPartyOutboxFailureRollsBackProfileAndCreatorAccess(t *testing.T) {
	r, db := partyHarness(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_supply_outbox_failure CHECK(subject <> 'helivanta.in.supply.party_updated.v1')`}}))
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	w := testutil.Do(r, "PUT", path, "owner", structuredPartyBody(0, "TEST Rolled Back"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "test_supply_outbox_failure")
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		for _, table := range []string{"supply_parties", "supply_party_access", "supply_party_revisions", "supply_party_contacts", "supply_party_addresses", "supply_party_tax_registrations", "audit_events", "outbox_events"} {
			var count int64
			if err := tx.Table(table).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, table)
		}
		return nil
	}))
}

func TestPartyContactReorderingPreservesIdentityAndAudit(t *testing.T) {
	r, db := partyHarness(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	second := uuid.Must(uuid.NewV7()).String()
	body := changedPartyBody(t, structuredPartyBody(0, "TEST Reordering"), func(v map[string]any) {
		v["contacts"] = append(v["contacts"].([]any), map[string]any{"id": second, "name": "TEST Finance", "role": "finance", "email": "finance@example.test", "phone": "", "alternate_phone": ""})
	})
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", body).Code)
	reordered := changedPartyBody(t, body, func(v map[string]any) {
		v["expected_version"] = 1
		contacts := v["contacts"].([]any)
		contacts[0], contacts[1] = contacts[1], contacts[0]
	})
	w := testutil.Do(r, "PUT", path, "owner", reordered)
	require.Equal(t, 200, w.Code, w.Body.String())
	var response struct{ Contacts []struct{ ID string } }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Contacts, 2)
	require.Equal(t, second, response.Contacts[0].ID)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", reordered).Body.String())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var actions []string
		if err := tx.Table("audit_events").Select("action").Where("resource_type='supply_party_contacts'").Order("action").Scan(&actions).Error; err != nil {
			return err
		}
		require.Equal(t, []string{"insert", "insert", "update", "update"}, actions)
		return nil
	}))
}

func TestPartyDatabaseDeniesImplicitVisibilityAndImmutableHistoryChanges(t *testing.T) {
	r, db := partyHarness(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/supply/parties/" + id
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", structuredPartyBody(0, "TEST Strict Isolation")).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_widened_visibility", SQL: `CREATE OR REPLACE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;`}}))
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.supply_subject','user-owner',true),set_config('app.supply_party',?,true),set_config('app.supply_directory','on',true),set_config('app.supply_manager','on',true)`, id).Error; err != nil {
				return err
			}
			for _, table := range []string{"supply_parties", "supply_party_access", "supply_party_revisions", "supply_party_contacts", "supply_party_addresses", "supply_party_tax_registrations"} {
				var count int64
				if err := tx.Table(table).Count(&count).Error; err != nil {
					return err
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
		`UPDATE supply_party_revisions SET version=99`,
		`DELETE FROM supply_party_revisions`,
		`DELETE FROM supply_parties`,
		fmt.Sprintf(`UPDATE supply_parties SET country_code='PK',version=version+1 WHERE id='%s'`, id),
	} {
		require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.supply_subject','user-owner',true),set_config('app.supply_party',?,true)`, id).Error; err != nil {
				return err
			}
			return tx.Exec(sql).Error
		}))
	}
}
