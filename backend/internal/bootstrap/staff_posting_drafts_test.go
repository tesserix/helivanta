package bootstrap_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingDraftFixture struct {
	t                                     *testing.T
	handler                               http.Handler
	db                                    *tenantdb.DB
	graph                                 *authz.Client
	hospital, staff, department, revision string
}

func (f *postingDraftFixture) call(method, path, actor string, input any) *httptest.ResponseRecorder {
	f.t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		require.NoError(f.t, err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+actor)
	r.Header.Set("Content-Type", "application/json")
	tenant := testutil.TenantA
	if actor == "foreign" {
		tenant = testutil.TenantB
	}
	r.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	r.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func newPostingDraftFixture(t *testing.T) *postingDraftFixture {
	return newPostingDraftFixtureWithFault(t, "")
}

type postingDraftFaultModule struct {
	platform.Module
	sql string
}

func (m postingDraftFaultModule) Migrations() []tenantdb.Migration {
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9001_posting_draft_fault", SQL: m.sql})
}

func newPostingDraftFixtureWithFault(t *testing.T, fault string) *postingDraftFixture {
	t.Helper()
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	registry := platform.NewRegistry()
	var modules []platform.Module
	for _, m := range bootstrap.Modules(nil, nil) {
		if m.Name() == "workforce" || m.Name() == "organization" {
			if m.Name() == "workforce" && fault != "" {
				m = postingDraftFaultModule{Module: m, sql: fault}
			}
			modules = append(modules, m)
			require.NoError(t, registry.Register(m))
		}
	}
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, platform.ReconcileTenant(t.Context(), registry, fga, tenant))
		require.NoError(t, fga.GrantTenantRole(t.Context(), tenant, authz.RoleTenantAdmin))
	}
	for _, actor := range []string{"manager", "reader"} {
		require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-"+actor, authz.RoleTenantAdmin))
	}
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantB, "user-foreign", authz.RoleTenantAdmin))
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Modules: modules, Resolver: fga, Membership: fga, Writer: fga, Roles: fga,
	})
	f := &postingDraftFixture{t: t, handler: r, db: db, graph: fga, hospital: uuid.NewString(), staff: uuid.Must(uuid.NewV7()).String(), department: uuid.Must(uuid.NewV7()).String(), revision: uuid.Must(uuid.NewV7()).String()}
	base := "/v1/workforce/hospitals/" + f.hospital
	w := f.call("PUT", base, "manager", map[string]any{"name": "Synthetic Posting Draft Hospital"})
	require.Equal(t, 200, w.Code, w.Body.String())
	for actor, level := range map[string]string{"manager": "manager", "reader": "reader"} {
		w = f.call("PUT", base+"/access/user-"+actor, "manager", map[string]any{"level": level})
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w = f.call("PUT", base+"/staff/"+f.staff, "manager", map[string]any{"employee_code": "SYN-POSTING-PHARMA", "display_name": "Synthetic Canonical Posting Applicant", "job_category": "pharmacist", "employment_status": "inactive"})
	require.Equal(t, 200, w.Code, w.Body.String())
	registration := "/v1/organization/hospitals/" + f.hospital + "/registrations/" + uuid.Must(uuid.NewV7()).String()
	w = f.call("PUT", registration, "manager", map[string]any{"reason": "Synthetic canonical hospital posting reference"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		return bytes.Contains(f.call("GET", registration, "manager", nil).Body.Bytes(), []byte(`"status":"verified"`))
	}, 10*time.Second, 20*time.Millisecond)
	w = f.call("PUT", "/v1/organization/hospitals/"+f.hospital+"/departments/"+f.department+"/revisions/"+f.revision, "manager", map[string]any{"expected_version": 0, "code": "SYN-PHARMA", "name": "Synthetic Canonical Pharmacy", "kind": "department", "status": "active", "reason": "Synthetic canonical structure"})
	require.Equal(t, 200, w.Code, w.Body.String())
	return f
}

func TestStaffPostingDraftPinsCanonicalSourceWithoutAssigningStaff(t *testing.T) {
	f := newPostingDraftFixture(t)
	staffPath := "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff
	draft := uuid.Must(uuid.NewV7()).String()
	path := staffPath + "/posting-drafts/" + draft + "/commands/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_version": 0, "staff_version": 1, "employment_epoch": 1, "department_id": f.department, "department_revision_id": f.revision, "starts_at": "2027-01-01T00:00:00Z", "reason": "Synthetic proposed Pharmacy placement; no operating authority"}
	w := f.call("PUT", path, "manager", input)
	require.Equal(t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, draft, out["id"])
	require.Equal(t, "draft", out["status"])
	require.Equal(t, false, out["grants_access"])
	require.Equal(t, float64(1), out["version"])
	require.Equal(t, f.department, out["department_id"])
	require.Equal(t, f.revision, out["department_revision_id"])
	require.NotContains(t, w.Body.String(), "Synthetic proposed Pharmacy placement")
	require.JSONEq(t, w.Body.String(), f.call("PUT", path, "manager", input).Body.String())
	require.JSONEq(t, w.Body.String(), f.call("GET", staffPath+"/posting-drafts/"+draft, "reader", nil).Body.String())
	unchanged := f.call("GET", staffPath, "manager", nil)
	require.Equal(t, 200, unchanged.Code)
	require.Contains(t, unchanged.Body.String(), `"employment_status":"inactive"`)
	require.Contains(t, unchanged.Body.String(), fmt.Sprintf(`"id":%q`, f.staff))
	links := f.call("GET", staffPath+"/login-links", "manager", nil)
	require.Equal(t, 200, links.Code)
	require.Contains(t, links.Body.String(), `"data":[]`)
}

func TestStaffPostingDraftSourceReleasesOnlyItsConcurrencyBasis(t *testing.T) {
	f := newPostingDraftFixture(t)
	path := "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff + "/posting-draft-source"
	w := f.call("GET", path, "manager", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "staff_id": f.staff, "staff_version": float64(1), "employment_epoch": float64(1)}, out)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, 404, f.call("GET", path, "foreign", nil).Code)
	require.NoError(t, f.graph.RevokeRole(t.Context(), testutil.TenantA, "user-manager", authz.RoleTenantAdmin))
	require.Equal(t, 403, f.call("GET", path, "manager", nil).Code)
}

func TestStaffPostingDraftEditsRetainExactReceiptsAndPagedHistory(t *testing.T) {
	f := newPostingDraftFixture(t)
	root := "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff + "/posting-drafts/" + uuid.Must(uuid.NewV7()).String()
	firstPath := root + "/commands/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_version": 0, "staff_version": 1, "employment_epoch": 1, "department_id": f.department, "department_revision_id": f.revision, "starts_at": "2027-01-01T00:00:00Z", "reason": "Synthetic initial posting draft"}
	first := f.call("PUT", firstPath, "manager", input)
	require.Equal(t, 200, first.Code, first.Body.String())
	changed := maps.Clone(input)
	changed["expected_version"], changed["starts_at"], changed["ends_at"], changed["reason"] = 1, "2027-02-01T00:00:00Z", "2027-06-01T00:00:00Z", "Synthetic corrected placement dates"
	second := f.call("PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), "manager", changed)
	require.Equal(t, 200, second.Code, second.Body.String())
	require.Contains(t, second.Body.String(), `"version":2`)
	require.JSONEq(t, first.Body.String(), f.call("PUT", firstPath, "manager", input).Body.String())
	receipt := f.call("GET", firstPath, "reader", nil)
	require.Equal(t, 200, receipt.Code, receipt.Body.String())
	require.JSONEq(t, first.Body.String(), receipt.Body.String())
	require.JSONEq(t, second.Body.String(), f.call("GET", root, "reader", nil).Body.String())
	require.Equal(t, 409, f.call("PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), "manager", changed).Code)
	page := f.call("GET", root+"/history?limit=1", "reader", nil)
	require.Equal(t, 200, page.Code, page.Body.String())
	var out struct {
		Data []json.RawMessage `json:"data"`
		Page struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &out))
	require.Len(t, out.Data, 1)
	require.JSONEq(t, second.Body.String(), string(out.Data[0]))
	require.True(t, out.Page.HasMore)
	last := f.call("GET", root+"/history?limit=1&cursor="+url.QueryEscape(out.Page.NextCursor), "reader", nil)
	require.Equal(t, 200, last.Code, last.Body.String())
	require.NoError(t, json.Unmarshal(last.Body.Bytes(), &out))
	require.Len(t, out.Data, 1)
	require.JSONEq(t, first.Body.String(), string(out.Data[0]))
	require.False(t, out.Page.HasMore)
}

func TestStaffPostingDraftRejectsInvalidRequiredFieldsAndIntervals(t *testing.T) {
	f := newPostingDraftFixture(t)
	root := "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff + "/posting-drafts/"
	valid := map[string]any{"expected_version": 0, "staff_version": 1, "employment_epoch": 1, "department_id": f.department, "department_revision_id": f.revision, "starts_at": "2027-01-01T00:00:00Z", "reason": "Synthetic validation evidence"}
	for _, field := range []string{"expected_version", "staff_version", "employment_epoch", "department_id", "department_revision_id", "starts_at", "reason"} {
		t.Run("missing_"+field, func(t *testing.T) {
			input := maps.Clone(valid)
			delete(input, field)
			w := f.call("PUT", root+uuid.Must(uuid.NewV7()).String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "manager", input)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	for name, change := range map[string]map[string]any{
		"null_version":                       {"expected_version": nil},
		"unknown_field":                      {"grants_access": true},
		"empty_reason":                       {"reason": "  "},
		"equal_end":                          {"ends_at": "2027-01-01T00:00:00Z"},
		"earlier_end":                        {"ends_at": "2026-12-31T23:59:59Z"},
		"zero_after_precision_normalization": {"starts_at": "0001-01-01T00:00:00.0000001Z"},
	} {
		t.Run(name, func(t *testing.T) {
			input := maps.Clone(valid)
			maps.Copy(input, change)
			w := f.call("PUT", root+uuid.Must(uuid.NewV7()).String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "manager", input)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	w := f.call("GET", strings.TrimSuffix(root, "/"), "manager", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"data":[]`)
}

func (f *postingDraftFixture) draftCommand() (string, map[string]any) {
	f.t.Helper()
	return "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff + "/posting-drafts/" + uuid.Must(uuid.NewV7()).String() + "/commands/" + uuid.Must(uuid.NewV7()).String(),
		map[string]any{"expected_version": 0, "staff_version": 1, "employment_epoch": 1, "department_id": f.department, "department_revision_id": f.revision, "starts_at": "2027-01-01T00:00:00Z", "reason": "Synthetic canonical placement proposal"}
}

func TestStaffPostingDraftReauthorizesReadsWritesAndRetainedCommands(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	w := f.call("PUT", path, "reader", input)
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Equal(t, 404, f.call("PUT", path, "foreign", input).Code)
	w = f.call("PUT", path, "manager", input)
	require.Equal(t, 200, w.Code, w.Body.String())
	root, _, _ := strings.Cut(path, "/commands/")
	for _, readPath := range []string{root, root + "/history", path} {
		require.Equal(t, 404, f.call("GET", readPath, "foreign", nil).Code)
		require.Equal(t, 200, f.call("GET", readPath, "reader", nil).Code)
	}
	access := "/v1/workforce/hospitals/" + f.hospital + "/access/user-manager"
	w = f.call("DELETE", access, "manager", nil)
	require.Equal(t, 204, w.Code, w.Body.String())
	for _, readPath := range []string{root, root + "/history", path} {
		require.Equal(t, 404, f.call("GET", readPath, "manager", nil).Code)
	}
	require.Equal(t, 404, f.call("PUT", path, "manager", input).Code)
	require.NoError(t, f.graph.RevokeRole(t.Context(), testutil.TenantA, "user-reader", authz.RoleTenantAdmin))
	require.Equal(t, 403, f.call("GET", path, "reader", nil).Code)
}

func TestStaffPostingDraftRejectsChangedIntentAndActorForRetainedCommand(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	first := f.call("PUT", path, "manager", input)
	require.Equal(t, 200, first.Code, first.Body.String())
	changed := maps.Clone(input)
	changed["reason"] = "Synthetic changed intent on reused command"
	require.Equal(t, 409, f.call("PUT", path, "manager", changed).Code)
	w := f.call("PUT", "/v1/workforce/hospitals/"+f.hospital+"/access/user-reader", "manager", map[string]any{"level": "manager"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, f.call("PUT", path, "reader", input).Code)
	require.JSONEq(t, first.Body.String(), f.call("PUT", path, "manager", input).Body.String())
}

func TestStaffPostingDraftRejectsStaleStaffAndDepartmentButKeepsHistoricalReceipt(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	first := f.call("PUT", path, "manager", input)
	require.Equal(t, 200, first.Code, first.Body.String())
	staffPath := "/v1/workforce/hospitals/" + f.hospital + "/staff/" + f.staff
	w := f.call("PUT", staffPath, "manager", map[string]any{"employee_code": "SYN-POSTING-PHARMA", "display_name": "Synthetic Corrected Applicant", "job_category": "pharmacist", "employment_status": "inactive"})
	require.Equal(t, 200, w.Code, w.Body.String())
	newPath, _ := f.draftCommand()
	require.Equal(t, 409, f.call("PUT", newPath, "manager", input).Code)
	current := f.call("GET", staffPath+"/posting-draft-source", "manager", nil)
	require.Equal(t, 200, current.Code, current.Body.String())
	var source struct {
		StaffVersion    int64 `json:"staff_version"`
		EmploymentEpoch int64 `json:"employment_epoch"`
	}
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &source))
	require.Greater(t, source.StaffVersion, int64(1))
	updated := maps.Clone(input)
	updated["staff_version"], updated["employment_epoch"] = source.StaffVersion, source.EmploymentEpoch
	deptPath := "/v1/organization/hospitals/" + f.hospital + "/departments/" + f.department + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	w = f.call("PUT", deptPath, "manager", map[string]any{"expected_version": 1, "code": "SYN-PHARMA", "name": "Synthetic retired department", "kind": "department", "status": "retired", "reason": "Synthetic retirement for stale draft acceptance"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, f.call("PUT", newPath, "manager", updated).Code)
	require.JSONEq(t, first.Body.String(), f.call("PUT", path, "manager", input).Body.String())
	require.JSONEq(t, first.Body.String(), f.call("GET", path, "reader", nil).Body.String())
}

func TestStaffPostingDraftConcurrentCommandsPreserveOneVersion(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	root, _, _ := strings.Cut(path, "/commands/")
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		commandPath := root + "/commands/" + uuid.Must(uuid.NewV7()).String()
		go func() {
			<-start
			results <- f.call("PUT", commandPath, "manager", input)
		}()
	}
	close(start)
	codes := map[int]int{}
	for range 2 {
		w := <-results
		codes[w.Code]++
	}
	require.Equal(t, map[int]int{200: 1, 409: 1}, codes)
	w := f.call("GET", root+"/history", "reader", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	var history struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history.Data, 1)
}

func TestStaffPostingDraftAuditAndOutboxFailuresRollbackEveryWrite(t *testing.T) {
	for name, fault := range map[string]string{
		"audit":  `ALTER TABLE workforce_audit ADD CONSTRAINT posting_draft_audit_fault CHECK(action<>'workforce.posting_draft.saved') NOT VALID;`,
		"outbox": `ALTER TABLE outbox_events ADD CONSTRAINT posting_draft_outbox_fault CHECK(subject<>'helivanta.in.workforce.posting_draft_changed.v1') NOT VALID;`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newPostingDraftFixtureWithFault(t, fault)
			path, input := f.draftCommand()
			w := f.call("PUT", path, "manager", input)
			require.Equal(t, 500, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "Synthetic canonical placement proposal")
			require.Equal(t, 404, f.call("GET", path, "manager", nil).Code)
			for _, table := range []string{"workforce_posting_drafts", "workforce_posting_draft_revisions"} {
				var count int64
				require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting','read',true)`, f.hospital, f.staff).Error; err != nil {
						return err
					}
					return tx.Table(table).Count(&count).Error
				}))
				require.Zero(t, count, table)
			}
			var audits, events int64
			require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
					return err
				}
				if err := tx.Table("workforce_audit").Where("action=?", "workforce.posting_draft.saved").Count(&audits).Error; err != nil {
					return err
				}
				return tx.Table("outbox_events").Where("tenant_id=? AND subject=?", testutil.TenantA, "helivanta.in.workforce.posting_draft_changed.v1").Count(&events).Error
			}))
			require.Zero(t, audits)
			require.Zero(t, events)
		})
	}
}

func TestStaffPostingDraftDatabaseScopeAndHistoryRemainPrivate(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	w := f.call("PUT", path, "manager", input)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, table := range []string{"workforce_posting_drafts", "workforce_posting_draft_revisions"} {
		for name, context := range map[string]struct{ hospital, staff, purpose string }{
			"unset":              {},
			"different_staff":    {f.hospital, uuid.NewString(), "read"},
			"different_hospital": {uuid.NewString(), f.staff, "read"},
			"different_purpose":  {f.hospital, f.staff, "credential_review"},
			"exact":              {f.hospital, f.staff, "read"},
		} {
			t.Run(table+"/"+name, func(t *testing.T) {
				var count int64
				require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting',?,true)`, context.hospital, context.staff, context.purpose).Error; err != nil {
						return err
					}
					return tx.Table(table).Count(&count).Error
				}))
				if name == "exact" {
					require.EqualValues(t, 1, count)
				} else {
					require.Zero(t, count)
				}
			})
		}
	}
	for _, statement := range []string{
		`UPDATE workforce_posting_draft_revisions SET reason='Synthetic attempted history rewrite'`,
		`DELETE FROM workforce_posting_draft_revisions`,
		`DELETE FROM workforce_posting_drafts`,
	} {
		err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting','write',true)`, f.hospital, f.staff).Error; err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
		require.Error(t, err)
	}
	require.JSONEq(t, w.Body.String(), f.call("GET", path, "reader", nil).Body.String())
}

func TestStaffPostingDraftReturnsUTCInstantsWithoutLosingMicroseconds(t *testing.T) {
	f := newPostingDraftFixture(t)
	path, input := f.draftCommand()
	input["starts_at"] = "0001-01-01T00:00:00.000001Z"
	input["ends_at"] = "2027-01-01T01:00:00.1234567+01:00"
	w := f.call("PUT", path, "manager", input)
	require.Equal(t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, "0001-01-01T00:00:00.000001Z", out["starts_at"])
	require.Equal(t, "2027-01-01T00:00:00.123456Z", out["ends_at"])
	for _, field := range []string{"created_at", "recorded_at"} {
		require.True(t, strings.HasSuffix(out[field].(string), "Z"), field)
	}
	root, _, _ := strings.Cut(path, "/commands/")
	for _, readPath := range []string{root, path} {
		require.JSONEq(t, w.Body.String(), f.call("GET", readPath, "reader", nil).Body.String())
	}
	for _, readPath := range []string{root + "/history", strings.Split(root, "/posting-drafts/")[0] + "/posting-drafts"} {
		page := f.call("GET", readPath, "reader", nil)
		require.Equal(t, 200, page.Code, page.Body.String())
		var rows struct {
			Data []json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(page.Body.Bytes(), &rows))
		require.Len(t, rows.Data, 1)
		require.JSONEq(t, w.Body.String(), string(rows.Data[0]))
	}
}
