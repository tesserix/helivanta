package workforce_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

// dutyFixture is a frozen two-week roster in the future with a person outside it who can cover.
type dutyFixture struct {
	rosterFixture
	t                  *testing.T
	roster, n1, n2, n4 string
	days               []string
	slots              map[string]string
	early, night       string
}

func newDutyFixture(t *testing.T) *dutyFixture {
	t.Helper()
	f := &dutyFixture{rosterFixture: newRosterFixture(t, "Asia/Kolkata", 0, "2026-01-01"), t: t, slots: map[string]string{}}
	// March of next year is always ahead of the test clock and clear of the synthetic holiday calendar.
	start := time.Date(time.Now().Year()+1, 3, 1, 0, 0, 0, 0, time.UTC)
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, 1)
	}
	for d := range 14 {
		f.days = append(f.days, start.AddDate(0, 0, d).Format(time.DateOnly))
	}
	f.n1, f.n2 = f.staff["SYN-N1"], f.staff["SYN-N2"]
	f.n4 = uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/staff/"+f.n4, "admin", `{"employee_code":"SYN-N4","display_name":"Synthetic SYN-N4","job_category":"nurse","employment_status":"active"}`).Code)
	f.roster = uuid.NewString()
	f.freeze(f.roster, f.days[0], f.days[13], func(version int64) int64 {
		var early, night string
		f.early, early = f.allocation(f.n4, f.days[1], f.earlyShift)
		w := f.change(t, f.roster, "planner", version, "employee", []string{early}, nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		_, late := f.allocation(f.n4, f.days[10], f.nightShift)
		w = f.change(t, f.roster, "planner", version+1, "employee", []string{late}, nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		f.night, night = f.allocation(f.n1, f.days[2], f.nightShift)
		w = f.change(t, f.roster, "planner", version+2, "additional", []string{night}, nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		return version + 3
	})
	for _, a := range f.allocations(t, f.roster, "") {
		if a["additional"] == false {
			f.slots[a["staff_id"].(string)+"@"+a["roster_date"].(string)] = a["id"].(string)
		}
	}
	return f
}

// freeze creates, populates and freezes a roster, letting the caller edit the draft first.
func (f *dutyFixture) freeze(roster, from, to string, edit func(version int64) int64) {
	t := f.t
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", fmt.Sprintf(`{"name":"Synthetic Duty %s","start_date":"%s","end_date":"%s","reviewed":true}`, from, from, to)).Code)
	w := f.write(t, roster, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	version := int64(2)
	if edit != nil {
		version = edit(version)
	}
	w = f.write(t, roster, "freeze", "planner", version, "")
	require.Equal(t, 200, w.Code, w.Body.String())
}

func (f *dutyFixture) slot(staff string, day int) string {
	id, ok := f.slots[staff+"@"+f.days[day]]
	require.True(f.t, ok, "no primary allocation for %s on day %d", staff, day)
	return id
}

func (f *dutyFixture) head() (head struct {
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	Allocations int    `json:"allocations"`
}) {
	w := f.do(f.t, "GET", "/hr/rosters/"+f.roster, "reader", "")
	require.Equal(f.t, 200, w.Code, w.Body.String())
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &head))
	return head
}

func (f *dutyFixture) call(method, path, user string, body any) (int, map[string]any) {
	f.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(f.t, err)
		data = string(raw)
	}
	w := f.do(f.t, method, path, user, data)
	var out map[string]any
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func cover(allocation, counterpart, reason string) map[string]any {
	return map[string]any{"kind": "cover", "requester_allocation_id": allocation, "counterpart_staff_id": counterpart, "counterpart_consent": true, "reason": reason}
}

func swap(allocation, counterpart, counterpartAllocation, reason string) map[string]any {
	return with(cover(allocation, counterpart, reason), map[string]any{"kind": "swap", "counterpart_allocation_id": counterpartAllocation})
}

func (f *dutyFixture) file(id, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", "/hr/duty-exchanges/"+id, user, body)
}

func (f *dutyFixture) act(id, action, user string, body map[string]any) (int, map[string]any) {
	return f.call("POST", "/hr/duty-exchanges/"+id+"/"+action, user, body)
}

func exchangeOf(out map[string]any) map[string]any {
	return out["exchange"].(map[string]any)
}

// held maps each allocation in a detail to its holder and whether it is current.
func held(out map[string]any) map[string]map[string]any {
	byID := map[string]map[string]any{}
	for _, a := range out["allocations"].([]any) {
		row := a.(map[string]any)
		byID[row["id"].(string)] = row
	}
	return byID
}

func TestDutyExchangesSwapAndCoverThroughFrozenRosters(t *testing.T) {
	f := newDutyFixture(t)
	a, b := f.slot(f.n1, 0), f.slot(f.n2, 0)
	headBefore := f.head()

	reason := "Synthetic family wedding"
	id := uuid.NewString()
	code, out := f.file(id, "filer", swap(a, f.n2, b, reason))
	require.Equal(t, 200, code, out)
	ex := exchangeOf(out)
	require.Equal(t, "pending", ex["status"])
	require.EqualValues(t, 1, ex["version"])
	require.Equal(t, reason, ex["reason"])
	require.Equal(t, f.n1, ex["requester_staff_id"])
	require.Equal(t, f.days[0], ex["duty_from"])
	require.Equal(t, f.days[0], ex["duty_to"])
	require.Len(t, out["allocations"], 2)

	code, out = f.file(id, "filer", swap(a, f.n2, b, reason))
	require.Equal(t, 200, code, "an identical retry returns the stored exchange")
	require.EqualValues(t, 1, exchangeOf(out)["version"])
	code, out = f.file(id, "filer", swap(a, f.n2, b, "Synthetic other"))
	require.Equal(t, 409, code)
	require.Equal(t, "exchange_reused", out["error"])
	code, out = f.file(uuid.NewString(), "filer", cover(a, f.n4, "Synthetic second claim"))
	require.Equal(t, 409, code)
	require.Equal(t, "allocation_in_exchange", out["error"], "an allocation sits in one live exchange at a time")

	code, out = f.call("GET", "/hr/duty-exchanges/"+id, "reader", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, exchangeOf(out)["reason"], "reasons stay with duty permission holders")
	code, _ = f.file(uuid.NewString(), "directory", cover(f.slot(f.n1, 7), f.n4, "Synthetic"))
	require.Equal(t, 403, code, "managers without the request permission cannot file")
	code, _ = f.call("GET", "/hr/duty-exchanges/"+id, "foreign", nil)
	require.Equal(t, 404, code, "other tenants see nothing")
	other := uuid.NewString()
	require.Equal(t, 200, testutil.Do(f.r, "PUT", "/v1/workforce/hospitals/"+other, "admin", `{"name":"Synthetic Sibling"}`).Code)
	require.Equal(t, 404, testutil.Do(f.r, "GET", "/v1/workforce/hospitals/"+other+"/hr/duty-exchanges/"+id, "admin", "").Code, "sibling hospitals see nothing")

	w := f.write(t, f.roster, "unfreeze", "admin", headBefore.Version, `,"reason":"Synthetic correction"`)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "pending_exchanges")

	code, _ = f.act(id, "decision", "filer", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, "the filer lacks the approve permission")
	code, out = f.act(id, "decision", "decider", map[string]any{"expected_version": 2, "decision": "approve"})
	require.Equal(t, 409, code, out)
	require.Equal(t, "stale_version", out["error"])
	code, out = f.act(id, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve", "note": "Synthetic ok"})
	require.Equal(t, 200, code, out)
	ex = exchangeOf(out)
	require.Equal(t, "approved", ex["status"])
	require.EqualValues(t, 2, ex["version"])
	require.Equal(t, "user-decider", ex["decided_by_subject"])
	allocs := held(out)
	require.Len(t, allocs, 4)
	toN2, toN1 := allocs[ex["requester_replacement_id"].(string)], allocs[ex["counterpart_replacement_id"].(string)]
	require.Equal(t, f.n2, toN2["staff_id"])
	require.Equal(t, f.n1, toN1["staff_id"])
	for original, replacement := range map[string]map[string]any{a: toN2, b: toN1} {
		require.Equal(t, false, allocs[original]["active"])
		require.Equal(t, true, replacement["active"])
		for _, field := range []string{"roster_id", "roster_date", "shift_code", "area_code", "starts_at", "ends_at"} {
			require.Equal(t, allocs[original][field], replacement[field], "the received duty is the given duty, field %s", field)
		}
	}
	code, out = f.act(id, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve", "note": "Synthetic ok"})
	require.Equal(t, 200, code, "replaying the approval returns it unchanged")
	require.EqualValues(t, 2, exchangeOf(out)["version"])

	head := f.head()
	require.Equal(t, "frozen", head.Status, "the roster stays published")
	require.Equal(t, headBefore.Version+1, head.Version)
	require.Equal(t, headBefore.Allocations, head.Allocations)
	revisions := decodeBody[struct {
		Data []map[string]any `json:"data"`
	}](t, f.do(t, "GET", "/hr/rosters/"+f.roster+"/revisions?limit=1", "reader", "")).Data
	require.Equal(t, "exchanged", revisions[0]["action"])
	require.EqualValues(t, 2, revisions[0]["added"])
	require.EqualValues(t, 2, revisions[0]["removed"])
	require.Empty(t, revisions[0]["reason"], "roster history never repeats the exchange reason")
	marked := 0
	for _, row := range f.allocations(t, f.roster, "") {
		if row["exchange_id"] == id {
			marked++
		}
	}
	require.Equal(t, 2, marked, "the grid marks both exchanged allocations")
	for _, row := range f.allocations(t, f.roster, "&staff_id="+f.n4) {
		require.Equal(t, f.n4, row["staff_id"], "the staff filter narrows the grid to one person")
	}
	require.Len(t, f.allocations(t, f.roster, "&staff_id="+f.n4), 2)
	require.Equal(t, 400, f.do(t, "GET", "/hr/rosters/"+f.roster+"/allocations?staff_id=nope", "reader", "").Code)

	// A cover hands one duty over and revocation hands it back before it starts.
	fri := f.slot(f.n1, 4)
	coverID := uuid.NewString()
	code, out = f.file(coverID, "filer", cover(fri, f.n4, "Synthetic course"))
	require.Equal(t, 200, code, out)
	code, out = f.act(coverID, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	ex = exchangeOf(out)
	require.Nil(t, ex["counterpart_replacement_id"])
	require.Equal(t, f.n4, held(out)[ex["requester_replacement_id"].(string)]["staff_id"])
	code, _ = f.act(coverID, "revocation", "decider2", map[string]any{"expected_version": 2})
	require.Equal(t, 400, code, "revocation needs a note")
	code, _ = f.act(coverID, "revocation", "filer", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 403, code)
	code, out = f.act(coverID, "revocation", "decider", map[string]any{"expected_version": 2, "note": "Synthetic course moved"})
	require.Equal(t, 200, code, out)
	ex = exchangeOf(out)
	require.Equal(t, "revoked", ex["status"])
	require.Equal(t, "user-decider", ex["revoked_by_subject"])
	restored := held(out)[ex["requester_restored_id"].(string)]
	require.Equal(t, f.n1, restored["staff_id"])
	require.Equal(t, true, restored["active"])
	require.Equal(t, false, held(out)[ex["requester_replacement_id"].(string)]["active"])

	// The restored duty is free to exchange again, and a pending exchange can be withdrawn.
	again := uuid.NewString()
	code, out = f.file(again, "filer", cover(restored["id"].(string), f.n4, "Synthetic second attempt"))
	require.Equal(t, 200, code, out)
	code, out = f.act(again, "cancellation", "filer", map[string]any{"expected_version": 1})
	require.Equal(t, 200, code, out)
	require.Equal(t, "cancelled", exchangeOf(out)["status"])
	require.Nil(t, exchangeOf(out)["decided_by_subject"])

	code, out = f.call("GET", "/hr/duty-exchanges?status=approved", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.Empty(t, out["data"].([]any)[0].(map[string]any)["reason"])
	code, out = f.call("GET", "/hr/duty-exchanges?staff_id="+f.n4+"&from="+f.days[4]+"&to="+f.days[4], "filer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2, "either side matches and duty dates overlap the window")
	code, out = f.call("GET", "/hr/duty-exchanges?from="+f.days[5], "filer", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"])
	for _, query := range []string{"status=done", "staff_id=nope", "from=" + f.days[3] + "&to=" + f.days[2], "limit=101", "from=1999-12-31"} {
		code, _ = f.call("GET", "/hr/duty-exchanges?"+query, "filer", nil)
		require.Equal(t, 400, code, query)
	}

	code, out = f.call("GET", "/hr/duty-exchanges/"+coverID+"/history", "filer", nil)
	require.Equal(t, 200, code, out)
	events := out["data"].([]any)
	require.Len(t, events, 3)
	require.Equal(t, "revoked", events[0].(map[string]any)["status"], "newest first")
	require.Equal(t, "Synthetic course moved", events[0].(map[string]any)["note"])
	code, out = f.call("GET", "/hr/duty-exchanges/"+coverID+"/history", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"].([]any)[0].(map[string]any)["note"], "notes follow reason redaction")
	code, _ = f.call("GET", "/hr/duty-exchanges/"+uuid.NewString()+"/history", "reader", nil)
	require.Equal(t, 404, code)

	require.EqualValues(t, 7, f.count(t, "outbox_events", "subject=?", workforce.DutyExchangeChanged), "one event per filing and transition")
	require.EqualValues(t, 3, f.count(t, "workforce_roster_revisions", "roster_id=? AND action='exchanged'", f.roster))
	var leaked []string
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject IN (?,?) UNION ALL SELECT changes::text FROM workforce_audit WHERE action LIKE 'workforce.duty_exchange.%' OR action='workforce.roster.exchanged'`,
			workforce.DutyExchangeChanged, workforce.RosterChanged).Scan(&leaked).Error
	}))
	require.NotEmpty(t, leaked)
	for _, text := range leaked {
		for _, secret := range []string{"Synthetic", "wedding", "course"} {
			require.NotContains(t, text, secret, "events and audit carry identifiers only")
		}
	}
}

func TestDutyExchangeRulesFailClosed(t *testing.T) {
	f := newDutyFixture(t)
	call := func(method, path, user string, body any) (int, map[string]any) {
		return f.call(method, path, user, body)
	}
	year := f.days[0][:4]
	y, err := strconv.Atoi(year)
	require.NoError(t, err)
	types := configureLeave(t, call, y)
	code, out := call("PUT", "/staff/"+f.n4+"/leave-assignment", "admin", map[string]any{"expected_version": 0, "structure_revision_id": types.structure, "active": true, "effective_on": year + "-01-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	leave := uuid.NewString()
	code, out = call("PUT", "/staff/"+f.n4+"/leave-requests/"+leave, "admin", full(types.duty, f.days[3], f.days[3]))
	require.Equal(t, 200, code, out)
	code, out = call("POST", "/staff/"+f.n4+"/leave-requests/"+leave+"/decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)

	refused := func(want string, status int, body map[string]any) map[string]any {
		t.Helper()
		code, out := f.file(uuid.NewString(), "filer", body)
		require.Equal(t, status, code, out)
		require.Equal(t, want, out["error"], out)
		return out
	}
	n1 := func(day int) string { return f.slot(f.n1, day) }
	refused("invalid_request", 400, with(cover(n1(7), f.n4, "Synthetic"), map[string]any{"counterpart_consent": false}))
	refused("invalid_request", 400, cover(n1(7), f.n4, "   "))
	refused("invalid_request", 400, cover(n1(7), f.n4, strings.Repeat("x", 501)))
	refused("invalid_request", 400, with(cover(n1(7), f.n4, "Synthetic"), map[string]any{"counterpart_allocation_id": f.slot(f.n2, 7)}))
	refused("invalid_request", 400, with(swap(n1(7), f.n2, f.slot(f.n2, 7), "Synthetic"), map[string]any{"counterpart_allocation_id": nil}))
	refused("invalid_reference", 400, cover(n1(7), f.n1, "Synthetic self"))
	refused("invalid_reference", 400, cover(uuid.NewString(), f.n4, "Synthetic unknown"))
	refused("invalid_reference", 400, cover(f.night, f.n4, "Synthetic additional"))
	refused("invalid_reference", 400, cover(n1(7), uuid.NewString(), "Synthetic stranger"))
	refused("invalid_reference", 400, swap(n1(7), f.n4, f.slot(f.n2, 7), "Synthetic wrong holder"))
	overlap := refused("conflict", 409, cover(n1(1), f.n4, "Synthetic overlap"))
	require.Equal(t, "allocation_overlap", overlap["reason"])
	require.Equal(t, []any{f.early}, overlap["allocation_ids"])
	refused("primary_taken", 409, cover(n1(10), f.n4, "Synthetic double"))
	refused("orphan_additional", 409, cover(n1(2), f.n4, "Synthetic orphan"))
	refused("on_leave", 409, cover(f.slot(f.n2, 3), f.n4, "Synthetic away"))
	refused("staff_inactive", 409, cover(n1(8), f.staff["SYN-N3"], "Synthetic inactive"))
	require.Zero(t, f.count(t, "workforce_duty_exchanges", "true"), "refusals leave nothing behind")

	// The filer cannot decide their own exchange, and rejection needs a note.
	own := uuid.NewString()
	code, out = f.file(own, "decider", cover(n1(9), f.n4, "Synthetic own"))
	require.Equal(t, 200, code, out)
	code, out = f.act(own, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, out)
	code, _ = f.act(own, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject"})
	require.Equal(t, 400, code)
	code, out = f.act(own, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic no"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "rejected", exchangeOf(out)["status"])
	code, out = f.act(own, "cancellation", "decider", map[string]any{"expected_version": 2})
	require.Equal(t, 409, code)
	require.Equal(t, "exchange_state", out["error"])
	code, out = f.act(own, "revocation", "decider2", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 409, code)
	require.Equal(t, "exchange_state", out["error"])

	// Past duty belongs to attendance, not exchanges.
	past := time.Now().AddDate(0, 0, -14)
	for past.Weekday() != time.Monday {
		past = past.AddDate(0, 0, -1)
	}
	old := uuid.NewString()
	f.freeze(old, past.Format(time.DateOnly), past.AddDate(0, 0, 6).Format(time.DateOnly), nil)
	var started string
	for _, a := range f.allocations(t, old, "") {
		if a["staff_id"] == f.n1 {
			started = a["id"].(string)
		}
	}
	require.NotEmpty(t, started)
	refused("exchange_started", 409, cover(started, f.n4, "Synthetic too late"))
}

func TestDutyExchangesSerialiseOnSharedDuty(t *testing.T) {
	f := newDutyFixture(t)
	duty := f.slot(f.n2, 7)
	ids := []string{uuid.NewString(), uuid.NewString()}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() { codes[i], _ = f.file(ids[i], "filer", cover(duty, f.n4, "Synthetic race")) })
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes, "one filing holds the duty")
	winner := ids[0]
	if codes[1] == 200 {
		winner = ids[1]
	}
	for i, user := range []string{"decider", "decider2"} {
		wg.Go(func() {
			codes[i], _ = f.act(winner, "decision", user, map[string]any{"expected_version": 1, "decision": "approve", "note": user})
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes, "one approval applies the exchange")
	require.EqualValues(t, 1, f.count(t, "workforce_roster_allocations", "staff_id=? AND roster_date=? AND removed_version IS NULL", f.n4, f.days[7]))
	require.EqualValues(t, 1, f.count(t, "workforce_roster_revisions", "roster_id=? AND action='exchanged'", f.roster))
}

// dutyExec runs statements in one transaction with hospital scope and both write flags, as the handlers do.
func (f *dutyFixture) dutyExec(sql string, args ...any) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_rostering','authorized',true),set_config('app.workforce_duty','authorized',true)`, f.hospital).Error; err != nil {
			return err
		}
		for statement := range strings.SplitSeq(sql, ";\n") {
			n := strings.Count(statement, "?")
			if err := tx.Exec(statement, args[:n]...).Error; err != nil {
				return err
			}
			args = args[n:]
		}
		return nil
	})
}

func TestDutyExchangeTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newDutyFixture(t)
	id := uuid.NewString()
	code, out := f.file(id, "filer", cover(f.slot(f.n1, 7), f.n4, "Synthetic"))
	require.Equal(t, 200, code, out)
	fails := func(contains, sql string, args ...any) {
		t.Helper()
		err := f.dutyExec(sql, args...)
		require.Error(t, err, sql)
		require.Contains(t, err.Error(), contains)
	}
	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_duty_exchanges SET status='cancelled',version=2 WHERE id=?`, id), "row-level security", "writes need the duty flag")
	fails("transition", `UPDATE workforce_duty_exchanges SET reason='changed',status='cancelled',version=2 WHERE id=?`, id)
	fails("transition", `UPDATE workforce_duty_exchanges SET status='approved',version=3,decided_by_subject='user-x',decided_at=now() WHERE id=?`, id)
	fails("workforce_duty_exchange_separation", `UPDATE workforce_duty_exchanges SET status='rejected',version=2,decided_by_subject=requested_by_subject,decided_at=now() WHERE id=?`, id)
	fails("event", `UPDATE workforce_duty_exchanges SET status='cancelled',version=2 WHERE id=?`, id)
	fails("check constraint", `UPDATE workforce_duty_exchanges SET status='approved',version=2,decided_by_subject='user-x',decided_at=now() WHERE id=?`, id)
	fails("append only", `DELETE FROM workforce_duty_exchange_events WHERE exchange_id=?`, id)
	fails("", `DELETE FROM workforce_duty_exchanges WHERE id=?`, id)
	fails("workforce_duty_exchange_held", `INSERT INTO workforce_duty_exchanges(tenant_id,hospital_id,id,kind,requester_staff_id,counterpart_staff_id,requester_allocation_id,duty_from,duty_to,counterpart_consent,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,'`+uuid.NewString()+`',kind,requester_staff_id,counterpart_staff_id,requester_allocation_id,duty_from,duty_to,true,'x','pending','user-y',1 FROM workforce_duty_exchanges WHERE id=?;
 INSERT INTO workforce_duty_exchange_holds(tenant_id,hospital_id,exchange_id,allocation_id,role,held) SELECT tenant_id,hospital_id,id,requester_allocation_id,'requester',true FROM workforce_duty_exchanges WHERE id<>?`, id, id)
	// A frozen roster only changes inside an exchanged revision, and that revision must name an exchange.
	fails("versions must advance", `UPDATE workforce_rosters SET version=version+1 WHERE id=?`, f.roster)
	fails("check constraint", `INSERT INTO workforce_roster_revisions(tenant_id,hospital_id,id,roster_id,action,status,added,removed,skipped_existing,skipped_unassigned,skipped_conflicts,reason,reviewed_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),id,'exchanged','frozen',0,0,0,0,0,'','user-admin',version+1 FROM workforce_rosters WHERE id=?`, f.roster)
	fails("hand over", `WITH v AS (INSERT INTO workforce_roster_revisions(tenant_id,hospital_id,id,roster_id,action,status,added,removed,skipped_existing,skipped_unassigned,skipped_conflicts,reason,reviewed_by_subject,version,exchange_id)
 SELECT tenant_id,hospital_id,gen_random_uuid(),id,'exchanged','frozen',0,0,0,0,0,'','user-admin',version+1,? FROM workforce_rosters WHERE id=? RETURNING id,version)
 UPDATE workforce_rosters r SET version=v.version,revision_id=v.id FROM v WHERE r.id=?;
 UPDATE workforce_roster_allocations SET removed_version=(SELECT version FROM workforce_rosters WHERE id=?),removed_revision_id=(SELECT revision_id FROM workforce_rosters WHERE id=?) WHERE id=?;
 UPDATE workforce_duty_exchanges SET status='approved',version=2,decided_by_subject='user-x',decided_at=now(),requester_replacement_id=? WHERE id=?;
 INSERT INTO workforce_duty_exchange_events(tenant_id,hospital_id,id,exchange_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,2,'approved','user-x' FROM workforce_duty_exchanges WHERE id=?`,
		id, f.roster, f.roster, f.roster, f.roster, f.slot(f.n1, 7), f.early, id, id)
	require.EqualValues(t, 1, f.count(t, "workforce_duty_exchanges", "id=? AND status='pending' AND version=1", id), "nothing above changed the exchange")
}
