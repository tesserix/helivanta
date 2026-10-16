package workforce_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

func regimeBody(version int, regime, intimated, note string) map[string]any {
	return map[string]any{"expected_version": version, "regime": regime, "intimated_on": intimated, "note": note}
}

func TestTaxRegimeElectionsAreVersionedAndExplicit(t *testing.T) {
	f := newPayFixture(t)
	put := func(year, user string, body map[string]any) (int, map[string]any) {
		return f.call("PUT", "/staff/"+f.staff+"/tax-regimes/"+year, user, body)
	}
	code, list := f.call("GET", "/staff/"+f.staff+"/tax-regimes", "viewer", nil)
	require.Equal(t, 200, code, list)
	require.Empty(t, list["data"], "nothing is recorded until the employee intimates a regime")

	code, out := put("2026-27", "maker", regimeBody(0, "new", "2026-04-03", " "))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, "new", out["regime"])
	require.Equal(t, "2026-04-03", out["intimated_on"])
	id := out["id"]
	code, out = put("2026-27", "maker", regimeBody(0, "new", "2026-04-03", ""))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"], "an identical replay returns the election")
	code, out = put("2026-27", "maker", regimeBody(1, "old", "2026-04-20", ""))
	require.Equal(t, 400, code, "a change needs a note: %v", out)
	code, out = put("2026-27", "maker", regimeBody(0, "old", "2026-04-20", "Synthetic correction"))
	require.Equal(t, 409, code, "a stale version: %v", out)
	code, out = put("2026-27", "maker", regimeBody(1, "old", "2026-04-20", "Synthetic correction"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	require.Equal(t, "old", out["regime"])
	require.Equal(t, id, out["id"], "the election keeps its identity")
	code, out = put("2026-27", "maker", regimeBody(1, "old", "2026-04-20", "Synthetic correction"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"], "replaying the change returns it unchanged")

	code, out = put("2025-26", "maker", regimeBody(0, "old", "2025-04-01", ""))
	require.Equal(t, 200, code, out)
	code, out = put("2027-28", "maker", regimeBody(0, "new", "2027-03-15", ""))
	require.Equal(t, 200, code, "an intimation can precede its year: %v", out)
	code, out = put("2027-28", "maker", regimeBody(1, "old", "2028-04-01", "Synthetic"))
	require.Equal(t, 400, code, "an intimation after the year ends: %v", out)
	code, out = put("2027-28", "maker", regimeBody(1, "old", "2028-03-31", "Synthetic"))
	require.Equal(t, 200, code, "31 March is the last day of the year: %v", out)

	for year, status := range map[string]int{"2026-28": 404, "2026": 404, "1999-00": 404, "2099-00": 200} {
		code, out = put(year, "maker", regimeBody(0, "new", "2026-04-01", ""))
		require.Equal(t, status, code, "%s: %v", year, out)
	}
	for name, body := range map[string]map[string]any{
		"an unknown regime": regimeBody(0, "default", "2026-04-01", ""),
		"an impossible day": regimeBody(0, "new", "2026-02-30", ""),
		"a note too long":   regimeBody(0, "new", "2026-04-01", string(slices.Repeat([]byte("x"), 501))),
	} {
		code, out = put("2030-31", "maker", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	for user, status := range map[string]int{"viewer": 403, "reader": 404, "foreign": 404, "directory": 403} {
		code, out = put("2031-32", user, regimeBody(0, "new", "2031-04-01", ""))
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, out = f.call("PUT", "/staff/"+uuid.NewString()+"/tax-regimes/2026-27", "maker", regimeBody(0, "new", "2026-04-01", ""))
	require.Equal(t, 404, code, out)
	code, raw := f.do("PUT", f.other+"/hr/payroll/staff/"+f.staff+"/tax-regimes/2026-27", "admin", `{"expected_version":0,"regime":"new","intimated_on":"2026-04-01","note":""}`)
	require.Equal(t, 404, code, "another hospital's staff member: %s", raw)

	code, list = f.call("GET", "/staff/"+f.staff+"/tax-regimes", "viewer", nil)
	require.Equal(t, 200, code, list)
	years := []string{}
	for _, e := range list["data"].([]any) {
		years = append(years, e.(map[string]any)["tax_year"].(string))
	}
	require.Equal(t, []string{"2099-00", "2027-28", "2025-26", "2026-27"}, years, "most recently recorded first")
	code, history := f.call("GET", "/staff/"+f.staff+"/tax-regimes/2026-27/history", "viewer", nil)
	require.Equal(t, 200, code, history)
	versions := []float64{}
	for _, r := range history["data"].([]any) {
		versions = append(versions, r.(map[string]any)["version"].(float64))
	}
	require.Equal(t, []float64{2, 1}, versions)
	require.Equal(t, "Synthetic correction", history["data"].([]any)[0].(map[string]any)["note"])
	require.Equal(t, "user-maker", history["data"].([]any)[0].(map[string]any)["recorded_by_subject"])
	for path, status := range map[string]int{"/tax-regimes/2030-31/history": 404, "/tax-regimes/bad/history": 404, "/tax-regimes?limit=101": 400} {
		code, _ = f.do("GET", f.base+"/hr/payroll/staff/"+f.staff+path, "viewer", "")
		require.Equal(t, status, code, path)
	}
	for user, status := range map[string]int{"directory": 403, "foreign": 404, "": 401} {
		code, _ = f.do("GET", f.base+"/hr/payroll/staff/"+f.staff+"/tax-regimes", user, "")
		require.Equal(t, status, code, user)
	}

	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var events []struct{ Payload []byte }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.TaxRegimeChanged).Find(&events).Error)
		require.Len(t, events, 6, "one event per recorded version, none for replays")
		for _, e := range events {
			var envelope struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(e.Payload, &envelope))
			require.ElementsMatch(t, []string{"hospital_id", "staff_id", "tax_year", "version"}, slices.Collect(maps.Keys(envelope.Data)), "events never carry the regime")
		}
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE ?", "workforce.tax_regime.%").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 6)
		for _, c := range changes {
			require.NotContains(t, c, "new")
			require.NotContains(t, c, "old")
			require.NotContains(t, c, "Synthetic")
		}
		return nil
	}))
}

func TestTaxRegimeDatabaseGuards(t *testing.T) {
	f := newPayFixture(t)
	code, out := f.call("PUT", "/staff/"+f.staff+"/tax-regimes/2026-27", "admin", regimeBody(0, "new", "2026-04-01", ""))
	require.Equal(t, 200, code, out)
	run := func(statements ...string) error {
		return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, f.hospital).Error; err != nil {
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
	advance := func(version int, regime, intimated string) string {
		return fmt.Sprintf(`UPDATE workforce_staff_tax_regimes SET version=%d,regime='%s',intimated_on='%s' WHERE staff_id='%s'`, version, regime, intimated, f.staff)
	}
	revision := func(version int, regime, intimated, note string) string {
		return `INSERT INTO workforce_staff_tax_regime_revisions(tenant_id,hospital_id,id,staff_id,tax_year,version,regime,intimated_on,note,recorded_by_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,tax_year,` + strconv.Itoa(version) + `,'` + regime + `','` + intimated + `','` + note + `','user-admin' FROM workforce_staff_tax_regimes WHERE staff_id='` + f.staff + `'`
	}
	for name, tc := range map[string]struct {
		statements []string
		guard      string
	}{
		"a change without a note":      {[]string{revision(2, "old", "2026-05-01", ""), advance(2, "old", "2026-05-01")}, "workforce_tax_regime_change_note"},
		"an intimation after the year": {[]string{revision(2, "old", "2027-04-01", "x"), advance(2, "old", "2027-04-01")}, "workforce_tax_regime_intimated"},
		"an election skips a version":  {[]string{revision(3, "old", "2026-05-01", "x"), advance(3, "old", "2026-05-01")}, "advances one version at a time"},
		"an election without its row":  {[]string{advance(2, "old", "2026-04-01")}, "workforce_staff_tax_regime_current"},
		"revisions are immutable":      {[]string{`UPDATE workforce_staff_tax_regime_revisions SET regime='old' WHERE staff_id='` + f.staff + `'`}, "append only"},
		"elections are never deleted":  {[]string{`DELETE FROM workforce_staff_tax_regimes WHERE staff_id='` + f.staff + `'`}, "destructive workforce operation"},
		"a malformed tax year": {[]string{`INSERT INTO workforce_staff_tax_regimes(tenant_id,hospital_id,id,staff_id,tax_year,regime,intimated_on,version) SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,'2026-28','new','2026-04-01',1 FROM workforce_staff_tax_regimes WHERE staff_id='` + f.staff + `'`},
			"workforce_staff_tax_regimes_tax_year_check"},
	} {
		require.ErrorContains(t, run(tc.statements...), tc.guard, name)
	}
	require.NoError(t, run(revision(2, "old", "2026-05-01", "x"), advance(2, "old", "2026-05-01")), "the legal change succeeds under the same session")
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_staff_tax_regimes", "workforce_staff_tax_regime_revisions"} {
			var n int64
			require.NoError(t, tx.Table(table).Count(&n).Error)
			require.Zero(t, n, "forced RLS without hospital scope: %s", table)
		}
		return nil
	}))
}
