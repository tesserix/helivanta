package workforce_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

type clashRoster struct {
	f               *credentialFixture
	id, shift, area string
	version         int64
}

func (f *credentialFixture) clashRoster(start, end string) *clashRoster {
	f.t.Helper()
	path := f.base + "/hr/shifts/" + uuid.NewString()
	shift := revisionOf(f.t, testutil.Do(f.r, "PUT", path, "hr", `{"expected_version":0,"shift_type":"rotating","code":"SYN-`+start+`","name":"Synthetic day","description":"","active":true,"start_time":"08:00","end_time":"16:00","break_minutes":30,"grace_minutes":0,"full_day_ot_minutes":0,"half_day_ot_minutes":0,"locum":false}`))
	area := revisionOf(f.t, testutil.Do(f.r, "PUT", f.base+"/hr/roster-configs/"+uuid.NewString(), "hr", `{"expected_version":0,"kind":"area","code":"SYN-`+start+`","name":"Synthetic Ward","description":"","active":true,"effective_on":"2026-01-01","reviewed":true,"shift_revision_ids":["`+shift+`"],"days":[]}`))
	r := &clashRoster{f: f, id: uuid.NewString(), shift: shift, area: area, version: 1}
	code, out := f.call("PUT", f.base+"/hr/rosters/"+r.id, "hr", map[string]any{"name": "Synthetic " + start, "start_date": start, "end_date": end, "reviewed": true})
	require.Equal(f.t, 200, code, out)
	return r
}

// add books each staff-date pair and returns the allocation ids in order.
func (r *clashRoster) add(pairs ...[2]string) []string {
	r.f.t.Helper()
	ids, rows := make([]string, len(pairs)), make([]string, len(pairs))
	for i, p := range pairs {
		ids[i] = uuid.NewString()
		rows[i] = fmt.Sprintf(`{"id":"%s","staff_id":"%s","roster_date":"%s","shift_revision_id":"%s","area_revision_id":"%s"}`, ids[i], p[0], p[1], r.shift, r.area)
	}
	w := testutil.Do(r.f.r, "POST", r.f.base+"/hr/rosters/"+r.id+"/changes", "hr", fmt.Sprintf(`{"change_id":"%s","expected_version":%d,"reviewed":true,"mode":"roster","add":[%s],"remove":[]}`, uuid.NewString(), r.version, strings.Join(rows, ",")))
	require.Equal(r.f.t, 200, w.Code, w.Body.String())
	r.version++
	return ids
}

func (r *clashRoster) transition(action, change string, acknowledged []string, extra map[string]any) (int, map[string]any) {
	body := map[string]any{"change_id": change, "expected_version": r.version, "reviewed": true}
	if acknowledged != nil {
		body["acknowledged_clashes"] = acknowledged
	}
	for k, v := range extra {
		body[k] = v
	}
	return r.f.call("POST", r.f.base+"/hr/rosters/"+r.id+"/"+action, "hr", body)
}

func (r *clashRoster) clashes(user, query string) (int, map[string]any) {
	return r.f.call("GET", r.f.base+"/hr/rosters/"+r.id+"/credential-clashes"+query, user, nil)
}

func (f *credentialFixture) rosterScoped(statement string, args ...any) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_rostering','authorized',true),set_config('app.workforce_enrollment','authorized',true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(statement, args...).Error
	})
}

func (f *credentialFixture) clashCount(table, where string, args ...any) int64 {
	f.t.Helper()
	var n int64
	require.NoError(f.t, f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Table(table).Where(where, args...).Count(&n).Error
	}))
	return n
}

func TestRosterFreezeRequiresAcknowledgedCredentialClashes(t *testing.T) {
	f := newCredentialFixture(t)
	f.setZone("Asia/Kolkata")
	lapsing, future, unreviewed := f.enrol("CLASH-01"), f.enrol("CLASH-02"), credentialStaff{id: uuid.NewString(), credential: uuid.NewString()}
	code, out := f.call("PUT", f.base+"/staff/"+unreviewed.id+"/enrollment", "hr", enrollmentBody(0, "CLASH-03", credentialRow(unreviewed.credential, "SYN-TYPED", "2024-01-01", "2026-10-20")))
	require.Equal(t, 200, code, out)
	f.approveUntil(lapsing, 0, "2026-01-01", "2026-10-25")
	f.approveUntil(future, 0, "2026-10-21", "2027-12-31")

	r := f.clashRoster("2026-10-19", "2026-11-01")
	ids := r.add([2]string{lapsing.id, "2026-10-25"}, [2]string{lapsing.id, "2026-10-26"}, [2]string{future.id, "2026-10-20"}, [2]string{future.id, "2026-10-21"}, [2]string{unreviewed.id, "2026-10-27"})
	expired, notYet := ids[1], ids[2]

	code, out = r.clashes("hr", "?limit=1")
	require.Equal(t, 200, code, out)
	page := out["data"].([]any)
	require.Len(t, page, 1)
	first := page[0].(map[string]any)
	require.Equal(t, notYet, first["allocation_id"], "earliest roster date first")
	require.Equal(t, "Synthetic CLASH-02", first["display_name"])
	credential := first["credentials"].([]any)[0].(map[string]any)
	require.Equal(t, "not_yet_valid", credential["standing"])
	require.Equal(t, "2026-10-21", credential["valid_from"])
	require.EqualValues(t, 1, credential["validity_version"])
	require.NotContains(t, credential, "number")
	require.NotContains(t, credential, "issuer")
	pageInfo := out["page"].(map[string]any)
	require.Equal(t, true, pageInfo["has_more"])
	code, out = r.clashes("hr", "?limit=1&cursor="+pageInfo["next_cursor"].(string))
	require.Equal(t, 200, code, out)
	second := out["data"].([]any)[0].(map[string]any)
	require.Equal(t, expired, second["allocation_id"], "the day after expiry clashes; the expiry day and unreviewed dates do not")
	require.Equal(t, "expired", second["credentials"].([]any)[0].(map[string]any)["standing"])
	require.Equal(t, false, out["page"].(map[string]any)["has_more"])
	w := testutil.Do(f.r, "GET", f.base+"/hr/rosters/"+r.id+"/credential-clashes", "hr", "")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.EqualValues(t, 3, f.clashCount("workforce_audit", "action='workforce.roster_credential_clashes.read' AND resource_id=?", r.id))

	code, _ = r.clashes("reviewer", "")
	require.Equal(t, 403, code, "roster manage is required")
	code, _ = r.clashes("foreign", "")
	require.Equal(t, 404, code, "cross-tenant")
	code, _ = f.call("GET", f.other+"/hr/rosters/"+r.id+"/credential-clashes", "hr", nil)
	require.Equal(t, 404, code, "cross-hospital")
	code, _ = r.clashes("hr", "?limit=101")
	require.Equal(t, 400, code)

	change := uuid.NewString()
	code, out = r.transition("freeze", change, nil, nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, "credential_clashes", out["reason"])
	require.EqualValues(t, 2, out["outstanding"])
	code, out = r.transition("freeze", change, []string{expired, uuid.NewString()}, nil)
	require.Equal(t, 409, code, out)
	require.EqualValues(t, 1, out["outstanding"], "a stale id does not stand in for a real clash")
	require.Zero(t, f.clashCount("workforce_roster_credential_acknowledgements", "1=1"), "a refused freeze records nothing")

	code, out = r.transition("freeze", change, []string{expired, notYet, ids[0], uuid.NewString()}, nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "frozen", out["roster"].(map[string]any)["status"])
	require.EqualValues(t, 2, out["revision"].(map[string]any)["credential_clashes"])
	code, out = r.transition("freeze", change, []string{expired, notYet}, nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, change, out["revision"].(map[string]any)["id"], "a replay returns the original revision")
	require.EqualValues(t, 2, f.clashCount("workforce_roster_credential_acknowledgements", "revision_id=? AND acknowledged_by_subject='user-hr'", change))
	require.EqualValues(t, 1, f.clashCount("workforce_roster_credential_acknowledgements", "allocation_id=? AND standing='expired' AND validity_version=1", expired))
	require.EqualValues(t, 1, f.clashCount("workforce_audit", "action='workforce.roster.frozen' AND (changes->>'credential_clashes')::int=2"))

	code, out = f.call("GET", f.base+"/hr/rosters/"+r.id+"/revisions", "hr", nil)
	require.Equal(t, 200, code, out)
	require.Contains(t, out["data"].([]any)[0].(map[string]any), "credential_clashes")

	f.approveUntil(lapsing, 1, "2026-01-01", "2027-12-31")
	code, out = r.clashes("hr", "")
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"].([]any), 1, "a renewal clears its clash on the next read")
}

func TestRosterClashBoundsAndLeapDay(t *testing.T) {
	f := newCredentialFixture(t)
	f.setZone("Asia/Kolkata")
	s := f.enrol("CLASH-04")
	f.approveUntil(s, 0, "2026-01-01", "2028-02-29")
	r := f.clashRoster("2028-02-21", "2028-03-05")
	ids := r.add([2]string{s.id, "2028-02-29"}, [2]string{s.id, "2028-03-01"})
	code, out := r.clashes("hr", "")
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"].([]any), 1)
	require.Equal(t, ids[1], out["data"].([]any)[0].(map[string]any)["allocation_id"], "29 February is the last valid day")

	code, out = r.transition("discard", uuid.NewString(), []string{ids[1]}, map[string]any{"reason": "Synthetic discard"})
	require.Equal(t, 400, code, out, "only a freeze acknowledges clashes")
	tooMany := make([]string, 2001)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	code, out = r.transition("freeze", uuid.NewString(), tooMany, nil)
	require.Equal(t, 400, code, out, "the contract bounds acknowledgements")

	workforce.LowerClashLimit(t, 0)
	code, out = r.transition("freeze", uuid.NewString(), []string{ids[1]}, nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, "too_many_clashes", out["error"])
	require.Equal(t, "draft", f.rosterStatus(r.id))
}

func (f *credentialFixture) rosterStatus(id string) string {
	f.t.Helper()
	code, out := f.call("GET", f.base+"/hr/rosters/"+id, "hr", nil)
	require.Equal(f.t, 200, code, out)
	return out["status"].(string)
}

func TestRosterCredentialStandingGuards(t *testing.T) {
	f := newCredentialFixture(t)
	s := f.enrol("CLASH-05")
	f.approveUntil(s, 0, "2026-01-01", "2026-12-31")
	require.EqualValues(t, 1, f.clashCount("workforce_roster_credential_standing", "staff_id=? AND valid_until='2026-12-31' AND version=1", s.id))
	f.approveUntil(s, 1, "2026-01-01", "2029-06-30")
	require.EqualValues(t, 1, f.clashCount("workforce_roster_credential_standing", "staff_id=? AND valid_until='2029-06-30' AND version=2", s.id), "re-approval moves the projection")

	requireGuard(t, f.rosterScoped(`UPDATE workforce_roster_credential_standing SET valid_until='2040-01-01' WHERE staff_id=?`, s.id), "workforce_roster_standing_matches", "standing mirrors validity")
	requireGuard(t, f.rosterScoped(`DELETE FROM workforce_roster_credential_standing WHERE staff_id=?`, s.id), "destructive workforce operation", "standing is never deleted")
	code, out := f.call("PUT", f.base+"/staff/"+s.id+"/enrollment", "hr", enrollmentBody(f.staffVersion(s), "CLASH-05"))
	require.Equal(t, 409, code, out, "a reviewed credential cannot be removed, so standing cannot be orphaned")

	var visible int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-reader',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Table("workforce_roster_credential_standing").Count(&visible).Error
	}))
	require.Zero(t, visible, "a hospital reader cannot see credential standing")
	require.EqualValues(t, 1, f.clashCount("workforce_roster_credential_standing", "1=1"), "the same query sees it as a manager")
}
