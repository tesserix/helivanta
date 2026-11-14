package workforce

import (
	_ "embed"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (h *handlers) governanceEmploymentRecord(operation employmentPreparationOperation) func(*gin.Context) (employmentRecord, bool) {
	return func(c *gin.Context) (employmentRecord, bool) {
		var allowed []string
		switch operation {
		case preparationList, preparationHistory:
			allowed = []string{"limit", "cursor", "lifecycle"}
		case preparationRead:
			allowed = []string{"lifecycle"}
		}
		for key, values := range c.Request.URL.Query() {
			if len(values) != 1 || !slices.Contains(allowed, key) {
				respondIntakeError(c, errIntakeProjection)
				return employmentRecord{}, false
			}
		}
		p, hospital, ok := profileScope(c)
		if !ok {
			return employmentRecord{}, false
		}
		record, ok := employmentIDs(c, p, establishment{kind: hospitalEstablishmentKind, id: hospital})
		if !ok {
			return employmentRecord{}, false
		}
		record.authority = func(required []authz.Permission, fn func(*gorm.DB) error) error {
			if len(required) != 1 || required[0] != PermEnrollmentManage || operation == preparationSave {
				return authz.ErrScopedAuthorizationUnavailable
			}
			return h.withEmploymentPreparation(c, p, hospital, record.staff, record.period, operation, nil, fn)
		}
		if operation == preparationSave {
			record.prepareDraft = func(input EmploymentPeriodDraftInput, fn func(*gorm.DB, EmploymentPeriodSource) error) error {
				selected := record
				selected.authority = func(required []authz.Permission, work func(*gorm.DB) error) error {
					if len(required) != 1 || required[0] != PermEnrollmentManage {
						return authz.ErrScopedAuthorizationUnavailable
					}
					return h.withEmploymentPreparation(c, p, hospital, record.staff, record.period, preparationSave, &input, work)
				}
				return selected.transaction(fn)
			}
		}
		return record, true
	}
}

//go:embed employment_preparation_targets.sql
var employmentPreparationTargetsSchema string

func employmentPreparationTargetInput(c *gin.Context) (PersonReviewTargetSearchInput, bool) {
	var input PersonReviewTargetSearchInput
	if !decodeSized(c, &input, 1024) {
		return input, false
	}
	if input.EmployeeCode == nil || (*input.EmployeeCode != "" && !employeeCode.MatchString(*input.EmployeeCode)) {
		respond.Error(c, 400, "invalid_request", "Enter a Staff ID, or an empty value to browse staff.")
		return input, false
	}
	normalized := strings.ToLower(*input.EmployeeCode)
	input.EmployeeCode = &normalized
	return input, true
}

func (h *handlers) employmentPreparationTargets(c *gin.Context, input PersonReviewTargetSearchInput, page pagination.Params) ([]PersonReviewTarget, error) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return nil, nil
	}
	rows := []PersonReviewTarget{}
	err := h.withEmploymentPreparation(c, p, hospital, uuid.Nil, uuid.Nil, preparationTargets, nil, func(tx *gorm.DB) error {
		var supported bool
		if err := tx.Raw(`SELECT pg_get_functiondef(to_regprocedure('public.workforce_governance_draft_source(uuid,uuid,uuid)')) LIKE '%targets%'
 AND pg_get_functiondef(to_regprocedure('public.workforce_governance_draft_staff_read(uuid,uuid,uuid,text)')) LIKE '%targets%'
 AND to_regprocedure('public.workforce_governance_target_audit(uuid,uuid,text,text,uuid,jsonb)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_audit') AND polpermissive AND pg_get_expr(polwithcheck,polrelid) LIKE '%workforce_governance_target_audit%')
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_audit') AND polname='governance_draft_purpose' AND NOT polpermissive AND pg_get_expr(polwithcheck,polrelid) LIKE '%workforce_governance_target_audit%')
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_staff_code_casefold'),('workforce_staff_tenant_id_hospital_id_created_at_id_idx')) required(index_name) WHERE NOT EXISTS(SELECT 1 FROM pg_index WHERE indrelid=to_regclass('public.workforce_staff') AND indexrelid=to_regclass('public.'||required.index_name) AND indisvalid AND indisready))`).Scan(&supported).Error; err != nil {
			return err
		}
		if !supported {
			return authz.ErrScopedAuthorizationUnavailable
		}
		query := tenantdb.ApplyKeyset(tx, page).Table("workforce_staff").Select("id,hospital_id,employee_code,display_name,employment_status,version AS staff_version,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if *input.EmployeeCode != "" {
			query = query.Where("lower(employee_code)=?", *input.EmployeeCode)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if err := employmentPeriodAudit(tx, p, hospital, row.ID, "workforce.employment_preparation.target.read", row.StaffVersion); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		respondIntakeError(c, err)
		return nil, nil
	}
	return rows, nil
}

func registerEmploymentPreparation(g *platform.Router, h *handlers) {
	platform.SealedSearchRoute(g, "/:hospital/employment-preparation-targets/search", authz.Public, "hospital", 20, employmentPreparationTargetInput, h.employmentPreparationTargets)
	periods := "/:hospital/staff/:id/employment-preparation-periods"
	g.GET("/:hospital/staff/:id/employment-preparation-source", authz.Public, employmentRoute(h.governanceEmploymentRecord(preparationSource), employmentSource, asIs[EmploymentPeriodSource]))
	g.PUT(periods+"/:period", authz.Public, employmentRoute(h.governanceEmploymentRecord(preparationSave), saveEmploymentPeriod, asIs[EmploymentPeriodDraft]))
	g.GET(periods+"/:period", authz.Public, employmentRoute(h.governanceEmploymentRecord(preparationRead), employmentPeriod, asIs[EmploymentPeriodDraft]))
	scan := func(c *gin.Context, page pagination.Params) ([]EmploymentPeriodDraft, error) {
		record, ok := h.governanceEmploymentRecord(preparationList)(c)
		if !ok {
			return nil, nil
		}
		rows, ok := employmentPeriods(c, record, page)
		if !ok {
			return nil, nil
		}
		return rows, nil
	}
	platform.SealedFilteredQueryListRoute(g, periods, authz.Public, "hospital", []platform.QueryFilter{{Key: "lifecycle"}}, 50, scan, func(_ *gin.Context, rows []EmploymentPeriodDraft) ([]EmploymentPeriodDraft, error) { return rows, nil }, "id")
	history := func(c *gin.Context, page pagination.Params) ([]employmentPeriodRevision, error) {
		record, ok := h.governanceEmploymentRecord(preparationHistory)(c)
		if !ok {
			return nil, nil
		}
		rows, ok := employmentPeriodHistory(c, record, page)
		if !ok {
			return nil, nil
		}
		return rows, nil
	}
	platform.SealedFilteredQueryListRoute(g, periods+"/:period/history", authz.Public, "hospital", []platform.QueryFilter{{Key: "lifecycle"}}, 50, history, func(_ *gin.Context, rows []employmentPeriodRevision) ([]employmentPeriodRevision, error) {
		return rows, nil
	}, "id", "period")
}
