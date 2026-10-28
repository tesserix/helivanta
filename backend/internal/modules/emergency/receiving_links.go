package emergency

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type receivingLink struct {
	DestinationName *string   `json:"destination_name"`
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"provider_tenant_id"`
	BaseID          uuid.UUID `json:"base_id"`
	ReceivingLinkInput
	RequestedBy             string    `json:"requested_by"`
	Status                  string    `json:"status"`
	Version                 int64     `json:"version"`
	ReceiverPolicyReference *string   `json:"receiver_policy_reference"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

const receivingLinkColumns = "id,tenant_id,base_id,destination_tenant_id,facility_id,policy_reference,valid_until,requested_by,status,version,receiver_policy_reference,destination_name,created_at,updated_at"

func (h *handlers) putReceivingLink(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "link")
	if !ok {
		return
	}
	var in ReceivingLinkInput
	if !decode(c, &in) {
		return
	}
	if !policyShape.MatchString(in.PolicyReference) || in.FacilityID.Version() != 7 {
		respond.Error(c, 400, "invalid_request", "invalid receiving proposal")
		return
	}
	in.ValidUntil = in.ValidUntil.Truncate(time.Microsecond)
	var out receivingLink
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		if err := clinicalBaseLock(tx, p, b, "NO KEY UPDATE"); err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_link_status").Select(receivingLinkColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Take(&out).Error
		if err == nil {
			if out.DestinationTenantID != in.DestinationTenantID || out.FacilityID != in.FacilityID || out.PolicyReference != in.PolicyReference || !out.ValidUntil.Equal(in.ValidUntil) || out.RequestedBy != p.Subject {
				return errCaseConflict
			}
			out.Status = "proposed"
			out.Version = 1
			out.ReceiverPolicyReference = nil
			out.DestinationName = nil
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_links(tenant_id,base_id,id,destination_tenant_id,facility_id,policy_reference,valid_until,requested_by)VALUES(?,?,?,?,?,?,?,?)`, p.TenantID, b, id, in.DestinationTenantID, in.FacilityID, in.PolicyReference, in.ValidUntil, p.Subject).Error; err != nil {
			return err
		}
		return tx.Table("emergency_receiving_link_status").Select(receivingLinkColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func receivingRecordScope(c *gin.Context, resource string) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	if c.Param("facility") == "" {
		p, b, ok := scope(c)
		if !ok {
			return p, b, uuid.Nil, uuid.Nil, false
		}
		id, ok := resourceID(c, resource)
		return p, b, uuid.MustParse(p.TenantID), id, ok
	}
	p, f, ok := receivingScope(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	provider, err := uuid.Parse(c.Param("provider"))
	if err != nil || provider == uuid.Nil {
		respond.NotFound(c, "receiving link")
		return p, uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, ok := resourceID(c, resource)
	return p, f, provider, id, ok
}
func linkQuery(tx *gorm.DB, c *gin.Context, p authn.Principal, scope, provider, id uuid.UUID) *gorm.DB {
	query := tx.Table("emergency_receiving_link_status").Where("tenant_id=? AND id=?", provider, id)
	if c.Param("facility") != "" {
		return query.Where("destination_tenant_id=? AND facility_id=?", p.TenantID, scope)
	}
	return query.Where("base_id=?", scope)
}
func (h *handlers) getReceivingLink(c *gin.Context) {
	p, s, provider, id, ok := receivingRecordScope(c, "link")
	if !ok {
		return
	}
	var out receivingLink
	b := s
	if c.Param("facility") != "" {
		b = uuid.Nil
	}
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		return linkQuery(tx, c, p, s, provider, id).Select(receivingLinkColumns).Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}

type receivingLinkCommand struct {
	ID                uuid.UUID `json:"id"`
	LinkID            uuid.UUID `json:"link_id"`
	ExpectedVersion   int64     `json:"expected_version"`
	Operation         string    `json:"operation"`
	EvidenceReference string    `json:"evidence_reference"`
	ActorTenantID     uuid.UUID `json:"actor_tenant_id"`
	ActorSubject      string    `json:"actor_subject"`
	Status            string    `json:"status" gorm:"column:result_status"`
	Version           int64     `json:"version" gorm:"column:result_version"`
	CreatedAt         time.Time `json:"created_at"`
}

const receivingLinkCommandColumns = "id,link_id,expected_version,operation,evidence_reference,actor_tenant_id,actor_subject,result_status,result_version,created_at"

func (h *handlers) putReceivingLinkCommand(c *gin.Context) {
	p, s, provider, id, ok := receivingRecordScope(c, "link")
	if !ok {
		return
	}
	command, ok := resourceID(c, "command")
	if !ok {
		return
	}
	var in ReceivingLinkCommandInput
	if !decode(c, &in) {
		return
	}
	recipient := c.Param("facility") != ""
	if !policyShape.MatchString(in.EvidenceReference) || (!recipient && in.Operation != "revoke") {
		respond.Error(c, 400, "invalid_request", "invalid agreement command")
		return
	}
	b := s
	if recipient {
		b = uuid.Nil
	}
	var out receivingLinkCommand
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		var link receivingLink
		if err := linkQuery(tx, c, p, s, provider, id).Select(receivingLinkColumns).Take(&link).Error; err != nil {
			return err
		}
		if !recipient {
			if err := requireAccess(tx, p, s, "close"); err != nil {
				return err
			}
		}
		var locked struct{ ID uuid.UUID }
		if err := tx.Table("emergency_receiving_links").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", provider, link.BaseID, id).Take(&locked).Error; err != nil {
			return err
		}
		err := tx.Table("emergency_receiving_link_commands").Select(receivingLinkCommandColumns).Where("tenant_id=? AND base_id=? AND id=?", provider, link.BaseID, command).Take(&out).Error
		if err == nil {
			if out.LinkID != id || out.ExpectedVersion != in.Version || out.Operation != in.Operation || out.EvidenceReference != in.EvidenceReference || out.ActorSubject != p.Subject || out.ActorTenantID.String() != p.TenantID {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_receiving_link_commands(tenant_id,base_id,link_id,id,expected_version,operation,evidence_reference,actor_tenant_id,actor_subject)VALUES(?,?,?,?,?,?,?,?,?)`, provider, link.BaseID, id, command, in.Version, in.Operation, in.EvidenceReference, p.TenantID, p.Subject).Error; err != nil {
			return err
		}
		return tx.Table("emergency_receiving_link_commands").Select(receivingLinkCommandColumns).Where("tenant_id=? AND base_id=? AND id=?", provider, link.BaseID, command).Take(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
