package practitioner_test

import (
	"encoding/json"
	"fmt"
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

func TestPractitionerQualificationKeepsControlledSpecialtyAndImmutableCorrections(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	specialty := uuid.Must(uuid.NewV7()).String()
	catalog := testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+specialty, "steward", `{"code":"synthetic-specialty","display":"Synthetic specialty"}`)
	require.Equal(t, 201, catalog.Code, catalog.Body.String())
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + id
	body := fmt.Sprintf(`{"degree":"Synthetic degree","awarding_body":"Synthetic university","completion_year":2010,"specialty_id":"%s"}`, specialty)
	created := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"verification_state":"unverified"`)
	revision := uuid.Must(uuid.NewV7()).String()
	corrected := testutil.Do(s.router, "PUT", path+"/revisions/"+revision, "steward", `{"expected_version":1,"degree":"Corrected degree","awarding_body":"Synthetic university","completion_year":null,"specialty_id":null}`)
	require.Equal(t, 201, corrected.Code, corrected.Body.String())
	require.Contains(t, corrected.Body.String(), `"version":2`)
	require.Contains(t, corrected.Body.String(), `"completion_year":null`)
	original := testutil.Do(s.router, "GET", path+"/revisions/"+id, "steward", "")
	require.Equal(t, 200, original.Code, original.Body.String())
	require.Contains(t, original.Body.String(), `"degree":"Synthetic degree"`)
	require.Contains(t, original.Body.String(), `"completion_year":2010`)
	head := testutil.Do(s.router, "GET", path, "reviewer", "")
	require.Equal(t, 200, head.Code, head.Body.String())
	require.Contains(t, head.Body.String(), `"id":"`+id+`"`)
	require.Contains(t, head.Body.String(), `"revision_id":"`+revision+`"`)
	require.Contains(t, head.Body.String(), `"degree":"Corrected degree"`)
}

func TestPractitionerQualificationRejectsForgedFactsForeignSpecialtyAndStaleCorrections(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + uuid.Must(uuid.NewV7()).String()
	body := `{"degree":"Synthetic degree","awarding_body":"Synthetic university","completion_year":2010,"specialty_id":null}`
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "reviewer", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "foreign", body).Code)
	for _, bad := range []string{`null`, `[]`, `{}`, strings.Replace(body, `"completion_year":2010`, `"completion_year":0`, 1), strings.Replace(body, `"completion_year":2010`, `"completion_year":9999`, 1), strings.Replace(body, `"completion_year":2010`, `"completion_year":2010.5`, 1), strings.Replace(body, `"degree":"Synthetic degree"`, `"degree":" "`, 1), strings.TrimSuffix(body, "}") + `,"verification_state":"verified"}`, strings.TrimSuffix(body, "}") + `,"tenant_id":"forged"}`} {
		got := testutil.Do(s.router, "PUT", path, "steward", bad)
		require.Equal(t, 400, got.Code, got.Body.String())
	}
	foreignID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+foreignID, "foreign", `{"code":"foreign-specialty","display":"Foreign specialty"}`).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, `"specialty_id":null`, `"specialty_id":"`+foreignID+`"`, 1)).Code)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	first := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	correction := `{"expected_version":1,"degree":"First correction","awarding_body":"Synthetic university","completion_year":null,"specialty_id":null}`
	saved := testutil.Do(s.router, "PUT", first, "steward", correction)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", correction).Code)
	second := testutil.Do(s.router, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", strings.Replace(correction, `"expected_version":1`, `"expected_version":2`, 1))
	require.Equal(t, 201, second.Code, second.Body.String())
	replay := testutil.Do(s.router, "PUT", first, "steward", correction)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", first, "steward", strings.Replace(correction, "First correction", "Changed command", 1)).Code)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "GET", path, "foreign", "").Code)
}

func TestPractitionerSpecialtyCurationIsSeparateScopedAndImmutable(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/practitioners/specialties/" + id
	body := `{"code":"synthetic-controlled","display":"Synthetic controlled specialty"}`
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "operator", body).Code)
	created := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	replay := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, "Synthetic controlled specialty", "Changed label", 1)).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "GET", path, "foreign", "").Code)
	list := testutil.Do(s.router, "GET", "/v1/practitioners/specialties?limit=1", "reviewer", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), "synthetic-controlled")
	for _, sql := range []string{"UPDATE practitioner_specialties SET display='Changed' WHERE tenant_id=? AND id=?", "DELETE FROM practitioner_specialties WHERE tenant_id=? AND id=?"} {
		err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_specialty_manage','true',true)").Error; err != nil {
				return err
			}
			return tx.Exec(sql, s.input.TenantID, id).Error
		})
		require.ErrorContains(t, err, "entry is immutable")
	}
}

func TestPractitionerQualificationPaginationKeepsStableHeadIdentityAfterCorrection(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	collection := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications"
	body := `{"degree":"Synthetic degree","awarding_body":"Synthetic university"}`
	first, second := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", collection+"/"+first, "steward", body).Code)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", collection+"/"+second, "steward", body).Code)
	type page struct {
		Data []struct {
			ID     string `json:"id"`
			Degree string `json:"degree"`
		}
		Page struct {
			NextCursor string `json:"next_cursor"`
		}
	}
	initial := testutil.Do(s.router, "GET", collection+"?limit=1", "steward", "")
	require.Equal(t, 200, initial.Code, initial.Body.String())
	var firstPage page
	require.NoError(t, json.Unmarshal(initial.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Data, 1)
	require.Equal(t, second, firstPage.Data[0].ID)
	require.NotEmpty(t, firstPage.Page.NextCursor)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", collection+"/"+first+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":1,"degree":"Corrected old qualification","awarding_body":"Synthetic university"}`).Code)
	next := testutil.Do(s.router, "GET", collection+"?limit=1&cursor="+url.QueryEscape(firstPage.Page.NextCursor), "steward", "")
	require.Equal(t, 200, next.Code, next.Body.String())
	var nextPage page
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &nextPage))
	require.Len(t, nextPage.Data, 1)
	require.Equal(t, first, nextPage.Data[0].ID)
	require.Equal(t, "Corrected old qualification", nextPage.Data[0].Degree)
	history := testutil.Do(s.router, "GET", collection+"/"+first+"/revisions?limit=2", "steward", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), `"version":1`)
	require.Contains(t, history.Body.String(), `"version":2`)
}

func TestPractitionerQualificationConcurrentCorrectionsAdvanceOnlyOnce(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", `{"degree":"Synthetic degree","awarding_body":"Synthetic university"}`).Code)
	command := path + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	body := `{"expected_version":1,"degree":"Corrected degree","awarding_body":"Synthetic university"}`
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", command, "steward", body).Code })
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
	require.Contains(t, testutil.Do(s.router, "GET", path, "steward", "").Body.String(), `"version":2`)
	err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true)").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE practitioner_qualification_revisions SET degree='Altered' WHERE tenant_id=? AND id=?", s.input.TenantID, command[len(command)-36:]).Error
	})
	require.ErrorContains(t, err, "revision is immutable")
}

func TestPractitionerQualificationAuditFailureRollsBackHeadAndBlocksReads(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", `{"degree":"Synthetic degree","awarding_body":"Synthetic university"}`).Code)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_qualification_audit_failure", SQL: `CREATE FUNCTION reject_qualification_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type IN ('practitioner_qualification_revisions','practitioner_qualifications') THEN RAISE EXCEPTION 'synthetic audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_qualification_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_qualification_audit();`}}))
	corrected := testutil.Do(s.router, "PUT", path+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":1,"degree":"Correction","awarding_body":"Synthetic university"}`)
	require.Equal(t, 500, corrected.Code, corrected.Body.String())
	require.NotContains(t, corrected.Body.String(), "synthetic audit")
	read := testutil.Do(s.router, "GET", path, "steward", "")
	require.Equal(t, 500, read.Code)
	require.NotContains(t, read.Body.String(), "Synthetic degree")
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_qualification_audit_recovery", SQL: "DROP TRIGGER reject_qualification_audit ON audit_events;"}}))
	head := testutil.Do(s.router, "GET", path, "steward", "")
	require.Equal(t, 200, head.Code, head.Body.String())
	require.Contains(t, head.Body.String(), `"version":1`)
}

func TestQualificationAndSpecialtyForceTenantRLSIndependentlyOfHandlerFilters(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	specialty := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/specialties/"+specialty, "steward", `{"code":"synthetic-rls","display":"Synthetic specialty"}`).Code)
	path := "/v1/practitioners/" + s.input.PractitionerID + "/qualifications/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", fmt.Sprintf(`{"degree":"Synthetic degree","awarding_body":"Synthetic university","specialty_id":"%s"}`, specialty)).Code)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage','true',true),set_config('app.practitioner_specialty_manage','true',true)").Error; err != nil {
			return err
		}
		for _, table := range []string{"practitioner_specialties", "practitioner_qualifications", "practitioner_qualification_revisions"} {
			var n int64
			if err := tx.Table(table).Where("tenant_id=?", s.input.TenantID).Count(&n).Error; err != nil {
				return err
			}
			require.Zero(t, n, table)
		}
		return nil
	}))
}

func TestEmptySpecialtyCollectionRequiresCommittedAudit(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	empty := testutil.Do(s.router, "GET", "/v1/practitioners/specialties", "reviewer", "")
	require.Equal(t, 200, empty.Code, empty.Body.String())
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_empty_specialty_audit", SQL: `CREATE FUNCTION reject_specialty_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_specialties' THEN RAISE EXCEPTION 'synthetic collection audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_specialty_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_specialty_read();`}}))
	failed := testutil.Do(s.router, "GET", "/v1/practitioners/specialties", "reviewer", "")
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), `"data"`)
}
