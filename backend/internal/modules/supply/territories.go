package supply

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type territory struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"-"`
	PartyID   uuid.UUID `json:"party_id"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	TerritoryFields
}

func (territory) TableName() string                 { return "supply_territories" }
func (r territory) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const territoryColumns = "id,tenant_id,party_id,code,name,country_code,region,city,description,status,branch_premise_id,version,created_at,updated_at"

func (in *territoryInput) normalize() bool {
	if in.AssignedSalesSubjects == nil {
		return false
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	if !cleanText(&in.Name, &in.Region, &in.City, &in.Description) || !validTerritoryFieldsPatterns(in.TerritoryFields) {
		return false
	}
	if in.BranchPremiseID != nil && (in.BranchPremiseID.Version() != 7 || in.BranchPremiseID.Variant() != uuid.RFC4122) {
		return false
	}
	sort.Strings(in.AssignedSalesSubjects)
	for i, s := range in.AssignedSalesSubjects {
		if !subjectID.MatchString(s) || (i > 0 && s == in.AssignedSalesSubjects[i-1]) {
			return false
		}
	}
	return true
}

func loadTerritorySubjects(tx *gorm.DB, p authn.Principal, saved *territory) error {
	saved.AssignedSalesSubjects = []string{}
	return tx.Table("supply_territory_representatives").Where("tenant_id=? AND party_id=? AND territory_id=?", p.TenantID, saved.PartyID, saved.ID).Order("subject").Pluck("subject", &saved.AssignedSalesSubjects).Error
}

func (h *handlers) putTerritory(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "territory")
	if !ok {
		return
	}
	var in territoryInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Use valid territory fields and unique sales representatives.")
		return
	}
	var saved territory
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := lockAccessAdministration(tx, p, partyID); err != nil {
			return err
		}
		if err := ownedStructureParty(tx, p, partyID, []string{"administrator"}); err != nil {
			return err
		}
		var owner party
		if err := tx.Select("country_code").Where("tenant_id=? AND id=?", p.TenantID, partyID).First(&owner).Error; err != nil {
			return err
		}
		if owner.CountryCode != in.CountryCode {
			return errInvalidRegistry
		}
		if err := lockPremiseStructure(tx, p, partyID); err != nil {
			return err
		}
		if err := lockRegistry(tx, p, partyID, id, "territory"); err != nil {
			return err
		}
		err := tx.Select(territoryColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists {
			if err = loadTerritorySubjects(tx, p, &saved); err != nil {
				return err
			}
		}
		replay, err := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.TerritoryFields, in.TerritoryFields))
		if err != nil || replay {
			return err
		}
		if !exists {
			saved = territory{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID}
		}
		saved.TerritoryFields = in.TerritoryFields
		saved.Version++
		if exists {
			err = tx.Model(&territory{}).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).Select("code", "name", "country_code", "region", "city", "description", "status", "branch_premise_id", "version").Updates(&saved).Error
		} else {
			err = tx.Create(&saved).Error
		}
		if err != nil {
			return err
		}
		if len(in.AssignedSalesSubjects) > 0 {
			err = tx.Exec(`DELETE FROM supply_territory_representatives WHERE tenant_id=? AND party_id=? AND territory_id=? AND subject NOT IN ?`, p.TenantID, partyID, id, in.AssignedSalesSubjects).Error
		} else {
			err = tx.Exec(`DELETE FROM supply_territory_representatives WHERE tenant_id=? AND party_id=? AND territory_id=?`, p.TenantID, partyID, id).Error
		}
		if err != nil {
			return err
		}
		subjects, err := json.Marshal(in.AssignedSalesSubjects)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO supply_territory_representatives(tenant_id,party_id,territory_id,subject) SELECT ?,?,?,value FROM jsonb_array_elements_text(?::jsonb) AS s(value) ON CONFLICT DO NOTHING`, p.TenantID, partyID, id, string(subjects)).Error; err != nil {
			return err
		}
		if err = tx.Select(territoryColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; err != nil {
			return err
		}
		if err = loadTerritorySubjects(tx, p, &saved); err != nil {
			return err
		}
		return h.recordTerritory(tx, p, saved)
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) recordTerritory(tx *gorm.DB, p authn.Principal, saved territory) error {
	snapshot, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err = tx.Exec(`INSERT INTO supply_territory_revisions(id,tenant_id,party_id,territory_id,version,snapshot,actor_subject) VALUES(?,?,?,?,?,?,?)`, id, p.TenantID, saved.PartyID, saved.ID, saved.Version, string(snapshot), p.Subject).Error; err != nil {
		return err
	}
	data, err := json.Marshal(supplycontract.TerritoryUpdatedData{PartyID: saved.PartyID.String(), TerritoryID: saved.ID.String(), Version: saved.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, supplycontract.SubjectTerritoryUpdated, events.Event{Type: supplycontract.SubjectTerritoryUpdated, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) getTerritory(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "territory")
	if !ok {
		return
	}
	var saved territory
	err := h.transaction(c.Request.Context(), p, partyID, true, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, partyID, append([]string{"sales"}, registryReaders...)); err != nil {
			return err
		}
		if err := tx.Select(territoryColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; err != nil {
			return err
		}
		return loadTerritorySubjects(tx, p, &saved)
	})
	if err != nil {
		structureError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) listTerritories(c *gin.Context, params pagination.Params) ([]territory, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []territory
	err := h.transaction(c.Request.Context(), p, partyID, true, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, partyID, append([]string{"sales"}, registryReaders...)); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(territoryColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
			rows[i].AssignedSalesSubjects = []string{}
		}
		var assignments []struct {
			TerritoryID uuid.UUID
			Subject     string
		}
		if err := tx.Table("supply_territory_representatives").Select("territory_id,subject").Where("tenant_id=? AND party_id=? AND territory_id IN ?", p.TenantID, partyID, ids).Order("subject").Find(&assignments).Error; err != nil {
			return err
		}
		grouped := make(map[uuid.UUID][]string, len(rows))
		for _, assignment := range assignments {
			grouped[assignment.TerritoryID] = append(grouped[assignment.TerritoryID], assignment.Subject)
		}
		for i := range rows {
			if subjects := grouped[rows[i].ID]; subjects != nil {
				rows[i].AssignedSalesSubjects = subjects
			}
		}
		return nil
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}

func (h *handlers) listTerritoryRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "territory")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := ownedStructureParty(tx, p, partyID, registryReaders); err != nil {
			return err
		}
		var head territory
		if err := tx.Select("id").Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&head).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_territory_revisions").Select("id,party_id,territory_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND territory_id=?", p.TenantID, partyID, id).Find(&rows).Error
	})
	if err != nil {
		structureError(c, err)
	}
	return rows, nil
}
