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
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

// Wire permission names are pinned here; foreign implementations come only from bootstrap.
const assignmentHospitalManage authz.Permission = "workforce.hospital.manage"
const assignmentLeaveApprove authz.Permission = "workforce.leave.approve"

func assignmentWorkforceModule() platform.Module {
	for _, module := range bootstrap.Modules(nil, nil) {
		if module.Name() == "workforce" {
			return module
		}
	}
	panic("bootstrap must register the workforce owner")
}

func TestAssignmentProposalVerifiesScopeThroughOwnerEvents(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga,
		Tokens: map[string]string{"admin": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB, "candidate": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, "iam.assignment.manage"},
			"outsider": {"iam.assignment.manage"}, "foreign": {"iam.assignment.manage", iam.PermMemberManage},
		},
		Modules: []platform.Module{iam.New(nil), assignmentWorkforceModule()},
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
	base := "/v1/workforce/hospitals/" + hospital
	for _, command := range []struct{ path, body string }{
		{base, `{"name":"Synthetic assignment scope"}`},
		{base + "/access/user-admin", `{"level":"manager"}`},
	} {
		w := call("PUT", command.path, "admin", command.body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	for _, actor := range []string{"admin", "outsider"} {
		w := call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"tenant_admin"}`, "user-"+actor))
		require.Equal(t, 202, w.Code, w.Body.String())
	}
	foreignMember := call("POST", "/v1/iam/members", "foreign", `{"subject":"user-foreign","role_key":"tenant_admin"}`)
	require.Equal(t, 202, foreignMember.Code, foreignMember.Body.String())
	w := call("POST", "/v1/iam/members", "admin", `{"subject":"user-candidate0x","role_key":"staff_member"}`)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		member, err := fga.IsMember(t.Context(), "user-candidate0x", testutil.TenantA)
		require.NoError(c, err)
		require.True(c, member)
	}, 20*time.Second, 100*time.Millisecond)

	body := fmt.Sprintf(`{"hospital_id":%q,"subject":"user-candidate0x","template_key":"workforce.leave_reviewer.v1","reason":"Synthetic independent leave review","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	path := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	w = call("PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending_scope"`)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		w = call("GET", path, "admin", "")
		require.Equal(c, 200, w.Code, w.Body.String())
		require.Contains(c, w.Body.String(), `"status":"pending_review"`)
	}, 20*time.Second, 100*time.Millisecond)
	w = call("PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	w = call("PUT", path, "admin", strings.Replace(body, "Synthetic independent leave review", "Changed reason", 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	var proof iam.ScopeResult
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Table("iam_assignment_proposals").Select("id AS proposal_id,request_id,hospital_id").Where("id=?", strings.TrimPrefix(path, "/v1/iam/assignment-proposals/")).Take(&proof).Error
	}))
	replayID := uuid.Must(uuid.NewV7()).String()
	data, err := json.Marshal(proof)
	require.NoError(t, err)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.workforce.assignment_scope_verified.v1", events.Event{ID: replayID, Type: "helivanta.in.workforce.assignment_scope_verified.v1", Version: 1, TenantID: testutil.TenantA, Data: data})
	}))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.NoError(c, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			var count int64
			err := tx.Table("processed_events").Where("event_id=? AND consumer=?", replayID, "iam-assignment-scope").Count(&count).Error
			require.Equal(c, int64(1), count)
			return err
		}))
	}, 20*time.Second, 100*time.Millisecond)
	w = call("GET", path, "admin", "")
	require.Contains(t, w.Body.String(), `"status":"pending_review"`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		var count int64
		err := tx.Table("iam_assignment_proposals").Count(&count).Error
		require.Zero(t, count)
		return err
	}))
	require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE iam_assignment_proposal_history SET status='scope_rejected'`).Error
	}))
	permissions, err := fga.Resolve(t.Context(), "user-candidate0x", testutil.TenantA)
	require.NoError(t, err)
	require.Empty(t, permissions)
	w = call("GET", path+"/history", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending_scope"`)
	require.Contains(t, w.Body.String(), `"status":"pending_review"`)
	var history struct{ Data []iam.ProposalHistory }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history.Data, 2)
	w = call("GET", "/v1/iam/assignment-proposals?limit=1", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic independent leave review")
	w = call("GET", path, "foreign", "")
	require.Equal(t, 404, w.Code, w.Body.String())
	w = call("GET", path, "candidate", "")
	require.Equal(t, 403, w.Code, w.Body.String())

	for _, bad := range []struct {
		body   string
		status int
	}{
		{strings.Replace(body, "user-candidate0x", "unknown-member", 1), 404},
		{strings.Replace(body, "workforce.leave_reviewer.v1", "tenant_admin", 1), 400},
		{strings.Replace(body, "user-candidate0x", "bad subject", 1), 400},
		{body + `{}`, 400},
		{strings.Replace(body, `"reason":`, `"unexpected":true,"reason":`, 1), 400},
	} {
		badPath := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
		w = call("PUT", badPath, "admin", bad.body)
		require.Equal(t, bad.status, w.Code, w.Body.String())
	}
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	mismatch := httptest.NewRecorder()
	gateway.ServeHTTP(mismatch, req)
	require.Equal(t, 409, mismatch.Code, mismatch.Body.String())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.iam.assignment_scope_requested.v1").Count(&count).Error
		require.Equal(t, int64(1), count, "retries and rejected input must not create new owner requests")
		return err
	}))
	other := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	w = call("PUT", other, "outsider", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		w = call("GET", other, "outsider", "")
		require.Equal(c, 200, w.Code, w.Body.String())
		require.Contains(c, w.Body.String(), `"status":"scope_rejected"`)
	}, 20*time.Second, 100*time.Millisecond)
}

func TestAssignmentDecisionsRequireIndependentScopedReview(t *testing.T) {
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "outsider": testutil.TenantA, "target": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, "iam.assignment.manage", "iam.assignment.review"},
			"reviewer": {"iam.assignment.review"}, "outsider": {"iam.assignment.review"}, "target": {"iam.assignment.review"},
		}, Modules: []platform.Module{iam.New(nil), assignmentWorkforceModule()},
	})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	call("PUT", base, "admin", `{"name":"Synthetic reviewed access"}`, 200)
	for _, actor := range []string{"admin", "reviewer", "outsider", "target"} {
		call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"tenant_admin"}`, "user-"+actor), 202)
	}
	for _, actor := range []string{"admin", "reviewer"} {
		call("PUT", base+"/access/user-"+actor, "admin", `{"level":"manager"}`, 200)
	}
	body := fmt.Sprintf(`{"hospital_id":%q,"subject":"user-target","template_key":"workforce.leave_reviewer.v1","reason":"Synthetic reviewed scope","valid_from":%q,"expires_at":%q}`, hospital, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	path := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", path, "admin", body, 200)
	wait := func(status string, version int64) {
		t.Helper()
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			w := call("GET", path, "admin", "", 200)
			var response iam.ProposalOutput
			require.NoError(c, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(c, status, response.Status)
			require.Equal(c, version, response.Version)
		}, 20*time.Second, 100*time.Millisecond)
	}
	wait("pending_review", 2)
	const approve = `{"expected_version":2,"decision":"approve","reason":"Independent synthetic verification"}`
	call("POST", path+"/decision", "admin", approve, 403)
	call("POST", path+"/decision", "target", approve, 403)
	call("GET", path, "reviewer", "", 200)
	call("POST", path+"/decision", "outsider", approve, 200)
	wait("pending_review", 4)
	const reviewed = `{"expected_version":4,"decision":"approve","reason":"Independent synthetic verification"}`
	call("POST", path+"/decision", "reviewer", reviewed, 200)
	wait("approved_pending_activation", 6)
	call("POST", path+"/decision", "reviewer", reviewed, 200)
	call("POST", path+"/decision", "reviewer", strings.Replace(reviewed, "verification", "changed", 1), 409)
	const withdraw = `{"expected_version":6,"decision":"withdraw","reason":"Synthetic proposal withdrawn before activation"}`
	call("POST", path+"/decision", "reviewer", withdraw, 403)
	call("POST", path+"/decision", "admin", withdraw, 200)
	wait("withdrawn", 7)
	call("POST", path+"/decision", "admin", withdraw, 200)
	w := call("GET", path+"/decisions", "reviewer", "", 200)
	require.Contains(t, w.Body.String(), "user-reviewer")
	require.Contains(t, w.Body.String(), "Synthetic proposal withdrawn before activation")
	require.Contains(t, w.Body.String(), "user-outsider")
	call("POST", path+"/decision", "reviewer", `{"expected_version":7,"decision":"approve","reason":"Cannot reopen"}`, 409)
	var oldScope iam.ScopeResult
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Table("iam_assignment_decisions").Select("proposal_id,request_id").Where("expected_version=4 AND decision='approve'").Take(&oldScope).Error
	}))
	oldScope.HospitalID = uuid.MustParse(hospital)
	oldScope.Eligible = true
	replayID := uuid.Must(uuid.NewV7()).String()
	data, err := json.Marshal(oldScope)
	require.NoError(t, err)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.workforce.assignment_scope_verified.v1", events.Event{ID: replayID, Type: "helivanta.in.workforce.assignment_scope_verified.v1", Version: 1, TenantID: testutil.TenantA, Data: data})
	}))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.NoError(c, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			var count int64
			err := tx.Table("processed_events").Where("event_id=? AND consumer=?", replayID, "iam-assignment-scope").Count(&count).Error
			require.Equal(c, int64(1), count)
			return err
		}))
	}, 20*time.Second, 100*time.Millisecond)
	wait("withdrawn", 7)
	path = "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", path, "admin", body, 200)
	wait("pending_review", 2)
	call("POST", path+"/decision", "reviewer", `{"expected_version":2,"decision":"reject","reason":"Synthetic independent rejection"}`, 200)
	wait("rejected", 4)
	call("POST", path+"/decision", "reviewer", `{"expected_version":4,"decision":"approve","reason":"Cannot reopen rejection"}`, 409)
	call("PUT", base+"/access/user-outsider", "admin", `{"level":"manager"}`, 200)
	path = "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", path, "admin", body, 200)
	wait("pending_review", 2)
	codes := make(chan int, 2)
	for _, token := range []string{"reviewer", "outsider"} {
		go func(actor string) {
			req := httptest.NewRequest("POST", path+"/decision", strings.NewReader(approve))
			req.Header.Set("Authorization", "Bearer "+actor)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			gateway.ServeHTTP(response, req)
			codes <- response.Code
		}(token)
	}
	require.ElementsMatch(t, []int{200, 409}, []int{<-codes, <-codes})
	wait("approved_pending_activation", 4)
	w = call("GET", path+"/decisions", "reviewer", "", 200)
	var commands struct {
		Data []iam.ProposalDecisionRecord
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &commands))
	require.Len(t, commands.Data, 1)
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if tenant == testutil.TenantB {
				if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
					return err
				}
			}
			var count int64
			err := tx.Table("iam_assignment_decisions").Count(&count).Error
			require.Zero(t, count, "decision reasons require both tenant isolation and the access guard")
			return err
		}))
	}
	for _, mutation := range []string{
		`UPDATE iam_assignment_decisions SET reason='tampered'`,
		`DELETE FROM iam_assignment_decisions`,
		`TRUNCATE iam_assignment_decisions`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(mutation).Error
		})
		if mutation == `TRUNCATE iam_assignment_decisions` {
			require.ErrorContains(t, err, "permission denied for table iam_assignment_decisions")
		} else {
			require.ErrorContains(t, err, "proposal history is immutable")
		}
	}
	// Fixture withdrawal tests current reviewer authority, not owner-governance recovery.
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-admin',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM iam_members WHERE subject='user-reviewer' AND role_key='tenant_admin'`).Error
	}))
	call("GET", path, "reviewer", "", 403)
	call("POST", path+"/decision", "reviewer", approve, 403)
}
