package supply

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"gorm.io/gorm"

	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type operationalAccessLimits struct {
	TenantID        uuid.UUID  `json:"-"`
	PartyID         uuid.UUID  `json:"party_id"`
	MaxGrantHours   int64      `json:"max_grant_hours"`
	MaxSupportHours int64      `json:"max_support_hours"`
	Reason          string     `json:"reason"`
	Version         int64      `json:"version"`
	CreatedAt       *time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt       *time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
}

func (operationalAccessLimits) TableName() string { return "supply_operational_access_limits" }

const accessLimitsColumns = "tenant_id,party_id,max_grant_hours,max_support_hours,reason,version,created_at,updated_at"

func registerAccessLimitsRoutes(g *platform.Router, h *handlers) {
	g.GET("/parties/:party/access-limits", PermAccessManage, h.getAccessLimits)
	g.PUT("/parties/:party/access-limits", PermAccessManage, h.putAccessLimits)
	platform.ListRoute(g, "/parties/:party/access-limits/revisions", PermAccessManage, h.listAccessLimitsRevisions)
}

func accessLimitsAdministrator(tx *gorm.DB, p authn.Principal, partyID uuid.UUID) error {
	if e := requireAccessAdministrator(tx, p, partyID); e != nil {
		return e
	}
	var owner struct{ ID uuid.UUID }
	return tx.Table("supply_parties").Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, partyID).First(&owner).Error
}

func (h *handlers) getAccessLimits(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	row := operationalAccessLimits{TenantID: uuid.MustParse(p.TenantID), PartyID: partyID}
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := accessLimitsAdministrator(tx, p, partyID); e != nil {
			return e
		}
		e := tx.Select(accessLimitsColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).First(&row).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return tx.Raw(`SELECT supply_operational_access_limit(?,?,'grant') AS max_grant_hours,supply_operational_access_limit(?,?,'support') AS max_support_hours`, p.TenantID, partyID, p.TenantID, partyID).Scan(&row).Error
		}
		return e
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) putAccessLimits(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	var in accessLimitsInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(&in.Reason) || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Provide a current version, reason, grant limit of 1–8784 hours and support limit of 1–24 hours.")
		return
	}
	var row operationalAccessLimits
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, partyID); e != nil {
			return e
		}
		if e := accessLimitsAdministrator(tx, p, partyID); e != nil {
			return e
		}
		e := tx.Select(accessLimitsColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).First(&row).Error
		exists := e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		equal := row.MaxGrantHours == in.MaxGrantHours && row.MaxSupportHours == in.MaxSupportHours && row.Reason == in.Reason
		replay, e := registryVersion(exists, *in.ExpectedVersion, row.Version, equal)
		if e != nil || replay {
			return e
		}
		if !exists {
			row.TenantID = uuid.MustParse(p.TenantID)
			row.PartyID = partyID
		}
		row.MaxGrantHours = in.MaxGrantHours
		row.MaxSupportHours = in.MaxSupportHours
		row.Reason = in.Reason
		row.Version++
		if exists {
			e = tx.Model(&operationalAccessLimits{}).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Select("max_grant_hours", "max_support_hours", "reason", "version").Updates(&row).Error
		} else {
			e = tx.Create(&row).Error
		}
		if e != nil {
			return e
		}
		if e := tx.Select(accessLimitsColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).First(&row).Error; e != nil {
			return e
		}
		return h.recordAccessLimits(tx, p, row)
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) recordAccessLimits(tx *gorm.DB, p authn.Principal, row operationalAccessLimits) error {
	snapshot, e := json.Marshal(row)
	if e != nil {
		return e
	}
	id, e := uuid.NewV7()
	if e != nil {
		return e
	}
	if e := tx.Exec(`INSERT INTO supply_operational_access_limit_revisions(id,tenant_id,party_id,version,snapshot,actor_subject) VALUES(?,?,?,?,?,?)`, id, p.TenantID, row.PartyID, row.Version, string(snapshot), p.Subject).Error; e != nil {
		return e
	}
	data, e := json.Marshal(supplycontract.OperationalAccessLimitsUpdatedData{PartyID: row.PartyID.String(), Version: row.Version})
	if e != nil {
		return e
	}
	return h.bus.Publish(tx, supplycontract.SubjectOperationalAccessLimitsUpdated, events.Event{Type: supplycontract.SubjectOperationalAccessLimitsUpdated, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) listAccessLimitsRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := accessLimitsAdministrator(tx, p, partyID); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_operational_access_limit_revisions").Select("id,party_id,party_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error
	})
	if e != nil {
		supplierError(c, e)
	}
	return rows, nil
}
