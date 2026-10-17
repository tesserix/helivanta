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
	"gorm.io/gorm/clause"
)

type storageBin struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"-"`
	PartyID     uuid.UUID `json:"party_id"`
	PremiseID   uuid.UUID `json:"premise_id"`
	CountryCode string    `json:"country_code"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	BinFields
}

func (storageBin) TableName() string                 { return "supply_bins" }
func (r storageBin) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const binColumns = "id,tenant_id,party_id,premise_id,country_code,version,created_at,updated_at,code,name,aisle,rack,shelf,storage_class,status"

var binReaders = []string{"administrator", "purchasing", "compliance", "auditor"}

func (in *binInput) normalize() bool {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	return cleanText(&in.Name, &in.Aisle, &in.Rack, &in.Shelf) && validBinFieldsPatterns(in.BinFields)
}
func binScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return p, partyID, uuid.Nil, false
	}
	premiseID, ok := resourceID(c, "premise")
	return p, partyID, premiseID, ok
}
func binParent(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (premise, error) {
	var parent premise
	// Missing grants deny even if a global permission or a cached UI says otherwise.
	if e := requireRoles(tx, p, partyID, roles); e != nil {
		return parent, e
	}
	var owner party
	if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, partyID).First(&owner).Error; e != nil {
		return parent, e
	}
	e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=? AND kind IN ?", p.TenantID, partyID, premiseID, []string{"warehouse", "depot"}).First(&parent).Error
	return parent, e
}
func binError(c *gin.Context, e error) {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && (pg.ConstraintName == "supply_premise_bins_kind" || pg.TableName == "supply_bins" && pg.Code == "23505") {
		respond.Conflict(c, "bin code already exists or the premise retains storage bins")
		return
	}
	registryError(c, e)
}
func (h *handlers) putBin(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return
	}
	var in binInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "invalid storage bin fields")
		return
	}
	var saved storageBin
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		parent, e := binParent(tx, p, partyID, premiseID, premiseWriters)
		if e != nil {
			return e
		}
		saved, e = h.saveBin(tx, p, partyID, premiseID, id, in, parent)
		return e
	})
	if e != nil {
		binError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) putOperatingBin(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return
	}
	var in binInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "invalid storage bin fields")
		return
	}
	var saved storageBin
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := lockAccessAdministration(tx, p, partyID); err != nil {
			return err
		}
		parent, err := operatingParent(tx, p, partyID, premiseID, []string{"administrator", "inventory"})
		if err != nil {
			return err
		}
		// Existing bin guards verify ownership through the private party row.
		if err = tx.Exec(`SELECT set_config('app.supply_directory','on',true),set_config('app.supply_manager','on',true)`).Error; err != nil {
			return err
		}
		saved, err = h.saveBin(tx, p, partyID, premiseID, id, in, parent)
		return err
	})
	if err != nil {
		binError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *handlers) saveBin(tx *gorm.DB, p authn.Principal, partyID, premiseID, id uuid.UUID, in binInput, parent premise) (storageBin, error) {
	var saved storageBin
	if e := lockRegistry(tx, p, partyID, id, "bin:"+premiseID.String()); e != nil {
		return saved, e
	}
	e := tx.Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error
	exists := e == nil
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return saved, e
	}
	replay, e := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.BinFields, in.BinFields))
	if e != nil || replay {
		return saved, e
	}
	if parent.Status != "active" && in.Status == "active" {
		return saved, errConflict
	}
	if !exists {
		saved = storageBin{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID, PremiseID: premiseID, CountryCode: parent.CountryCode}
	}
	saved.BinFields = in.BinFields
	saved.Version++
	if exists {
		e = tx.Model(&storageBin{}).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).Select("code", "name", "aisle", "rack", "shelf", "storage_class", "status", "version").Updates(&saved).Error
	} else {
		e = tx.Create(&saved).Error
	}
	if e != nil {
		return saved, e
	}
	if e = tx.Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error; e != nil {
		return saved, e
	}
	return saved, h.recordBin(tx, p, saved)
}

func (h *handlers) recordBin(tx *gorm.DB, p authn.Principal, saved storageBin) error {
	snapshot, e := json.Marshal(saved)
	if e != nil {
		return e
	}
	id, e := uuid.NewV7()
	if e != nil {
		return e
	}
	row := map[string]any{"id": id, "tenant_id": p.TenantID, "party_id": saved.PartyID, "premise_id": saved.PremiseID, "bin_id": saved.ID, "version": saved.Version, "snapshot": string(snapshot), "actor_subject": p.Subject}
	if e := tx.Table("supply_bin_revisions").Create(row).Error; e != nil {
		return e
	}
	data, e := json.Marshal(supplycontract.BinUpdatedData{PartyID: saved.PartyID.String(), PremiseID: saved.PremiseID.String(), BinID: saved.ID.String(), Version: saved.Version})
	if e != nil {
		return e
	}
	return h.bus.Publish(tx, supplycontract.SubjectBinUpdated, events.Event{Type: supplycontract.SubjectBinUpdated, Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *handlers) getBin(c *gin.Context) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return
	}
	var saved storageBin
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, e := binParent(tx, p, partyID, premiseID, binReaders); e != nil {
			return e
		}
		return tx.Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&saved).Error
	})
	if e != nil {
		binError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) listBins(c *gin.Context, params pagination.Params) ([]storageBin, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	var rows []storageBin
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, e := binParent(tx, p, partyID, premiseID, binReaders); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Select(binColumns).Where("tenant_id=? AND party_id=? AND premise_id=?", p.TenantID, partyID, premiseID).Find(&rows).Error
	})
	if e != nil {
		binError(c, e)
	}
	return rows, nil
}
func (h *handlers) listBinRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, premiseID, ok := binScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "bin")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if _, e := binParent(tx, p, partyID, premiseID, binReaders); e != nil {
			return e
		}
		var head storageBin
		if e := tx.Select("id").Where("tenant_id=? AND party_id=? AND premise_id=? AND id=?", p.TenantID, partyID, premiseID, id).First(&head).Error; e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_bin_revisions").Select("id,party_id,bin_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND premise_id=? AND bin_id=?", p.TenantID, partyID, premiseID, id).Find(&rows).Error
	})
	if e != nil {
		binError(c, e)
	}
	return rows, nil
}
