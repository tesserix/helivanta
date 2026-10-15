package supply

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type partyRevision struct {
	ID           uuid.UUID       `json:"id"`
	PartyID      uuid.UUID       `json:"party_id"`
	Version      int64           `json:"version"`
	Snapshot     json.RawMessage `json:"snapshot" gorm:"type:jsonb"`
	ActorSubject string          `json:"actor_subject"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (partyRevision) TableName() string                 { return "supply_party_revisions" }
func (r partyRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *handlers) listRevisions(c *gin.Context, params pagination.Params) ([]partyRevision, error) {
	p, ok := principal(c)
	if !ok {
		return nil, nil
	}
	id, ok := partyID(c)
	if !ok {
		return nil, nil
	}
	var rows []partyRevision
	err := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, id, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select("id,party_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=?", p.TenantID, id).Find(&rows).Error
	})
	if err != nil {
		supplierError(c, err)
	}
	return rows, nil
}
