package supply

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

var operatingReaders = []string{"administrator", "warehouse", "inventory", "picker", "packer", "dispatcher", "driver", "auditor", "regulator"}

var operatingBinReaders = []string{"administrator", "warehouse", "inventory", "picker", "packer", "dispatcher", "auditor", "regulator"}

type operatingLocation struct {
	ID             uuid.UUID `json:"id"`
	PartyID        uuid.UUID `json:"party_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	CountryCode    string    `json:"country_code"`
	TimeZone       string    `json:"time_zone"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	AllowedActions []string  `json:"allowed_actions" gorm:"serializer:json;type:jsonb"`
}

func (r operatingLocation) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const operatingColumns = `id,party_id,code,name,kind,country_code,time_zone,status,created_at,
CASE WHEN status='inactive' THEN CASE WHEN supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','auditor']) AND supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','auditor']) THEN jsonb_build_array('read_deliveries') ELSE '[]'::jsonb END ELSE
(CASE WHEN supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','auditor','regulator']) THEN jsonb_build_array('read_bins','read_bin_history') ELSE '[]'::jsonb END
|| CASE WHEN supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','inventory']) THEN jsonb_build_array('manage_bins') ELSE '[]'::jsonb END
|| CASE WHEN supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','dispatcher']) THEN jsonb_build_array('manage_deliveries') ELSE '[]'::jsonb END
|| CASE WHEN supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','dispatcher','auditor','regulator']) THEN jsonb_build_array('read_deliveries') ELSE '[]'::jsonb END
|| CASE WHEN supply_operating_owned(tenant_id,party_id,id,ARRAY['driver']) THEN jsonb_build_array('read_assigned_deliveries') ELSE '[]'::jsonb END) END AS allowed_actions`

func registerOperatingRoutes(g *platform.Router, h *handlers) {
	platform.ListRoute(g, "/parties/:party/operating-locations", PermOperatingRead, h.listOperatingLocations)
	g.GET("/parties/:party/operating-locations/:premise", PermOperatingRead, h.getOperatingLocation)
	platform.ListRoute(g, "/parties/:party/operating-locations/:premise/bins", PermOperatingRead, h.listOperatingBins)
	g.GET("/parties/:party/operating-locations/:premise/bins/:bin", PermOperatingRead, h.getOperatingBin)
	platform.ListRoute(g, "/parties/:party/operating-locations/:premise/bins/:bin/revisions", PermOperatingRead, h.listOperatingBinRevisions)
}

func requireOperating(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) error {
	allowed, err := operatingAllowed(tx, p, partyID, premiseID, roles)
	if err != nil {
		return err
	}
	if !allowed {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func operatingAllowed(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (bool, error) {
	data, err := json.Marshal(roles)
	if err != nil {
		return false, err
	}
	var allowed bool
	if err = tx.Raw(`SELECT supply_operating_owned(?,?,?,ARRAY(SELECT jsonb_array_elements_text(?::jsonb)))`, p.TenantID, partyID, premiseID, string(data)).Scan(&allowed).Error; err != nil {
		return false, err
	}
	return allowed, nil
}

func operatingParent(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (premise, error) {
	var parent premise
	if err := requireOperating(tx, p, partyID, premiseID, roles); err != nil {
		return parent, err
	}
	err := tx.Select("id,party_id,country_code,kind,status").Where("tenant_id=? AND party_id=? AND id=? AND kind IN ? AND status='active'", p.TenantID, partyID, premiseID, []string{"warehouse", "depot"}).First(&parent).Error
	return parent, err
}

func operatingHistoryParent(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (premise, error) {
	parent, err := operatingParent(tx, p, partyID, premiseID, roles)
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return parent, err
	}
	archiveRoles := []string{"administrator", "auditor"}
	if err := requireRoles(tx, p, partyID, archiveRoles); err != nil {
		return premise{}, err
	}
	if err := requireOperating(tx, p, partyID, premiseID, archiveRoles); err != nil {
		return premise{}, err
	}
	err = tx.Select("id,party_id,country_code,kind,status").Where("tenant_id=? AND party_id=? AND id=? AND kind IN ? AND status='inactive'", p.TenantID, partyID, premiseID, []string{"warehouse", "depot"}).First(&parent).Error
	return parent, err
}

func (h *handlers) listOperatingLocations(c *gin.Context, params pagination.Params) ([]operatingLocation, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []operatingLocation
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Table("supply_premises").Select(operatingColumns).Where("tenant_id=? AND party_id=? AND kind IN ? AND (status='active' OR (status='inactive' AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','auditor']))) AND supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator'])", p.TenantID, partyID, []string{"warehouse", "depot"}).Find(&rows).Error
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}

func (h *handlers) getOperatingLocation(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	var saved operatingLocation
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingHistoryParent(tx, p, partyID, premiseID, operatingReaders); err != nil {
			return err
		}
		return tx.Table("supply_premises").Select(operatingColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, premiseID).First(&saved).Error
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listOperatingBins(c *gin.Context, params pagination.Params) ([]storageBin, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	var rows []storageBin
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, operatingBinReaders); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=?", p.TenantID, partyID, premiseID).Find(&rows).Error
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}

func (h *handlers) getOperatingBin(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return
	}
	var saved storageBin
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, operatingBinReaders); err != nil {
			return err
		}
		return tx.Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listOperatingBinRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, err := operatingParent(tx, p, partyID, premiseID, operatingBinReaders); err != nil {
			return err
		}
		var head storageBin
		if err := tx.Select("id").Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&head).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_bin_revisions").Select("id,party_id,bin_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND premise_id=? AND bin_id=?", p.TenantID, partyID, premiseID, id).Find(&rows).Error
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}
