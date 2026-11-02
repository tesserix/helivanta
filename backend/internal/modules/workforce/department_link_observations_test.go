package workforce_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type departmentReplyFixture struct {
	router     *gin.Engine
	db         *tenantdb.DB
	path, body string
	row        workforce.DepartmentLinkOutput
	consumer   events.Consumer
}

func pendingDepartmentReply(t *testing.T, module platform.Module) departmentReplyFixture {
	t.Helper()
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermMasterManage}}, Modules: []platform.Module{module}})
	hospital, master := uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Department Reply Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	masterPath := base + "/hr/masters/" + master
	w := testutil.Do(r, "PUT", masterPath, "admin", `{"expected_version":0,"kind":"department","code":"SYN-REPLY","name":"Synthetic Reply Source","active":true}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var source struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &source))
	fixture := departmentReplyFixture{router: r, db: db, path: masterPath + "/department-links/" + uuid.Must(uuid.NewV7()).String()}
	fixture.body = fmt.Sprintf(`{"expected_version":0,"master_revision_id":%q,"department_id":%q,"department_revision_id":%q,"reason":"Synthetic retained reply fixture"}`, source.RevisionID, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
	w = testutil.Do(r, "PUT", fixture.path, "admin", fixture.body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fixture.row))
	for _, c := range module.Consumers(platform.Deps{Bus: bus}) {
		if c.Name == "workforce-department-link-observation" {
			fixture.consumer = c
		}
	}
	require.NotNil(t, fixture.consumer.Handle)
	return fixture
}

func (f departmentReplyFixture) event(t *testing.T, matched bool) events.Event {
	t.Helper()
	raw, err := json.Marshal(organizationcontract.DepartmentLinkObservedData{RequestID: f.row.RequestID, HospitalID: f.row.HospitalID, MasterID: f.row.MasterID, MasterRevisionID: f.row.MasterRevisionID, DepartmentID: f.row.DepartmentID, DepartmentRevisionID: f.row.DepartmentRevisionID, MatchesCurrent: &matched, ObservedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return events.Event{Type: organizationcontract.SubjectDepartmentLinkObserved, Version: 1, TenantID: testutil.TenantA, Data: raw}
}

func (f departmentReplyFixture) apply(t *testing.T, event events.Event) error {
	t.Helper()
	return f.db.WithTenant(t.Context(), event.TenantID, func(tx *gorm.DB) error { return f.consumer.Handle(t.Context(), tx, event) })
}

func TestDepartmentReplyRejectsMalformedOrMismatchedClaimsWithoutDeciding(t *testing.T) {
	f := pendingDepartmentReply(t, workforce.New())
	valid := f.event(t, false)
	change := func(field string, value any) []byte {
		var fields map[string]any
		require.NoError(t, json.Unmarshal(valid.Data, &fields))
		if value == nil {
			delete(fields, field)
		} else {
			fields[field] = value
		}
		raw, err := json.Marshal(fields)
		require.NoError(t, err)
		return raw
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"null", []byte(`null`)}, {"empty", []byte(`{}`)},
		{"unknown field", change("unreviewed", true)},
		{"extra object", append(append([]byte{}, valid.Data...), []byte(` {}`)...)},
		{"missing boolean", change("matches_current", nil)},
		{"null boolean", change("matches_current", json.RawMessage(`null`))},
		{"string boolean", change("matches_current", "false")},
		{"number boolean", change("matches_current", 0)},
		{"missing observation time", change("observed_at", nil)},
		{"nil hospital", change("hospital_id", uuid.Nil)},
		{"legacy request", change("request_id", uuid.New())},
		{"unknown request", change("request_id", uuid.Must(uuid.NewV7()))},
		{"other hospital", change("hospital_id", uuid.New())},
		{"other HR master", change("master_id", uuid.New())},
		{"other HR revision", change("master_revision_id", uuid.New())},
		{"other department", change("department_id", uuid.Must(uuid.NewV7()))},
		{"other department revision", change("department_revision_id", uuid.Must(uuid.NewV7()))},
		{"oversized", []byte(strings.Repeat(" ", 2049))},
	} {
		t.Run(tc.name, func(t *testing.T) { evt := valid; evt.Data = tc.data; require.Error(t, f.apply(t, evt)) })
	}
	for _, tc := range []struct {
		name   string
		change func(*events.Event)
	}{
		{"type", func(e *events.Event) { e.Type = "unexpected" }},
		{"version", func(e *events.Event) { e.Version = 2 }},
		{"directed", func(e *events.Event) { e.DestinationTenantID = testutil.TenantB }},
		{"foreign tenant", func(e *events.Event) { e.TenantID = testutil.TenantB }},
	} {
		t.Run(tc.name, func(t *testing.T) { evt := valid; tc.change(&evt); require.Error(t, f.apply(t, evt)) })
	}
	w := testutil.Do(f.router, "GET", f.path, "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending"`)
	require.NoError(t, f.apply(t, valid))
	require.NoError(t, f.apply(t, valid))
	w = testutil.Do(f.router, "GET", f.path, "admin", "")
	require.Contains(t, w.Body.String(), `"status":"rejected"`)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workforce_audit").Where("action='workforce.department_link.observed'").Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}

type departmentReplyFaultModule struct{ *workforce.Module }

func (m departmentReplyFaultModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9003_workforce_department_reply_fault", SQL: `ALTER TABLE workforce_audit ADD CONSTRAINT department_reply_test_fault CHECK(action<>'workforce.department_link.observed') NOT VALID;`})
}

func TestDepartmentReplyAuditFailurePreservesPendingAndCanBeReplayed(t *testing.T) {
	f := pendingDepartmentReply(t, departmentReplyFaultModule{workforce.New()})
	evt := f.event(t, true)
	require.Error(t, f.apply(t, evt))
	w := testutil.Do(f.router, "GET", f.path, "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending"`)
	require.NotContains(t, w.Body.String(), `"observed_at"`)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "9004_workforce_department_reply_repair", SQL: `ALTER TABLE workforce_audit DROP CONSTRAINT department_reply_test_fault;`}}))
	require.NoError(t, f.apply(t, evt))
	require.NoError(t, f.apply(t, evt))
	w = testutil.Do(f.router, "GET", f.path, "admin", "")
	require.Contains(t, w.Body.String(), `"status":"matched"`)
}

func TestOldDepartmentReplyCannotOverwriteNewerMappingOrTerminalObservation(t *testing.T) {
	f := pendingDepartmentReply(t, workforce.New())
	first := f.event(t, true)
	require.NoError(t, f.apply(t, first))
	old := testutil.Do(f.router, "GET", f.path, "admin", "").Body.String()
	newPath := strings.TrimSuffix(f.path, f.row.RequestID.String()) + uuid.Must(uuid.NewV7()).String()
	newBody := strings.Replace(f.body, `"expected_version":0`, `"expected_version":1`, 1)
	w := testutil.Do(f.router, "PUT", newPath, "admin", newBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	var newer workforce.DepartmentLinkOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &newer))
	secondFixture := f
	secondFixture.row = newer
	second := secondFixture.event(t, false)
	require.NoError(t, f.apply(t, second))
	require.NoError(t, f.apply(t, first))
	w = testutil.Do(f.router, "GET", newPath, "admin", "")
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Contains(t, w.Body.String(), `"status":"rejected"`)
	require.JSONEq(t, old, testutil.Do(f.router, "GET", f.path, "admin", "").Body.String())
	contradiction := f.event(t, false)
	require.Error(t, f.apply(t, contradiction))
	var changed organizationcontract.DepartmentLinkObservedData
	require.NoError(t, json.Unmarshal(first.Data, &changed))
	changed.ObservedAt = changed.ObservedAt.Add(time.Second)
	var err error
	contradiction.Data, err = json.Marshal(changed)
	require.NoError(t, err)
	require.Error(t, f.apply(t, contradiction))
	require.JSONEq(t, old, testutil.Do(f.router, "GET", f.path, "admin", "").Body.String())
}
