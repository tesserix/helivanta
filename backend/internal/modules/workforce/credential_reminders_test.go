package workforce_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/testutil"
)

// approveUntil approves a review moving the credential to the given expiry, returning the new validity version.
func (f *credentialFixture) approveUntil(s credentialStaff, version int, from, until string) int64 {
	f.t.Helper()
	body := reviewBody(version, until, f.evidence(s, cleanScan))
	body["valid_from"] = from
	review, code, out := f.propose(s, "hr", body)
	require.Equal(f.t, 200, code, out)
	code, out = f.decide(s, review, "reviewer", version, "approve", "")
	require.Equal(f.t, 200, code, out)
	return int64(version + 1)
}

func (f *credentialFixture) localDay() func(int) string {
	f.t.Helper()
	var now time.Time
	require.NoError(f.t, f.db.WithSystem(f.t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error }))
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(f.t, err)
	today := now.In(loc)
	return func(offset int) string { return today.AddDate(0, 0, offset).Format(time.DateOnly) }
}

func (f *credentialFixture) advance(s credentialStaff, version int64) workforcecontract.CredentialReminderStep {
	f.t.Helper()
	advancer, err := workforce.NewCredentialReminderAdvancer(f.t.Context(), f.worker)
	require.NoError(f.t, err)
	step, err := advancer.Advance(f.t.Context(), workforcecontract.CredentialReminderInput{TenantID: testutil.TenantA, HospitalID: f.hospital, StaffID: s.id, CredentialID: s.credential, Version: version})
	require.NoError(f.t, err)
	return step
}

func (f *credentialFixture) reminders(status string) []map[string]any {
	f.t.Helper()
	code, out := f.call("GET", f.base+"/hr/credential-reminders?status="+status, "hr", nil)
	require.Equal(f.t, 200, code, out)
	var rows []map[string]any
	for _, item := range out["data"].([]any) {
		rows = append(rows, item.(map[string]any))
	}
	return rows
}

func (f *credentialFixture) setPolicy(expected int, thresholds ...int) (int, map[string]any) {
	return f.call("PUT", f.base+"/hr/credential-reminder-policy", "hr", map[string]any{"expected_version": expected, "thresholds": thresholds})
}

func TestReminderPolicyIsVersionedAndValidated(t *testing.T) {
	f := newCredentialFixture(t)
	code, out := f.call("GET", f.base+"/hr/credential-reminder-policy", "hr", nil)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 0, out["version"], "reminders are off until a policy is saved")
	require.Empty(t, out["thresholds"])

	code, out = f.setPolicy(0, 7, 30, 0)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, []any{30.0, 7.0, 0.0}, out["thresholds"], "stored soonest-last")
	require.Equal(t, "user-hr", out["created_by_subject"])
	code, out = f.setPolicy(0, 0, 7, 30)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"], "a replay of the same thresholds is idempotent")
	code, out = f.setPolicy(0, 60, 0)
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])
	code, out = f.setPolicy(1, 60, 0)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	require.Len(t, f.payloads(workforce.CredentialReminderPolicyChanged), 2)

	for name, body := range map[string]map[string]any{
		"duplicate":      {"expected_version": 2, "thresholds": []int{7, 7}},
		"too many":       {"expected_version": 2, "thresholds": []int{1, 2, 3, 4, 5, 6, 7}},
		"none":           {"expected_version": 2, "thresholds": []int{}},
		"beyond a year":  {"expected_version": 2, "thresholds": []int{366}},
		"negative":       {"expected_version": 2, "thresholds": []int{-1}},
		"no version":     {"thresholds": []int{7}},
		"unknown fields": {"expected_version": 2, "thresholds": []int{7}, "channel": "sms"},
	} {
		code, out := f.call("PUT", f.base+"/hr/credential-reminder-policy", "hr", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = f.call("PUT", f.base+"/hr/credential-reminder-policy", "reader", map[string]any{"expected_version": 2, "thresholds": []int{1}})
	require.Equal(t, 404, code, "a reader grant cannot change the policy: %v", out)
	code, out = f.call("GET", f.base+"/hr/credential-reminder-policy", "foreign", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", strings.Replace(f.base, f.hospital, uuid.NewString(), 1)+"/hr/credential-reminder-policy", "hr", nil)
	require.Equal(t, 404, code, out)
	requireGuard(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_credential_reminder_policies SET thresholds='{1}'`).Error
	}), "", "policy versions are append-only")
}

func TestRemindersOpenOnceSupersedeAndAcknowledge(t *testing.T) {
	f := newCredentialFixture(t)
	day := f.localDay()
	a := f.enrol("REM-01")
	va := f.approveUntil(a, 0, day(-400), day(10))
	require.False(t, f.advance(a, va).Done)
	require.Empty(t, f.reminders("open"), "no time zone and no policy write nothing")

	f.setZone("Asia/Kolkata")
	step := f.advance(a, va)
	require.False(t, step.Done)
	require.Empty(t, f.reminders("open"), "no policy writes nothing")
	code, out := f.setPolicy(0, 30, 7, 0)
	require.Equal(t, 200, code, out)

	step = f.advance(a, va)
	require.False(t, step.Done)
	require.False(t, f.advance(a, va).Done, "a replayed cycle changes nothing")
	open := f.reminders("open")
	require.Len(t, open, 1, "only one reminder for the due threshold")
	require.EqualValues(t, 30, open[0]["threshold_days"])
	require.Equal(t, day(-20), open[0]["due_on"])
	require.Equal(t, day(10), open[0]["valid_until"])
	require.Equal(t, "REM-01", open[0]["employee_code"])
	require.Equal(t, "Nursing registration", open[0]["type"])
	require.NotContains(t, open[0], "number", "the inbox never carries the licence number")
	require.Len(t, f.payloads(workforce.CredentialReminderChanged), 1)
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	next, err := time.ParseInLocation(time.DateOnly, day(3), loc)
	require.NoError(t, err)
	require.True(t, !step.WakeAt.After(next), "wakes no later than the next threshold's local midnight")

	// A tighter policy makes 10 due, which supersedes the open 30-day reminder of the same revision.
	code, out = f.setPolicy(1, 30, 10, 0)
	require.Equal(t, 200, code, out)
	require.False(t, f.advance(a, va).Done)
	open = f.reminders("open")
	require.Len(t, open, 1)
	require.EqualValues(t, 10, open[0]["threshold_days"])
	superseded := f.reminders("superseded")
	require.Len(t, superseded, 1)
	require.EqualValues(t, 30, superseded[0]["threshold_days"])
	reminder := open[0]["id"].(string)

	ack := f.base + "/hr/credential-reminders/" + reminder + "/acknowledgement"
	code, out = f.call("POST", ack, "hr", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "acknowledged", out["status"])
	require.Equal(t, "user-hr", out["acknowledged_by_subject"])
	acknowledgedAt := out["acknowledged_at"]
	code, out = f.call("POST", ack, "reviewer", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "user-hr", out["acknowledged_by_subject"], "a second acknowledgement keeps the first")
	require.Equal(t, acknowledgedAt, out["acknowledged_at"])
	code, out = f.call("POST", f.base+"/hr/credential-reminders/"+superseded[0]["id"].(string)+"/acknowledgement", "hr", nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, "reminder_superseded", out["reason"])
	for _, user := range []string{"foreign", "reader"} {
		code, out = f.call("POST", ack, user, nil)
		require.Equal(t, 404, code, "%s: %v", user, out)
	}
	code, out = f.call("POST", strings.Replace(ack, f.hospital, uuid.NewString(), 1), "hr", nil)
	require.Equal(t, 404, code, out)
	statuses := map[string]int{}
	for _, p := range f.payloads(workforce.CredentialReminderChanged) {
		statuses[p["status"].(string)]++
	}
	require.Equal(t, map[string]int{"open": 2, "superseded": 1, "acknowledged": 1}, statuses)

	// Approving a renewal closes the older revision's open reminders and ends its schedule.
	b := f.enrol("REM-02")
	vb := f.approveUntil(b, 0, day(-400), day(5))
	require.False(t, f.advance(b, vb).Done)
	require.Len(t, f.reminders("open"), 1)
	renewed := f.approveUntil(b, int(vb), day(-400), day(200))
	require.Empty(t, f.reminders("open"), "the renewal superseded the open reminder")
	require.True(t, f.advance(b, vb).Done, "the old revision's schedule ends")
	step = f.advance(b, renewed)
	require.False(t, step.Done)
	require.Empty(t, f.reminders("open"), "nothing is due for the renewed licence yet")

	// A late approval with every threshold due writes only the expiry-day reminder and finishes.
	c := f.enrol("REM-03")
	vc := f.approveUntil(c, 0, day(-400), day(-1))
	require.True(t, f.advance(c, vc).Done)
	open = f.reminders("open")
	require.Len(t, open, 1)
	require.EqualValues(t, 0, open[0]["threshold_days"])

	code, out = f.call("GET", f.base+"/hr/credential-reminders?status=superseded&limit=1", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	cursor, ok := out["page"].(map[string]any)["next_cursor"].(string)
	require.True(t, ok, out)
	code, out = f.call("GET", f.base+"/hr/credential-reminders?status=superseded&limit=1&cursor="+cursor, "hr", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.NotEqual(t, superseded[0]["id"], out["data"].([]any)[0].(map[string]any)["id"])
	code, out = f.call("GET", f.base+"/hr/credential-reminders?status=deleted", "hr", nil)
	require.Equal(t, 400, code, out)
	code, out = f.call("GET", f.base+"/hr/credential-reminders", "foreign", nil)
	require.Equal(t, 404, code, out)
}

func TestReminderWorkerIsConfinedToOneCredential(t *testing.T) {
	f := newCredentialFixture(t)
	day := f.localDay()
	f.setZone("Asia/Kolkata")
	code, out := f.setPolicy(0, 30, 0)
	require.Equal(t, 200, code, out)
	a, b := f.enrol("REM-11"), f.enrol("REM-12")
	va := f.approveUntil(a, 0, day(-400), day(10))
	vb := f.approveUntil(b, 0, day(-400), day(10))
	f.advance(b, vb)
	scoped := func(s credentialStaff, statements ...string) error {
		return f.worker.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','service:workforce-credential-reminders',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_reminder_staff',?,true),set_config('app.workforce_reminder_credential',?,true)`, f.hospital, s.id, s.credential).Error; err != nil {
				return err
			}
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	count := func(s credentialStaff, query string) int64 {
		var n int64
		require.NoError(t, f.worker.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_reminder_staff',?,true),set_config('app.workforce_reminder_credential',?,true)`, f.hospital, s.id, s.credential).Error; err != nil {
				return err
			}
			return tx.Raw(query).Scan(&n).Error
		}))
		return n
	}
	require.EqualValues(t, 0, count(a, `SELECT count(*) FROM workforce_credential_reminders`), "another credential's reminders are invisible")
	require.EqualValues(t, 1, count(b, `SELECT count(*) FROM workforce_credential_reminders`))
	require.EqualValues(t, 1, count(a, `SELECT count(*) FROM workforce_credential_validity`), "only the named credential's validity is visible")

	requireGuard(t, scoped(a, `SELECT number FROM workforce_credential_validity`), "permission denied", "the licence number is not granted")
	requireGuard(t, scoped(a, `SELECT issuer FROM workforce_credential_validity`), "permission denied", "the issuer is not granted")
	requireGuard(t, scoped(a, `SELECT count(*) FROM workforce_staff`), "permission denied", "staff records stay out of reach")
	insert := func(s credentialStaff, version int64, policy int) string {
		return `INSERT INTO workforce_credential_reminders(tenant_id,hospital_id,staff_id,credential_id,validity_version,policy_version,threshold_days,channel,valid_until,due_on,status)
 VALUES('` + testutil.TenantA + `','` + f.hospital + `','` + s.id + `','` + s.credential + `',` + strconv.FormatInt(version, 10) + `,` + strconv.Itoa(policy) + `,30,'hr_inbox','` + day(10) + `','` + day(-20) + `','open')`
	}
	requireGuard(t, scoped(a, insert(b, vb, 1)), "workforce_credential_reminder_current", "another credential's validity is invisible, so its reminder cannot open")
	requireGuard(t, scoped(a, insert(a, va+1, 1)), "", "cannot write a revision that does not exist")
	require.NoError(t, scoped(a, `UPDATE workforce_credential_reminders SET status='superseded',superseded_at=now()`))
	require.EqualValues(t, 1, count(b, `SELECT count(*) FROM workforce_credential_reminders WHERE status='open'`), "another credential's reminder cannot be closed")
	requireGuard(t, scoped(b, `UPDATE workforce_credential_reminders SET status='acknowledged',acknowledged_by_subject='service:x',acknowledged_at=now()`), "", "the worker never acknowledges")
	requireGuard(t, scoped(a, `INSERT INTO outbox_events(id,tenant_id,subject,payload) VALUES(gen_random_uuid(),'`+testutil.TenantA+`','helivanta.in.workforce.enrollment_changed.v1',jsonb_build_object('tenant_id','`+testutil.TenantA+`','type','helivanta.in.workforce.enrollment_changed.v1','data',jsonb_build_object('hospital_id','`+f.hospital+`')))`), "row-level security", "the worker emits no other subject")
	requireGuard(t, scoped(a, `INSERT INTO workforce_credential_reminder_policies(tenant_id,hospital_id,version,thresholds,created_by_subject) VALUES('`+testutil.TenantA+`','`+f.hospital+`',2,'{1}','service:x')`), "", "the worker cannot set policy")

	code, out = f.setPolicy(1, 30, 7, 0)
	require.Equal(t, 200, code, out)
	requireGuard(t, scoped(a, insert(a, va, 1)), "workforce_credential_reminder_current", "a superseded policy version cannot open a reminder")
	stale := f.approveUntil(a, int(va), day(-400), day(300))
	requireGuard(t, scoped(a, insert(a, va, 2)), "workforce_credential_reminder_current", "a superseded revision cannot open a reminder")
	require.True(t, f.advance(a, va).Done)
	require.False(t, f.advance(a, stale).Done)
}
