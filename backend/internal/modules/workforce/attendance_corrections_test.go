package workforce_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

func addBody(staff, direction, at, reason string) map[string]any {
	return map[string]any{"staff_id": staff, "kind": "add", "direction": direction, "punched_at": at, "reason": reason}
}

func voidBody(staff, target, reason string) map[string]any {
	return map[string]any{"staff_id": staff, "kind": "void", "target_punch_id": target, "reason": reason}
}

func replaceBody(staff, target, direction, at, reason string) map[string]any {
	return map[string]any{"staff_id": staff, "kind": "replace", "target_punch_id": target, "direction": direction, "punched_at": at, "reason": reason}
}

func (f *attendanceFixture) fileCorrection(id, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", "/hr/attendance/corrections/"+id, user, body)
}

// filedCorrection files a correction as filer that must succeed and returns its id.
func (f *attendanceFixture) filedCorrection(body map[string]any) string {
	f.t.Helper()
	id := uuid.NewString()
	code, out := f.fileCorrection(id, "filer", body)
	require.Equal(f.t, 200, code, out)
	require.Equal(f.t, "pending", out["status"])
	return id
}

func (f *attendanceFixture) decide(id, user string, version int, decision, note string) (int, map[string]any) {
	return f.call("POST", "/hr/attendance/corrections/"+id+"/decision", user, map[string]any{"expected_version": version, "decision": decision, "note": note})
}

func (f *attendanceFixture) punch(id string) map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/hr/attendance/punches/"+id, "admin", nil)
	require.Equal(f.t, 200, code, out)
	return out
}

func TestAttendanceCorrectionsAddVoidAndRetimePunchesOnlyWhenApproved(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days

	// Add: the missing out punch is filed, has no effect while pending, and counts once approved.
	f.punched(f.n1, "in", istAt(d[0], "08:00"))
	require.Equal(t, "missing_out", firstShift(f.sheet(d[0])[f.n1])["status"])
	body := addBody(f.n1, "out", istAt(d[0], "16:00"), "  Forgot to punch out  ")
	add := uuid.NewString()
	code, out = f.fileCorrection(add, "filer", body)
	require.Equal(t, 200, code, out)
	require.Equal(t, "Forgot to punch out", out["reason"])
	require.Equal(t, d[0], out["local_date"])
	require.Nil(t, out["applied_punch_id"])
	code, again := f.fileCorrection(add, "filer", body)
	require.Equal(t, 200, code, again, "an identical retry returns the stored request")
	require.Equal(t, out["created_at"], again["created_at"])
	code, out = f.fileCorrection(add, "filer", addBody(f.n1, "out", istAt(d[0], "16:01"), "Forgot to punch out"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_reused", out["error"])
	code, out = f.fileCorrection(uuid.NewString(), "filer", addBody(f.n1, "out", istAt(d[0], "16:03"), "Second request"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "duplicate_correction", out["reason"])
	require.Equal(t, add, out["correction_id"])
	require.Equal(t, "missing_out", firstShift(f.sheet(d[0])[f.n1])["status"], "a pending correction never feeds attendance")

	code, out = f.decide(add, "filer", 1, "approve", "")
	require.Equal(t, 403, code, out, "filing does not grant deciding")
	code, out = f.decide(add, "decider", 2, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "stale_version", out["error"])
	code, out = f.decide(add, "decider", 1, "approve", "Checked against the ward register")
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", out["status"])
	require.Equal(t, "user-decider", out["decided_by_subject"])
	applied := out["applied_punch_id"].(string)
	code, replay := f.decide(add, "decider", 1, "approve", "Checked against the ward register")
	require.Equal(t, 200, code, replay, "replaying the decision returns the request")
	require.Equal(t, applied, replay["applied_punch_id"])
	code, out = f.decide(add, "decider2", 2, "reject", "Too late")
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_state", out["error"])
	p := f.punch(applied)
	require.Equal(t, "correction", p["source"])
	require.Equal(t, add, p["correction_id"])
	require.Equal(t, "user-decider", p["recorded_by_subject"])
	require.Equal(t, "Forgot to punch out", p["reason"])
	shift := firstShift(f.sheet(d[0])[f.n1])
	require.Equal(t, "present", shift["status"])
	require.Equal(t, applied, shift["last_out_punch_id"])
	require.EqualValues(t, 1, f.count(t, "workforce_attendance_punches", "correction_id=?", add))

	code, out = f.call("GET", "/hr/attendance/corrections/"+add+"/history", "planner", nil)
	require.Equal(t, 200, code, out)
	events := out["data"].([]any)
	require.Len(t, events, 2)
	require.Equal(t, "approved", events[0].(map[string]any)["status"])
	require.Empty(t, events[0].(map[string]any)["note"], "notes are redacted for readers without attendance permissions")
	code, out = f.call("GET", "/hr/attendance/corrections/"+add, "planner", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["reason"], "reasons are redacted for readers without attendance permissions")
	code, out = f.call("GET", "/hr/attendance/corrections/"+add, "decider2", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "Forgot to punch out", out["reason"])
	require.EqualValues(t, 1, f.count(t, "workforce_audit", "action='workforce.attendance.correction_approved' AND resource_id=?", add))
	require.EqualValues(t, 0, f.count(t, "workforce_audit", "changes::text LIKE '%Forgot%'"), "audit rows never carry reasons")

	// Void: a rejected request writes nothing; an approved one voids the target with its link.
	stray := f.punched(f.n1, "in", istAt(d[1], "20:00"))
	void := f.filedCorrection(voidBody(f.n1, stray, "Wrong person punched"))
	code, out = f.fileCorrection(uuid.NewString(), "filer", voidBody(f.n1, stray, "Again"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "correction_exists", out["error"])
	code, out = f.decide(void, "decider", 1, "reject", " ")
	require.Equal(t, 400, code, out, "rejection needs a note")
	code, out = f.decide(void, "decider", 1, "reject", "The register shows them on duty")
	require.Equal(t, 200, code, out)
	require.Equal(t, "rejected", out["status"])
	require.Equal(t, false, f.punch(stray)["voided"], "a rejected correction never changes punches")
	void = f.filedCorrection(voidBody(f.n1, stray, "Wrong person punched"))
	code, out = f.decide(void, "decider", 1, "approve", "")
	require.Equal(t, 200, code, out)
	require.Nil(t, out["applied_punch_id"])
	p = f.punch(stray)
	require.Equal(t, true, p["voided"])
	require.Equal(t, void, p["void_correction_id"])
	require.Equal(t, "Wrong person punched", p["void_reason"])
	code, out = f.fileCorrection(uuid.NewString(), "filer", voidBody(f.n1, stray, "Again"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "punch_voided", out["error"])

	// Replace: the late in punch is voided and the corrected one recorded; the original row is untouched.
	late := f.punched(f.n1, "in", istAt(d[2], "09:30"))
	require.Positive(t, firstShift(f.sheet(d[2])[f.n1])["late_minutes"])
	retime := f.filedCorrection(replaceBody(f.n1, late, "in", istAt(d[2], "08:00"), "Clock was running fast"))
	code, out = f.call("GET", "/hr/attendance/corrections/"+retime, "filer", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "in", out["target_direction"])
	require.Equal(t, istAt(d[2], "09:30"), out["target_punched_at"], "before values are returned beside the after values")
	require.Equal(t, istAt(d[2], "08:00"), out["punched_at"])
	code, out = f.decide(retime, "decider", 1, "approve", "")
	require.Equal(t, 200, code, out)
	require.Equal(t, true, f.punch(late)["voided"])
	require.Equal(t, istAt(d[2], "09:30"), f.punch(late)["punched_at"], "the source punch stays as recorded")
	shift = firstShift(f.sheet(d[2])[f.n1])
	require.EqualValues(t, 0, shift["late_minutes"])
	require.Equal(t, out["applied_punch_id"], shift["first_in_punch_id"])

	// Cancellation by a filer, and nothing is applied.
	cancelled := f.filedCorrection(addBody(f.n2, "in", istAt(d[3], "08:00"), "Missed punch"))
	code, out = f.call("POST", "/hr/attendance/corrections/"+cancelled+"/cancellation", "filer", map[string]any{"expected_version": 1})
	require.Equal(t, 200, code, out)
	require.Equal(t, "cancelled", out["status"])
	require.Nil(t, out["decided_by_subject"])
	code, out = f.decide(cancelled, "decider", 2, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_state", out["error"])
	require.EqualValues(t, 0, f.count(t, "workforce_attendance_punches", "correction_id=?", cancelled))

	// Five filings and five transitions; approvals add four punch events to the three direct punches.
	require.EqualValues(t, 10, f.count(t, "outbox_events", "subject=?", workforce.AttendanceCorrectionChanged))
	require.EqualValues(t, 7, f.count(t, "outbox_events", "subject=?", workforce.AttendancePunchChanged))
	require.EqualValues(t, 0, f.count(t, "outbox_events", "payload::text LIKE '%Forgot%' OR payload::text LIKE '%Clock was%' OR payload::text LIKE '%ward register%'"))

	// The list filters by status, person and date, and the board sees every kind.
	code, out = f.call("GET", "/hr/attendance/corrections?status=approved&staff_id="+f.n1, "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 3)
	code, out = f.call("GET", "/hr/attendance/corrections?from="+d[3]+"&to="+d[3], "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	for _, query := range []string{"status=revoked", "staff_id=x", "from=2026-02-01&to=2026-01-01", "limit=101"} {
		code, out = f.call("GET", "/hr/attendance/corrections?"+query, "reader", nil)
		require.Equal(t, 400, code, "%s: %v", query, out)
	}
}

func TestAttendanceCorrectionRulesFailClosed(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days
	other := f.punched(f.n2, "in", istAt(d[0], "08:00"))
	mine := f.punched(f.n1, "in", istAt(d[0], "08:00"))

	for name, body := range map[string]map[string]any{
		"add without time":     {"staff_id": f.n1, "kind": "add", "direction": "in", "reason": "x"},
		"add with target":      {"staff_id": f.n1, "kind": "add", "direction": "in", "punched_at": istAt(d[0], "09:00"), "target_punch_id": mine, "reason": "x"},
		"void with time":       {"staff_id": f.n1, "kind": "void", "target_punch_id": mine, "direction": "in", "punched_at": istAt(d[0], "09:00"), "reason": "x"},
		"replace no target":    {"staff_id": f.n1, "kind": "replace", "direction": "in", "punched_at": istAt(d[0], "09:00"), "reason": "x"},
		"blank reason":         addBody(f.n1, "out", istAt(d[0], "16:00"), "  "),
		"sub-minute":           addBody(f.n1, "out", time.Date(2026, 1, 5, 10, 30, 30, 0, time.UTC).Format(time.RFC3339), "x"),
		"future":               addBody(f.n1, "out", time.Now().Add(time.Hour).Truncate(time.Minute).Format(time.RFC3339), "x"),
		"unknown kind":         {"staff_id": f.n1, "kind": "move", "reason": "x"},
		"someone else's punch": voidBody(f.n1, other, "x"),
		"unknown punch":        voidBody(f.n1, uuid.NewString(), "x"),
	} {
		code, out := f.fileCorrection(uuid.NewString(), "filer", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = f.fileCorrection(uuid.NewString(), "filer", addBody(f.n1, "in", istAt(d[0], "08:03"), "Duplicate"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "duplicate_punch", out["reason"])
	require.Equal(t, mine, out["punch_id"])
	code, out = f.fileCorrection(uuid.NewString(), "filer", replaceBody(f.n1, mine, "in", istAt(d[0], "08:03"), "Retime by three minutes"))
	require.Equal(t, 200, code, out, "a retime does not collide with the punch it replaces")
	old := time.Now().AddDate(0, 0, -60).UTC().Truncate(time.Minute).Format(time.RFC3339)
	code, out = f.fileCorrection(uuid.NewString(), "filer", addBody(f.n1, "in", old, "Too old"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "outside_backdate_window", out["error"])
	code, out = f.fileCorrection(uuid.NewString(), "filer", addBody(uuid.NewString(), "in", istAt(d[1], "08:00"), "Nobody"))
	require.Equal(t, 404, code, out)
	code, out = f.fileCorrection(uuid.NewString(), "planner", addBody(f.n1, "out", istAt(d[1], "16:00"), "No permission"))
	require.Equal(t, 403, code, out)

	// Deciding on one's own filing is refused even with both permissions.
	own := uuid.NewString()
	code, out = f.fileCorrection(own, "decider", addBody(f.n2, "out", istAt(d[0], "16:00"), "Own filing"))
	require.Equal(t, 200, code, out)
	code, out = f.decide(own, "decider", 1, "approve", "")
	require.Equal(t, 403, code, out)

	// A target voided directly after filing blocks approval rather than double-voiding.
	target := f.punched(f.n2, "in", istAt(d[1], "21:00"))
	void := f.filedCorrection(voidBody(f.n2, target, "Wrong person"))
	code, out = f.call("POST", "/hr/attendance/punches/"+target+"/void", "admin", map[string]any{"reason": "Voided at the desk"})
	require.Equal(t, 200, code, out)
	code, out = f.decide(void, "decider", 1, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "punch_voided", out["error"])
	code, out = f.call("GET", "/hr/attendance/corrections/"+void, "filer", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "pending", out["status"], "a refused approval leaves the request pending")

	// A manual punch recorded after filing makes the add a duplicate at approval.
	add := f.filedCorrection(addBody(f.n2, "out", istAt(d[1], "16:00"), "Missed out"))
	f.punched(f.n2, "out", istAt(d[1], "16:02"))
	code, out = f.decide(add, "decider", 1, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "duplicate_punch", out["reason"])

	// Other tenants and unknown ids see nothing.
	code, out = f.call("GET", "/hr/attendance/corrections/"+add, "foreign", nil)
	require.Equal(t, 404, code, out)
	code, out = f.decide(add, "foreign", 1, "approve", "")
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", "/hr/attendance/corrections/"+uuid.NewString()+"/history", "filer", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", "/hr/attendance/corrections/not-a-uuid", "filer", nil)
	require.Equal(t, 404, code, out)
}

func TestAttendanceCorrectionTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days
	punch := f.punched(f.n1, "in", istAt(d[0], "08:00"))
	pending := f.filedCorrection(addBody(f.n1, "out", istAt(d[0], "16:00"), "Missed out"))
	rejected := f.filedCorrection(voidBody(f.n1, punch, "Wrong"))
	code, out = f.decide(rejected, "decider", 1, "reject", "No")
	require.Equal(t, 200, code, out)
	fails := func(contains, sql string, args ...any) {
		t.Helper()
		err := f.attendanceExec(true, sql, args...)
		require.Error(t, err, sql)
		require.Contains(t, err.Error(), contains)
	}

	forged := `INSERT INTO workforce_attendance_punches(tenant_id,hospital_id,id,staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,source,correction_id,reason,recorded_by_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,%s,%s,'x','user-admin' FROM workforce_attendance_corrections WHERE id=?`
	copied := func(flagged bool) error {
		return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			flag := map[bool]string{true: "authorized"}[flagged]
			id := uuid.NewString()
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_attendance',?,true)`, []any{f.hospital, flag}},
				{`INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,?,staff_id,kind,direction,punched_at-interval '1 hour',local_date,timezone,settings_revision_id,policy_revision_id,reason,'pending',requested_by_subject,1 FROM workforce_attendance_corrections WHERE id=?`, []any{id, pending}},
				{`INSERT INTO workforce_attendance_correction_events(tenant_id,hospital_id,id,correction_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,1,'pending',requested_by_subject FROM workforce_attendance_corrections WHERE id=?`, []any{id}},
			} {
				if err := tx.Exec(q.sql, q.args...).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	require.ErrorContains(t, copied(false), "row-level security", "writes need the attendance flag")
	require.NoError(t, copied(true), "the flagged copy is valid, so the refusal above is row-level security")
	fails("approved add or replace", replace3(forged, "'correction'", "id", ""), pending)
	approved := f.filedCorrection(addBody(f.n2, "out", istAt(d[0], "16:00"), "Missed out"))
	code, out = f.decide(approved, "decider", 1, "approve", "")
	require.Equal(t, 200, code, out)
	// Copies of an approved add pass the trigger, so the source/link CHECK is what refuses them.
	fails("check constraint", replace3(forged, "'correction'", "NULL", ""), approved)
	fails("check constraint", replace3(forged, "'manual'", "id", ""), approved)
	fails("approved void or replace", `INSERT INTO workforce_attendance_punch_voids(tenant_id,hospital_id,punch_id,reason,voided_by_subject,correction_id) SELECT tenant_id,hospital_id,target_punch_id,'x','user-admin',id FROM workforce_attendance_corrections WHERE id=?`, rejected)

	fails("filing facts", `UPDATE workforce_attendance_corrections SET reason='changed',version=version+1 WHERE id=?`, pending)
	fails("filing facts", `UPDATE workforce_attendance_corrections SET status='pending',version=version+1 WHERE id=?`, rejected)
	fails("needs its event", `UPDATE workforce_attendance_corrections SET status='cancelled',version=version+1 WHERE id=?`, pending)
	fails("separation", `UPDATE workforce_attendance_corrections SET status='rejected',version=version+1,decided_by_subject=requested_by_subject,decided_at=now() WHERE id=?`, pending)
	fails("punch changes", `WITH c AS (UPDATE workforce_attendance_corrections SET status='approved',version=version+1,decided_by_subject='user-other',decided_at=now() WHERE id=? RETURNING *)
 INSERT INTO workforce_attendance_correction_events(tenant_id,hospital_id,id,correction_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,version,status,'user-other' FROM c`, pending)
	fails("", `DELETE FROM workforce_attendance_corrections WHERE id=?`, pending)
	fails("append only", `UPDATE workforce_attendance_correction_events SET note='x'`)
	fails("must match", `INSERT INTO workforce_attendance_correction_events(tenant_id,hospital_id,id,correction_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,9,'approved','user-x' FROM workforce_attendance_corrections WHERE id=?`, pending)
	fails("filed pending", `INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,kind,direction,punched_at-interval '1 hour',local_date,timezone,settings_revision_id,policy_revision_id,reason,'approved',requested_by_subject,1 FROM workforce_attendance_corrections WHERE id=?`, pending)
	fails("back-dating window", `INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,kind,direction,punched_at-interval '60 days',local_date-60,timezone,settings_revision_id,policy_revision_id,reason,'pending',requested_by_subject,1 FROM workforce_attendance_corrections WHERE id=?`, pending)
	fails("same person", `INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,target_punch_id,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),?,'void',id,local_date,timezone,settings_revision_id,policy_revision_id,'x','pending','user-filer',1 FROM workforce_attendance_punches WHERE id=?`, f.n2, punch)
	fails("carries its target", `INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,target_punch_id,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,'void',id,local_date-1,timezone,settings_revision_id,policy_revision_id,'x','pending','user-filer',1 FROM workforce_attendance_punches WHERE id=?`, punch)
}
