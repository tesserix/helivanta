package workforce

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
	"strconv"
	"time"
)

func (h *handlers) governanceEmploymentBindings(c *gin.Context, operation bindingPreparationOperation) (employmentEvidenceScope, *employmentPreparationBindings, bool) {
	c.Header("Cache-Control", "no-store")
	p, hospital, ok := profileScope(c)
	if !ok {
		return employmentEvidenceScope{}, nil, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respondIntakeError(c, errIntakeWorkspaceRequired)
		return employmentEvidenceScope{}, nil, false
	}
	query := c.Request.URL.Query()
	for key, values := range query {
		allowed := contains(key, "staff_version", "employment_epoch") || (operation == bindingPreparationList || operation == bindingPreparationHistory) && contains(key, "limit", "cursor")
		if len(values) != 1 || !allowed {
			respondIntakeError(c, errIntakeProjection)
			return employmentEvidenceScope{}, nil, false
		}
	}
	values := map[string]int64{}
	for _, key := range []string{"staff_version", "employment_epoch"} {
		v, err := strconv.ParseInt(query.Get(key), 10, 64)
		if err != nil || v < 1 || v > 9007199254740990 || strconv.FormatInt(v, 10) != query.Get(key) {
			respondIntakeError(c, errIntakeProjection)
			return employmentEvidenceScope{}, nil, false
		}
		values[key] = v
	}
	record, ok := employmentIDs(c, p, establishment{kind: hospitalEstablishmentKind, id: hospital})
	if !ok {
		return employmentEvidenceScope{}, nil, false
	}
	scope, ok := employmentEvidenceParams(c, record)
	if !ok {
		return scope, nil, false
	}
	selected := employmentBindingSelection{Staff: record.staff, Period: record.period, Binding: scope.binding, PeriodVersion: scope.version, StaffVersion: values["staff_version"], EmploymentEpoch: values["employment_epoch"], Operation: operation}
	gate := &employmentPreparationBindings{h: h, principal: p, hospital: hospital, selected: selected}
	scope.authority = func(required []authz.Permission, fn func(*gorm.DB) error) error {
		if len(required) != 2 || required[0] != PermEnrollmentManage || required[1] != PermDocumentManage {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return gate.transaction(c, fn)
	}
	scope.prepareCommand = gate.prepare
	return scope, gate, true
}
func (h *handlers) nativeEmploymentBindingRoute(operation bindingPreparationOperation) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		scope, gate, ok := h.governanceEmploymentBindings(c, operation)
		if !ok {
			return
		}
		var result EmploymentEvidenceBinding
		switch operation {
		case bindingPreparationRead:
			result, ok = employmentEvidence(c, scope)
		case bindingPreparationAttach:
			result, ok = attachEmploymentEvidence(c, scope)
		case bindingPreparationWithdraw:
			result, ok = withdrawEmploymentEvidence(c, scope)
		case bindingPreparationDownload:
			var binding EmploymentEvidenceBinding
			err := employmentEvidenceTransaction(scope, false, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
				return evidenceWhere(tx, scope, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", scope.binding).Take(&binding).Error
			})
			if err != nil {
				evidenceError(c, err)
				return
			}
			h.sendDocument(c, &governanceBindingDocumentGate{employmentEvidenceGate: employmentEvidenceGate{scope: scope, file: binding.DocumentID}, authority: gate}, scope.p, scope.owner.id, scope.staff, binding.DocumentID)
			return
		default:
			respondIntakeError(c, authz.ErrScopedAuthorizationUnavailable)
			return
		}
		if ok {
			respond.OK(c, result)
		}
	}
}

type governanceBindingDocumentGate struct {
	employmentEvidenceGate
	authority *employmentPreparationBindings
}

func (g *governanceBindingDocumentGate) permissions(bool) (authz.Permission, authz.Permission) {
	return PermEmploymentPrepare, authz.Public
}
func (g *governanceBindingDocumentGate) authorize(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, writing bool) error {
	if writing || p.TenantID != g.authority.principal.TenantID || p.Subject != g.authority.principal.Subject || hospital != g.authority.hospital || staff != g.authority.selected.Staff {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return tx.Exec("SELECT set_config('app.workforce_employment_evidence','read',true)").Error
}
func (g *governanceBindingDocumentGate) auditDownload(tx *gorm.DB) error {
	return evidenceAudit(tx, g.scope, "workforce.employment_evidence.downloaded", 0)
}
func registerEmploymentPreparationBindings(r *platform.Router, h *handlers) {
	path := "/:hospital/staff/:id/employment-preparation-periods/:period/revisions/:version/evidence"
	r.PUT(path+"/:binding", authz.Public, h.nativeEmploymentBindingRoute(bindingPreparationAttach))
	r.GET(path+"/:binding", authz.Public, h.nativeEmploymentBindingRoute(bindingPreparationRead))
	r.POST(path+"/:binding/withdraw", authz.Public, h.nativeEmploymentBindingRoute(bindingPreparationWithdraw))
	r.GET(path+"/:binding/content", authz.Public, h.nativeEmploymentBindingRoute(bindingPreparationDownload))
	filters := []platform.QueryFilter{{Key: "staff_version"}, {Key: "employment_epoch"}}
	platform.SealedFilteredQueryListRoute(r, path, authz.Public, "hospital", filters, 20, func(c *gin.Context, page pagination.Params) ([]EmploymentEvidenceBinding, error) {
		scope, _, ok := h.governanceEmploymentBindings(c, bindingPreparationList)
		if !ok {
			return nil, nil
		}
		rows, ok := employmentEvidenceList(c, scope, page)
		if !ok {
			return nil, nil
		}
		return rows, nil
	}, func(_ *gin.Context, rows []EmploymentEvidenceBinding) ([]EmploymentEvidenceBinding, error) {
		return rows, nil
	}, "id", "period", "version")
	platform.SealedFilteredQueryListRoute(r, path+"/:binding/history", authz.Public, "hospital", filters, 20, func(c *gin.Context, page pagination.Params) ([]employmentEvidenceRevision, error) {
		scope, _, ok := h.governanceEmploymentBindings(c, bindingPreparationHistory)
		if !ok {
			return nil, nil
		}
		rows, ok := employmentEvidenceHistory(c, scope, page)
		if !ok {
			return nil, nil
		}
		return rows, nil
	}, func(_ *gin.Context, rows []employmentEvidenceRevision) ([]employmentEvidenceRevision, error) {
		return rows, nil
	}, "id", "period", "version", "binding")
}
