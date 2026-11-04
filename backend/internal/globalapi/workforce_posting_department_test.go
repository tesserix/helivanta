package globalapi

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// scopedCheckBarrier pauses the first graph check of one subject and permission.
type scopedCheckBarrier struct {
	subject         string
	permission      authz.Permission
	paused, release chan struct{}
}

func (b *scopedCheckBarrier) matches(body []byte) bool {
	return bytes.Contains(body, []byte(`"user:`+b.subject+`"`)) && bytes.Contains(body, []byte(`/`+string(b.permission)+`"`))
}

type postingDepartmentFixture struct {
	call                                    func(*testing.T, string, string, string, any, int) map[string]any
	request                                 func(method, path, subject string, body []byte, contentType string) *httptest.ResponseRecorder
	grantDepartment                         func(subject, department string, path []string)
	pauseScopedCheck                        func(subject string, permission authz.Permission) (<-chan struct{}, func())
	unavailable                             *atomic.Bool
	db                                      *tenantdb.DB
	hospital, base, staffBase, organization string
	submitter, reviewer, maker              string
	parent, sub, sibling                    string
	parentInput, subInput                   map[string]any
	draft                                   string
	receipt, source, generation             map[string]any
}

func (f postingDepartmentFixture) reviewScope(t *testing.T) map[string]any {
	t.Helper()
	for _, row := range f.call(t, "GET", "/v1/workforce/posting-review-scopes", f.reviewer, nil, 200)["data"].([]any) {
		if scope := row.(map[string]any); scope["hospital_id"] == f.hospital {
			return scope
		}
	}
	return nil
}

func (f postingDepartmentFixture) revise(t *testing.T, department string, input map[string]any, change map[string]any) {
	t.Helper()
	for key, value := range change {
		input[key] = value
	}
	path := f.organization + "/departments/" + department + "/revisions/" + uuid.Must(uuid.NewV7()).String()
	saved := f.call(t, "PUT", path, f.maker, input, 200)
	input["expected_version"] = saved["version"]
}

// widenParent seeds sub-units under the held parent until its subtree exceeds the bound, and returns their retirement.
func (f postingDepartmentFixture) widenParent(t *testing.T) func() {
	t.Helper()
	write := func(statements ...[]any) {
		t.Helper()
		require.NoError(t, f.db.WithTenant(tenantdb.WithActor(t.Context(), f.maker), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; err != nil {
				return err
			}
			for _, statement := range statements {
				if err := tx.Exec(statement[0].(string), statement[1:]...).Error; err != nil {
					return err
				}
			}
			return nil
		}))
	}
	prefix := func() string { return uuid.Must(uuid.NewV7()).String()[:24] }
	// The parent and its remaining sub-unit plus these nodes total exactly one past the bound.
	write([]any{`INSERT INTO organization_departments(tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,effective_at,created_at,updated_at)
 SELECT ?::uuid,?::uuid,(?||lpad(to_hex(n),12,'0'))::uuid,(?||lpad(to_hex(n),12,'0'))::uuid,'SYN-P-WIDE-'||n,'sub_unit','Synthetic wide sub-unit',?::uuid,'active',1,
 transaction_timestamp(),transaction_timestamp(),transaction_timestamp() FROM generate_series(1,?::int) n`, testutil.TenantA, f.hospital, prefix(), prefix(), f.parent, authz.DepartmentSubtreeLimit - 1},
		[]any{`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,kind,name,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
 SELECT tenant_id,hospital_id,id,revision_id,code,kind,name,parent_id,status,version,0,'Synthetic wide fixture',?,effective_at,created_at,created_at,updated_at
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND code LIKE 'SYN-P-WIDE-%'`, f.maker, testutil.TenantA, f.hospital})
	return func() {
		t.Helper()
		write([]any{`INSERT INTO organization_department_revisions(tenant_id,hospital_id,node_id,id,code,kind,name,parent_id,status,version,expected_version,reason,actor_subject,effective_at,node_created_at,created_at,updated_at)
 SELECT tenant_id,hospital_id,id,(?||lpad(to_hex(substring(code from 12)::int),12,'0'))::uuid,code,kind,name,parent_id,'retired',2,1,'Synthetic wide retirement',?,transaction_timestamp(),created_at,transaction_timestamp(),transaction_timestamp()
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND code LIKE 'SYN-P-WIDE-%' AND status='active'`, prefix(), f.maker, testutil.TenantA, f.hospital},
			[]any{`UPDATE organization_departments d SET revision_id=r.id,status='retired',version=2,effective_at=r.effective_at,updated_at=r.updated_at
 FROM organization_department_revisions r WHERE d.tenant_id=? AND d.hospital_id=? AND d.code LIKE 'SYN-P-WIDE-%' AND d.status='active'
 AND (r.tenant_id,r.hospital_id,r.node_id,r.version)=(d.tenant_id,d.hospital_id,d.id,2)`, testutil.TenantA, f.hospital})
	}
}

func departmentChoiceIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	ids := []string{}
	for _, row := range body["data"].([]any) {
		ids = append(ids, row.(map[string]any)["id"].(string))
	}
	return ids
}

// assertPostingDepartmentReviewer drives a department-only reviewer through the Global API (ADR-0055).
func assertPostingDepartmentReviewer(t *testing.T, f postingDepartmentFixture) {
	t.Helper()
	require.Nil(t, f.reviewScope(t), "a reviewer without review authority discovers no hospital")
	proposal := f.staffBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pending := f.call(t, "PUT", proposal+"/commands/"+uuid.Must(uuid.NewV7()).String(), f.submitter, map[string]any{"expected_version": 0, "draft_id": f.draft, "draft_revision_id": f.receipt["revision_id"], "draft_version": f.receipt["version"], "generation_id": f.generation["id"], "staff_version": f.source["staff_version"], "employment_epoch": f.source["employment_epoch"], "reason": "Synthetic sub-unit placement for department review", "sources_examined": true}, 200)
	require.Equal(t, f.sub, pending["department_id"])
	f.grantDepartment(f.reviewer, f.parent, []string{f.sub, f.parent})

	scope := f.reviewScope(t)
	require.NotNil(t, scope, "a department grant discovers its hospital")
	require.Equal(t, []any{f.parent}, scope["departments"])
	require.Equal(t, true, scope["can_review"])
	require.Equal(t, false, scope["can_submit"], "the department reviewer template cannot submit")

	choices := f.organization + "/posting-department-choices?limit=50"
	f.call(t, "GET", choices, f.reviewer, nil, 403)
	require.ElementsMatch(t, []string{f.parent, f.sub}, departmentChoiceIDs(t, f.call(t, "GET", choices+"&department="+f.parent, f.reviewer, nil, 200)))
	require.Equal(t, []string{f.sub}, departmentChoiceIDs(t, f.call(t, "GET", choices+"&department="+f.sub, f.reviewer, nil, 200)))
	f.call(t, "GET", choices+"&department="+f.sibling, f.reviewer, nil, 404)
	f.call(t, "GET", choices+"&department=not-a-department", f.reviewer, nil, 400)

	inbox := f.base + "/posting-proposal-inbox"
	f.call(t, "GET", inbox, f.reviewer, nil, 403)
	listed := f.call(t, "GET", inbox+"?department="+f.parent, f.reviewer, nil, 200)["data"].([]any)
	require.Len(t, listed, 1, "the parent's inbox covers the sub-unit")
	require.Equal(t, pending["id"], listed[0].(map[string]any)["proposal"].(map[string]any)["id"])
	f.call(t, "GET", inbox+"?department="+f.sibling, f.reviewer, nil, 404)

	source := f.staffBase + "/posting-review-source"
	f.call(t, "GET", source, f.reviewer, nil, 403)
	require.Equal(t, f.generation["id"], f.call(t, "GET", source+"?department="+f.sub, f.reviewer, nil, 200)["generation_id"])
	f.call(t, "GET", source+"?department="+f.sibling, f.reviewer, nil, 404)

	review := fmt.Sprintf("%s/context?purpose=review&expected_version=1&expected_revision_id=%s", proposal, pending["revision_id"])
	f.call(t, "GET", review, f.reviewer, nil, 403)
	f.call(t, "GET", review+"&department="+f.sibling, f.reviewer, nil, 404)
	f.call(t, "GET", review+"&department="+f.parent, f.reviewer, nil, 404)
	require.Equal(t, pending["id"], f.call(t, "GET", review+"&department="+f.sub, f.reviewer, nil, 200)["proposal"].(map[string]any)["id"])
	submission := fmt.Sprintf("%s/context?purpose=submission&expected_version=1&expected_revision_id=%s&department=%s", proposal, pending["revision_id"], f.sub)
	f.call(t, "GET", submission, f.reviewer, nil, 403)

	func() {
		f.unavailable.Store(true)
		defer f.unavailable.Store(false)
		f.call(t, "GET", "/v1/workforce/posting-review-scopes", f.reviewer, nil, 503)
		f.call(t, "GET", inbox+"?department="+f.parent, f.reviewer, nil, 503)
		f.call(t, "GET", choices+"&department="+f.parent, f.reviewer, nil, 503)
	}()

	retireWide := f.widenParent(t)
	f.call(t, "GET", inbox+"?department="+f.parent, f.reviewer, nil, 503)
	f.call(t, "GET", choices+"&department="+f.parent, f.reviewer, nil, 503)
	require.Len(t, f.call(t, "GET", inbox+"?department="+f.sub, f.reviewer, nil, 200)["data"], 1, "a bounded inner subtree still answers")
	retireWide()
	require.ElementsMatch(t, []string{f.parent, f.sub}, departmentChoiceIDs(t, f.call(t, "GET", choices+"&department="+f.parent, f.reviewer, nil, 200)))

	decision := proposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	rejection := map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "reject", "reason": "Independent department review rejection", "sources_examined": true}
	f.grantDepartment(f.submitter, f.parent, []string{f.sub, f.parent})
	require.Len(t, f.call(t, "GET", inbox+"?department="+f.parent, f.submitter, nil, 200)["data"], 1, "the submitter now holds department review there")
	f.call(t, "GET", inbox+"?department="+uuid.Must(uuid.NewV7()).String(), f.submitter, nil, 404)
	f.call(t, "PUT", decision+"?department="+f.sub, f.submitter, rejection, 403)
	require.Len(t, f.call(t, "GET", inbox+"?department="+f.parent, f.reviewer, nil, 200)["data"], 1, "a department grant never permits self-review; the proposal stays pending")
	f.call(t, "PUT", decision, f.reviewer, rejection, 403)
	f.call(t, "PUT", decision+"?department="+f.sibling, f.reviewer, rejection, 404)
	rejected := f.call(t, "PUT", decision+"?department="+f.sub, f.reviewer, rejection, 200)
	require.Equal(t, "rejected", rejected["status"])
	require.Equal(t, rejected, f.call(t, "GET", decision+"?department="+f.sub, f.reviewer, nil, 200))
	f.call(t, "GET", decision, f.reviewer, nil, 403)
	require.Len(t, f.call(t, "GET", inbox+"?department="+f.parent+"&status=rejected", f.reviewer, nil, 200)["data"], 1)

	paused, release := f.pauseScopedCheck(f.reviewer, "workforce.posting.proposal.read")
	defer release()
	moved := make(chan *httptest.ResponseRecorder, 1)
	go func() { moved <- f.request("GET", source+"?department="+f.sub, f.reviewer, nil, "application/json") }()
	select {
	case <-paused:
	case <-time.After(10 * time.Second):
		release()
		t.Fatal("the review-source read never reached its department graph check")
	}
	f.revise(t, f.sub, f.subInput, map[string]any{"parent_id": f.sibling, "reason": "Synthetic sub-unit move between hint and lock"})
	release()
	response := <-moved
	require.Equal(t, 409, response.Code, "a path changed after the hint is refused, never allowed: %s", response.Body.String())
	require.Contains(t, response.Body.String(), `"department_scope_changed"`)
	f.call(t, "GET", source+"?department="+f.sub, f.reviewer, nil, 404)
	require.Empty(t, f.call(t, "GET", inbox+"?department="+f.parent+"&status=rejected", f.reviewer, nil, 200)["data"], "moving a sub-unit out removes it from the held subtree")

	paused, release = f.pauseScopedCheck(f.reviewer, "workforce.posting.proposal.read")
	defer release()
	retiring := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		retiring <- f.request("GET", inbox+"?department="+f.parent, f.reviewer, nil, "application/json")
	}()
	select {
	case <-paused:
	case <-time.After(10 * time.Second):
		release()
		t.Fatal("the inbox read never reached its department graph check")
	}
	f.revise(t, f.parent, f.parentInput, map[string]any{"status": "retired", "reason": "Synthetic held department retirement"})
	release()
	response = <-retiring
	require.Equal(t, 409, response.Code, "a department retired after the hint authorizes nothing: %s", response.Body.String())
	require.Contains(t, response.Body.String(), `"department_scope_changed"`)
	require.Nil(t, f.reviewScope(t), "a retired held department stops discovering its hospital")
	f.call(t, "GET", inbox+"?department="+f.parent, f.reviewer, nil, 404)
	f.call(t, "GET", choices+"&department="+f.parent, f.reviewer, nil, 404)
}
