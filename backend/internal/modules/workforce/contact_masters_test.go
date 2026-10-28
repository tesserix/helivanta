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

func TestContactMasterPinsCanonicalRelationshipAndStableRow(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms: map[string][]authz.Permission{"admin": {
			workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage,
		}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Contact Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	masterPath := base + "/hr/masters/" + uuid.NewString()
	w := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":0,"kind":"relationship","code":"SYN-REL","name":"Synthetic reviewed relationship","description":"Synthetic only","active":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var master struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
	rowID := uuid.NewString()
	in := enrollmentBody(0, "SYN-CONTACT-001")
	in["emergency_contacts"] = []any{map[string]any{
		"id": rowID, "name": "Synthetic contact", "relationship": "Untrusted label", "phone": "+61400000001", "priority": 1,
		"relationship_revision_id": master.RevisionID,
	}}
	staffID := uuid.NewString()
	path := base + "/staff/" + staffID + "/enrollment?contact_masters=v1"
	w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	var saved struct {
		Contacts   []map[string]any `json:"emergency_contacts"`
		Projection string           `json:"contact_masters_version"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, "v1", saved.Projection)
	require.Len(t, saved.Contacts, 1)
	require.Equal(t, rowID, saved.Contacts[0]["id"])
	require.Equal(t, "Synthetic reviewed relationship", saved.Contacts[0]["relationship"])
	require.Equal(t, master.RevisionID, saved.Contacts[0]["relationship_revision_id"])
	read := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, w.Body.String(), read.Body.String())
	retry := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, retry.Code, retry.Body.String())
	require.JSONEq(t, w.Body.String(), retry.Body.String())
	retired := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":1,"kind":"relationship","code":"SYN-REL","name":"Synthetic retired relationship","description":"Synthetic only","active":false}`)
	require.Equal(t, 200, retired.Code, retired.Body.String())
	newAssignment := enrollmentBody(0, "SYN-CONTACT-RETIRED")
	newAssignment["emergency_contacts"] = in["emergency_contacts"]
	newPath := base + "/staff/" + uuid.NewString() + "/enrollment?contact_masters=v1"
	rejected := testutil.Do(r, "PUT", newPath, "admin", string(mustJSON(t, newAssignment)))
	require.Equal(t, 400, rejected.Code, rejected.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", newPath, "admin", "").Code, "another staff record cannot inherit a retired pin through a matching child ID")
	in["expected_version"] = 1
	in["profile"].(map[string]any)["preferred_name"] = "Synthetic contact amendment"
	contact := in["emergency_contacts"].([]any)[0].(map[string]any)
	delete(contact, "relationship_revision_id")
	w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, rowID, saved.Contacts[0]["id"])
	require.Equal(t, master.RevisionID, saved.Contacts[0]["relationship_revision_id"])
	require.Equal(t, "Synthetic reviewed relationship", saved.Contacts[0]["relationship"])
	newAssignment["staff"].(map[string]any)["employee_code"] = "SYN-CONTACT-INDEPENDENT"
	independent := testutil.Do(r, "PUT", newPath, "admin", string(mustJSON(t, newAssignment)))
	require.Equal(t, 200, independent.Code, independent.Body.String())
	var other struct {
		Contacts []map[string]any `json:"emergency_contacts"`
	}
	require.NoError(t, json.Unmarshal(independent.Body.Bytes(), &other))
	require.Equal(t, "", other.Contacts[0]["relationship_revision_id"], "omission cannot copy a different staff record's reference")
	for _, statement := range []string{
		`UPDATE workforce_enrollment_emergency_contacts SET relationship_revision_id=NULL WHERE tenant_id=? AND hospital_id=? AND staff_id=?`,
		`DELETE FROM workforce_enrollment_emergency_contacts WHERE tenant_id=? AND hospital_id=? AND staff_id=?`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.actor_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_hr_references','supported',true),set_config('app.workforce_profile_master_references','supported',true),set_config('app.workforce_qualification_master_references','supported',true)`, hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement, testutil.TenantA, hospital, staffID).Error
		})
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg, "a qualification-aware writer cannot erase an unknown contact pin")
		require.Equal(t, "23514", pg.Code)
		require.Equal(t, "workforce_contact_master_writer", pg.ConstraintName)
	}
	var indexReady bool
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT coalesce(bool_and(i.indisvalid AND i.indisready),false) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='workforce_contact_master_ref_idx'`).Scan(&indexReady).Error
	}))
	require.True(t, indexReady, "the composite reference index must be ready and valid")
}

func TestContactMasterRetainsTheSixtyCodePointConsumerLimit(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Contact Limit Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for _, tc := range []struct {
		name, label                  string
		ownerAccepts, contactAccepts bool
	}{
		{"ASCII60", strings.Repeat("A", 60), true, true},
		{"ASCII61", strings.Repeat("A", 61), true, false},
		{"UNICODE60", strings.Repeat("𑀓", 60), true, true},
		{"UNICODE61", strings.Repeat("𑀓", 61), true, false},
		{"OWNER80", strings.Repeat("A", 80), true, false},
		{"OWNER81", strings.Repeat("A", 81), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			masterBody := map[string]any{"expected_version": 0, "kind": "relationship", "code": "SYN-" + tc.name, "name": tc.label, "description": "Synthetic only", "active": true}
			w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), "admin", string(mustJSON(t, masterBody)))
			if !tc.ownerAccepts {
				require.Equal(t, 400, w.Code, w.Body.String())
				return
			}
			require.Equal(t, 200, w.Code, w.Body.String())
			var master struct {
				RevisionID string `json:"revision_id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
			in := enrollmentBody(0, "SYN-CONTACT-"+tc.name)
			in["emergency_contacts"] = []any{map[string]any{"id": uuid.NewString(), "name": "Synthetic contact", "relationship": "Untrusted label", "phone": "+61400000001", "priority": 1, "relationship_revision_id": master.RevisionID}}
			path := base + "/staff/" + uuid.NewString() + "/enrollment?contact_masters=v1"
			w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, in)))
			if tc.contactAccepts {
				require.Equal(t, 200, w.Code, w.Body.String())
				var result struct {
					Contacts []map[string]any `json:"emergency_contacts"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
				require.Equal(t, tc.label, result.Contacts[0]["relationship"], "canonical labels must not be truncated")
			} else {
				require.Equal(t, 400, w.Code, w.Body.String())
				require.JSONEq(t, `{"error":"invalid_request","message":"invalid enrollment fields"}`, w.Body.String(), "reject during enrollment validation, before relying on a SQL write failure")
				require.Equal(t, 404, testutil.Do(r, "GET", path, "admin", "").Code, "rejected selections must not create staff")
			}
		})
	}
}

func TestContactMasterProjectionScopesAndExplicitClear(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"a": testutil.TenantA, "b": testutil.TenantB, "viewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"a":      {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage},
			"b":      {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage},
			"viewer": {workforce.PermEnrollmentManage},
		}, Modules: []platform.Module{workforce.New()},
	})
	create := func(token, kind, base string) (string, string) {
		if base == "" {
			base = "/v1/workforce/hospitals/" + uuid.NewString()
			require.Equal(t, 200, testutil.Do(r, "PUT", base, token, `{"name":"Synthetic Contact Scope Hospital"}`).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+token, token, `{"level":"manager"}`).Code)
		}
		body := map[string]any{"expected_version": 0, "kind": kind, "code": "SYN-CONTACT-SCOPE", "name": "Synthetic scoped classification", "description": "Synthetic only", "active": true}
		w := testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), token, string(mustJSON(t, body)))
		require.Equal(t, 200, w.Code, w.Body.String())
		var master struct {
			RevisionID string `json:"revision_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &master))
		return base, master.RevisionID
	}
	base, revision := create("a", "relationship", "")
	_, wrongHospital := create("a", "relationship", "")
	_, wrongTenant := create("b", "relationship", "")
	_, wrongKind := create("a", "department", base)
	_, qualification := create("a", "qualification", base)
	for name, ref := range map[string]string{"hospital": wrongHospital, "tenant": wrongTenant, "kind": wrongKind, "malformed": "invalid"} {
		t.Run(name, func(t *testing.T) {
			in := enrollmentBody(0, "SYN-CONTACT-"+name)
			in["emergency_contacts"] = []any{map[string]any{"id": uuid.NewString(), "name": "Synthetic contact", "relationship": "Untrusted", "phone": "+61400000001", "priority": 1, "relationship_revision_id": ref}}
			w := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "a", string(mustJSON(t, in)))
			expected := 404
			if name == "malformed" {
				expected = 400
			}
			require.Equal(t, expected, w.Code, w.Body.String())
		})
	}
	path := base + "/staff/" + uuid.NewString() + "/enrollment"
	in := enrollmentBody(0, "SYN-CONTACT-PROJECTION")
	contact := map[string]any{"id": uuid.NewString(), "name": "Synthetic contact", "relationship": "Untrusted", "phone": "+61400000001", "priority": 1, "relationship_revision_id": revision}
	in["emergency_contacts"] = []any{contact}
	in["qualifications"] = []any{map[string]any{"id": uuid.NewString(), "qualification": "Untrusted", "institution": "Synthetic University", "qualification_revision_id": qualification}}
	for _, query := range []string{"contact_masters=", "contact_masters=v2", "contact_masters=v1&contact_masters=v1"} {
		w := testutil.Do(r, "PUT", path+"?"+query, "a", string(mustJSON(t, in)))
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Equal(t, 404, testutil.Do(r, "GET", path, "a", "").Code, "invalid selectors cannot create staff")
	}
	w := testutil.Do(r, "PUT", path, "a", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	for mask := 0; mask < 8; mask++ {
		var parts []string
		for bit, key := range []string{"profile_masters", "qualification_masters", "contact_masters"} {
			if mask&(1<<bit) != 0 {
				parts = append(parts, key+"=v1")
			}
		}
		query := ""
		if len(parts) > 0 {
			query = "?" + strings.Join(parts, "&")
		}
		w := testutil.Do(r, "GET", path+query, "a", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var response struct {
			Profile              map[string]any   `json:"profile"`
			Qualifications       []map[string]any `json:"qualifications"`
			Contacts             []map[string]any `json:"emergency_contacts"`
			ProfileVersion       string           `json:"profile_masters_version"`
			QualificationVersion string           `json:"qualification_masters_version"`
			ContactVersion       string           `json:"contact_masters_version"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		_, profilePresent := response.Profile["contract_type_revision_id"]
		_, qualificationPresent := response.Qualifications[0]["qualification_revision_id"]
		_, contactPresent := response.Contacts[0]["relationship_revision_id"]
		require.Equal(t, mask&1 != 0, profilePresent, query)
		require.Equal(t, mask&2 != 0, qualificationPresent, query)
		require.Equal(t, mask&4 != 0, contactPresent, query)
		for bit, value := range []string{response.ProfileVersion, response.QualificationVersion, response.ContactVersion} {
			expected := ""
			if mask&(1<<bit) != 0 {
				expected = "v1"
			}
			require.Equal(t, expected, value, query)
		}
	}
	require.Equal(t, 404, testutil.Do(r, "GET", path+"?contact_masters=v1", "viewer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "viewer", string(mustJSON(t, in))).Code)
	in["expected_version"] = 1
	contact["relationship_revision_id"] = nil
	w = testutil.Do(r, "PUT", path, "a", string(mustJSON(t, in)))
	require.Equal(t, 400, w.Code, w.Body.String())
	contact["relationship_revision_id"] = ""
	contact["relationship"] = "Synthetic historical free text"
	w = testutil.Do(r, "PUT", path+"?contact_masters=v1", "a", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	var cleared struct {
		Contacts []map[string]any `json:"emergency_contacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cleared))
	require.Equal(t, "", cleared.Contacts[0]["relationship_revision_id"])
	require.Equal(t, "Synthetic historical free text", cleared.Contacts[0]["relationship"])
	require.Equal(t, contact["id"], cleared.Contacts[0]["id"])
}
