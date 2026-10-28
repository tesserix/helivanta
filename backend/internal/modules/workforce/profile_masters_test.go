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

func TestProfileMastersPinCanonicalHistoricalLabels(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Profile Master Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for _, tc := range []struct{ kind, field string }{
		{"language", "preferred_language"}, {"exit_reason", "exit_reason"}, {"contract_type", "contract_type"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			masterPath := base + "/hr/masters/" + uuid.NewString()
			originalLabel := strings.Repeat("𑀓", 80)
			master := map[string]any{"expected_version": 0, "kind": tc.kind, "code": "SYN-" + tc.kind, "name": originalLabel, "description": "Synthetic classification only", "active": true}
			w := testutil.Do(r, "PUT", masterPath, "admin", string(mustJSON(t, master)))
			require.Equal(t, 200, w.Code, w.Body.String())
			var revision struct {
				RevisionID string `json:"revision_id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &revision))
			staffID := uuid.NewString()
			path := base + "/staff/" + staffID + "/enrollment?profile_masters=v1"
			in := enrollmentBody(0, "SYN-PROFILE-"+tc.kind)
			profile := in["profile"].(map[string]any)
			profile[tc.field] = "FORGED CLIENT LABEL"
			profile[tc.field+"_revision_id"] = revision.RevisionID
			w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 200, w.Code, w.Body.String())
			var saved struct {
				Version int            `json:"version"`
				Profile map[string]any `json:"profile"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
			require.Equal(t, 1, saved.Version)
			require.Equal(t, master["name"], saved.Profile[tc.field])
			require.Equal(t, revision.RevisionID, saved.Profile[tc.field+"_revision_id"])
			retry := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 200, retry.Code, retry.Body.String())
			require.JSONEq(t, w.Body.String(), retry.Body.String())
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var payloads []string
				err := tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject=? AND payload->'data'->>'staff_id'=?`, workforce.EnrollmentChanged, staffID).Scan(&payloads).Error
				require.NoError(t, err)
				require.Len(t, payloads, 1, "exact retry must not emit a second enrollment event")
				require.NotContains(t, payloads[0], originalLabel)
				require.NotContains(t, payloads[0], revision.RevisionID)
				require.NotContains(t, payloads[0], "FORGED CLIENT LABEL")
				return nil
			}))
			master["expected_version"] = 1
			master["name"] = "Synthetic " + tc.kind + " retired"
			master["active"] = false
			w = testutil.Do(r, "PUT", masterPath, "admin", string(mustJSON(t, master)))
			require.Equal(t, 200, w.Code, w.Body.String())

			profile["preferred_name"] = "Synthetic stale edit"
			stale := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 409, stale.Code, stale.Body.String())
			fresh := enrollmentBody(0, "SYN-RETIRED-"+tc.kind)
			fresh["profile"].(map[string]any)[tc.field+"_revision_id"] = revision.RevisionID
			inactive := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, fresh)))
			require.Equal(t, 400, inactive.Code, inactive.Body.String())
			in["expected_version"] = 1
			profile["preferred_name"] = "Synthetic preserved history"
			delete(profile, tc.field+"_revision_id")
			if tc.kind == "contract_type" {
				delete(profile, tc.field)
			}
			w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
			require.Equal(t, 2, saved.Version)
			require.Equal(t, originalLabel, saved.Profile[tc.field])
			require.Equal(t, revision.RevisionID, saved.Profile[tc.field+"_revision_id"])
			read := testutil.Do(r, "GET", path, "admin", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
			require.JSONEq(t, w.Body.String(), read.Body.String())
			directory := testutil.Do(r, "GET", base+"/staff", "admin", "")
			require.Equal(t, 200, directory.Code, directory.Body.String())
			require.NotContains(t, directory.Body.String(), revision.RevisionID)
			require.NotContains(t, directory.Body.String(), originalLabel)
			in["expected_version"] = 2
			profile[tc.field+"_revision_id"] = ""
			profile[tc.field] = ""
			w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
			require.Equal(t, "", saved.Profile[tc.field])
			require.Equal(t, "", saved.Profile[tc.field+"_revision_id"])
		})
	}
}

func TestProfileMastersRejectNullNewFields(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Null Reference Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for _, field := range []string{"preferred_language_revision_id", "exit_reason_revision_id", "contract_type_revision_id", "contract_type"} {
		t.Run(field, func(t *testing.T) {
			in := enrollmentBody(0, "SYN-NULL-"+field)
			in["profile"].(map[string]any)[field] = nil
			w := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, in)))
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
}

func TestProfileMastersPreserveOriginalNullReferenceCompatibility(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Legacy Null Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	in := enrollmentBody(0, "SYN-LEGACY-NULL")
	profile := in["profile"].(map[string]any)
	for _, field := range []string{"department_revision_id", "designation_revision_id", "grade_revision_id"} {
		profile[field] = nil
	}
	w := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"department_revision_id":""`)
	require.Contains(t, w.Body.String(), `"designation_revision_id":""`)
	require.Contains(t, w.Body.String(), `"grade_revision_id":""`)
}

func TestProfileMastersRejectOlderWriterAndWrongKindSQL(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Mixed Version Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	createMaster := func(kind, code string) string {
		w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), "admin", string(mustJSON(t, map[string]any{"expected_version": 0, "kind": kind, "code": code, "name": "Synthetic " + code, "description": "", "active": true})))
		require.Equal(t, 200, w.Code, w.Body.String())
		var row struct {
			RevisionID string `json:"revision_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
		return row.RevisionID
	}
	wrongKind := createMaster("department", "SYN-WRONG-KIND")
	scope := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_hr_references','supported',true)`, hospital).Error
	}
	for _, tc := range []struct{ kind, field, update string }{
		{"language", "preferred_language", `UPDATE workforce_enrollment_profile SET preferred_language_revision_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`},
		{"exit_reason", "exit_reason", `UPDATE workforce_enrollment_profile SET exit_reason_revision_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`},
		{"contract_type", "contract_type", `UPDATE workforce_enrollment_profile SET contract_type_revision_id=? WHERE tenant_id=? AND hospital_id=? AND staff_id=?`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			revision := createMaster(tc.kind, "SYN-GUARD-"+tc.kind)
			staff := uuid.NewString()
			path := base + "/staff/" + staff + "/enrollment"
			in := enrollmentBody(0, "SYN-GUARD-"+tc.kind)
			in["profile"].(map[string]any)[tc.field+"_revision_id"] = revision
			w := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			require.Equal(t, 200, w.Code, w.Body.String())
			err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := scope(tx); err != nil {
					return err
				}
				return tx.Exec(`DELETE FROM workforce_enrollment_profile WHERE tenant_id=? AND hospital_id=? AND staff_id=?`, testutil.TenantA, hospital, staff).Error
			})
			var pg *pgconn.PgError
			require.ErrorAs(t, err, &pg, "an older writer must not erase unknown profile pins")
			require.Equal(t, "23514", pg.Code)
			require.Equal(t, "workforce_profile_master_writer", pg.ConstraintName)
			read := testutil.Do(r, "GET", path+"?profile_masters=v1", "admin", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.Contains(t, read.Body.String(), revision)
			err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := scope(tx); err != nil {
					return err
				}
				return tx.Exec(tc.update, wrongKind, testutil.TenantA, hospital, staff).Error
			})
			require.ErrorAs(t, err, &pg)
			require.Equal(t, "23503", pg.Code, "typed composite FK must reject another category")
		})
	}
}

func TestProfileMastersRejectUnauthorizedAndHiddenReferences(t *testing.T) {
	manage := []authz.Permission{workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "foreign": testutil.TenantB, "reader": testutil.TenantA, "directory": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": manage, "foreign": manage, "reader": {workforce.PermEnrollmentManage}, "directory": {}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := func(token string) string {
		base := "/v1/workforce/hospitals/" + uuid.NewString()
		require.Equal(t, 200, testutil.Do(r, "PUT", base, token, `{"name":"Synthetic Scope Hospital"}`).Code)
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+token, token, `{"level":"manager"}`).Code)
		return base
	}
	base, other, foreign := hospital("admin"), hospital("admin"), hospital("foreign")
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "admin", `{"level":"reader"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-directory", "admin", `{"level":"manager"}`).Code)
	master := func(base, token, kind string) string {
		w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), token, string(mustJSON(t, map[string]any{"expected_version": 0, "kind": kind, "code": "SYN-" + uuid.NewString()[:8], "name": "Synthetic private classification " + kind, "description": "", "active": true})))
		require.Equal(t, 200, w.Code, w.Body.String())
		var row struct {
			RevisionID string `json:"revision_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row))
		return row.RevisionID
	}
	wrongKind := master(base, "admin", "department")
	for _, tc := range []struct{ kind, field string }{{"language", "preferred_language"}, {"exit_reason", "exit_reason"}, {"contract_type", "contract_type"}} {
		local, elsewhere, foreignID := master(base, "admin", tc.kind), master(other, "admin", tc.kind), master(foreign, "foreign", tc.kind)
		for _, denial := range []struct {
			name, token, reference string
			status                 int
		}{
			{"wrong category", "admin", wrongKind, 404},
			{"another hospital", "admin", elsewhere, 404},
			{"another tenant reference", "admin", foreignID, 404},
			{"reader cannot write", "reader", local, 404},
			{"missing permission", "directory", local, 403},
			{"foreign actor", "foreign", local, 404},
			{"malformed identifier", "admin", "not-a-uuid", 400},
		} {
			t.Run(tc.kind+"/"+denial.name, func(t *testing.T) {
				path := base + "/staff/" + uuid.NewString() + "/enrollment"
				in := enrollmentBody(0, "SYN-NEG-"+uuid.NewString()[:8])
				in["profile"].(map[string]any)[tc.field+"_revision_id"] = denial.reference
				w := testutil.Do(r, "PUT", path, denial.token, string(mustJSON(t, in)))
				require.Equal(t, denial.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "Synthetic private classification")
				require.NotContains(t, w.Body.String(), denial.reference)
				read := testutil.Do(r, "GET", path, "admin", "")
				require.Equal(t, 404, read.Code, read.Body.String())
			})
		}
	}
	in := enrollmentBody(0, "SYN-BARE-CONTRACT")
	in["profile"].(map[string]any)["contract_type"] = "Synthetic unreferenced contract classification"
	w := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, in)))
	require.Equal(t, 400, w.Code, w.Body.String())
	directory := testutil.Do(r, "GET", base+"/staff", "admin", "")
	require.Equal(t, 200, directory.Code, directory.Body.String())
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(directory.Body.Bytes(), &page))
	require.Empty(t, page.Data, "failed commands must not partially create staff")
}

func TestProfileMastersProjectLegacyResponsesWithoutLosingPins(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Projection Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString() + "/enrollment"
	in := enrollmentBody(0, "SYN-PROJECTION")
	created := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, created.Code, created.Body.String())
	decode := func(data []byte) map[string]any {
		var result map[string]any
		require.NoError(t, json.Unmarshal(data, &result))
		return result
	}
	extensionFields := []string{"preferred_language_revision_id", "exit_reason_revision_id", "contract_type", "contract_type_revision_id"}
	legacy := decode(created.Body.Bytes())
	for _, field := range extensionFields {
		require.NotContains(t, legacy["profile"], field, "default responses must retain the older strict editor's profile shape")
	}
	require.NotContains(t, legacy, "profile_masters_version")
	in["expected_version"] = 1
	profile := in["profile"].(map[string]any)
	pins := map[string]string{}
	for _, tc := range []struct{ kind, field string }{{"language", "preferred_language"}, {"exit_reason", "exit_reason"}, {"contract_type", "contract_type"}} {
		w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), "admin", string(mustJSON(t, map[string]any{
			"expected_version": 0, "kind": tc.kind, "code": "SYN-" + tc.kind, "name": "Synthetic " + tc.kind, "description": "", "active": true,
		})))
		require.Equal(t, 200, w.Code, w.Body.String())
		pins[tc.field+"_revision_id"] = decode(w.Body.Bytes())["revision_id"].(string)
		profile[tc.field+"_revision_id"] = pins[tc.field+"_revision_id"]
	}
	w := testutil.Do(r, "PUT", path+"?profile_masters=v1", "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	extended := decode(w.Body.Bytes())
	require.Equal(t, "v1", extended["profile_masters_version"])
	for field, pin := range pins {
		require.Equal(t, pin, extended["profile"].(map[string]any)[field])
	}
	read := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	legacy = decode(read.Body.Bytes())
	want := decode(w.Body.Bytes())
	delete(want, "profile_masters_version")
	for _, field := range extensionFields {
		delete(want["profile"].(map[string]any), field)
	}
	require.Equal(t, want, legacy, "legacy projection must preserve every original field")
	in["expected_version"] = 2
	in["profile"] = legacy["profile"]
	in["profile"].(map[string]any)["preferred_name"] = "Synthetic legacy editor amendment"
	for range 2 {
		saved := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
		require.Equal(t, 200, saved.Code, saved.Body.String())
		result := decode(saved.Body.Bytes())
		require.EqualValues(t, 3, result["version"])
		for _, field := range extensionFields {
			require.NotContains(t, result["profile"], field)
		}
	}
	read = testutil.Do(r, "GET", path+"?profile_masters=v1", "admin", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	extended = decode(read.Body.Bytes())
	require.Equal(t, "v1", extended["profile_masters_version"])
	require.Equal(t, "Synthetic legacy editor amendment", extended["profile"].(map[string]any)["preferred_name"])
	for field, pin := range pins {
		require.Equal(t, pin, extended["profile"].(map[string]any)[field])
	}
	require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
}

func TestProfileMastersRejectInvalidProjectionWithoutCreatingStaff(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Projection Validation Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for _, query := range []string{"profile_masters=", "profile_masters=v2", "profile_masters=v1&profile_masters=v1", "profile_masters=v1&profile_masters=v2"} {
		t.Run(query, func(t *testing.T) {
			path := base + "/staff/" + uuid.NewString() + "/enrollment"
			w := testutil.Do(r, "PUT", path+"?"+query, "admin", string(mustJSON(t, enrollmentBody(0, "SYN-INVALID-PROJECTION"))))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Equal(t, 400, testutil.Do(r, "GET", path+"?"+query, "admin", "").Code)
			require.Equal(t, 404, testutil.Do(r, "GET", path, "admin", "").Code, "invalid projection must not commit enrollment")
		})
	}
}
