package workforce_test

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const coverageHeader = "as_of,enrollment_id,employee_code,employee_name,employment_status,plan_code,plan_name,plan_kind,covered_person,dependent_name,dependent_relationship,dependent_status,coverage_from,coverage_until,approved_version,approved_at_utc\r\n"

func (f *benefitFixture) export(query, user string) *httptest.ResponseRecorder {
	f.t.Helper()
	return testutil.Do(f.r, "GET", f.base+"/hr/benefits/coverage.csv?"+query, user, "")
}

// approvedAt returns when the enrollment's coverage in force was approved, formatted as the export writes it.
func (f *benefitFixture) approvedAt(staff, enrollment string) string {
	f.t.Helper()
	for _, item := range f.ok("GET", f.base+"/staff/"+staff+"/benefit-enrollments/"+enrollment+"/history", "hr", nil)["data"].([]any) {
		if r := item.(map[string]any); r["status"] == "approved" {
			at, err := time.Parse(time.RFC3339Nano, r["decided_at"].(string))
			require.NoError(f.t, err)
			return at.UTC().Format(time.RFC3339Nano)
		}
	}
	f.t.Fatalf("enrollment %s has no approved proposal", enrollment)
	return ""
}

func requireCoverageFile(t *testing.T, w *httptest.ResponseRecorder, asOf string, rows int) string {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "text/csv; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, `attachment; filename="benefits-coverage-`+asOf+`.csv"`, w.Header().Get("Content-Disposition"))
	require.Equal(t, strconv.Itoa(rows), w.Header().Get("X-Export-Row-Count"))
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes())), w.Header().Get("X-Export-Sha256"))
	return w.Body.String()
}

func requireNoFile(t *testing.T, w *httptest.ResponseRecorder, status int, context string) {
	t.Helper()
	require.Equal(t, status, w.Code, context+": "+w.Body.String())
	for _, header := range []string{"Content-Disposition", "X-Export-Row-Count", "X-Export-Sha256"} {
		require.Empty(t, w.Header().Get(header), context)
	}
	require.NotContains(t, w.Body.String(), "enrollment_id,employee_code", context)
}

func TestBenefitCoverageExportListsExactlyTheCoverageInForceOnTheDate(t *testing.T) {
	f := newBenefitFixture(t)
	first, second := f.staff("EXP-01"), f.staff("EXP-02")
	family, _ := f.plan("SYN-EXP-A", f.child)
	solo, _ := f.plan("SYN-EXP-B")
	kid := uuid.NewString()
	f.ok("PUT", f.dependentPath(first, kid), "hr", dependentBody(0, "Synthetic Kid", f.masterRevision(f.child), "active"))

	ended := f.cover(first, family, nil, "2024-01-01", "2024-12-31")
	child := f.cover(first, family, kid, "2024-06-01", nil)
	narrowed := uuid.NewString()
	f.ok("PUT", f.proposalPath(first, child, narrowed, ""), "hr", proposalBody(1, family, kid, "2024-06-01", "2025-06-30"))
	f.ok("POST", f.proposalPath(first, child, narrowed, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
	single := f.cover(second, solo, nil, "2025-01-01", nil)
	f.ok("PUT", f.proposalPath(first, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, solo, nil, "2024-01-01", nil))
	rejected, refusal := uuid.NewString(), uuid.NewString()
	f.ok("PUT", f.proposalPath(second, rejected, refusal, ""), "hr", proposalBody(0, family, nil, "2024-01-01", nil))
	f.ok("POST", f.proposalPath(second, rejected, refusal, "/decision"), "approver", map[string]any{"expected_version": 0, "decision": "reject", "note": "Synthetic not eligible"})

	line := func(asOf, staff, enrollment, code, plan, dependent, from, until, version string) string {
		fields := []string{asOf, enrollment, code, "Synthetic " + code, "active", plan, "Synthetic " + plan, "health"}
		if dependent == "" {
			fields = append(fields, "employee", "", "", "")
		} else {
			fields = append(fields, "dependent", dependent, "Synthetic child", "active")
		}
		return strings.Join(append(fields, from, until, version, f.approvedAt(staff, enrollment)), ",") + "\r\n"
	}
	cases := []struct {
		query, asOf, plan string
		rows              []string
	}{
		{"as_of=2023-12-31", "2023-12-31", "", nil},
		{"as_of=2024-01-01", "2024-01-01", "", []string{line("2024-01-01", first, ended, "EXP-01", "SYN-EXP-A", "", "2024-01-01", "2024-12-31", "1")}},
		{"as_of=2024-12-31", "2024-12-31", "", []string{line("2024-12-31", first, ended, "EXP-01", "SYN-EXP-A", "", "2024-01-01", "2024-12-31", "1"),
			line("2024-12-31", first, child, "EXP-01", "SYN-EXP-A", "Synthetic Kid", "2024-06-01", "2025-06-30", "2")}},
		{"as_of=2025-06-30", "2025-06-30", "", []string{line("2025-06-30", first, child, "EXP-01", "SYN-EXP-A", "Synthetic Kid", "2024-06-01", "2025-06-30", "2"),
			line("2025-06-30", second, single, "EXP-02", "SYN-EXP-B", "", "2025-01-01", "", "1")}},
		{"as_of=2025-07-01", "2025-07-01", "", []string{line("2025-07-01", second, single, "EXP-02", "SYN-EXP-B", "", "2025-01-01", "", "1")}},
		{"plan_id=" + family + "&as_of=2025-06-30", "2025-06-30", family, []string{line("2025-06-30", first, child, "EXP-01", "SYN-EXP-A", "Synthetic Kid", "2024-06-01", "2025-06-30", "2")}},
	}
	var audits []map[string]any
	for _, c := range cases {
		w := f.export(c.query, "exporter")
		require.Equal(t, coverageHeader+strings.Join(c.rows, ""), requireCoverageFile(t, w, c.asOf, len(c.rows)), c.query)
		plan := any(nil)
		if c.plan != "" {
			plan = c.plan
		}
		audits = append(audits, map[string]any{"report": "workforce.benefits.coverage", "as_of": c.asOf, "plan_id": plan, "row_count": float64(len(c.rows)), "sha256": w.Header().Get("X-Export-Sha256")})
	}
	require.Equal(t, audits, f.auditChanges("workforce.benefits.exported"), "each file is audited once with its filters, count and digest, and no cell values")
}

func TestBenefitCoverageExportOrdersByEmployeeThenPlanWithDependentsAfterTheEmployee(t *testing.T) {
	f := newBenefitFixture(t)
	later, earlier := f.staff("ORD-02"), f.staff("ORD-01")
	family, _ := f.plan("SYN-ORD-A", f.child)
	solo, _ := f.plan("SYN-ORD-B")
	f.cover(later, family, nil, "2024-01-01", nil)
	f.cover(earlier, solo, nil, "2024-01-01", nil)
	f.cover(earlier, family, nil, "2024-01-01", nil)
	for _, name := range []string{"Synthetic Beta", "Synthetic Alpha"} {
		kid := uuid.NewString()
		f.ok("PUT", f.dependentPath(earlier, kid), "hr", dependentBody(0, name, f.masterRevision(f.child), "active"))
		f.cover(earlier, family, kid, "2024-01-01", nil)
	}
	var order []string
	for _, line := range strings.Split(strings.TrimSuffix(requireCoverageFile(t, f.export("as_of=2024-06-01", "exporter"), "2024-06-01", 5), "\r\n"), "\r\n")[1:] {
		fields := strings.Split(line, ",")
		order = append(order, fields[2]+" "+fields[5]+" "+fields[9])
	}
	require.Equal(t, []string{"ORD-01 SYN-ORD-A ", "ORD-01 SYN-ORD-A Synthetic Alpha", "ORD-01 SYN-ORD-A Synthetic Beta", "ORD-01 SYN-ORD-B ", "ORD-02 SYN-ORD-A "}, order,
		"the file is in a stable order however the rows were written")
}

func TestBenefitCoverageExportNeedsItsOwnPermissionAndAHospitalGrant(t *testing.T) {
	f := newBenefitFixture(t)
	plan, _ := f.plan("SYN-EXP-DENY")
	f.cover(f.staff("DENY-01"), plan, nil, "2024-01-01", nil)
	query := "as_of=2024-06-01"
	for user, status := range map[string]int{"hr": 403, "approver": 403, "reader": 403, "staff-only": 403, "export-ungranted": 404, "foreign": 404, "unknown": 401} {
		requireNoFile(t, f.export(query, user), status, user)
	}
	requireNoFile(t, testutil.Do(f.r, "GET", f.other+"/hr/benefits/coverage.csv?"+query, "exporter", ""), 404, "another hospital")

	require.Contains(t, requireCoverageFile(t, f.export(query, "exporter"), "2024-06-01", 1), ",DENY-01,", "a reader grant is enough")
	f.ok("PUT", f.base+"/access/user-exporter", "hr", map[string]any{"level": "manager"})
	require.Contains(t, requireCoverageFile(t, f.export(query, "exporter"), "2024-06-01", 1), ",DENY-01,", "so is a manager grant")
	require.Equal(t, 204, testutil.Do(f.r, "DELETE", f.base+"/access/user-exporter", "hr", "").Code)
	requireNoFile(t, f.export(query, "exporter"), 404, "revoked grant")
	require.Equal(t, 2, f.audits("workforce.benefits.exported"), "refused requests write no export audit")
}

func TestBenefitCoverageExportRefusesUnknownRepeatedEmptyAndMalformedFilters(t *testing.T) {
	f := newBenefitFixture(t)
	plan, _ := f.plan("SYN-EXP-FILTER")
	query := "as_of=2024-06-01"
	for _, bad := range []string{"", "as_of=", "as_of=2024-6-01", "as_of=2024-02-30", "as_of=1999-12-31", "as_of=2201-01-01", "as_of=2024-06-01T00:00:00Z", "as_of=%202024-06-01",
		query + "&as_of=2024-06-02", query + "&plan_id=", query + "&plan_id=no", query + "&plan_id=" + uuid.Nil.String(), query + "&plan_id=" + plan + "&plan_id=" + plan,
		query + "&limit=100", query + "&cursor=no", query + "&staff_id=" + uuid.NewString(), query + "&broken=%ZZ"} {
		w := f.export(bad, "exporter")
		requireNoFile(t, w, 400, bad)
		require.Contains(t, w.Body.String(), "invalid_request", bad)
	}
	requireNoFile(t, f.export(query+"&plan_id="+uuid.NewString(), "exporter"), 404, "unknown plan")
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.other+"/access/user-hr", "hr", `{"level":"manager"}`).Code)
	elsewhere := uuid.NewString()
	f.ok("PUT", f.other+"/hr/benefits/plans/"+elsewhere, "hr", planBody(0, "SYN-EXP-ELSEWHERE", "2020-01-01", nil))
	requireNoFile(t, f.export(query+"&plan_id="+elsewhere, "exporter"), 404, "another hospital's plan")
	require.Equal(t, 0, f.audits("workforce.benefits.exported"))
	require.Equal(t, coverageHeader, requireCoverageFile(t, f.export(query+"&plan_id="+plan, "exporter"), "2024-06-01", 0), "an empty result is a header-only file")
}

func TestBenefitCoverageExportProtectsFormulaTextAndRefusesRowOverflow(t *testing.T) {
	f := newBenefitFixture(t)
	staff := f.staff("CSV-01")
	plan, body := uuid.NewString(), planBody(0, "SYN-EXP-CSV", "2020-01-01", nil, f.child)
	body["name"] = `+Synthetic "Gold", family`
	f.ok("PUT", f.base+"/hr/benefits/plans/"+plan, "hr", body)
	kid, name := uuid.NewString(), `=HYPERLINK("https://invalid.example","Synthetic")`
	f.ok("PUT", f.dependentPath(staff, kid), "hr", dependentBody(0, name, f.masterRevision(f.child), "active"))
	f.cover(staff, plan, kid, "2024-01-01", nil)
	rows, err := csv.NewReader(strings.NewReader(requireCoverageFile(t, f.export("as_of=2024-06-01", "exporter"), "2024-06-01", 1))).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	values := map[string]string{}
	for i, key := range rows[0] {
		values[key] = rows[1][i]
	}
	require.Equal(t, `'+Synthetic "Gold", family`, values["plan_name"])
	require.Equal(t, "'"+name, values["dependent_name"])
	require.Equal(t, "CSV-01", values["employee_code"])

	bulk, saved := f.plan("SYN-EXP-BULK")
	seed := func(first, last int) {
		t.Helper()
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','user-hr',true),set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_benefits','write',true),set_config('app.workforce_benefit_approval','authorized',true)`, f.hospital).Error; err != nil {
				return err
			}
			id := func(prefix string) string { return `('` + prefix + `-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid` }
			for _, statement := range []string{
				`INSERT INTO workforce_staff(tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) SELECT ?,?,` + id("0199e0b1") + `,'SEED-'||n,'Synthetic seed '||n,'nurse','active' FROM generate_series(?::integer,?::integer) n`,
				`INSERT INTO workforce_benefit_enrollment_proposals(tenant_id,hospital_id,id,enrollment_id,staff_id,plan_id,plan_revision_id,base_version,status,coverage_from,eligibility_basis,proposed_by_subject,decision_note)
				 SELECT ?,?,` + id("0199e0b3") + `,` + id("0199e0b2") + `,` + id("0199e0b1") + `,'` + bulk + `','` + saved["revision_id"].(string) + `',0,'pending','2024-01-01','Synthetic seed','user-hr','' FROM generate_series(?::integer,?::integer) n`,
				`UPDATE workforce_benefit_enrollment_proposals SET status='approved',approved_version=1,decided_by_subject='user-approver',decided_at=clock_timestamp()
				 WHERE tenant_id=? AND hospital_id=? AND id IN (SELECT ` + id("0199e0b3") + ` FROM generate_series(?::integer,?::integer) n)`,
				`INSERT INTO workforce_benefit_enrollments(tenant_id,hospital_id,id,staff_id,plan_id,original_plan_revision_id,coverage_from,version,proposal_id)
				 SELECT ?,?,` + id("0199e0b2") + `,` + id("0199e0b1") + `,'` + bulk + `','` + saved["revision_id"].(string) + `','2024-01-01',1,` + id("0199e0b3") + ` FROM generate_series(?::integer,?::integer) n`,
			} {
				if err := tx.Exec(statement, testutil.TenantA, f.hospital, first, last).Error; err != nil {
					return err
				}
			}
			return nil
		}))
	}
	path := "as_of=2024-06-01&plan_id=" + bulk
	seed(1, 1000)
	file := requireCoverageFile(t, f.export(path, "exporter"), "2024-06-01", 1000)
	require.Len(t, strings.Split(strings.TrimSuffix(file, "\r\n"), "\r\n"), 1001)
	require.Contains(t, file, ",SEED-1000,")
	seed(1001, 1001)
	w := f.export(path, "exporter")
	requireNoFile(t, w, 413, "1,001 rows")
	require.Contains(t, w.Body.String(), "benefits_export_too_large")
	require.NotContains(t, w.Body.String(), "SEED-")
	require.Len(t, f.auditChanges("workforce.benefits.exported"), 2, "a refused file is not audited")
	requireCoverageFile(t, f.export("as_of=2024-06-01&plan_id="+plan, "exporter"), "2024-06-01", 1)
}

func TestBenefitCoverageExportSendsNoFileWhenItsAuditCannotBeWritten(t *testing.T) {
	f := newBenefitFixture(t)
	plan, _ := f.plan("SYN-EXP-AUDIT")
	f.cover(f.staff("AUD-01"), plan, nil, "2024-01-01", nil)
	requireCoverageFile(t, f.export("as_of=2024-06-01", "exporter"), "2024-06-01", 1)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_refuse_export_audit", SQL: `
CREATE FUNCTION test_refuse_export_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.action='workforce.benefits.exported' THEN RAISE EXCEPTION 'test audit unavailable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER test_refuse_export_audit BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION test_refuse_export_audit();`}}))
	w := f.export("as_of=2024-06-01", "exporter")
	requireNoFile(t, w, 500, "audit refused")
	require.NotContains(t, w.Body.String(), "AUD-01")
	require.NotContains(t, w.Body.String(), "test audit unavailable")
	require.Equal(t, 1, f.audits("workforce.benefits.exported"))
}
