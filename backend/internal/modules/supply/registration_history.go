package supply

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

type registryRevision struct {
	ID           uuid.UUID       `json:"id"`
	PartyID      uuid.UUID       `json:"party_id"`
	RecordID     uuid.UUID       `json:"record_id"`
	Version      int64           `json:"version"`
	Snapshot     json.RawMessage `json:"snapshot"`
	ActorSubject string          `json:"actor_subject"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (r registryRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *handlers) listRegistryRevisions(c *gin.Context, params pagination.Params, kind string) ([]registryRevision, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, kind)
	if !ok {
		return nil, nil
	}
	table, column, head := "supply_premise_revisions", "premise_id", "supply_premises"
	if kind == "license" {
		table, column, head = "supply_license_revisions", "license_id", "supply_licenses"
	}
	var rows []registryRevision
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, registryReaders); e != nil {
			return e
		}
		var exists struct{ ID uuid.UUID }
		if e := tx.Table(head).Select("id").Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&exists).Error; e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Table(table).Select("id,party_id,"+column+" AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND "+column+"=?", p.TenantID, partyID, id).Find(&rows).Error
	})
	if e != nil {
		registryError(c, e)
	}
	return rows, nil
}
func (h *handlers) listPremiseRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	return h.listRegistryRevisions(c, params, "premise")
}
func (h *handlers) listLicenseRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	return h.listRegistryRevisions(c, params, "license")
}
