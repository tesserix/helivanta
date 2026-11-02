package organization_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func departmentHarness(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context, string) {
	t.Helper()
	return departmentHarnessWithModule(t, organization.New())
}
func departmentHarnessWithModule(t *testing.T, module platform.Module) (*gin.Engine, *tenantdb.DB, context.Context, string) {
	t.Helper()
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read"}, "reader": {"organization.structure.read"}, "foreign": {"organization.structure.manage", "organization.structure.read"}},
		Modules: []platform.Module{module},
	})
	hospital := uuid.NewString()
	request := uuid.Must(uuid.NewV7())
	response := organizationCall(r, "PUT", "/v1/organization/hospitals/"+hospital+"/registrations/"+request.String(), "manager", testutil.TenantA, `{"reason":"Synthetic department reference"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	for _, consumer := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if consumer.Name != "organization-hospital-observation" {
			continue
		}
		data, err := json.Marshal(map[string]any{"request_id": request, "hospital_id": hospital, "exists": true})
		require.NoError(t, err)
		evt := events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
	}
	return r, db, ctx, hospital
}
func departmentPath(hospital, node, revision string) string {
	return "/v1/organization/hospitals/" + hospital + "/departments/" + node + "/revisions/" + revision
}
func departmentIntent(version int, code, name, kind, parent, status string) string {
	parentJSON := "null"
	if parent != "" {
		parentJSON = fmt.Sprintf("%q", parent)
	}
	return fmt.Sprintf(`{"expected_version":%d,"code":%q,"name":%q,"kind":%q,"parent_id":%s,"status":%q,"reason":"Synthetic structure command"}`, version, code, name, kind, parentJSON, status)
}
func TestDepartmentCreateUsesVerifiedHospitalAndImmutableReceipt(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	node := uuid.Must(uuid.NewV7()).String()
	revision := uuid.Must(uuid.NewV7()).String()
	path := departmentPath(hospital, node, revision)
	body := departmentIntent(0, " phArma ", " Hospital Pharmacy ", "department", "", "active")
	w := organizationCall(r, "PUT", path, "manager", testutil.TenantA, body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &wire))
	require.JSONEq(t, fmt.Sprintf(`%q`, node), string(wire["id"]))
	require.JSONEq(t, `"PHARMA"`, string(wire["code"]))
	require.JSONEq(t, `"Hospital Pharmacy"`, string(wire["name"]))
	require.JSONEq(t, `1`, string(wire["version"]))
	retry := organizationCall(r, "PUT", path, "manager", testutil.TenantA, departmentIntent(0, "PHARMA", "Hospital Pharmacy", "department", "", "active"))
	require.JSONEq(t, w.Body.String(), retry.Body.String())
	require.Equal(t, 403, organizationCall(r, "PUT", path, "reader", testutil.TenantA, body).Code)
	require.Equal(t, 404, organizationCall(r, "GET", path, "foreign", testutil.TenantB, "").Code)
	list := organizationCall(r, "GET", "/v1/organization/hospitals/"+hospital+"/departments?limit=1", "reader", testutil.TenantA, "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), node)
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("organization_department_revisions").Count(&count).Error }))
	require.EqualValues(t, 1, count)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("action=? AND resource_type=? AND resource_id=?", "department_revision_recorded", "organization_department_revisions", revision).Count(&count).Error
	}))
	require.EqualValues(t, 1, count, "a department revision audit must identify the correct owner record")

}

func TestDepartmentRevisionsProtectMovesRetirementAndHistory(t *testing.T) {
	r, _, _, hospital := departmentHarness(t)
	root, other, child, grandchild := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	create := func(node, code, kind, parent string) string {
		t.Helper()
		revision := uuid.Must(uuid.NewV7()).String()
		w := organizationCall(r, "PUT", departmentPath(hospital, node, revision), "manager", testutil.TenantA, departmentIntent(0, code, code, kind, parent, "active"))
		require.Equal(t, 200, w.Code, w.Body.String())
		return revision
	}
	first := create(root, "PHARMA", "department", "")
	create(other, "NURSING", "department", "")
	create(child, "DISPENSARY", "sub_unit", root)
	create(grandchild, "COUNTER", "sub_unit", child)
	command := func(node string, version int, code, name, kind, parent, status string) int {
		t.Helper()
		w := organizationCall(r, "PUT", departmentPath(hospital, node, uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(version, code, name, kind, parent, status))
		return w.Code
	}
	require.Equal(t, 409, command(child, 1, "DISPENSARY", "Dispensary", "sub_unit", grandchild, "active"), "a subtree cannot be moved into itself")
	require.Equal(t, 409, command(root, 1, "PHARMA", "Pharmacy", "department", "", "retired"), "active children block retirement")
	revision := uuid.Must(uuid.NewV7()).String()
	w := organizationCall(r, "PUT", departmentPath(hospital, root, revision), "manager", testutil.TenantA, departmentIntent(1, "PHARMA", "Updated Pharmacy", "department", "", "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Equal(t, 409, command(root, 1, "PHARMA", "Stale Pharmacy", "department", "", "active"))
	require.Equal(t, 409, command(root, 2, "NEWCODE", "Updated Pharmacy", "department", "", "active"))
	require.Equal(t, 409, command(root, 2, "PHARMA", "Updated Pharmacy", "sub_unit", other, "active"))
	replay := organizationCall(r, "PUT", departmentPath(hospital, root, first), "manager", testutil.TenantA, departmentIntent(0, "PHARMA", "PHARMA", "department", "", "active"))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"version":1`)
	require.Equal(t, 409, organizationCall(r, "PUT", departmentPath(hospital, root, first), "manager", testutil.TenantA, departmentIntent(0, "PHARMA", "Different intent", "department", "", "active")).Code)
	history := organizationCall(r, "GET", "/v1/organization/hospitals/"+hospital+"/departments/"+root+"/revisions?limit=1", "reader", testutil.TenantA, "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var page struct {
		Items []json.RawMessage `json:"data"`
		Page  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Contains(t, string(page.Items[0]), revision)
	require.NotEmpty(t, page.Page.NextCursor)
	second := organizationCall(r, "GET", "/v1/organization/hospitals/"+hospital+"/departments/"+root+"/revisions?limit=1&cursor="+page.Page.NextCursor, "reader", testutil.TenantA, "")
	require.Equal(t, 200, second.Code, second.Body.String())
	require.Contains(t, second.Body.String(), first)
	require.NotContains(t, second.Body.String(), revision)
	require.Equal(t, 200, command(child, 1, "DISPENSARY", "Dispensary", "sub_unit", other, "active"))
	require.Equal(t, 200, command(root, 2, "PHARMA", "Updated Pharmacy", "department", "", "retired"))
	require.Equal(t, 409, command(root, 3, "PHARMA", "Reopened Pharmacy", "department", "", "active"))
	require.Equal(t, 409, command(grandchild, 1, "COUNTER", "Counter", "sub_unit", root, "active"))
}

func TestDepartmentDepthLimitAndConcurrentEditsRemainBounded(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	parent := ""
	root := ""
	for level := 1; level <= 9; level++ {
		node := uuid.Must(uuid.NewV7()).String()
		kind := "sub_unit"
		if level == 1 {
			kind = "department"
			root = node
		}
		code := fmt.Sprintf("LEVEL_%d", level)
		w := organizationCall(r, "PUT", departmentPath(hospital, node, uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(0, code, code, kind, parent, "active"))
		expected := 200
		if level == 9 {
			expected = 409
		}
		require.Equal(t, expected, w.Code, w.Body.String())
		parent = node
	}
	results := make(chan int, 2)
	for _, name := range []string{"Concurrent first", "Concurrent second"} {
		go func() {
			w := organizationCall(r, "PUT", departmentPath(hospital, root, uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(1, "LEVEL_1", name, "department", "", "active"))
			results <- w.Code
		}()
	}
	codes := []int{<-results, <-results}
	require.ElementsMatch(t, []int{200, 409}, codes)
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("organization_department_revisions").Where("node_id=?", root).Count(&count).Error
	}))
	require.EqualValues(t, 2, count, "a stale command must not append a revision")
}

func TestDepartmentCommandsDenyIncompleteOrForeignStructure(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	node := uuid.Must(uuid.NewV7()).String()
	path := departmentPath(hospital, node, uuid.Must(uuid.NewV7()).String())
	valid := departmentIntent(0, "PHARMA", "Pharmacy", "department", "", "active")
	for _, body := range []string{`{}`, `null`, `[]`, strings.Replace(valid, `"expected_version":0,`, "", 1), strings.Replace(valid, `"expected_version":0`, `"expected_version":null`, 1), strings.Replace(valid, `"kind":"department"`, `"kind":"committee"`, 1), strings.Replace(valid, `"name":"Pharmacy"`, `"name":""`, 1), strings.Replace(valid, `"code":"PHARMA"`, `"code":"BAD CODE"`, 1), strings.Replace(valid, `"reason":"Synthetic structure command"`, `"reason":""`, 1), strings.TrimSuffix(valid, "}") + `,"extra":true}`, valid + `{}`} {
		w := organizationCall(r, "PUT", path, "manager", testutil.TenantA, body)
		require.Equal(t, 400, w.Code, body+": "+w.Body.String())
	}
	pendingHospital := uuid.NewString()
	req := uuid.Must(uuid.NewV7()).String()
	w := organizationCall(r, "PUT", "/v1/organization/hospitals/"+pendingHospital+"/registrations/"+req, "manager", testutil.TenantA, `{"reason":"Synthetic pending structure source"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, organizationCall(r, "PUT", departmentPath(pendingHospital, node, uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, valid).Code)
	require.Equal(t, 409, organizationCall(r, "GET", "/v1/organization/hospitals/"+pendingHospital+"/departments", "manager", testutil.TenantA, "").Code)
	require.Equal(t, 404, organizationCall(r, "GET", "/v1/organization/hospitals/"+hospital+"/departments", "foreign", testutil.TenantB, "").Code)
	require.Equal(t, 403, organizationCall(r, "PUT", path, "reader", testutil.TenantA, valid).Code)
	require.Equal(t, 404, organizationCall(r, "PUT", path, "manager", testutil.TenantA, departmentIntent(0, "CHILD", "Child", "sub_unit", uuid.Must(uuid.NewV7()).String(), "active")).Code)
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("organization_departments").Count(&count).Error }))
	require.Zero(t, count)
}

func TestDepartmentOutboxFailureLeavesNoHeadRevisionOrAudit(t *testing.T) {
	r, db, ctx, hospital := departmentHarnessWithModule(t, departmentOutboxFailureModule{organization.New()})
	w := organizationCall(r, "PUT", departmentPath(hospital, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()), "manager", testutil.TenantA, departmentIntent(0, "PHARMA", "Pharmacy", "department", "", "active"))
	require.Equal(t, 500, w.Code, w.Body.String())
	for _, table := range []string{"organization_departments", "organization_department_revisions"} {
		var count int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Table(table).Count(&count).Error }))
		require.Zero(t, count, table)
	}
	var count int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("action='department_revision_recorded'").Count(&count).Error
	}))
	require.Zero(t, count)
}

type departmentOutboxFailureModule struct{ *organization.Module }

func (m departmentOutboxFailureModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9001_department_test_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT department_test_outbox_fault CHECK(subject<>'helivanta.in.organization.department_revision_changed.v1') NOT VALID;`})
}

func TestDepartmentDatabaseGuardsRetainIdentityAndHistory(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	node, id := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	w := organizationCall(r, "PUT", departmentPath(hospital, node, id), "manager", testutil.TenantA, departmentIntent(0, "PHARMA", "Pharmacy", "department", "", "active"))
	require.Equal(t, 200, w.Code, w.Body.String())

	ghostErr := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,name,kind,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
   SELECT tenant_id,hospital_id,id,?,code,name,kind,parent_id,status,version+1,version,'Synthetic uncommitted ghost','user-manager',effective_at+interval '1 second',created_at,effective_at+interval '1 second',effective_at+interval '1 second' FROM organization_departments WHERE id=?`, uuid.Must(uuid.NewV7()), node).Error
	})
	require.Error(t, ghostErr, "a revision cannot commit unless its node head advances with it")
	for _, query := range []string{
		`UPDATE organization_departments SET code='REWRITTEN' WHERE id=?`,
		`UPDATE organization_departments SET name='Unrecorded edit', version=2,effective_at=effective_at+interval '1 second',updated_at=updated_at+interval '1 second' WHERE id=?`,
		`DELETE FROM organization_departments WHERE id=?`,
	} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; e != nil {
				return e
			}
			return tx.Exec(query, node).Error
		})
		require.Error(t, err, query)
	}
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; e != nil {
			return e
		}
		return tx.Exec(`UPDATE organization_department_revisions SET reason='Rewritten evidence' WHERE id=?`, id).Error
	})
	require.Error(t, err)
	receipt := organizationCall(r, "GET", departmentPath(hospital, node, id), "reader", testutil.TenantA, "")
	require.Equal(t, 200, receipt.Code, receipt.Body.String())
	require.Contains(t, receipt.Body.String(), `"code":"PHARMA"`)
	require.Contains(t, receipt.Body.String(), `"version":1`)
}
