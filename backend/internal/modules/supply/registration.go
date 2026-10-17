package supply

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
)

type premise struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"-"`
	PartyID   uuid.UUID `json:"party_id"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	PremiseFields
}

func (premise) TableName() string                 { return "supply_premises" }
func (r premise) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const premiseColumns = "id,tenant_id,party_id,version,created_at,updated_at,code,name,kind,line1,line2,city,region,postal_code,country_code,time_zone,status"

type license struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"-"`
	PartyID     uuid.UUID `json:"party_id"`
	CountryCode string    `json:"country_code"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	LicenseFields
}

func (license) TableName() string                 { return "supply_licenses" }
func (r license) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const licenseColumns = "id,tenant_id,party_id,country_code,status,version,created_at,updated_at,kind,number,issuing_authority,jurisdiction,premise_id,issued_on::text AS issued_on,expires_on::text AS expires_on,product_categories"

var registryReaders = []string{"administrator", "purchasing", "finance", "compliance", "auditor"}
var premiseWriters = []string{"administrator", "purchasing"}
var licenseWriters = []string{"administrator", "purchasing", "compliance"}

func requireRoles(tx *gorm.DB, p authn.Principal, partyID uuid.UUID, roles []string) error {
	var access partyGrant
	return tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("tenant_id=? AND party_id=? AND subject=? AND role IN ?", p.TenantID, partyID, p.Subject, roles).First(&access).Error
}
func registryScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, ok := principal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	id, ok := partyID(c)
	return p, id, ok
}
func resourceID(c *gin.Context, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(key))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, key)
		return uuid.Nil, false
	}
	return id, true
}
func cleanText(values ...*string) bool {
	for _, v := range values {
		*v = strings.TrimSpace(*v)
		for _, r := range *v {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return true
}
func (in *premiseInput) normalize() bool {
	f := &in.PremiseFields
	f.Code = strings.ToUpper(strings.TrimSpace(f.Code))
	f.CountryCode = strings.ToUpper(strings.TrimSpace(f.CountryCode))
	return cleanText(&f.Name, &f.Line1, &f.Line2, &f.City, &f.Region, &f.PostalCode, &f.TimeZone) && validPremiseFieldsPatterns(*f) && countryprofile.ValidTimezone(f.CountryCode, f.TimeZone)
}
func (in *licenseInput) normalize() bool {
	f := &in.LicenseFields
	f.Kind = strings.ToUpper(strings.TrimSpace(f.Kind))
	f.Number = strings.ToUpper(strings.TrimSpace(f.Number))
	if !cleanText(&f.IssuingAuthority, &f.Jurisdiction, &f.Number) || !validLicenseFieldsPatterns(*f) || f.PremiseID.Version() != 7 {
		return false
	}
	issued, e := time.Parse("2006-01-02", f.IssuedOn)
	if e != nil || issued.Year() < 1900 {
		return false
	}
	expires, e := time.Parse("2006-01-02", f.ExpiresOn)
	if e != nil || expires.Before(issued) {
		return false
	}
	sort.Strings(f.ProductCategories)
	for i := 1; i < len(f.ProductCategories); i++ {
		if f.ProductCategories[i] == f.ProductCategories[i-1] {
			return false
		}
	}
	return true
}
func registryVersion(exists bool, expected, current int64, equal bool) (bool, error) {
	if !exists && expected != 0 {
		return false, gorm.ErrRecordNotFound
	}
	if exists && equal && (expected == current || expected == current-1) {
		return true, nil
	}
	if exists && expected != current {
		return false, errConflict
	}
	return false, nil
}
func lockRegistry(tx *gorm.DB, p authn.Principal, partyID, id uuid.UUID, kind string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-"+kind+":"+partyID.String()+":"+id.String()).Error
}
func (h *handlers) recordRegistry(tx *gorm.DB, p authn.Principal, partyID, id uuid.UUID, version int64, snapshot any, kind string) error {
	data, e := json.Marshal(snapshot)
	if e != nil {
		return e
	}
	revision, e := uuid.NewV7()
	if e != nil {
		return e
	}
	table, column, subject := "supply_premise_revisions", "premise_id", supplycontract.SubjectPremiseUpdated
	if kind == "license" {
		table, column, subject = "supply_license_revisions", "license_id", supplycontract.SubjectLicenseDeclared
	}
	row := map[string]any{"id": revision, "tenant_id": p.TenantID, "party_id": partyID, column: id, "version": version, "snapshot": string(data), "actor_subject": p.Subject}
	if e := tx.Table(table).Create(row).Error; e != nil {
		return e
	}
	var payload []byte
	if kind == "license" {
		payload, e = json.Marshal(supplycontract.LicenseDeclaredData{PartyID: partyID.String(), LicenseID: id.String(), Version: version})
	} else {
		payload, e = json.Marshal(supplycontract.PremiseUpdatedData{PartyID: partyID.String(), PremiseID: id.String(), Version: version})
	}
	if e != nil {
		return e
	}
	return h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: p.TenantID, Data: payload})
}
func (h *handlers) putPremise(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "premise")
	if !ok {
		return
	}
	var in premiseInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "invalid premises fields")
		return
	}
	var saved premise
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, premiseWriters); e != nil {
			return e
		}
		if e := lockRegistry(tx, p, partyID, id, "premise"); e != nil {
			return e
		}
		var parent party
		if e := tx.Select("id,country_code").Where("tenant_id=? AND id=?", p.TenantID, partyID).First(&parent).Error; e != nil {
			return e
		}
		if parent.CountryCode != in.CountryCode {
			return errInvalidRegistry
		}
		e := tx.Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
		exists := e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		replay, e := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.PremiseFields, in.PremiseFields))
		if e != nil || replay {
			return e
		}
		if !exists {
			saved = premise{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID}
		}
		saved.PremiseFields = in.PremiseFields
		saved.Version++
		if exists {
			e = tx.Model(&premise{}).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).Select("code", "name", "kind", "line1", "line2", "city", "region", "postal_code", "country_code", "time_zone", "status", "version").Updates(&saved).Error
		} else {
			e = tx.Create(&saved).Error
		}
		if e != nil {
			return e
		}
		if e := tx.Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; e != nil {
			return e
		}
		return h.recordRegistry(tx, p, partyID, id, saved.Version, saved, "premise")
	})
	if err != nil {
		registryError(c, err)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) putLicense(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "license")
	if !ok {
		return
	}
	var in licenseInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "invalid licence fields")
		return
	}
	var saved license
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, licenseWriters); e != nil {
			return e
		}
		if e := lockRegistry(tx, p, partyID, id, "license"); e != nil {
			return e
		}
		var location premise
		if e := tx.Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, in.PremiseID).First(&location).Error; e != nil {
			return e
		}
		e := tx.Select(licenseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
		exists := e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		replay, e := registryVersion(exists, *in.ExpectedVersion, saved.Version, reflect.DeepEqual(saved.LicenseFields, in.LicenseFields))
		if e != nil || replay {
			return e
		}
		if !exists {
			saved = license{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID, CountryCode: location.CountryCode, Status: "declared"}
		}
		saved.LicenseFields = in.LicenseFields
		saved.Version++
		if exists {
			e = tx.Model(&license{}).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).Select("kind", "number", "issuing_authority", "jurisdiction", "premise_id", "issued_on", "expires_on", "product_categories", "version").Updates(&saved).Error
		} else {
			e = tx.Create(&saved).Error
		}
		if e != nil {
			return e
		}
		if e := tx.Select(licenseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; e != nil {
			return e
		}
		return h.recordRegistry(tx, p, partyID, id, saved.Version, saved, "license")
	})
	if err != nil {
		registryError(c, err)
		return
	}
	respond.OK(c, saved)
}

var errInvalidRegistry = errors.New("premise or licence declaration is incompatible with the party")

func registryError(c *gin.Context, e error) {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.ConstraintName == "supply_premise_bins_kind" {
		respond.Conflict(c, "premise has retained bins; use a separate non-storage address")
		return
	}
	if errors.As(e, &pg) && pg.Code == "23505" && (pg.TableName == "supply_premises" || pg.TableName == "supply_licenses") {
		respond.Conflict(c, "premise code or licence identity already exists")
		return
	}
	if errors.Is(e, errInvalidRegistry) {
		respond.Error(c, 400, "invalid_request", "premise and licence must belong to the same organisation and country")
		return
	}
	supplierError(c, e)
}

func (h *handlers) getPremise(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "premise")
	if !ok {
		return
	}
	var saved premise
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, registryReaders); e != nil {
			return e
		}
		return tx.Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
	})
	if e != nil {
		registryError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) getLicense(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "license")
	if !ok {
		return
	}
	var saved license
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, registryReaders); e != nil {
			return e
		}
		return tx.Select(licenseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
	})
	if e != nil {
		registryError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) listPremises(c *gin.Context, params pagination.Params) ([]premise, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []premise
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, registryReaders); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Select(premiseColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error
	})
	if e != nil {
		registryError(c, e)
	}
	return rows, nil
}
func (h *handlers) listLicenses(c *gin.Context, params pagination.Params) ([]license, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []license
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireRoles(tx, p, partyID, registryReaders); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Select(licenseColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error
	})
	if e != nil {
		registryError(c, e)
	}
	return rows, nil
}
