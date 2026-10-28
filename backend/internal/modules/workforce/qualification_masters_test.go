package workforce_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestQualificationMasterRevisionLifecycleAndCompatibility(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms: map[string][]authz.Permission{"admin": {
			workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage,
		}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Qualification Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	masterPath := base + "/hr/masters/" + uuid.NewString()
	w := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":0,"kind":"qualification","code":"SYN-QUAL","name":"Synthetic reviewed qualification","description":"Synthetic only","active":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var master struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
	rowID := uuid.NewString()
	in := enrollmentBody(0, "SYN-QUAL-001")
	in["qualifications"] = []any{map[string]any{
		"id": rowID, "qualification": "Untrusted label", "institution": "Synthetic University",
		"qualification_revision_id": master.RevisionID,
	}}
	staffID := uuid.NewString()
	path := base + "/staff/" + staffID + "/enrollment?qualification_masters=v1"
	w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	var saved struct {
		Version        int              `json:"version"`
		Qualifications []map[string]any `json:"qualifications"`
		Projection     string           `json:"qualification_masters_version"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, "v1", saved.Projection)
	require.Len(t, saved.Qualifications, 1)
	require.Equal(t, rowID, saved.Qualifications[0]["id"])
	require.Equal(t, "Synthetic reviewed qualification", saved.Qualifications[0]["qualification"])
	require.Equal(t, master.RevisionID, saved.Qualifications[0]["qualification_revision_id"])
	retry := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, retry.Code, retry.Body.String())
	require.JSONEq(t, w.Body.String(), retry.Body.String())
	read := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, w.Body.String(), read.Body.String())
	retired := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":1,"kind":"qualification","code":"SYN-QUAL","name":"Synthetic retired qualification","description":"Synthetic only","active":false}`)
	require.Equal(t, 200, retired.Code, retired.Body.String())
	in["expected_version"] = 1
	in["profile"].(map[string]any)["preferred_name"] = "Synthetic unrelated amendment"
	delete(in["qualifications"].([]any)[0].(map[string]any), "qualification_revision_id")
	w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, 2, saved.Version)
	require.Equal(t, rowID, saved.Qualifications[0]["id"])
	require.Equal(t, "Synthetic reviewed qualification", saved.Qualifications[0]["qualification"])
	require.Equal(t, master.RevisionID, saved.Qualifications[0]["qualification_revision_id"])
	in["expected_version"] = 2
	in["qualifications"].([]any)[0].(map[string]any)["qualification_revision_id"] = nil
	null := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 400, null.Code, null.Body.String())
	for _, statement := range []string{
		`UPDATE workforce_enrollment_qualifications SET qualification_revision_id=NULL WHERE tenant_id=? AND hospital_id=? AND staff_id=?`,
		`DELETE FROM workforce_enrollment_qualifications WHERE tenant_id=? AND hospital_id=? AND staff_id=?`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_hr_references','supported',true),set_config('app.workforce_profile_master_references','supported',true)`, hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement, testutil.TenantA, hospital, staffID).Error
		})
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg, "an intermediate profile-aware writer cannot erase qualification pins")
		require.Equal(t, "23514", pg.Code)
		require.Equal(t, "workforce_qualification_master_writer", pg.ConstraintName)
	}
	active := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":2,"kind":"qualification","code":"SYN-QUAL","name":"Synthetic reviewed qualification","description":"Synthetic only","active":true}`)
	require.Equal(t, 200, active.Code, active.Body.String())
	require.NoError(t, json.Unmarshal(active.Body.Bytes(), &master))
	duplicate := enrollmentBody(0, "SYN-QUAL-DUPLICATE")
	duplicate["qualifications"] = []any{
		map[string]any{"id": uuid.NewString(), "qualification": "Untrusted A", "institution": "Synthetic University", "qualification_revision_id": master.RevisionID},
		map[string]any{"id": uuid.NewString(), "qualification": "Untrusted B", "institution": "Synthetic University", "qualification_revision_id": master.RevisionID},
	}
	w = testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, duplicate)))
	require.Equal(t, 400, w.Code, w.Body.String())
	var indexReady bool
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT coalesce(bool_and(i.indisvalid AND i.indisready),false) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='workforce_qualification_master_ref_idx'`).Scan(&indexReady).Error
	}))
	require.True(t, indexReady, "the composite foreign-key index must be ready and valid")
}

func TestQualificationMasterScopesProjectionsAndExplicitClear(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"a": testutil.TenantA, "b": testutil.TenantB, "viewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"a":      {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage},
			"b":      {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage},
			"viewer": {workforce.PermEnrollmentManage},
		},
		Modules: []platform.Module{workforce.New()},
	})
	create := func(token, kind, base string) (string, string) {
		if base == "" {
			base = "/v1/workforce/hospitals/" + uuid.NewString()
			require.Equal(t, 200, testutil.Do(r, "PUT", base, token, `{"name":"Synthetic scope hospital"}`).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+token, token, `{"level":"manager"}`).Code)
		}
		body := map[string]any{"expected_version": 0, "kind": kind, "code": "SYN-QUAL-SCOPE", "name": "Synthetic scoped qualification", "description": "Synthetic only", "active": true}
		w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), token, string(mustJSON(t, body)))
		require.Equal(t, 200, w.Code, w.Body.String())
		var master struct {
			RevisionID string `json:"revision_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
		return base, master.RevisionID
	}
	base, revision := create("a", "qualification", "")
	_, wrongHospital := create("a", "qualification", "")
	_, wrongTenant := create("b", "qualification", "")
	_, wrongKind := create("a", "department", base)
	for name, ref := range map[string]string{"hospital": wrongHospital, "tenant": wrongTenant, "kind": wrongKind, "malformed": "not-a-uuid"} {
		t.Run(name, func(t *testing.T) {
			in := enrollmentBody(0, "SYN-QUAL-"+name)
			in["qualifications"] = []any{map[string]any{"id": uuid.NewString(), "qualification": "Untrusted", "institution": "Synthetic University", "qualification_revision_id": ref}}
			w := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "a", string(mustJSON(t, in)))
			expected := 404
			if name == "malformed" {
				expected = 400
			}
			require.Equal(t, expected, w.Code, w.Body.String())
		})
	}
	path := base + "/staff/" + uuid.NewString() + "/enrollment"
	in := enrollmentBody(0, "SYN-QUAL-PROJECTION")
	row := map[string]any{"id": uuid.NewString(), "qualification": "Untrusted", "institution": "Synthetic University", "qualification_revision_id": revision}
	in["qualifications"] = []any{row}
	for _, query := range []string{"qualification_masters=", "qualification_masters=v2", "qualification_masters=v1&qualification_masters=v1"} {
		w := testutil.Do(r, "PUT", path+"?"+query, "a", string(mustJSON(t, in)))
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Equal(t, 404, testutil.Do(r, "GET", path, "a", "").Code, "unsupported views cannot create staff")
	}
	w := testutil.Do(r, "PUT", path, "a", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, query := range []string{"", "?profile_masters=v1", "?qualification_masters=v1", "?profile_masters=v1&qualification_masters=v1"} {
		w := testutil.Do(r, "GET", path+query, "a", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var response struct {
			Profile                 map[string]any   `json:"profile"`
			Qualifications          []map[string]any `json:"qualifications"`
			QualificationProjection string           `json:"qualification_masters_version"`
			ProfileProjection       string           `json:"profile_masters_version"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		if strings.Contains(query, "qualification_masters=v1") {
			require.Equal(t, revision, response.Qualifications[0]["qualification_revision_id"])
			require.Equal(t, "v1", response.QualificationProjection)
		} else {
			require.NotContains(t, response.Qualifications[0], "qualification_revision_id")
			require.Empty(t, response.QualificationProjection)
		}
		if strings.Contains(query, "profile_masters=v1") {
			require.Contains(t, response.Profile, "contract_type_revision_id")
			require.Equal(t, "v1", response.ProfileProjection)
		} else {
			require.NotContains(t, response.Profile, "contract_type_revision_id")
			require.Empty(t, response.ProfileProjection)
		}
	}
	require.Equal(t, 404, testutil.Do(r, "GET", path+"?qualification_masters=v1", "viewer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "viewer", string(mustJSON(t, in))).Code)
	in["expected_version"] = 1
	row["qualification_revision_id"] = ""
	row["qualification"] = "Synthetic historical free text"
	w = testutil.Do(r, "PUT", path+"?qualification_masters=v1", "a", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	var cleared struct {
		Qualifications []map[string]any `json:"qualifications"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cleared))
	require.Equal(t, "", cleared.Qualifications[0]["qualification_revision_id"])
	require.Equal(t, "Synthetic historical free text", cleared.Qualifications[0]["qualification"])
	require.Equal(t, row["id"], cleared.Qualifications[0]["id"])
}
