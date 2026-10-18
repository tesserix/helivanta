package pharmacy

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type pharmacyOrganization struct {
	ID                 uuid.UUID `json:"id"`
	TenantID           uuid.UUID `json:"-"`
	OrganizationFields `gorm:"embedded"`
	OnboardingStatus   string    `json:"onboarding_status"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (pharmacyOrganization) TableName() string                 { return "pharmacy_organizations" }
func (r pharmacyOrganization) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type pharmacyOutlet struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"-"`
	PharmacyID       uuid.UUID `json:"pharmacy_id"`
	OutletFields     `gorm:"embedded"`
	RegulatoryStatus string    `json:"regulatory_status"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (pharmacyOutlet) TableName() string                 { return "pharmacy_outlets" }
func (r pharmacyOutlet) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type hospitalAffiliation struct {
	ID                 uuid.UUID `json:"id"`
	TenantID           uuid.UUID `json:"-"`
	PharmacyID         uuid.UUID `json:"pharmacy_id"`
	AffiliationFields  `gorm:"embedded"`
	VerificationStatus string    `json:"verification_status"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (hospitalAffiliation) TableName() string                 { return "pharmacy_hospital_affiliations" }
func (r hospitalAffiliation) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const organizationColumns = "id,tenant_id,code,name,ownership_type,onboarding_status,created_at"
const outletColumns = "id,tenant_id,pharmacy_id,code,name,country_code,timezone,regulatory_status,created_at"
const affiliationColumns = "id,tenant_id,pharmacy_id,hospital_id,relationship,verification_status,created_at"

func registryRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB, bus: deps.Bus}
	g.GET("/configuration", PermCatalogueRead, requirePharmacyTenant, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		respond.OK(c, countryprofile.Configuration())
	})
	platform.ListRoute(g, "/pharmacies", PermCatalogueRead, h.listOrganizations)
	g.PUT("/pharmacies/:pharmacy", PermAccessManage, h.putOrganization)
	platform.ListRoute(g, "/pharmacies/:pharmacy/outlets", PermCatalogueRead, h.listOutlets)
	g.PUT("/pharmacies/:pharmacy/outlets/:outlet", PermAccessManage, h.putOutlet)
	platform.ListRoute(g, "/pharmacies/:pharmacy/hospital-affiliations", PermCatalogueRead, h.listAffiliations)
	g.PUT("/pharmacies/:pharmacy/hospital-affiliations/:id", PermAccessManage, h.putAffiliation)
}

func registryID(c *gin.Context, param string) (uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param(param))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, param)
		return uuid.Nil, false
	}
	return id, true
}
func registryManager(c *gin.Context) bool {
	permissions, ok := authz.PermissionsFrom(c)
	return ok && permissions.Has(PermAccessManage)
}
func normalizeRegistry(code, name *string) bool {
	*code = strings.ToUpper(strings.TrimSpace(*code))
	*name = strings.TrimSpace(*name)
	return skuPattern.MatchString(*code) && *name != ""
}

func pharmacyVisible(tx *gorm.DB, p authn.Principal, id uuid.UUID, manager bool) error {
	var n int64
	err := tx.Table("pharmacy_organizations").Where(`tenant_id=? AND id=? AND (? OR EXISTS (
 SELECT 1 FROM pharmacy_outlets o JOIN pharmacy_outlet_access a ON a.tenant_id=o.tenant_id AND a.outlet_id=o.id
 WHERE o.tenant_id=pharmacy_organizations.tenant_id AND o.pharmacy_id=pharmacy_organizations.id AND a.subject=?))`, p.TenantID, id, manager, p.Subject).Count(&n).Error
	if err != nil {
		return err
	}
	if n != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (h *catalogueHandlers) listOrganizations(c *gin.Context, params pagination.Params) ([]pharmacyOrganization, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []pharmacyOrganization
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(organizationColumns).Where(`tenant_id=? AND (? OR EXISTS (
  SELECT 1 FROM pharmacy_outlets o JOIN pharmacy_outlet_access a ON a.tenant_id=o.tenant_id AND a.outlet_id=o.id
  WHERE o.tenant_id=pharmacy_organizations.tenant_id AND o.pharmacy_id=pharmacy_organizations.id AND a.subject=?))`, p.TenantID, registryManager(c), p.Subject).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
func (h *catalogueHandlers) putOrganization(c *gin.Context) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	id, ok := registryID(c, "pharmacy")
	if !ok {
		return
	}
	var in OrganizationFields
	if !decodeCatalogue(c, &in) {
		return
	}
	if !normalizeRegistry(&in.Code, &in.Name) {
		respond.Error(c, 400, "invalid_request", "invalid pharmacy identity")
		return
	}
	var result pharmacyOrganization
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":pharmacy:"+id.String()); err != nil {
			return err
		}
		err := tx.Select(organizationColumns).Where("tenant_id=? AND id=?", p.TenantID, id).First(&result).Error
		if err == nil {
			if result.OrganizationFields != in {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = pharmacyOrganization{ID: id, TenantID: uuid.MustParse(p.TenantID), OrganizationFields: in, OnboardingStatus: "draft"}
		return tx.Create(&result).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *catalogueHandlers) listOutlets(c *gin.Context, params pagination.Params) ([]pharmacyOutlet, error) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	id, ok := registryID(c, "pharmacy")
	if !ok {
		return nil, nil
	}
	var rows []pharmacyOutlet
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, id, registryManager(c)); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(outletColumns).Where(`tenant_id=? AND pharmacy_id=? AND (? OR EXISTS (SELECT 1 FROM pharmacy_outlet_access a WHERE a.tenant_id=pharmacy_outlets.tenant_id AND a.outlet_id=pharmacy_outlets.id AND a.subject=?))`, p.TenantID, id, registryManager(c), p.Subject).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
func (h *catalogueHandlers) putOutlet(c *gin.Context) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	id, ok := registryID(c, "outlet")
	if !ok {
		return
	}
	pharmacy, ok := registryID(c, "pharmacy")
	if !ok {
		return
	}
	var in OutletFields
	if !decodeCatalogue(c, &in) {
		return
	}
	if !normalizeRegistry(&in.Code, &in.Name) || !countryprofile.ValidTimezone(in.CountryCode, in.Timezone) {
		respond.Error(c, 400, "invalid_request", "invalid outlet identity or timezone")
		return
	}
	var result pharmacyOutlet
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":outlet:"+id.String()); err != nil {
			return err
		}
		err := tx.Select(outletColumns).Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, pharmacy, id).First(&result).Error
		if err == nil {
			if result.OutletFields != in {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = pharmacyOutlet{ID: id, TenantID: uuid.MustParse(p.TenantID), PharmacyID: pharmacy, OutletFields: in, RegulatoryStatus: "pending_review"}
		return tx.Create(&result).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *catalogueHandlers) listAffiliations(c *gin.Context, params pagination.Params) ([]hospitalAffiliation, error) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	id, ok := registryID(c, "pharmacy")
	if !ok {
		return nil, nil
	}
	var rows []hospitalAffiliation
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, id, registryManager(c)); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(affiliationColumns).Where("tenant_id=? AND pharmacy_id=?", p.TenantID, id).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
func (h *catalogueHandlers) putAffiliation(c *gin.Context) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	id, ok := registryID(c, "id")
	if !ok {
		return
	}
	pharmacy, ok := registryID(c, "pharmacy")
	if !ok {
		return
	}
	var in AffiliationFields
	if !decodeCatalogue(c, &in) {
		return
	}
	var result hospitalAffiliation
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":affiliation:"+id.String()); err != nil {
			return err
		}
		err := tx.Select(affiliationColumns).Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, pharmacy, id).First(&result).Error
		if err == nil {
			if !reflect.DeepEqual(result.AffiliationFields, in) {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = hospitalAffiliation{ID: id, TenantID: uuid.MustParse(p.TenantID), PharmacyID: pharmacy, AffiliationFields: in, VerificationStatus: "pending_validation"}
		return tx.Create(&result).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}
