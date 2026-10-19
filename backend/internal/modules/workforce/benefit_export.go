package workforce

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/csvsafe"
)

const (
	benefitExportRowLimit  = 1000
	benefitExportByteLimit = 8 * 1024 * 1024
	benefitCoverageReport  = "workforce.benefits.coverage"
)

var (
	errBenefitExportTooLarge      = errors.New("benefit export exceeds its row limit")
	errBenefitDependentUnreadable = errors.New("covered dependent unreadable")
)

// benefitCoverageRow is one approved enrollment in force on the export date, carrying only what reconciling coverage needs.
type benefitCoverageRow struct {
	EnrollmentID          uuid.UUID
	EmployeeCode          string
	EmployeeName          string
	EmploymentStatus      string
	PlanCode              string
	PlanName              string
	PlanKind              string
	DependentID           *uuid.UUID
	DependentName         *string
	DependentRelationship *string
	DependentStatus       *string
	CoverageFrom          string
	CoverageUntil         *string
	ApprovedVersion       int64
	ApprovedAt            time.Time
}

// benefitExportFilters accepts as_of and plan_id only, each once and nonempty, so a mistyped filter never silently widens the file.
func benefitExportFilters(c *gin.Context) (string, *uuid.UUID, bool) {
	invalid := func(message string) (string, *uuid.UUID, bool) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", message)
		return "", nil, false
	}
	filters, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return invalid("Use a well-formed query.")
	}
	for key, values := range filters {
		if key != "as_of" && key != "plan_id" {
			return invalid("Filter the export by as_of and plan_id only.")
		}
		if len(values) != 1 || values[0] == "" {
			return invalid("Give each export filter once, with a value.")
		}
	}
	asOf := filters.Get("as_of")
	if asOf < "2000-01-01" || !validDate(asOf) {
		return invalid("Choose an as-of date between 2000-01-01 and 2200-12-31.")
	}
	if _, ok := filters["plan_id"]; !ok {
		return asOf, nil, true
	}
	plan, err := uuid.Parse(filters.Get("plan_id"))
	if err != nil || plan == uuid.Nil {
		return invalid("Choose a benefit plan.")
	}
	return asOf, &plan, true
}

func (h *handlers) exportBenefitCoverage(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	asOf, plan, ok := benefitExportFilters(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var data []byte
	var count int
	var digest string
	err := h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		q := tx.Table("workforce_benefit_enrollments e").
			Select(`e.id AS enrollment_id,s.employee_code,s.display_name AS employee_name,s.employment_status,p.code AS plan_code,p.name AS plan_name,r.kind AS plan_kind,
			 e.dependent_id,d.full_name AS dependent_name,m.name AS dependent_relationship,d.status AS dependent_status,
			 e.coverage_from::text AS coverage_from,e.coverage_until::text AS coverage_until,e.version AS approved_version,a.decided_at AS approved_at`).
			Joins("CROSS JOIN "+benefitExportLookup("workforce_staff", "employee_code,display_name,employment_status", "e.staff_id")+" s").
			Joins("CROSS JOIN "+benefitExportLookup("workforce_benefit_plans", "code,name,revision_id", "e.plan_id")+" p").
			Joins("CROSS JOIN "+benefitExportLookup("workforce_benefit_plan_revisions", "kind", "p.revision_id")+" r").
			Joins("CROSS JOIN "+benefitExportLookup("workforce_benefit_enrollment_proposals", "decided_at", "e.proposal_id")+" a").
			Joins("LEFT JOIN "+benefitExportLookup("workforce_staff_dependents", "full_name,status,revision_id", "e.dependent_id")+" d ON true").
			Joins("LEFT JOIN "+benefitExportLookup("workforce_staff_dependent_revisions", "relationship_revision_id", "d.revision_id")+" dr ON true").
			Joins("LEFT JOIN "+benefitExportLookup("workforce_hr_master_revisions", "name", "dr.relationship_revision_id")+" m ON true").
			Where("e.tenant_id=? AND e.hospital_id=? AND e.coverage_from<=?::date AND (e.coverage_until IS NULL OR e.coverage_until>=?::date)", p.TenantID, hospital, asOf, asOf)
		if plan != nil {
			if err := tx.Table("workforce_benefit_plans").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, *plan).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
				return err
			}
			q = q.Where("e.plan_id=?", *plan)
		}
		// The probe is unordered so no sort can steer the plan; over the limit nothing is sent, under it every row is.
		var rows []benefitCoverageRow
		if err := q.Limit(benefitExportRowLimit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > benefitExportRowLimit {
			return errBenefitExportTooLarge
		}
		slices.SortFunc(rows, compareBenefitCoverageRows)
		var err error
		if data, err = encodeBenefitCoverageCSV(rows, asOf); err != nil {
			return err
		}
		count, digest = len(rows), fmt.Sprintf("%x", sha256.Sum256(data))
		return benefitAudit(tx, p, hospital, "exported", hospital, map[string]any{"report": benefitCoverageReport, "as_of": asOf, "plan_id": plan, "row_count": count, "sha256": digest})
	})
	if errors.Is(err, errBenefitExportTooLarge) || errors.Is(err, csvsafe.ErrTooLarge) {
		respond.Error(c, http.StatusRequestEntityTooLarge, "benefits_export_too_large", "The export exceeds 1,000 rows or 8 MiB. Narrow it to one plan.")
		return
	}
	if err != nil {
		respondBenefitError(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="benefits-coverage-%s.csv"`, asOf))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(count))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

// benefitExportLookup reads one row by primary key per enrollment; OFFSET 0 stops Postgres flattening it into a join that underestimated statistics turn quadratic.
func benefitExportLookup(table, columns, id string) string {
	return "LATERAL (SELECT " + columns + " FROM " + table + " WHERE tenant_id=e.tenant_id AND hospital_id=e.hospital_id AND id=" + id + " OFFSET 0)"
}

// compareBenefitCoverageRows orders by employee, then plan, the employee's own coverage before dependents, so the same coverage always yields the same bytes.
func compareBenefitCoverageRows(a, b benefitCoverageRow) int {
	return cmp.Or(
		strings.Compare(a.EmployeeCode, b.EmployeeCode),
		strings.Compare(a.PlanCode, b.PlanCode),
		cmp.Compare(boolRank(a.DependentID != nil), boolRank(b.DependentID != nil)),
		strings.Compare(deref(a.DependentName), deref(b.DependentName)),
		bytes.Compare(a.EnrollmentID[:], b.EnrollmentID[:]),
	)
}

func boolRank(v bool) int {
	if v {
		return 1
	}
	return 0
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func encodeBenefitCoverageCSV(rows []benefitCoverageRow, asOf string) ([]byte, error) {
	w := csvsafe.NewWriter(benefitExportByteLimit)
	if err := w.Write([]string{"as_of", "enrollment_id", "employee_code", "employee_name", "employment_status", "plan_code", "plan_name", "plan_kind", "covered_person", "dependent_name", "dependent_relationship", "dependent_status", "coverage_from", "coverage_until", "approved_version", "approved_at_utc"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		person, name, relationship, status, until := "employee", "", "", "", ""
		if row.DependentID != nil {
			// A covered dependent the scope cannot read fails the file rather than passing as the employee's own coverage.
			if row.DependentName == nil || row.DependentRelationship == nil || row.DependentStatus == nil {
				return nil, errBenefitDependentUnreadable
			}
			person, name, relationship, status = "dependent", *row.DependentName, *row.DependentRelationship, *row.DependentStatus
		}
		if row.CoverageUntil != nil {
			until = *row.CoverageUntil
		}
		if err := w.Write([]string{asOf, row.EnrollmentID.String(), csvsafe.Text(row.EmployeeCode), csvsafe.Text(row.EmployeeName), row.EmploymentStatus, csvsafe.Text(row.PlanCode), csvsafe.Text(row.PlanName), row.PlanKind,
			person, csvsafe.Text(name), csvsafe.Text(relationship), status, row.CoverageFrom, until, strconv.FormatInt(row.ApprovedVersion, 10), row.ApprovedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
