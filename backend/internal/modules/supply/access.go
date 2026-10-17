package supply

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

type partyGrant struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"-"`
	PartyID   uuid.UUID `json:"party_id"`
	Subject   string    `json:"subject"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

func (partyGrant) TableName() string                 { return "supply_party_access" }
func (r partyGrant) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const grantColumns = "id,tenant_id,party_id,subject,role,created_at"

var errAccessScope = errors.New("unsupported operational access scope")

var subjectID = regexp.MustCompile(`^[A-Za-z0-9._:@|+\-]{1,200}$`)

func validPartyRole(role string) bool {
	switch role {
	case "administrator", "purchasing", "finance", "compliance", "auditor", "warehouse", "sales", "driver":
		return true
	}
	return false
}
func (h *handlers) changeGrant(c *gin.Context, remove bool) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := partyID(c)
	if !ok {
		return
	}
	subject, role := c.Param("subject"), c.Param("role")
	if subject != strings.TrimSpace(subject) || !subjectID.MatchString(subject) || !validPartyRole(role) {
		respond.Error(c, 400, "invalid_request", "use a valid identity user ID and scoped role")
		return
	}
	verifiedNewTarget := false
	if !remove {
		var existing int64
		err := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
			if e := requireAccessAdministrator(tx, p, id); e != nil {
				return e
			}
			return tx.Model(&partyGrant{}).Where("tenant_id=? AND party_id=? AND subject=? AND role=?", p.TenantID, id, subject, role).Count(&existing).Error
		})
		if err != nil {
			supplierError(c, err)
			return
		}
		if existing == 0 {
			if !h.verifyAccessMember(c, p, subject) {
				return
			}
			verifiedNewTarget = true
		}
	}
	var row partyGrant
	e := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, id); e != nil {
			return e
		}
		if e := requireAccessAdministrator(tx, p, id); e != nil {
			return e
		}
		var header struct{ ID uuid.UUID }
		if e := tx.Table("supply_parties").Select("id").Where("tenant_id=? AND id=?", p.TenantID, id).First(&header).Error; e != nil {
			return e
		}
		if e := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-access:"+id.String()+":"+subject+":"+role).Error; e != nil {
			return e
		}
		e := tx.Select(grantColumns).Where("tenant_id=? AND party_id=? AND subject=? AND role=?", p.TenantID, id, subject, role).First(&row).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if remove {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if row.Role == "administrator" {
				if e := retainAdministrator(tx, p, id, row.Subject); e != nil {
					return e
				}
			}
			if e := tx.Where("tenant_id=? AND id=?", p.TenantID, row.ID).Delete(&partyGrant{}).Error; e != nil {
				return e
			}
			return h.publishAccess(tx, p, row, "revoked")
		}
		if e == nil {
			return nil
		}
		if !verifiedNewTarget {
			return errConflict
		}
		var permanentAdministrator int64
		if e := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND subject=? AND role='administrator'", p.TenantID, id, p.Subject).Count(&permanentAdministrator).Error; e != nil {
			return e
		}
		if permanentAdministrator == 0 {
			return gorm.ErrRecordNotFound
		}
		var explicit int64
		if e := tx.Table("supply_access_grants").Where("tenant_id=? AND party_id=? AND subject=? AND role=? AND premise_id IS NULL AND status='active'", p.TenantID, id, subject, role).Count(&explicit).Error; e != nil {
			return e
		}
		if explicit > 0 {
			return errConflict
		}

		grantID, e := uuid.NewV7()
		if e != nil {
			return e
		}
		row = partyGrant{ID: grantID, TenantID: uuid.MustParse(p.TenantID), PartyID: id, Subject: subject, Role: role}
		if e := tx.Create(&row).Error; e != nil {
			return e
		}
		return h.publishAccess(tx, p, row, "granted")
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	if remove {
		c.Status(204)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) verifyAccessMember(c *gin.Context, p authn.Principal, subject string) bool {
	if h.roles == nil {
		respond.Error(c, 503, "identity_unavailable", "Tenant membership verification is temporarily unavailable.")
		return false
	}
	bindings, err := h.roles.ListRoles(c.Request.Context(), subject)
	if err != nil {
		respond.Error(c, 503, "identity_unavailable", "Tenant membership verification is temporarily unavailable.")
		return false
	}
	for _, binding := range bindings {
		if binding.TenantID == p.TenantID {
			return true
		}
	}
	respond.NotFound(c, "party")
	return false
}
func (h *handlers) putGrant(c *gin.Context)    { h.changeGrant(c, false) }
func (h *handlers) deleteGrant(c *gin.Context) { h.changeGrant(c, true) }
func (h *handlers) publishAccess(tx *gorm.DB, p authn.Principal, row partyGrant, operation string) error {
	data, e := json.Marshal(supplycontract.PartyAccessChangedData{PartyID: row.PartyID.String(), AccessID: row.ID.String(), Operation: operation})
	if e != nil {
		return e
	}
	return h.bus.Publish(tx, supplycontract.SubjectPartyAccessChanged, events.Event{Type: "SupplyPartyAccessChanged", Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *handlers) getAccess(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := partyID(c)
	if !ok {
		return
	}
	var roles, operatingRoles []string
	e := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		var row struct{ Roles, OperatingRoles json.RawMessage }
		if err := tx.Raw("SELECT supply_access_roles(?,?,?) AS roles,supply_operating_roles(?,?,?) AS operating_roles", p.TenantID, id, p.Subject, p.TenantID, id, p.Subject).Scan(&row).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(row.Roles, &roles); err != nil {
			return err
		}
		return json.Unmarshal(row.OperatingRoles, &operatingRoles)
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	if len(roles)+len(operatingRoles) == 0 {
		respond.NotFound(c, "party")
		return
	}
	respond.OK(c, gin.H{"roles": roles, "operating_roles": operatingRoles})
}
