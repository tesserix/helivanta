package workforce

import (
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
	"slices"
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

func registerEmploymentPreparation(g *platform.Router, h *handlers) {
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
