package pharmacy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type hospitalStocktakeDepartmentRow struct {
	choice    HospitalStocktakeDepartmentChoice
	createdAt time.Time
}

func (r hospitalStocktakeDepartmentRow) MarshalJSON() ([]byte, error) { return json.Marshal(r.choice) }
func (r hospitalStocktakeDepartmentRow) PageKey() (time.Time, uuid.UUID) {
	return r.createdAt, r.choice.DepartmentID
}

func (h *catalogueHandlers) scanHospitalStocktakeDepartments(c *gin.Context, page pagination.Params) ([]hospitalStocktakeDepartmentRow, error) {
	p, ok := hospitalStocktakeChoicePrincipal(c)
	if !ok {
		return nil, nil
	}
	reader, ok := h.stocktakeReferences.(authz.StocktakeDepartmentReader)
	if !ok {
		stocktakeOperatingError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(tenantdb.WithActor(c.Request.Context(), p.Subject), 4*time.Second)
	defer cancel()
	rows := []hospitalStocktakeDepartmentRow{}
	err := h.withHospitalStocktakeActor(ctx, p, c.Param("hospital"), c.Param("department"), PermStocktakeRead, func(held context.Context, source *hospitalStocktakeUse) error {
		choices, err := reader.StocktakeDepartments(held, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: source.hospital, DepartmentPath: source.path}, page)
		if err != nil {
			return err
		}
		for _, choice := range choices {
			rows = append(rows, hospitalStocktakeDepartmentRow{createdAt: choice.CreatedAt, choice: HospitalStocktakeDepartmentChoice{OwnerTenantID: uuid.MustParse(p.TenantID), HospitalID: source.hospital, ScopeDepartmentID: source.department, DepartmentID: choice.ID, DepartmentRevisionID: choice.RevisionID, DepartmentName: choice.Name, DepartmentCode: choice.Code}})
		}
		return nil
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return nil, nil
	}
	return rows, nil
}
