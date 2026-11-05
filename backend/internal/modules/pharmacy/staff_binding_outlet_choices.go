package pharmacy

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

type staffBindingOutletChoiceRow struct {
	StaffBindingOutletChoice
	CreatedAt time.Time `json:"-"`
}

func (r staffBindingOutletChoiceRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.OutletID }

func (h *staffDepartmentBindingHandlers) outletChoices(c *gin.Context, page pagination.Params) ([]staffBindingOutletChoiceRow, error) {
	s, ok := bindingHospitalScope(c)
	if !ok {
		return nil, nil
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.BadRequest(c, errors.New("invalid outlet choice query"))
		return nil, nil
	}
	for key, values := range query {
		if (key != "purpose" && key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, errors.New("choose one outlet purpose, limit and optional cursor"))
			return nil, nil
		}
	}
	purpose := c.DefaultQuery("purpose", "read")
	permission := PermStaffBindingRead
	switch purpose {
	case "read":
	case "prepare":
		permission = PermStaffBindingPrepare
	case "review":
		permission = PermStaffBindingReview
	default:
		respond.BadRequest(c, errors.New("choose a relationship read, preparation or review purpose"))
		return nil, nil
	}
	required := []authz.Permission{PermStaffBindingRead}
	if permission != PermStaffBindingRead {
		required = append(required, permission)
	}
	required = append(required, authz.StaffActorUse)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	rows := []staffBindingOutletChoiceRow{}
	err = h.withActor(c, s, required, func(actorCtx context.Context, actor authz.StaffActorReference) error {
		return h.db.WithTenantCommand(actorCtx, s.principal.TenantID, func(tx *gorm.DB) error {
			query := tx.Table("pharmacy_outlets o").Select("o.pharmacy_id,o.id AS outlet_id,p.name AS pharmacy_name,o.name AS outlet_name,o.created_at").
				Joins("JOIN pharmacy_organizations p ON p.tenant_id=o.tenant_id AND p.id=o.pharmacy_id").
				Joins("JOIN pharmacy_outlet_access a ON a.tenant_id=o.tenant_id AND a.outlet_id=o.id").
				Where("o.tenant_id=? AND a.subject=? AND p.ownership_type='hospital_owned'", s.principal.TenantID, s.principal.Subject)
			if permission != PermStaffBindingRead {
				query = query.Where("a.level='editor'")
			}
			if page.After != nil {
				query = query.Where("(o.created_at,o.id)<(?,?)", page.After.CreatedAt, page.After.ID)
			}
			if err := query.Order("o.created_at DESC,o.id DESC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
				return err
			}
			ordered := slices.Clone(rows)
			slices.SortFunc(ordered, func(a, b staffBindingOutletChoiceRow) int {
				return strings.Compare(a.OutletID.String(), b.OutletID.String())
			})
			var at time.Time
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&at).Error; err != nil {
				return err
			}
			for _, row := range ordered {
				scoped := s
				scoped.pharmacy, scoped.outlet = row.PharmacyID, row.OutletID
				if err := h.outlet(tx, scoped, permission != PermStaffBindingRead); err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return authz.ErrScopedCommandDenied
					}
					return err
				}
				if err := setStaffBindingContext(tx, scoped, PermStaffBindingRead, actor, false); err != nil {
					return err
				}
				if err := auditStaffBinding(tx, scoped, actor, "read", nil, at, map[string]any{"projection": "staff_binding_outlet_choice", "purpose": purpose}); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		if errors.Is(err, errStaffBindingEvidenceUnavailable) {
			respond.Error(c, 503, "binding_evidence_unavailable", "Outlet choices could not be recorded. Refresh the workspace.")
		} else {
			staffBindingError(c, err)
		}
		return nil, nil
	}
	return rows, nil
}
