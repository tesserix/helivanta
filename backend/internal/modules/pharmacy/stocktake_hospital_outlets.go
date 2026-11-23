package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type hospitalStocktakeOutletRow struct {
	BindingID            uuid.UUID
	BindingVersion       int64
	OutletID             uuid.UUID
	DepartmentRevisionID uuid.UUID
	CreatedAt            time.Time
	Deadline             time.Time                     `gorm:"-"`
	Choice               HospitalStocktakeOutletChoice `gorm:"-"`
}

func (r hospitalStocktakeOutletRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Choice) }
func (r hospitalStocktakeOutletRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.BindingID }

func hospitalStocktakeChoicePrincipal(c *gin.Context) (authn.Principal, bool) {
	if !authn.Refresh(c) {
		return authn.Principal{}, false
	}
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return p, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your active account changed. Reload before choosing an outlet.")
		return p, false
	}
	hospital, hospitalErr := uuid.Parse(c.Param("hospital"))
	department, departmentOK := operatingStockID(c.Param("department"))
	if hospitalErr != nil || hospital == uuid.Nil || hospital.Variant() != uuid.RFC4122 || hospital.String() != c.Param("hospital") || !departmentOK || department.String() != c.Param("department") {
		respond.NotFound(c, "stocktake department")
		return p, false
	}
	return p, stocktakeRead(c, true)
}

func stocktakeHospitalChoicesIntact(tx *gorm.DB) error {
	var intact bool
	if err := tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()AND pharmacy_stocktake_hospital_choice_integrity()`).Scan(&intact).Error; err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if !intact {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return nil
}

func hospitalStocktakeChoicesFrame(tx *gorm.DB, p authn.Principal, source *hospitalStocktakeUse, outlet, pharmacy uuid.UUID, action func() error) (err error) {
	proof, err := json.Marshal(map[string]any{"purpose": "hospital_stocktake_outlet_choices", "permission": string(PermStocktakeRead), "tenant_id": p.TenantID, "hospital_id": source.hospital, "department_id": source.department, "actor_subject": p.Subject, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline, "actor": source.actor, "operation": source.operation, "source": source.source, "outlet_id": outlet, "pharmacy_id": pharmacy})
	if err != nil {
		return err
	}
	var previous string
	if err = tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')`).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices',?,true)`, previous).Error)
	}()
	if err = tx.Exec(`SELECT set_config('app.pharmacy_stocktake_choices',?,true)`, string(proof)).Error; err != nil {
		return err
	}
	return action()
}

func (h *catalogueHandlers) scanHospitalStocktakeOutlets(c *gin.Context, page pagination.Params) ([]hospitalStocktakeOutletRow, error) {
	p, ok := hospitalStocktakeChoicePrincipal(c)
	if !ok {
		return nil, nil
	}
	deadline := time.Now().Add(4 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), deadline)
	defer cancel()
	rows := []hospitalStocktakeOutletRow{}
	err := h.withHospitalStocktakeActor(ctx, p, c.Param("hospital"), c.Param("department"), PermStocktakeRead, func(actorCtx context.Context, source *hospitalStocktakeUse) error {
		return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
			if err := stocktakeHospitalChoicesIntact(tx); err != nil {
				return err
			}
			if err := hospitalStocktakeChoicesFrame(tx, p, source, uuid.Nil, uuid.Nil, func() error {
				query := tx.Table("pharmacy_staff_department_bindings").Select("id AS binding_id,version AS binding_version,outlet_id,department_revision_id,created_at").Where("tenant_id=? AND hospital_id=? AND department_id=? AND status='approved'", p.TenantID, source.hospital, source.department)
				if page.After != nil {
					query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
				}
				return query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Scan(&rows).Error
			}); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'pharmacy.stocktake_outlet_choices.scanned','hospital',?)`, p.TenantID, p.Subject, source.hospital).Error
		})
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].Deadline = deadline
	}
	return rows, nil
}

func (h *catalogueHandlers) filterHospitalStocktakeOutlets(c *gin.Context, candidates []hospitalStocktakeOutletRow) ([]hospitalStocktakeOutletRow, error) {
	rows := make([]hospitalStocktakeOutletRow, 0, len(candidates))
	if len(candidates) == 0 {
		return rows, nil
	}
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), candidates[0].Deadline)
	defer cancel()
	for _, candidate := range candidates {
		err := h.withHospitalStocktakeActor(ctx, p, c.Param("hospital"), c.Param("department"), PermStocktakeRead, func(actorCtx context.Context, source *hospitalStocktakeUse) error {
			return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
				if err := stocktakeHospitalChoicesIntact(tx); err != nil {
					return err
				}
				if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":pharmacy-staff-bindings:"+candidate.OutletID.String()).Error; err != nil {
					return err
				}
				var outlet struct {
					PharmacyID                        uuid.UUID
					Name, Code, CountryCode, Timezone string
				}
				var pharmacy struct{ Name string }
				if err := hospitalStocktakeChoicesFrame(tx, p, source, candidate.OutletID, uuid.Nil, func() error {
					var binding struct{ ID uuid.UUID }
					if err := tx.Table("pharmacy_staff_department_bindings").Select("id").Where("tenant_id=? AND hospital_id=? AND department_id=? AND id=? AND outlet_id=? AND status='approved' AND version=? AND department_revision_id=?", p.TenantID, source.hospital, source.department, candidate.BindingID, candidate.OutletID, candidate.BindingVersion, candidate.DepartmentRevisionID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&binding).Error; err != nil {
						return err
					}
					return tx.Table("pharmacy_outlets").Select("pharmacy_id,name,code,country_code,timezone").Where("tenant_id=? AND id=? AND country_code='IN'", p.TenantID, candidate.OutletID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&outlet).Error
				}); err != nil {
					return err
				}
				if err := hospitalStocktakeChoicesFrame(tx, p, source, candidate.OutletID, outlet.PharmacyID, func() error {
					return tx.Table("pharmacy_organizations").Select("name").Where("tenant_id=? AND id=? AND ownership_type='hospital_owned'", p.TenantID, outlet.PharmacyID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&pharmacy).Error
				}); err != nil {
					return err
				}
				for value, maxSize := range map[string]int{pharmacy.Name: 200, outlet.Name: 200, outlet.Code: 40} {
					if value == "" || strings.TrimSpace(value) != value || utf8.RuneCountInString(value) > maxSize || strings.ContainsRune(value, 0) {
						return authz.ErrScopedAuthorizationUnavailable
					}
				}
				if _, err := time.LoadLocation(outlet.Timezone); err != nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				return h.stocktakeReferences.WithCurrentDepartment(actorCtx, authz.DepartmentReference{TenantID: p.TenantID, HospitalID: source.hospital, DepartmentID: source.department, DepartmentRevision: candidate.DepartmentRevisionID}, func(context.Context) error {
					if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'pharmacy.stocktake_outlet_choices.read','pharmacy_staff_department_bindings',?)`, p.TenantID, p.Subject, candidate.BindingID).Error; err != nil {
						return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
					}
					candidate.Choice = HospitalStocktakeOutletChoice{OwnerTenantID: uuid.MustParse(p.TenantID), HospitalID: source.hospital, DepartmentID: source.department, DepartmentRevisionID: candidate.DepartmentRevisionID, PharmacyID: outlet.PharmacyID, OutletID: candidate.OutletID, BindingID: candidate.BindingID, BindingVersion: candidate.BindingVersion, PharmacyName: pharmacy.Name, OutletName: outlet.Name, OutletCode: outlet.Code, TimeZone: outlet.Timezone, CountryCode: outlet.CountryCode}
					return nil
				})
			})
		})
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, authz.ErrDepartmentReferenceChanged) {
			continue
		}
		if err != nil {
			stocktakeOperatingError(c, err)
			return nil, nil
		}
		rows = append(rows, candidate)
	}
	return rows, nil
}
