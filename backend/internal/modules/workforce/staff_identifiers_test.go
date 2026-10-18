package workforce_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/transit"
)

// Synthetic identifiers only; none is a real person's account, PAN or UAN.
const (
	synAccount = "000111222333"
	synIFSC    = "SYNT0000001"
	synPAN     = "AAAPZ1234Z"
	synOtherPA = "BBBPY5678Y"
	synUAN     = "100000000001"
)

type identifierFixture struct {
	*payrollHarness
	router *gin.Engine
	staff  string
}

func vaultClient(t *testing.T) *transit.Client {
	t.Helper()
	addr, _ := testinfra.StartOpenBao(t)
	user, password := testinfra.OpenBaoUser(t, time.Hour, "helivanta-api-transit")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	return c
}

func downVault(t *testing.T) *transit.Client {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	addr := server.URL
	server.Close()
	c, err := transit.New(addr, "transit", func(context.Context, *http.Client, string) (string, time.Duration, error) { return "t", time.Hour, nil }, &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	return c
}

func newIdentifierFixture(t *testing.T, vault *transit.Client) *identifierFixture {
	t.Helper()
	return newIdentifierFixtureWith(t, vault, nil)
}

func newIdentifierFixtureWith(t *testing.T, vault *transit.Client, files *attachments.GCS) *identifierFixture {
	t.Helper()
	all := []authz.Permission{workforce.PermPayrollConfigure, workforce.PermPayrollView, workforce.PermPayrollApprove, workforce.PermPayrollIdentifiersReveal}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Transit: vault,
		Tokens:  map[string]string{"admin": testutil.TenantA, "approver": testutil.TenantA, "auditor": testutil.TenantA, "viewer": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"admin": append([]authz.Permission{workforce.PermHospitalManage, workforce.PermMasterManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}, all...), "approver": all, "auditor": all,
			"viewer": {workforce.PermPayrollView, workforce.PermPayrollConfigure, workforce.PermPayrollApprove}, "reader": all, "foreign": all},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	h := &payrollHarness{t: t, db: db, hospital: uuid.NewString(), do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	h.base = "/v1/workforce/hospitals/" + h.hospital
	h.other = "/v1/workforce/hospitals/" + uuid.NewString()
	for _, base := range []string{h.base, h.other} {
		code, out := h.do("PUT", base, "admin", `{"name":"Synthetic Identifier Hospital"}`)
		require.Equal(t, 200, code, out)
	}
	for user, level := range map[string]string{"admin": "manager", "approver": "manager", "auditor": "manager", "viewer": "manager", "reader": "reader"} {
		code, out := h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	f := &identifierFixture{payrollHarness: h, router: r}
	f.staff = f.enrol("ID-01")
	return f
}

func (f *identifierFixture) enrol(code string) string {
	id := uuid.NewString()
	status, raw := f.do("PUT", f.base+"/staff/"+id, "admin", `{"employee_code":"`+code+`","display_name":"Synthetic `+code+`","job_category":"nurse","employment_status":"active"}`)
	require.Equal(f.t, 200, status, raw)
	return id
}

func keep() map[string]any  { return map[string]any{"action": "keep"} }
func clear() map[string]any { return map[string]any{"action": "clear"} }
func set(v string) map[string]any {
	return map[string]any{"action": "set", "value": v}
}
func bank(account string) map[string]any {
	return map[string]any{"action": "set", "account_number": account, "ifsc": synIFSC, "holder_name": "Synthetic Holder"}
}

func identifiersBody(version int, bankChange, pan, uan map[string]any) map[string]any {
	return map[string]any{"expected_version": version, "reason": "Synthetic onboarding", "bank": bankChange, "pan": pan, "uan": uan}
}

func (f *identifierFixture) idPath(staff, suffix string) string {
	return "/staff/" + staff + "/identifiers" + suffix
}

func (f *identifierFixture) propose(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", f.idPath(staff, ""), user, body)
}

func (f *identifierFixture) decideIDs(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("POST", f.idPath(staff, "/decision"), user, body)
}

func (f *identifierFixture) reveal(staff, user, revision, field string) (int, map[string]any) {
	return f.call("POST", f.idPath(staff, "/reveal"), user, map[string]any{"revision_id": revision, "field": field, "reason": "Synthetic payout check"})
}

// approvedIDs proposes as admin and approves as approver, returning the approved revision id.
func (f *identifierFixture) approvedIDs(staff string, body map[string]any) string {
	f.t.Helper()
	code, out := f.propose(staff, "admin", body)
	require.Equal(f.t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"]
	code, out = f.decideIDs(staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": body["expected_version"], "decision": "approve", "note": ""})
	require.Equal(f.t, 200, code, out)
	return proposal.(string)
}

// asManager runs a direct read under the admin's hospital grant, bypassing the API but not row security.
func (f *identifierFixture) asManager(run func(tx *gorm.DB) error) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return run(tx)
	})
}

// sealed reads one revision's stored ciphertext columns directly, bypassing the API.
func (f *identifierFixture) sealed(revision string) map[string]any {
	f.t.Helper()
	row := map[string]any{}
	require.NoError(f.t, f.asManager(func(tx *gorm.DB) error {
		return tx.Table("workforce_staff_identifier_revisions").Select("bank_account_ciphertext,pan_ciphertext,pan_index,uan_ciphertext,bank_account_masked,pan_masked,uan_masked").Where("id=?", revision).Take(&row).Error
	}))
	return row
}

func TestStaffIdentifiersNeedASecondPerson(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	code, out := f.call("GET", f.idPath(f.staff, ""), "reader", nil)
	require.Equal(t, 404, code, "nothing before a proposal: %v", out)

	body := with(identifiersBody(0, bank(" 0001 1122 2333 "), set(" aaapz1234z "), set(synUAN)), map[string]any{"reason": "  Synthetic   onboarding "})
	code, out = f.propose(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 0, out["version"])
	require.Nil(t, out["current"], "nothing is in force before approval")
	pending := out["pending"].(map[string]any)
	proposal := pending["id"].(string)
	require.Equal(t, "Synthetic onboarding", pending["reason"])
	require.Equal(t, "XXXX2333", pending["bank_account_masked"])
	require.Equal(t, synIFSC, pending["bank_ifsc"])
	require.Equal(t, "Synthetic Holder", pending["bank_holder_name"])
	require.Equal(t, "XXXXXX234Z", pending["pan_masked"])
	require.Equal(t, "XXXXXXXX0001", pending["uan_masked"])
	require.NotContains(t, pending, "pan_ciphertext", "ciphertext never leaves the API")

	code, out = f.propose(f.staff, "admin", body)
	requireSalaryConflict(t, code, out, "proposal_pending", "", "", "")
	code, out = f.propose(f.enrol("ID-02"), "admin", identifiersBody(1, bank(synAccount), keep(), keep()))
	require.Equal(t, 409, code, "a first proposal starts at version 0: %v", out)
	code, out = f.propose(uuid.NewString(), "admin", identifiersBody(0, bank(synAccount), keep(), keep()))
	require.Equal(t, 404, code, "an unknown staff member: %v", out)

	approve := map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "approve", "note": ""}
	code, out = f.decideIDs(f.staff, "admin", approve)
	require.Equal(t, 403, code, "the proposer cannot approve: %v", out)
	code, out = f.decideIDs(f.staff, "approver", with(approve, map[string]any{"expected_version": 1}))
	require.Equal(t, 409, code, "a stale version: %v", out)
	code, out = f.decideIDs(f.staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "reject", "note": " "})
	require.Equal(t, 400, code, "a rejection needs a note: %v", out)
	code, out = f.decideIDs(f.staff, "approver", approve)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Nil(t, out["pending"])
	current := out["current"].(map[string]any)
	require.Equal(t, proposal, current["id"])
	require.Equal(t, "approved", current["status"])
	require.EqualValues(t, 1, current["approved_version"])
	code, replay := f.decideIDs(f.staff, "approver", approve)
	require.Equal(t, 200, code, replay)
	require.Equal(t, out["version"], replay["version"], "a replayed decision changes nothing")
	code, out = f.decideIDs(f.staff, "auditor", approve)
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")

	code, out = f.call("GET", f.idPath(f.staff, ""), "reader", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "XXXXXX234Z", out["current"].(map[string]any)["pan_masked"])
	code, out = f.call("GET", f.idPath(f.staff, "/history"), "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)

	for user, status := range map[string]int{"foreign": 404, "": 401} {
		code, _ := f.do("GET", f.base+"/hr/payroll"+f.idPath(f.staff, ""), user, "")
		require.Equal(t, status, code, user)
	}
	for user, status := range map[string]int{"reader": 404, "foreign": 404} {
		code, out := f.propose(f.staff, user, identifiersBody(1, clear(), keep(), keep()))
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, raw := f.do("GET", f.other+"/hr/payroll"+f.idPath(f.staff, ""), "admin", "")
	require.Equal(t, 404, code, "another hospital: %s", raw)
	code, raw = f.do("POST", f.other+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "auditor", `{"revision_id":"`+proposal+`","field":"pan","reason":"Synthetic"}`)
	require.Equal(t, 404, code, "another hospital cannot reveal: %s", raw)
	code, raw = f.do("POST", f.base+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "foreign", `{"revision_id":"`+proposal+`","field":"pan","reason":"Synthetic"}`)
	require.Equal(t, 404, code, "another tenant cannot reveal: %s", raw)
}

func TestStaffIdentifierRejectAndWithdrawEraseCiphertext(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	code, out := f.propose(f.staff, "admin", identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	require.Equal(t, 200, code, out)
	rejected := out["pending"].(map[string]any)["id"].(string)
	require.NotNil(t, f.sealed(rejected)["pan_ciphertext"])
	code, out = f.decideIDs(f.staff, "approver", map[string]any{"proposal_id": rejected, "expected_version": 0, "decision": "reject", "note": "Synthetic mismatch"})
	require.Equal(t, 200, code, out)

	code, out = f.propose(f.staff, "admin", identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	require.Equal(t, 200, code, "a rejected PAN no longer blocks a proposal: %v", out)
	withdrawn := out["pending"].(map[string]any)["id"].(string)
	code, out = f.call("POST", f.idPath(f.staff, "/withdraw"), "approver", map[string]any{"proposal_id": withdrawn, "note": ""})
	require.Equal(t, 403, code, "only the proposer withdraws: %v", out)
	code, out = f.call("POST", f.idPath(f.staff, "/withdraw"), "admin", map[string]any{"proposal_id": withdrawn, "note": ""})
	require.Equal(t, 200, code, out)

	for _, id := range []string{rejected, withdrawn} {
		row := f.sealed(id)
		for _, col := range []string{"bank_account_ciphertext", "pan_ciphertext", "pan_index", "uan_ciphertext"} {
			require.Nil(t, row[col], "%s keeps no %s once decided against", id, col)
		}
		require.Equal(t, "XXXXXX234Z", row["pan_masked"], "the masked form survives as history")
		code, out = f.reveal(f.staff, "auditor", id, "pan")
		requireSalaryConflict(t, code, out, "not_revealable", "", "", "")
	}
	code, out = f.call("GET", f.idPath(f.staff, "/history"), "reader", nil)
	require.Equal(t, 200, code, out)
	statuses := []string{}
	for _, r := range out["data"].([]any) {
		statuses = append(statuses, r.(map[string]any)["status"].(string))
	}
	require.ElementsMatch(t, []string{"rejected", "withdrawn"}, statuses)
}

func TestStaffIdentifierKeepCopiesTheInForceCiphertext(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	first := f.approvedIDs(f.staff, identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	code, out := f.propose(f.staff, "admin", identifiersBody(1, keep(), keep(), keep()))
	requireSalaryConflict(t, code, out, "nothing_to_change", "", "", "")
	code, out = f.propose(f.staff, "admin", identifiersBody(1, keep(), set(synPAN), keep()))
	requireSalaryConflict(t, code, out, "nothing_to_change", "", "", "")

	second := f.approvedIDs(f.staff, identifiersBody(1, keep(), keep(), clear()))
	before, after := f.sealed(first), f.sealed(second)
	require.Equal(t, before["bank_account_ciphertext"], after["bank_account_ciphertext"], "keep copies the ciphertext")
	require.Equal(t, before["pan_ciphertext"], after["pan_ciphertext"])
	require.Equal(t, before["pan_index"], after["pan_index"])
	require.Nil(t, after["uan_ciphertext"])
	require.Nil(t, after["uan_masked"])

	code, out = f.reveal(f.staff, "auditor", second, "bank_account")
	require.Equal(t, 200, code, out)
	require.Equal(t, synAccount, out["value"], "a kept ciphertext still opens under its context")
	code, out = f.reveal(f.staff, "auditor", second, "uan")
	require.Equal(t, 404, code, "a cleared field has nothing to reveal: %v", out)
	code, out = f.reveal(f.staff, "auditor", first, "pan")
	requireSalaryConflict(t, code, out, "not_revealable", "", "", "")
}

func TestStaffIdentifierPANIsUniquePerHospital(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	other := f.enrol("ID-02")
	code, out := f.propose(f.staff, "admin", identifiersBody(0, keep(), set(synPAN), keep()))
	require.Equal(t, 200, code, out)
	code, out = f.propose(other, "admin", identifiersBody(0, keep(), set(synPAN), keep()))
	requireSalaryConflict(t, code, out, "pan_in_use", "", "", "")
	f.approvedIDs(other, identifiersBody(0, keep(), set(synOtherPA), keep()))

	pending := f.staffPending(f.staff)
	code, out = f.decideIDs(f.staff, "approver", map[string]any{"proposal_id": pending, "expected_version": 0, "decision": "approve", "note": ""})
	require.Equal(t, 200, code, out)
	code, out = f.propose(other, "admin", identifiersBody(1, keep(), set(synPAN), keep()))
	requireSalaryConflict(t, code, out, "pan_in_use", "", "", "")
	f.approvedIDs(f.staff, identifiersBody(1, keep(), clear(), keep()))
	code, out = f.propose(other, "admin", identifiersBody(1, keep(), set(synPAN), keep()))
	require.Equal(t, 200, code, "a released PAN can move to another staff member: %v", out)
}

func (f *identifierFixture) staffPending(staff string) string {
	f.t.Helper()
	code, out := f.call("GET", f.idPath(staff, ""), "reader", nil)
	require.Equal(f.t, 200, code, out)
	return out["pending"].(map[string]any)["id"].(string)
}

func TestStaffIdentifierRevealIsAuditedAndRestricted(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	code, out := f.propose(f.staff, "admin", identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	require.Equal(t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"].(string)

	code, out = f.reveal(f.staff, "admin", proposal, "pan")
	require.Equal(t, 403, code, "the proposer cannot check their own values: %v", out)
	code, out = f.reveal(f.staff, "viewer", proposal, "pan")
	require.Equal(t, 403, code, "revealing needs its own permission: %v", out)
	code, out = f.reveal(f.staff, "reader", proposal, "pan")
	require.Equal(t, 404, code, "a reader grant is not a locked manager: %v", out)
	for _, body := range []string{`{"revision_id":"` + proposal + `","field":"pan","reason":" "}`, `{"revision_id":"` + proposal + `","field":"aadhaar","reason":"Synthetic"}`,
		`{"revision_id":"` + proposal + `","field":"pan","reason":"` + strings.Repeat("x", 201) + `"}`} {
		code, raw := f.do("POST", f.base+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "auditor", body)
		require.Equal(t, 400, code, raw)
	}
	code, out = f.reveal(f.staff, "approver", proposal, "pan")
	require.Equal(t, 200, code, out)
	require.Equal(t, synPAN, out["value"], "an approver checks the proposed value before deciding")
	code, out = f.reveal(f.staff, "auditor", proposal, "uan")
	require.Equal(t, 200, code, out)
	require.Equal(t, synUAN, out["value"])
	code, out = f.reveal(f.staff, "auditor", f.enrol("ID-03"), "pan")
	require.Equal(t, 404, code, out)

	code, raw := f.do("POST", f.base+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "auditor", `{"revision_id":"`+proposal+`","field":"bank_account","reason":"Synthetic payout check"}`)
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, synAccount)

	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		var audits []struct{ ActorSubject, Changes string }
		require.NoError(t, tx.Table("workforce_audit").Select("actor_subject,changes::text AS changes").Where("action=?", "workforce.staff_identifier.revealed").Order("created_at").Find(&audits).Error)
		require.Len(t, audits, 3, "each reveal and only each reveal is audited")
		require.Equal(t, "user-approver", audits[0].ActorSubject)
		for _, a := range audits {
			require.Contains(t, a.Changes, "Synthetic payout check", "the stated reason is recorded")
			for _, v := range []string{synAccount, synPAN, synUAN} {
				require.NotContains(t, a.Changes, v)
			}
		}
		var revealed, changed []struct{ Payload []byte }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.StaffIdentifierRevealed).Find(&revealed).Error)
		require.Len(t, revealed, 3)
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.StaffIdentifiersChanged).Find(&changed).Error)
		require.Len(t, changed, 1)
		for _, e := range append(revealed, changed...) {
			var envelope struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(e.Payload, &envelope))
			keys := slices.Collect(maps.Keys(envelope.Data))
			require.Subset(t, []string{"hospital_id", "staff_id", "revision_id", "status", "version", "field"}, keys, "events carry identifiers only")
			for _, v := range []string{synAccount, synPAN, synUAN, "XXXX"} {
				require.NotContains(t, string(e.Payload), v)
			}
		}
		return nil
	}))
}

func TestStaffIdentifierRevealIsNeverCached(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	id := f.approvedIDs(f.staff, identifiersBody(0, keep(), set(synPAN), keep()))
	w := testutil.Do(f.router, "POST", f.base+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "auditor", `{"revision_id":"`+id+`","field":"pan","reason":"Synthetic"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	w = testutil.Do(f.router, "POST", f.base+"/hr/payroll"+f.idPath(f.staff, "/reveal"), "admin", `{"revision_id":"`+id+`","field":"pan","reason":"Synthetic"}`)
	require.Equal(t, 200, w.Code, "the proposer may view values once they are in force: %s", w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestStaffIdentifiersStoreNoPlaintext(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	f.approvedIDs(f.staff, identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	code, out := f.propose(f.staff, "admin", identifiersBody(1, bank("999888777666"), set(synOtherPA), set("100000000002")))
	require.Equal(t, 200, code, out)
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_staff_identifiers", "workforce_staff_identifier_revisions", "workforce_audit", "outbox_events"} {
			var rows []string
			require.NoError(t, tx.Raw("SELECT to_jsonb(t)::text FROM "+table+" t").Scan(&rows).Error)
			require.NotEmpty(t, rows, table)
			for _, row := range rows {
				for _, v := range []string{synAccount, synPAN, synUAN, "999888777666", synOtherPA, "100000000002"} {
					require.NotContains(t, row, v, "%s stores %s in the clear", table, v)
				}
			}
		}
		return nil
	}))
}

func TestStaffIdentifiersFailClosedWithoutTheVault(t *testing.T) {
	for name, vault := range map[string]*transit.Client{"unconfigured": nil, "down": downVault(t)} {
		t.Run(name, func(t *testing.T) {
			f := newIdentifierFixture(t, vault)
			code, out := f.propose(f.staff, "admin", identifiersBody(0, bank(synAccount), keep(), keep()))
			require.Equal(t, 503, code, out)
			require.Equal(t, "identifier_vault_unavailable", out["error"], out)
			code, out = f.call("GET", f.idPath(f.staff, ""), "reader", nil)
			require.Equal(t, 404, code, "nothing was stored: %v", out)
		})
	}
}

func TestStaffIdentifiersAuthoriseBeforeTouchingTheVault(t *testing.T) {
	f := newIdentifierFixture(t, downVault(t))
	code, out := f.propose(f.staff, "reader", identifiersBody(0, bank(synAccount), keep(), keep()))
	require.Equal(t, 404, code, "a reader grant is refused before any encryption is attempted: %v", out)
	code, out = f.propose(uuid.NewString(), "admin", identifiersBody(0, bank(synAccount), keep(), keep()))
	require.Equal(t, 404, code, "an unknown staff member is refused before encryption: %v", out)

	unconfigured := newIdentifierFixture(t, nil)
	code, out = unconfigured.reveal(unconfigured.staff, "auditor", uuid.NewString(), "pan")
	require.Equal(t, 503, code, "without a vault no reveal is attempted: %v", out)
	require.Equal(t, "identifier_vault_unavailable", out["error"], out)
}

func TestStaffIdentifierProposalsNeedTheCurrentVersion(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	f.approvedIDs(f.staff, identifiersBody(0, keep(), set(synPAN), keep()))
	for _, stale := range []int{0, 2} {
		code, out := f.propose(f.staff, "admin", identifiersBody(stale, keep(), keep(), set(synUAN)))
		require.Equal(t, 409, code, "version %d: %v", stale, out)
		require.Nil(t, out["reason"], "a stale version is a plain conflict: reload")
	}
	code, out := f.propose(f.staff, "admin", identifiersBody(1, keep(), keep(), set(synUAN)))
	require.Equal(t, 200, code, out)
}

func TestStaffIdentifiersDegradeToMaskedWhenTheVaultGoesDown(t *testing.T) {
	addr, _ := testinfra.StartOpenBao(t)
	target, err := url.Parse(addr)
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	user, password := testinfra.OpenBaoUser(t, time.Hour, "helivanta-api-transit")
	vault, err := transit.New(proxy.URL, "transit", transit.UserpassLogin(user, password), &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	f := newIdentifierFixture(t, vault)
	id := f.approvedIDs(f.staff, identifiersBody(0, bank(synAccount), set(synPAN), set(synUAN)))
	proxy.Close()

	code, out := f.call("GET", f.idPath(f.staff, ""), "reader", nil)
	require.Equal(t, 200, code, "masked values stay readable: %v", out)
	require.Equal(t, "XXXXXX234Z", out["current"].(map[string]any)["pan_masked"])
	code, out = f.reveal(f.staff, "auditor", id, "pan")
	require.Equal(t, 503, code, out)
	require.Equal(t, "identifier_vault_unavailable", out["error"], out)
	code, out = f.propose(f.staff, "admin", identifiersBody(1, keep(), set(synOtherPA), keep()))
	require.Equal(t, 503, code, out)
	code, out = f.propose(f.staff, "admin", identifiersBody(1, keep(), keep(), clear()))
	require.Equal(t, 200, code, "keep and clear need no vault: %v", out)
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_audit").Where("action=?", "workforce.staff_identifier.revealed").Count(&n).Error)
		require.Zero(t, n, "a failed reveal leaves no audit row claiming a view")
		return nil
	}))
}

func TestStaffIdentifiersValidateEveryField(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	for name, body := range map[string]map[string]any{
		"short_account":  identifiersBody(0, bank("12345678"), keep(), keep()),
		"long_account":   identifiersBody(0, bank(strings.Repeat("1", 19)), keep(), keep()),
		"bad_ifsc":       identifiersBody(0, with(bank(synAccount), map[string]any{"ifsc": "SYNT1000001"}), keep(), keep()),
		"no_holder":      identifiersBody(0, with(bank(synAccount), map[string]any{"holder_name": " "}), keep(), keep()),
		"partial_bank":   identifiersBody(0, map[string]any{"action": "set", "account_number": synAccount}, keep(), keep()),
		"company_pan":    identifiersBody(0, keep(), set("AAACZ1234Z"), keep()),
		"short_uan":      identifiersBody(0, keep(), keep(), set("10000000001")),
		"value_on_keep":  identifiersBody(0, keep(), map[string]any{"action": "keep", "value": synPAN}, keep()),
		"value_on_clear": identifiersBody(0, map[string]any{"action": "clear", "ifsc": synIFSC}, keep(), keep()),
		"no_reason":      with(identifiersBody(0, keep(), set(synPAN), keep()), map[string]any{"reason": "  "}),
		"bad_action":     identifiersBody(0, keep(), map[string]any{"action": "replace", "value": synPAN}, keep()),
		"no_version":     {"reason": "Synthetic", "bank": keep(), "pan": set(synPAN), "uan": keep()},
	} {
		code, out := f.propose(f.staff, "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out := f.propose(f.staff, "admin", identifiersBody(0, keep(), keep(), keep()))
	requireSalaryConflict(t, code, out, "nothing_to_change", "", "", "")

	status, raw := f.do("PUT", f.base+"/staff/"+f.staff, "admin", `{"employee_code":"ID-01","display_name":"Synthetic ID-01","job_category":"nurse","employment_status":"inactive"}`)
	require.Equal(t, 200, status, raw)
	code, out = f.propose(f.staff, "admin", identifiersBody(0, keep(), set(synPAN), keep()))
	requireSalaryConflict(t, code, out, "staff_inactive", "", "", "")
}
