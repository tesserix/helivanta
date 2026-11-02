package workforce_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestDepartmentHandoffRetainsPendingIntentWithoutChangingHRMaster(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermMasterManage}, "reader": {workforce.PermMasterManage}, "foreign": {workforce.PermMasterManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Department Handoff"}`).Code)
	for subject, level := range map[string]string{"admin": "manager", "reader": "reader"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+subject, "admin", fmt.Sprintf(`{"level":%q}`, level)).Code)
	}
	master := uuid.NewString()
	masterPath := base + "/hr/masters/" + master
	w := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":0,"kind":"department","code":"SYN-PHARMA","name":"Synthetic Pharma Vocabulary","active":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var source struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &source))
	request := uuid.Must(uuid.NewV7()).String()
	path := masterPath + "/department-links/" + request
	body := fmt.Sprintf(`{"expected_version":0,"master_revision_id":%q,"department_id":%q,"department_revision_id":%q,"reason":"Synthetic explicit handoff"}`, source.RevisionID, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
	w = testutil.Do(r, "PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending"`)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), source.RevisionID)
	require.NotContains(t, w.Body.String(), "Synthetic explicit handoff")
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "admin", body).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "reader", "").Body.String())
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Go(func() { codes[i] = testutil.Do(r, "PUT", path, "admin", body).Code })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(body, "Synthetic explicit handoff", "Changed intent", 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", masterPath+"/department-links/"+uuid.Must(uuid.NewV7()).String(), "admin", body).Code)
	for _, invalid := range []string{
		strings.Replace(body, `"expected_version":0,`, "", 1),
		strings.Replace(body, `"expected_version":0`, `"expected_version":null`, 1),
		strings.Replace(body, `"expected_version":0`, `"expected_version":-1`, 1),
		strings.Replace(body, `"expected_version":0`, `"expected_version":9007199254740991`, 1),
		strings.Replace(body, "Synthetic explicit handoff", "   ", 1),
		strings.Replace(body, `"reason":`, `"unknown":true,"reason":`, 1),
		body + ` {}`,
	} {
		require.Equal(t, 400, testutil.Do(r, "PUT", path, "admin", invalid).Code)
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "reader", body).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	w = testutil.Do(r, "GET", masterPath+"/history", "reader", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.NotContains(t, w.Body.String(), `"version":2`)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workforce_audit").Where("action='workforce.department_link.requested'").Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count, "exact retries retain one command audit")
		return nil
	}))
	for _, mutation := range []string{`UPDATE workforce_department_links SET version=3 WHERE id=?`, `UPDATE workforce_department_links SET department_id=gen_random_uuid() WHERE id=?`, `DELETE FROM workforce_department_links WHERE id=?`} {
		require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_department_observation','authorized',true)`, hospital).Error; err != nil {
				return err
			}
			return tx.Exec(mutation, request).Error
		}))
	}
}

type departmentLinkFailureModule struct {
	*workforce.Module
	target string
}

func (m departmentLinkFailureModule) Migrations() []tenantdb.Migration {
	sql := `ALTER TABLE outbox_events ADD CONSTRAINT test_department_link_outbox_fault CHECK(subject<>'helivanta.in.workforce.department_link_requested.v1') NOT VALID;`
	if m.target == "audit" {
		sql = `ALTER TABLE workforce_audit ADD CONSTRAINT test_department_link_audit_fault CHECK(action<>'workforce.department_link.requested') NOT VALID;`
	}
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9000_workforce_department_link_test_fault", SQL: sql})
}
func TestDepartmentHandoffRollsBackWhenAuditOrOutboxFails(t *testing.T) {
	for _, target := range []string{"audit", "outbox"} {
		t.Run(target, func(t *testing.T) {
			r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermMasterManage}}, Modules: []platform.Module{departmentLinkFailureModule{workforce.New(), target}}})
			hospital, master := uuid.NewString(), uuid.NewString()
			base := "/v1/workforce/hospitals/" + hospital
			require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Rollback Hospital"}`).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
			masterPath := base + "/hr/masters/" + master
			w := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":0,"kind":"department","code":"SYN-PHARMA","name":"Synthetic rollback source","active":true}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			var source struct {
				RevisionID string `json:"revision_id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &source))
			path := masterPath + "/department-links/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"expected_version":0,"master_revision_id":%q,"department_id":%q,"department_revision_id":%q,"reason":"Synthetic rollback"}`, source.RevisionID, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
			require.Equal(t, 500, testutil.Do(r, "PUT", path, "admin", body).Code)
			require.Equal(t, 404, testutil.Do(r, "GET", path, "admin", "").Code)
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table("workforce_audit").Where("action='workforce.department_link.requested'").Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count)
				return nil
			}))
		})
	}
}
