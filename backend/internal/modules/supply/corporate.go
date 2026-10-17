package supply

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type corporateStructure struct {
	TenantID  uuid.UUID  `json:"-"`
	PartyID   uuid.UUID  `json:"party_id"`
	Version   int64      `json:"version"`
	CreatedAt *time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt *time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	CorporateFields
}

func (corporateStructure) TableName() string { return "supply_corporate_structures" }

const corporateColumns = "tenant_id,party_id,parent_party_id,version,created_at,updated_at"

func ownedStructureParty(tx *gorm.DB, p authn.Principal, id uuid.UUID, roles []string) error {
	if err := requireRoles(tx, p, id, roles); err != nil {
		return err
	}
	var owner party
	return tx.Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, id).First(&owner).Error
}

func structureError(c *gin.Context, err error) {
	if errors.Is(err, errInvalidRegistry) {
		respond.Error(c, 400, "invalid_request", "The territory country must match its organisation.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "supply_corporate_root_reference" {
		respond.Conflict(c, "Choose a configured group root; a group with subsidiaries must remain a root.")
		return
	}
	if errors.As(err, &pg) && (strings.HasPrefix(pg.ConstraintName, "supply_corporate_") || strings.HasPrefix(pg.ConstraintName, "supply_territory_")) {
		if pg.Code == "23503" || pg.Code == "42501" {
			respond.NotFound(c, "organisation structure")
			return
		}
		if pg.Code == "23514" || pg.Code == "23502" {
			respond.Error(c, 400, "invalid_request", "Invalid organisation structure fields.")
			return
		}
	}
	supplierError(c, err)
}

func (h *handlers) getCorporate(c *gin.Context) {
	p, id, ok := registryScope(c)
	if !ok {
		return
	}
	saved := corporateStructure{TenantID: uuid.MustParse(p.TenantID), PartyID: id}
	err := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, id, registryReaders); err != nil {
			return err
		}
		err := tx.Select(corporateColumns).Where("tenant_id=? AND party_id=?", p.TenantID, id).First(&saved).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) putCorporate(c *gin.Context) {
	p, id, ok := registryScope(c)
	if !ok {
		return
	}
	var in corporateInput
	if !decode(c, &in) {
		return
	}
	if binding.Validator.ValidateStruct(&in) != nil || (in.ParentPartyID != nil && (in.ParentPartyID.Version() != 7 || in.ParentPartyID.Variant() != uuid.RFC4122 || *in.ParentPartyID == id)) {
		respond.Error(c, 400, "invalid_request", "Choose a different owned organisation as group root.")
		return
	}
	var saved corporateStructure
	err := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, id, []string{"administrator"}); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-corporate").Error; err != nil {
			return err
		}
		err := tx.Select(corporateColumns).Where("tenant_id=? AND party_id=?", p.TenantID, id).First(&saved).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		replay, err := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.CorporateFields, in.CorporateFields))
		if err != nil || replay {
			return err
		}
		if !exists {
			saved = corporateStructure{TenantID: uuid.MustParse(p.TenantID), PartyID: id}
		}
		saved.CorporateFields = in.CorporateFields
		saved.Version++
		if exists {
			err = tx.Model(&corporateStructure{}).Where("tenant_id=? AND party_id=?", p.TenantID, id).Select("parent_party_id", "version").Updates(&saved).Error
		} else {
			err = tx.Create(&saved).Error
		}
		if err != nil {
			return err
		}
		if err = tx.Select(corporateColumns).Where("tenant_id=? AND party_id=?", p.TenantID, id).First(&saved).Error; err != nil {
			return err
		}
		snapshot, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		revision, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO supply_corporate_revisions(id,tenant_id,party_id,version,snapshot,actor_subject) VALUES(?,?,?,?,?,?)`, revision, p.TenantID, id, saved.Version, string(snapshot), p.Subject).Error; err != nil {
			return err
		}
		data, err := json.Marshal(supplycontract.CorporateUpdatedData{PartyID: id.String(), Version: saved.Version})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, supplycontract.SubjectCorporateUpdated, events.Event{Type: supplycontract.SubjectCorporateUpdated, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listCorporateRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, id, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	err := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, id, registryReaders); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_corporate_revisions").Select("id,party_id,party_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=?", p.TenantID, id).Find(&rows).Error
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}
