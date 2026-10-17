package supply

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type accessGrant struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"-"`
	PartyID         uuid.UUID  `json:"party_id"`
	Subject         string     `json:"subject"`
	Role            string     `json:"role"`
	PremiseID       *uuid.UUID `json:"premise_id"`
	ValidFrom       time.Time  `json:"valid_from"`
	ExpiresAt       time.Time  `json:"expires_at"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	EffectiveStatus string     `json:"effective_status" gorm:"-"`
}

func (accessGrant) TableName() string                 { return "supply_access_grants" }
func (r accessGrant) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const accessGrantColumns = "id,tenant_id,party_id,subject,role,premise_id,valid_from,expires_at,reason,status,version,created_at,updated_at"

var errPermanentAdministrator = errors.New("permanent organisation administrator required")

func registerAccessRoutes(g *platform.Router, h *handlers) {
	g.PUT("/parties/:party/grants/:grant", PermAccessManage, h.putAccessGrant)
	g.GET("/parties/:party/grants/:grant", PermAccessManage, h.getAccessGrant)
	platform.ListRoute(g, "/parties/:party/grants", PermAccessManage, h.listAccessGrants)
	platform.ListRoute(g, "/parties/:party/grants/:grant/revisions", PermAccessManage, h.listAccessGrantRevisions)
	platform.ListRoute(g, "/parties/:party/access/grants", PermAccessManage, h.listLegacyGrants)
}
func lockAccessAdministration(tx *gorm.DB, p authn.Principal, partyID uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-access-admin:"+partyID.String()).Error
}
func requireAccessAdministrator(tx *gorm.DB, p authn.Principal, partyID uuid.UUID) error {
	if e := requireRoles(tx, p, partyID, []string{"administrator"}); e != nil {
		return e
	}
	return tx.Exec(`SELECT set_config('app.supply_access_admin','on',true)`).Error
}

func retainAdministrator(tx *gorm.DB, p authn.Principal, partyID uuid.UUID, legacySubject string) error {
	var remaining int64
	e := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND role='administrator' AND subject<>?", p.TenantID, partyID, legacySubject).Count(&remaining).Error
	if e != nil {
		return e
	}
	if remaining == 0 {
		return errPermanentAdministrator
	}
	return nil
}
func boundDelegatedAccess(tx *gorm.DB, p authn.Principal, partyID uuid.UUID, expires time.Time) error {
	var permanent int64
	if e := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND subject=? AND role='administrator'", p.TenantID, partyID, p.Subject).Count(&permanent).Error; e != nil {
		return e
	}
	if permanent > 0 {
		return nil
	}
	var horizon struct{ ExpiresAt *time.Time }
	if e := tx.Table("supply_access_grants").Select("max(expires_at) AS expires_at").Where("tenant_id=? AND party_id=? AND subject=? AND role='administrator' AND premise_id IS NULL AND status='active' AND valid_from<=clock_timestamp() AND expires_at>clock_timestamp()", p.TenantID, partyID, p.Subject).Scan(&horizon).Error; e != nil {
		return e
	}
	if horizon.ExpiresAt == nil || expires.After(*horizon.ExpiresAt) {
		return errConflict
	}
	return nil
}

func effectiveGrant(r *accessGrant) {
	now := time.Now()
	r.EffectiveStatus = r.Status
	if r.Status == "active" {
		if !r.ExpiresAt.After(now) {
			r.EffectiveStatus = "expired"
		} else if r.ValidFrom.After(now) {
			r.EffectiveStatus = "scheduled"
		}
	}
}
func validAccessRole(role string) bool {
	if validPartyRole(role) {
		return true
	}
	switch role {
	case "inventory", "picker", "packer", "dispatcher", "regulator":
		return true
	}
	return false
}

func grantFromInput(in accessGrantInput) (accessGrant, bool) {
	start, e := time.Parse(time.RFC3339, in.ValidFrom)
	if e != nil {
		return accessGrant{}, false
	}
	end, e := time.Parse(time.RFC3339, in.ExpiresAt)
	if e != nil {
		return accessGrant{}, false
	}
	valid := in.ExpectedVersion != nil && *in.ExpectedVersion >= 0 && *in.ExpectedVersion < 9007199254740991 && subjectID.MatchString(in.Subject) && validAccessRole(in.Role) && cleanText(&in.Reason) && len(in.Reason) >= 5 && len(in.Reason) <= 500 && (in.Status == "active" || in.Status == "revoked") && end.After(start) && end.Sub(start) <= 366*24*time.Hour && validaccessGrantInputScope(in) && (in.PremiseID == nil || (in.PremiseID.Version() == 7 && in.PremiseID.Variant() == uuid.RFC4122))
	return accessGrant{Subject: in.Subject, Role: in.Role, PremiseID: in.PremiseID, ValidFrom: start.UTC(), ExpiresAt: end.UTC(), Reason: in.Reason, Status: in.Status}, valid
}
func sameAccessGrant(a, b accessGrant) bool {
	return a.Subject == b.Subject && a.Role == b.Role && reflect.DeepEqual(a.PremiseID, b.PremiseID) && a.ValidFrom.Equal(b.ValidFrom) && a.ExpiresAt.Equal(b.ExpiresAt) && a.Reason == b.Reason && a.Status == b.Status
}
func (h *handlers) putAccessGrant(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "grant")
	if !ok {
		return
	}
	var in accessGrantInput
	if !decode(c, &in) {
		return
	}
	desired, valid := grantFromInput(in)
	if !valid || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Provide a valid role, identity, reason and bounded access period.")
		return
	}
	if desired.Status == "active" {
		if err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error { return requireAccessAdministrator(tx, p, partyID) }); err != nil {
			supplierError(c, err)
			return
		}
		if !h.verifyAccessMember(c, p, desired.Subject) {
			return
		}
	}

	var saved accessGrant
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, partyID); e != nil {
			return e
		}
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		e := tx.Select(accessGrantColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
		exists := e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if exists && sameAccessGrant(saved, desired) && (*in.ExpectedVersion == saved.Version || *in.ExpectedVersion == saved.Version-1) {
			return nil
		}
		if !exists && *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		if exists && (*in.ExpectedVersion != saved.Version || saved.Status == "revoked") {
			return errConflict
		}
		if desired.Status == "active" {
			if e := boundDelegatedAccess(tx, p, partyID, desired.ExpiresAt); e != nil {
				return e
			}
			if !desired.ExpiresAt.After(time.Now()) {
				return errConflict
			}
		}
		if !exists && desired.Status != "active" {
			return errConflict
		}
		if exists && (saved.Subject != desired.Subject || saved.Role != desired.Role || !reflect.DeepEqual(saved.PremiseID, desired.PremiseID) || !saved.ValidFrom.Equal(desired.ValidFrom)) {
			return errConflict
		}
		if exists && saved.Role == "administrator" && desired.Status == "revoked" {
			if e := retainAdministrator(tx, p, partyID, ""); e != nil {
				return e
			}
		}
		var partyRow struct{ ID uuid.UUID }
		if e := tx.Table("supply_parties").Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, partyID).First(&partyRow).Error; e != nil {
			return e
		}
		if desired.Status == "active" && desired.PremiseID != nil {
			var location premise
			if e := tx.Select("id,kind").Where("tenant_id=? AND party_id=? AND id=? AND status='active'", p.TenantID, partyID, *desired.PremiseID).First(&location).Error; e != nil {
				return e
			}
			if !validaccessGrantInputPremiseKind(location.Kind) {
				return errAccessScope
			}
		}
		if !exists && desired.PremiseID == nil {
			var n int64
			if e := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND subject=? AND role=?", p.TenantID, partyID, desired.Subject, desired.Role).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return errConflict
			}
		}
		if !exists {
			desired.ID = id
			desired.PartyID = partyID
			desired.TenantID = uuid.MustParse(p.TenantID)
			desired.Version = 1
			if e := tx.Create(&desired).Error; e != nil {
				return e
			}
		} else {
			updates := map[string]any{"expires_at": desired.ExpiresAt, "reason": desired.Reason, "status": desired.Status, "version": saved.Version + 1}
			if e := tx.Model(&accessGrant{}).Where("tenant_id=? AND party_id=? AND id=? AND version=?", p.TenantID, partyID, id, saved.Version).Updates(updates).Error; e != nil {
				return e
			}
		}
		if e := tx.Select(accessGrantColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; e != nil {
			return e
		}
		return h.recordAccessGrant(tx, p, saved)
	})
	if err != nil {
		if errors.Is(err, errAccessScope) {
			respond.Error(c, 400, "invalid_request", "Location access requires an active warehouse or depot.")
			return
		}
		supplierError(c, err)
		return
	}
	effectiveGrant(&saved)
	respond.OK(c, saved)
}

func (h *handlers) recordAccessGrant(tx *gorm.DB, p authn.Principal, saved accessGrant) error {
	effectiveGrant(&saved)
	data, e := json.Marshal(saved)
	if e != nil {
		return e
	}
	revision, e := uuid.NewV7()
	if e != nil {
		return e
	}
	row := map[string]any{"tenant_id": p.TenantID, "party_id": saved.PartyID, "grant_id": saved.ID, "id": revision, "version": saved.Version, "snapshot": string(data), "actor_subject": p.Subject}
	if e := tx.Table("supply_access_grant_revisions").Create(row).Error; e != nil {
		return e
	}
	operation := "granted"
	if saved.Version > 1 {
		operation = "updated"
	}
	if saved.Status == "revoked" {
		operation = "revoked"
	}
	return h.publishAccess(tx, p, partyGrant{ID: saved.ID, PartyID: saved.PartyID}, operation)
}
func (h *handlers) getAccessGrant(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "grant")
	if !ok {
		return
	}
	var row accessGrant
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		return tx.Select(accessGrantColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&row).Error
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	effectiveGrant(&row)
	respond.OK(c, row)
}
func (h *handlers) listAccessGrants(c *gin.Context, params pagination.Params) ([]accessGrant, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []accessGrant
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Select(accessGrantColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error
	})
	if e != nil {
		supplierError(c, e)
	}
	for i := range rows {
		effectiveGrant(&rows[i])
	}
	return rows, nil
}
func (h *handlers) listLegacyGrants(c *gin.Context, params pagination.Params) ([]partyGrant, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []partyGrant
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Select(grantColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error
	})
	if e != nil {
		supplierError(c, e)
	}
	return rows, nil
}
func (h *handlers) listAccessGrantRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "grant")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		var row accessGrant
		if e := tx.Select("id").Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&row).Error; e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_access_grant_revisions").Select("id,party_id,grant_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND grant_id=?", p.TenantID, partyID, id).Find(&rows).Error
	})
	if e != nil {
		supplierError(c, e)
	}
	return rows, nil
}
