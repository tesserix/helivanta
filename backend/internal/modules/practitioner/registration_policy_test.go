package practitioner_test

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

type registrationPolicyScenario struct {
	router                          *gin.Engine
	db                              *tenantdb.DB
	path, body, appointment, record string
}

func newRegistrationPolicyScenario(t *testing.T) registrationPolicyScenario {
	t.Helper()
	router, db, _, parent := evidenceFixtureWithFiles(t, nil)
	appointment := uuid.Must(uuid.NewV7()).String()
	nomination := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(router, "PUT", "/v1/practitioners/steward-appointments/"+appointment, "steward", nomination).Code)
	require.Equal(t, 200, testutil.Do(router, "POST", "/v1/practitioners/my/steward-appointments/"+appointment+"/accept", "reviewer", `{"expected_version":1}`).Code)
	path := "/v1/practitioners/registration-policies/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"source_system":"synthetic.council","normalization":"ascii_upper","expiry_time_zone":"Asia/Kolkata","max_source_age_seconds":86400,"date_rule":"date_required","valid_until":"2097-01-01T00:00:00Z","review_reference":"%s","appointment_id":"%s","appointment_version":2}`, uuid.Must(uuid.NewV7()), appointment)
	return registrationPolicyScenario{router, db, path, body, appointment, strings.Split(parent, "/registrations/")[0]}
}
func TestRegistrationPolicyRetainsExplicitIssuerRulesAndAcceptedAuthority(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	router, path, body := s.router, s.path, s.body
	created := testutil.Do(router, "PUT", path, "reviewer", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"normalization":"ascii_upper"`)
	require.Contains(t, created.Body.String(), `"max_source_age_seconds":86400`)
	replay := testutil.Do(router, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	record := testutil.Do(router, "GET", s.record, "reviewer", "")
	require.Equal(t, 200, record.Code, record.Body.String())
	require.Contains(t, record.Body.String(), `"verification_state":"unverified"`)
}

func policyEditionBody(s registrationPolicyScenario, expected int64, age int64) string {
	return fmt.Sprintf(`{"expected_version":%d,"max_source_age_seconds":%d,"date_rule":"date_required","valid_until":"2097-01-01T00:00:00Z","review_reference":"%s","appointment_id":"%s","appointment_version":2}`, expected, age, "019a0000-0000-7000-8000-000000000001", s.appointment)
}
func TestRegistrationPolicyRejectsMissingExplicitRulesUnappointedActorsAndForeignScope(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", s.path, "operator", s.body).Code)
	require.Equal(t, 403, testutil.Do(s.router, "GET", s.path, "reader", "").Code)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", s.path, "steward", s.body).Code, "manage permission alone cannot use another person's appointment")
	for _, bad := range []string{`null`, `[]`, `{}`, strings.Replace(s.body, `"ascii_upper"`, `"casefold"`, 1), strings.Replace(s.body, `"Asia/Kolkata"`, `"Local"`, 1), strings.Replace(s.body, `"Asia/Kolkata"`, `"Unknown/Zone"`, 1), strings.Replace(s.body, `"max_source_age_seconds":86400`, `"max_source_age_seconds":0`, 1), strings.Replace(s.body, `"max_source_age_seconds":86400`, `"max_source_age_seconds":31536001`, 1), strings.Replace(s.body, `"date_rule":"date_required"`, `"date_rule":""`, 1), strings.Replace(s.body, `"date_rule":"date_required",`, "", 1), strings.Replace(s.body, "2097-01-01", "2000-01-01", 1), strings.TrimSuffix(s.body, "}") + `,"tenant_id":"forged"}`, strings.TrimSuffix(s.body, "}") + `,"verified":true}`} {
		got := testutil.Do(s.router, "PUT", s.path, "reviewer", bad)
		require.Equal(t, 400, got.Code, got.Body.String())
	}
	created := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Equal(t, 404, testutil.Do(s.router, "GET", s.path, "foreign", "").Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path, "reviewer", strings.Replace(s.body, `"ascii_upper"`, `"exact"`, 1)).Code)
	duplicate := testutil.Do(s.router, "PUT", s.path[:len(s.path)-36]+uuid.Must(uuid.NewV7()).String(), "reviewer", s.body)
	require.Equal(t, 409, duplicate.Code)
	require.Contains(t, duplicate.Body.String(), "policy_source_conflict")
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_policy_manage','true',true)").Error; err != nil {
			return err
		}
		for _, table := range []string{"practitioner_registration_policies", "practitioner_registration_policy_revisions"} {
			var n int64
			if err := tx.Table(table).Where("tenant_id=?", testutil.TenantA).Count(&n).Error; err != nil {
				return err
			}
			require.Zero(t, n)
		}
		return nil
	}))
}

func TestRegistrationPolicyEditionsRetainHistoryAndDoNotChangeIssuerIdentity(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	command := s.path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	body := policyEditionBody(s, 1, 3600)
	saved := testutil.Do(s.router, "PUT", command, "reviewer", body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	require.Equal(t, 400, testutil.Do(s.router, "PUT", command, "reviewer", strings.TrimSuffix(body, "}")+`,"normalization":"exact"}`).Code)
	stale := testutil.Do(s.router, "PUT", s.path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body)
	require.Equal(t, 409, stale.Code)
	require.Contains(t, stale.Body.String(), "policy_version_conflict")
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", policyEditionBody(s, 2, 7200)).Code)
	retained := testutil.Do(s.router, "PUT", command, "reviewer", body)
	require.Equal(t, 200, retained.Code)
	require.JSONEq(t, saved.Body.String(), retained.Body.String())
	changed := testutil.Do(s.router, "PUT", command, "reviewer", strings.Replace(body, `"max_source_age_seconds":3600`, `"max_source_age_seconds":1800`, 1))
	require.Equal(t, 409, changed.Code)
	require.Contains(t, changed.Body.String(), "policy_intent_conflict")
	require.Equal(t, 200, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	head := testutil.Do(s.router, "GET", s.path, "reviewer", "")
	require.Equal(t, 200, head.Code)
	require.Contains(t, head.Body.String(), `"version":3`)
	require.Contains(t, head.Body.String(), `"normalization":"ascii_upper"`)
	history := testutil.Do(s.router, "GET", s.path+"/revisions?limit=2", "reviewer", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var page struct {
		Data []struct{ Version int64 }
		Page struct {
			NextCursor string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.NotEmpty(t, page.Page.NextCursor)
	last := testutil.Do(s.router, "GET", s.path+"/revisions?limit=2&cursor="+url.QueryEscape(page.Page.NextCursor), "reviewer", "")
	require.Equal(t, 200, last.Code, last.Body.String())
	require.Contains(t, last.Body.String(), `"version":1`)
	read := testutil.Do(s.router, "GET", command, "reviewer", "")
	require.Equal(t, 200, read.Code)
	require.JSONEq(t, saved.Body.String(), read.Body.String())
}

func TestRegistrationPolicyConcurrentReplayAndRevokedAppointment(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code })
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	revoke := fmt.Sprintf(`{"expected_version":2,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/steward-appointments/"+s.appointment+"/revoke", "steward", revoke).Code)
	denied := testutil.Do(s.router, "PUT", s.path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", policyEditionBody(s, 1, 3600))
	require.Equal(t, 403, denied.Code)
	require.Contains(t, denied.Body.String(), "policy_authority_required")
	require.Equal(t, 200, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.practitioner.registration_policy_changed.v1").Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
}

func TestRegistrationPolicyAuditAndOutboxFailureRollBackConfiguration(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s := newRegistrationPolicyScenario(t)
			table, condition := "audit_events", "NEW.resource_type IN ('practitioner_registration_policies','practitioner_registration_policy_revisions')"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='helivanta.in.practitioner.registration_policy_changed.v1'"
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_policy_failure", SQL: "CREATE FUNCTION reject_policy_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic policy dependency outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_policy_write BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_policy_write();"}}))
			failed := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
			require.Equal(t, 500, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic")
			require.Equal(t, 404, testutil.Do(s.router, "GET", s.path, "reviewer", "").Code)
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_policy_recovery", SQL: "DROP TRIGGER reject_policy_write ON " + table + ";"}}))
			require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_policy_read_failure", SQL: `CREATE FUNCTION reject_policy_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type IN ('practitioner_registration_policies','practitioner_registration_policy_revisions') THEN RAISE EXCEPTION 'synthetic read outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_policy_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_policy_read();`}}))
			for _, path := range []string{s.path, "/v1/practitioners/registration-policies", s.path + "/revisions"} {
				got := testutil.Do(s.router, "GET", path, "reviewer", "")
				require.Equal(t, 500, got.Code, got.Body.String())
				require.NotContains(t, got.Body.String(), "max_source_age_seconds")
			}
			require.Equal(t, 500, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
		})
	}
}

func TestRegistrationPolicyDatabaseGuardsRetainHistoryAndRequireAttributedEdition(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	id := s.path[len(s.path)-36:]
	for _, sql := range []string{"UPDATE practitioner_registration_policy_revisions SET max_source_age_seconds=1 WHERE tenant_id=? AND id=?", "DELETE FROM practitioner_registration_policy_revisions WHERE tenant_id=? AND id=?", "UPDATE practitioner_registration_policies SET version=2 WHERE tenant_id=? AND id=?"} {
		err := s.db.WithTenant(tenantdb.WithActor(t.Context(), "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_policy_manage','true',true)").Error; err != nil {
				return err
			}
			return tx.Exec(sql, testutil.TenantA, id).Error
		})
		require.Error(t, err)
	}
	require.Contains(t, testutil.Do(s.router, "GET", s.path, "reviewer", "").Body.String(), `"version":1`)
}

func TestRegistrationPolicyRequiresAcceptedAndUnexpiredAuthorityAndAuditsEmptyCollections(t *testing.T) {
	s := newRegistrationPolicyScenario(t)
	empty := testutil.Do(s.router, "GET", "/v1/practitioners/registration-policies", "reviewer", "")
	require.Equal(t, 200, empty.Code, empty.Body.String())
	nominee := uuid.Must(uuid.NewV7()).String()
	request := fmt.Sprintf(`{"subject":"user-peer","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+nominee, "steward", request).Code)
	pending := testutil.Do(s.router, "PUT", s.path, "peer", strings.Replace(s.body, s.appointment, nominee, 1))
	require.Equal(t, 403, pending.Code)
	require.Contains(t, pending.Body.String(), "policy_authority_required")
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_policy_expiry", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT '2099-01-01T00:00:00Z'::timestamptz $$;`}}))
	elapsed := testutil.Do(s.router, "PUT", s.path, "reviewer", strings.Replace(s.body, "2097-01-01", "2100-01-01", 1))
	require.Equal(t, 403, elapsed.Code)
	require.Contains(t, elapsed.Body.String(), "policy_authority_required")
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_policy_empty_audit", SQL: `CREATE FUNCTION reject_empty_policy_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_registration_policies' THEN RAISE EXCEPTION 'synthetic empty read audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_empty_policy_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_empty_policy_read();`}}))
	denied := testutil.Do(s.router, "GET", "/v1/practitioners/registration-policies", "reviewer", "")
	require.Equal(t, 500, denied.Code)
	require.NotContains(t, denied.Body.String(), `"data"`)
}
