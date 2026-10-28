package practitioner_test

import (
	"fmt"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestSpecialtyRetirementPreservesQualificationHistoryAndBlocksNewUse(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	old, next := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	for code, id := range map[string]string{"old-specialty": old, "new-specialty": next} {
		got := testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+id, "steward", fmt.Sprintf(`{"code":"%s","display":"Synthetic specialty"}`, code))
		require.Equal(t, 201, got.Code, got.Body.String())
	}
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"degree":"Synthetic degree","awarding_body":"Synthetic university","specialty_id":"%s"}`, old)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	retirePath := "/v1/practitioners/specialties/" + old + "/retirements/" + uuid.Must(uuid.NewV7()).String()
	retirement := fmt.Sprintf(`{"review_reference":"%s","replacement_id":"%s"}`, uuid.Must(uuid.NewV7()), next)
	saved := testutil.Do(s.router, "PUT", retirePath, "steward", retirement)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	replay := testutil.Do(s.router, "PUT", retirePath, "steward", retirement)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	catalog := testutil.Do(s.router, "GET", "/v1/practitioners/specialties/"+old, "steward", "")
	require.Equal(t, 200, catalog.Code, catalog.Body.String())
	require.Contains(t, catalog.Body.String(), `"active":false`)
	require.Contains(t, catalog.Body.String(), `"retirement_id":"`+retirePath[len(retirePath)-36:]+`"`)
	require.Contains(t, catalog.Body.String(), `"replacement_id":"`+next+`"`)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "steward", body).Code, "original command replay remains valid")
	require.Contains(t, testutil.Do(s.router, "GET", path, "steward", "").Body.String(), old)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path[:len(path)-36]+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	retiredNext := testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+next+"/retirements/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7())))
	require.Equal(t, 201, retiredNext.Code, retiredNext.Body.String())
	retained := testutil.Do(s.router, "PUT", retirePath, "steward", retirement)
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.JSONEq(t, saved.Body.String(), retained.Body.String())
}

func retirementFixture(t *testing.T) (professionalEvidenceScenario, string, string, string) {
	t.Helper()
	s := newProfessionalEvidenceScenario(t)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+id, "steward", `{"code":"retirement-fixture","display":"Synthetic specialty"}`).Code)
	path := "/v1/practitioners/specialties/" + id + "/retirements/" + uuid.Must(uuid.NewV7()).String()
	return s, id, path, fmt.Sprintf(`{"review_reference":"%s","replacement_id":null}`, uuid.Must(uuid.NewV7()))
}

func TestSpecialtyRetirementValidatesScopeAuthorityAndChangedIntent(t *testing.T) {
	s, id, path, body := retirementFixture(t)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "operator", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "foreign", body).Code)
	for _, bad := range []string{`null`, `[]`, `{}`, `{"review_reference":"not-a-reference"}`, strings.Replace(body, `"replacement_id":null`, `"replacement_id":"`+id+`"`, 1), strings.TrimSuffix(body, "}") + `,"active":false}`, strings.TrimSuffix(body, "}") + `,"tenant_id":"forged"}`} {
		got := testutil.Do(s.router, "PUT", path, "steward", bad)
		require.Equal(t, 400, got.Code, got.Body.String())
	}
	foreign := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+foreign, "foreign", `{"code":"foreign-retirement","display":"Foreign specialty"}`).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, `"replacement_id":null`, `"replacement_id":"`+foreign+`"`, 1)).Code)
	saved := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path[:len(path)-36]+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	read := testutil.Do(s.router, "GET", path, "reviewer", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, saved.Body.String(), read.Body.String())
	require.Equal(t, 404, testutil.Do(s.router, "GET", path, "foreign", "").Code)
	for _, sql := range []string{"UPDATE practitioner_specialty_retirements SET replacement_id=NULL WHERE tenant_id=? AND specialty_id=?", "DELETE FROM practitioner_specialty_retirements WHERE tenant_id=? AND specialty_id=?"} {
		err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_specialty_manage','true',true)").Error; err != nil {
				return err
			}
			return tx.Exec(sql, s.input.TenantID, id).Error
		})
		require.ErrorContains(t, err, "receipt is immutable")
	}
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		err := tx.Table("practitioner_specialty_retirements").Where("tenant_id=?", s.input.TenantID).Count(&n).Error
		require.Zero(t, n)
		return err
	}))
}

func TestSpecialtyRetirementConcurrentReplayKeepsOneReceiptAndPreventsReplacementCycles(t *testing.T) {
	s, id, path, body := retirementFixture(t)
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", path, "steward", body).Code })
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
	a, b := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	for code, id := range map[string]string{"cycle-a": a, "cycle-b": b} {
		require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+id, "steward", fmt.Sprintf(`{"code":"%s","display":"Synthetic specialty"}`, code)).Code)
	}
	results := make(chan int, 2)
	for source, replacement := range map[string]string{a: b, b: a} {
		wg.Go(func() {
			request := fmt.Sprintf(`{"review_reference":"%s","replacement_id":"%s"}`, uuid.Must(uuid.NewV7()), replacement)
			results <- testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+source+"/retirements/"+uuid.Must(uuid.NewV7()).String(), "steward", request).Code
		})
	}
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for code := range results {
		if code == 201 {
			succeeded++
		} else {
			require.Equal(t, 409, code)
			conflicted++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	// A previously retired source cannot be selected as a replacement either.
	next := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+next, "steward", `{"code":"retired-target","display":"Synthetic specialty"}`).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+next+"/retirements/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s","replacement_id":"%s"}`, uuid.Must(uuid.NewV7()), id)).Code)
}

func TestSpecialtyRetirementRollsBackOnAuditFailureAndPreservesReadAuditBoundary(t *testing.T) {
	s, id, path, body := retirementFixture(t)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retirement_audit_failure", SQL: `CREATE FUNCTION reject_retirement_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='practitioner_specialty_retirements' THEN RAISE EXCEPTION 'synthetic retirement audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_retirement_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_retirement_audit();`}}))
	failed := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic retirement")
	require.Equal(t, 404, testutil.Do(s.router, "GET", path, "reviewer", "").Code)
	require.Contains(t, testutil.Do(s.router, "GET", "/v1/practitioners/specialties/"+id, "reviewer", "").Body.String(), `"active":true`)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retirement_audit_recovery", SQL: "DROP TRIGGER reject_retirement_audit ON audit_events;"}}))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retirement_read_audit_failure", SQL: `CREATE FUNCTION reject_retirement_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_specialty_retirements' THEN RAISE EXCEPTION 'synthetic read outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_retirement_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_retirement_read();`}}))
	read := testutil.Do(s.router, "GET", path, "reviewer", "")
	require.Equal(t, 500, read.Code)
	require.NotContains(t, read.Body.String(), "review_reference")
	require.Equal(t, 500, testutil.Do(s.router, "PUT", path, "steward", body).Code)
}

func TestQualificationDatabaseGuardRejectsRetiredSpecialtyWithoutHandlerValidation(t *testing.T) {
	s, id, path, body := retirementFixture(t)
	qualification := uuid.Must(uuid.NewV7()).String()
	qualPath := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + qualification
	require.Equal(t, 201, testutil.Do(s.router, "PUT", qualPath, "steward", `{"degree":"Synthetic degree","awarding_body":"Synthetic university"}`).Code)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true)").Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO practitioner_qualification_revisions(tenant_id,practitioner_id,qualification_id,id,expected_version,version,degree,awarding_body,specialty_id,created_by) VALUES(?,?,?,?,1,2,'Synthetic degree','Synthetic university',?,'user-steward')", s.input.TenantID, s.input.PractitionerID, qualification, uuid.Must(uuid.NewV7()), id).Error
	})
	require.ErrorContains(t, err, "retired or unavailable")
	require.Contains(t, testutil.Do(s.router, "GET", qualPath, "steward", "").Body.String(), `"version":1`)
}
