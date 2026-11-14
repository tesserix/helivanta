package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

// Wire names are pinned here so a rename is a visible contract change.
const (
	departmentScopeRequested = "helivanta.in.iam.assignment_department_scope_requested.v1"
	departmentVerified       = "helivanta.in.organization.assignment_department_verified.v1"
	departmentVerifier       = "iam-assignment-department-scope"
)

type departmentProposalRow struct {
	RequestID string
	Status    string
	Version   int64
}

func TestDepartmentProposalNeedsBothScopeOwners(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga,
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, "iam.assignment.manage"},
			"reviewer": {"iam.assignment.review"},
		},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(membershipIdentity{}), assignmentWorkforceModule()},
	})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		return w
	}
	hospital := uuid.NewString()
	department := uuid.Must(uuid.NewV7()).String()
	for _, command := range []struct{ path, body string }{
		{"/v1/workforce/hospitals/" + hospital, `{"name":"Synthetic department scope"}`},
		{"/v1/workforce/hospitals/" + hospital + "/access/user-admin", `{"level":"manager"}`},
		{"/v1/workforce/hospitals/" + hospital + "/access/user-reviewer", `{"level":"manager"}`},
	} {
		w := call("PUT", command.path, "admin", command.body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	for subject, role := range map[string]string{"user-admin": "tenant_admin", "user-reviewer": "tenant_admin", "user-department0x": "staff_member"} {
		w := call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":%q}`, subject, role))
		require.Equal(t, 202, w.Code, w.Body.String())
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		member, err := fga.IsMember(t.Context(), "user-department0x", testutil.TenantA)
		require.NoError(c, err)
		require.True(c, member)
	}, 20*time.Second, 100*time.Millisecond)

	proposal := func(template, departmentField string) string {
		return fmt.Sprintf(`{"hospital_id":%q,%s"subject":"user-department0x","template_key":%q,"reason":"Synthetic department review","valid_from":%q,"expires_at":%q}`,
			hospital, departmentField, template, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	}
	departmentField := fmt.Sprintf(`"department_id":%q,`, department)
	ctx := t.Context()
	row := func(t require.TestingT, id string) departmentProposalRow {
		var out departmentProposalRow
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.assignment_department_scope','enabled',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_assignment_proposals").Select("request_id,status,version").Where("id=?", id).Take(&out).Error
		}))
		return out
	}
	answers := func(t require.TestingT, id, request string) map[string]bool {
		var rows []struct {
			Owner    string
			Eligible bool
		}
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_assignment_department_verifications").Select("owner,eligible").Where("proposal_id=? AND request_id=?", id, request).Find(&rows).Error
		}))
		out := map[string]bool{}
		for _, r := range rows {
			out[r.Owner] = r.Eligible
		}
		return out
	}
	deliver := func(t *testing.T, subject, consumer string, payload map[string]any) {
		t.Helper()
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		eventID := uuid.Must(uuid.NewV7()).String()
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return bus.Publish(tx, subject, events.Event{ID: eventID, Type: subject, Version: 1, TenantID: testutil.TenantA, Data: data})
		}))
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var count int64
			require.NoError(c, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("processed_events").Where("event_id=? AND consumer=?", eventID, consumer).Count(&count).Error
			}))
			require.Equal(c, int64(1), count)
		}, 20*time.Second, 100*time.Millisecond)
	}
	answer := func(t *testing.T, id, request, departmentID string, eligible bool) {
		t.Helper()
		deliver(t, departmentVerified, departmentVerifier, map[string]any{"proposal_id": id, "request_id": request, "hospital_id": hospital, "department_id": departmentID, "eligible": eligible})
	}
	workforceAnswered := func(t *testing.T, id string) departmentProposalRow {
		t.Helper()
		var current departmentProposalRow
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			current = row(c, id)
			_, ok := answers(c, id, current.RequestID)["workforce"]
			require.True(c, ok, "Workforce answers the current request")
		}, 20*time.Second, 100*time.Millisecond)
		return current
	}

	t.Run("proposals bind the template to its scope kind", func(t *testing.T) {
		for _, bad := range []string{
			proposal("workforce.department_posting_reviewer.v1", ""),
			proposal("workforce.posting_reviewer.v1", departmentField),
			proposal("workforce.department_posting_reviewer.v1", fmt.Sprintf(`"department_id":%q,`, uuid.NewString())),
			proposal("workforce.department_posting_reviewer.v1", `"department_id":null,`),
			proposal("workforce.department_posting_reviewer.v1", fmt.Sprintf(`"department_id":%q,`, uuid.Nil)),
		} {
			w := call("PUT", "/v1/iam/assignment-proposals/"+uuid.Must(uuid.NewV7()).String(), "admin", bad)
			require.Equal(t, 400, w.Code, bad)
		}
	})

	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/iam/assignment-proposals/" + id
	body := proposal("workforce.department_posting_reviewer.v1", departmentField)
	w := call("PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var created iam.ProposalOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.Equal(t, "department", created.ScopeKind)
	require.NotNil(t, created.Input.DepartmentID)
	require.Equal(t, department, created.Input.DepartmentID.String())
	require.Equal(t, "pending_scope", created.Status)

	t.Run("replays are idempotent and a changed department conflicts", func(t *testing.T) {
		w := call("PUT", path, "admin", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		w = call("PUT", path, "admin", proposal("workforce.department_posting_reviewer.v1", fmt.Sprintf(`"department_id":%q,`, uuid.Must(uuid.NewV7()))))
		require.Equal(t, 409, w.Code, w.Body.String())
		var requests []struct{ Payload []byte }
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT payload FROM outbox_events WHERE subject=? AND payload->'data'->>'proposal_id'=?`, departmentScopeRequested, id).Scan(&requests).Error
		}))
		require.Len(t, requests, 1, "one Organization request per proposal command")
		var evt events.Event
		require.NoError(t, json.Unmarshal(requests[0].Payload, &evt))
		require.JSONEq(t, fmt.Sprintf(`{"proposal_id":%q,"request_id":%q,"hospital_id":%q,"department_id":%q}`, id, row(t, id).RequestID, hospital, department), string(evt.Data))
	})

	first := workforceAnswered(t, id)
	t.Run("Workforce alone leaves the proposal pending", func(t *testing.T) {
		require.Equal(t, "pending_scope", first.Status, "Organization has not answered")
		require.Equal(t, int64(1), first.Version)
	})
	t.Run("stale and mismatched Organization answers are ignored", func(t *testing.T) {
		answer(t, id, uuid.Must(uuid.NewV7()).String(), department, true)
		answer(t, id, first.RequestID, uuid.Must(uuid.NewV7()).String(), true)
		answer(t, uuid.Must(uuid.NewV7()).String(), first.RequestID, department, true)
		deliver(t, "helivanta.in.workforce.assignment_scope_verified.v1", "iam-assignment-scope", map[string]any{"proposal_id": id, "request_id": first.RequestID, "hospital_id": hospital, "eligible": false})
		require.Equal(t, first, row(t, id))
		require.True(t, answers(t, id, first.RequestID)["workforce"], "the first Workforce answer is retained")
		require.NotContains(t, answers(t, id, first.RequestID), "organization")
	})
	t.Run("both eligible answers move the proposal to review", func(t *testing.T) {
		answer(t, id, first.RequestID, department, true)
		current := row(t, id)
		require.Equal(t, "pending_review", current.Status)
		require.Equal(t, int64(2), current.Version)
		answer(t, id, first.RequestID, department, false)
		require.Equal(t, current, row(t, id), "a duplicate answer for the same request changes nothing")
		require.Equal(t, map[string]bool{"workforce": true, "organization": true}, answers(t, id, first.RequestID))
		var actor string
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("audit_events").Select("actor_subject").Where("resource_id=? AND action='proposal_pending_review'", id).Take(&actor).Error
		}))
		require.Equal(t, "system:organization-department-verifier", actor)
	})

	t.Run("an Organization refusal rejects the scope", func(t *testing.T) {
		other := uuid.Must(uuid.NewV7()).String()
		w := call("PUT", "/v1/iam/assignment-proposals/"+other, "admin", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		answer(t, other, row(t, other).RequestID, department, false)
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			require.Equal(c, "scope_rejected", row(c, other).Status)
		}, 20*time.Second, 100*time.Millisecond)
	})

	decide := func(t *testing.T, decision string) departmentProposalRow {
		t.Helper()
		current := row(t, id)
		w := call("POST", path+"/decision", "reviewer", fmt.Sprintf(`{"expected_version":%d,"decision":%q,"reason":"Synthetic department decision"}`, current.Version, decision))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"pending_decision_scope"`)
		return workforceAnswered(t, id)
	}
	t.Run("a department retired before approval returns the proposal to review", func(t *testing.T) {
		pending := decide(t, "approve")
		require.Equal(t, "pending_decision_scope", pending.Status, "approval waits for Organization")
		require.NotEqual(t, first.RequestID, pending.RequestID, "the decision asks both owners again")
		answer(t, id, first.RequestID, department, true)
		require.Equal(t, pending, row(t, id), "an answer for the superseded request is ignored")
		answer(t, id, pending.RequestID, department, false)
		require.Equal(t, "pending_review", row(t, id).Status)
	})
	t.Run("approval needs both fresh answers", func(t *testing.T) {
		pending := decide(t, "approve")
		answer(t, id, pending.RequestID, department, true)
		require.Equal(t, "approved_pending_activation", row(t, id).Status)
		var scope struct {
			ScopeKind    string
			DepartmentID string
		}
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.assignment_department_scope','enabled',true)`).Error; err != nil {
				return err
			}
			return tx.Table("iam_assignment_snapshots").Select("scope_kind,department_id").Where("id=?", id).Order("version DESC").Take(&scope).Error
		}))
		require.Equal(t, "department", scope.ScopeKind)
		require.Equal(t, department, scope.DepartmentID)
	})
	t.Run("history and reads carry the department", func(t *testing.T) {
		w := call("GET", path, "admin", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"scope_kind":"department"`)
		require.Contains(t, w.Body.String(), department)
		w = call("GET", "/v1/iam/posting-assignment-proposals?limit=10", "admin", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), id)
	})
}
