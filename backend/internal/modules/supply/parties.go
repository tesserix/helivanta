package supply

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type party struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"-"`
	PartyFields      `gorm:"embedded"`
	OnboardingStatus string    `json:"onboarding_status"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (party) TableName() string                 { return "supply_parties" }
func (p party) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

type handlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

var errConflict = errors.New("supply version or identity conflict")

const partyColumns = "id,tenant_id,code,record_kind,legal_name,trading_name,business_kind,country_code,jurisdiction,registration_number,corporate_group,website,default_credit_days,onboarding_status,version,created_at,updated_at"

func principal(c *gin.Context) (authn.Principal, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	return p, ok
}
func partyID(c *gin.Context) (uuid.UUID, bool) {
	id, e := uuid.Parse(c.Param("party"))
	if e != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "party")
		return uuid.Nil, false
	}
	return id, true
}
func decode(c *gin.Context, in any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	e := d.Decode(in)
	if e == nil {
		e = d.Decode(new(any))
		if errors.Is(e, io.EOF) {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(e, &tooLarge) {
		respond.Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "supplier request exceeds 32 KiB")
	} else {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "expected one valid supplier object")
	}
	return false
}
func has(c *gin.Context, permission authz.Permission) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && set.Has(permission)
}
func (h *handlers) transaction(ctx context.Context, p authn.Principal, id uuid.UUID, directory, manager bool, fn func(*gorm.DB) error) error {
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		flag := func(v bool) string {
			if v {
				return "on"
			}
			return "off"
		}
		if e := tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_party',?,true),set_config('app.supply_directory',?,true),set_config('app.supply_manager',?,true)`, p.Subject, id.String(), flag(directory), flag(manager)).Error; e != nil {
			return errors.New("supply authorization unavailable")
		}
		return fn(tx)
	})
}
func requireAccess(tx *gorm.DB, p authn.Principal, id uuid.UUID, write bool) error {
	if write {
		return requireRoles(tx, p, id, premiseWriters)
	}
	return requireRoles(tx, p, id, registryReaders)
}
func supplierError(c *gin.Context, e error) {
	if errors.Is(e, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "party")
		return
	}
	if errors.Is(e, errConflict) {
		respond.Conflict(c, "supplier version or identity already differs")
		return
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) && strings.HasPrefix(pg.TableName, "supply_") {
		switch pg.Code {
		case "23505":
			respond.Conflict(c, "supplier code or registration already exists")
			return
		case "23503", "42501":
			respond.NotFound(c, "party")
			return
		case "23514", "23502":
			if pg.ConstraintName == "supply_party_identity" {
				respond.Conflict(c, "supplier identity is immutable")
			} else {
				respond.Error(c, 400, "invalid_request", "invalid supplier fields")
			}
			return
		}
	}
	respond.InternalErr(c, errors.New("supply operation unavailable"), "could not process supplier request")
}
func (in *PartyFields) normalize() bool {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.RegistrationNumber = strings.ToUpper(strings.TrimSpace(in.RegistrationNumber))
	for _, s := range []*string{&in.LegalName, &in.TradingName, &in.Jurisdiction, &in.CorporateGroup, &in.Website} {
		*s = strings.TrimSpace(*s)
	}
	if !validPartyFieldsPatterns(*in) || in.LegalName == "" || in.Jurisdiction == "" || in.RegistrationNumber == "" {
		return false
	}
	if in.Website != "" {
		u, e := url.Parse(in.Website)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return false
		}
	}
	return in.normalizeParts()
}
func (h *handlers) putParty(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := partyID(c)
	if !ok {
		return
	}
	var in partyInput
	if !decode(c, &in) {
		return
	}
	if !in.normalize() || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "invalid supplier fields")
		return
	}
	if in.RecordKind == "owned_organisation" && !has(c, PermOrganizationManage) {
		respond.Forbidden(c, "organisation management permission required")
		return
	}
	var saved party
	e := h.transaction(c.Request.Context(), p, id, true, true, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply:"+id.String()).Error; e != nil {
			return e
		}
		e := tx.Select(partyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).First(&saved).Error
		exists := e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if exists {
			if e := requireAccess(tx, p, id, true); e != nil {
				return e
			}
			if e := loadPartyParts(tx, &saved); e != nil {
				return e
			}
			if reflect.DeepEqual(saved.PartyFields, in.PartyFields) && (*in.ExpectedVersion == saved.Version || *in.ExpectedVersion == saved.Version-1) {
				return nil
			}
		}
		if !exists && *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		if exists && *in.ExpectedVersion != saved.Version {
			return errConflict
		}
		if !exists {
			grantID, e := uuid.NewV7()
			if e != nil {
				return e
			}
			if e := tx.Exec(`INSERT INTO supply_party_access(id,tenant_id,party_id,subject,role) VALUES (?,?,?,?,'administrator')`, grantID, p.TenantID, id, p.Subject).Error; e != nil {
				return e
			}
			saved = party{ID: id, TenantID: uuid.MustParse(p.TenantID), OnboardingStatus: "draft"}
		}
		before := saved.PartyFields
		saved.PartyFields = in.PartyFields
		saved.Version++
		if exists {
			if e := tx.Model(&party{}).Where("tenant_id=? AND id=?", p.TenantID, id).Select("code", "record_kind", "legal_name", "trading_name", "business_kind", "country_code", "jurisdiction", "registration_number", "corporate_group", "website", "default_credit_days", "version", "updated_at").Updates(&saved).Error; e != nil {
				return e
			}
		} else if e := tx.Create(&saved).Error; e != nil {
			return e
		}
		if e := storePartyParts(tx, &saved, before); e != nil {
			return e
		}
		if e := tx.Select(partyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).First(&saved).Error; e != nil {
			return e
		}
		if e := loadPartyParts(tx, &saved); e != nil {
			return e
		}
		snapshot, e := json.Marshal(saved)
		if e != nil {
			return e
		}
		revision, e := uuid.NewV7()
		if e != nil {
			return e
		}
		if e := tx.Exec(`INSERT INTO supply_party_revisions(id,tenant_id,party_id,version,snapshot,actor_subject) VALUES (?,?,?,?,?::jsonb,?)`, revision, p.TenantID, id, saved.Version, string(snapshot), p.Subject).Error; e != nil {
			return e
		}
		data, e := json.Marshal(supplycontract.PartyUpdatedData{PartyID: id.String(), Version: saved.Version})
		if e != nil {
			return e
		}
		return h.bus.Publish(tx, supplycontract.SubjectPartyUpdated, events.Event{Type: "SupplyPartyUpdated", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) getParty(c *gin.Context) {
	p, ok := principal(c)
	if !ok {
		return
	}
	id, ok := partyID(c)
	if !ok {
		return
	}
	var row party
	e := h.transaction(c.Request.Context(), p, id, false, false, func(tx *gorm.DB) error {
		if e := requireAccess(tx, p, id, false); e != nil {
			return e
		}
		if e := tx.Select(partyColumns).Where("tenant_id=? AND id=?", p.TenantID, id).First(&row).Error; e != nil {
			return e
		}
		return loadPartyParts(tx, &row)
	})
	if e != nil {
		supplierError(c, e)
		return
	}
	respond.OK(c, row)
}
