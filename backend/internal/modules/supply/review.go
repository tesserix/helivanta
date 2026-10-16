package supply

import (
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed review.sql
var reviewSchema string

type licenseReview struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"-"`
	PartyID        uuid.UUID `json:"party_id"`
	LicenseID      uuid.UUID `json:"license_id"`
	Version        int64     `json:"version"`
	PartyVersion   int64     `json:"party_version"`
	PremiseID      uuid.UUID `json:"premise_id"`
	PremiseVersion int64     `json:"premise_version"`
	LicenseVersion int64     `json:"license_version"`
	EvidenceID     uuid.UUID `json:"evidence_id"`
	Action         string    `json:"action"`
	Status         string    `json:"status"`
	Reason         string    `json:"reason"`
	Method         string    `json:"method"`
	Reference      string    `json:"reference"`
	ActorSubject   string    `json:"actor_subject"`
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
}

func (licenseReview) TableName() string                 { return "supply_license_reviews" }
func (r licenseReview) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// MarshalJSON keeps the wire contract independent of database and host time zones.
func (r licenseReview) MarshalJSON() ([]byte, error) {
	type wire licenseReview
	v := wire(r)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	return json.Marshal(v)
}

const reviewColumns = "id,tenant_id,party_id,license_id,version,party_version,premise_id,premise_version,license_version,evidence_id,action,status,reason,method,reference,actor_subject,created_at,updated_at"

func reviewScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, partyID, licenceID, ok := evidenceScope(c)
	if !ok {
		return p, partyID, licenceID, false
	}
	if expected := c.GetHeader("X-Helivanta-Expected-Subject"); expected != "" && expected != p.Subject {
		// A stale browser identity must never commit a review as a newly signed-in user.
		respond.Error(c, 409, "session_changed", "Your signed-in user changed. Refresh the page before continuing.")
		return p, partyID, licenceID, false
	}
	return p, partyID, licenceID, true
}

func reviewError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "supply_review_conflict" {
		respond.Conflict(c, "Review prerequisites changed or an independent reviewer is required. Refresh the licence before continuing.")
		return
	}
	registryError(c, err)
}

func (h *handlers) reviewSources(tx *gorm.DB, p authn.Principal, partyID, licenceID uuid.UUID) (party, license, premise, error) {
	var organisation party
	var licence license
	var location premise
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(licenseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, licenceID).First(&licence).Error
	if err == nil {
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(partyColumns).Where("tenant_id=? AND id=?", p.TenantID, partyID).First(&organisation).Error
	}
	if err == nil {
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(premiseColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, licence.PremiseID).First(&location).Error
	}
	return organisation, licence, location, err
}

func (r licenseReview) input() reviewInput {
	version := r.Version - 1
	return reviewInput{ExpectedVersion: &version, ExpectedPartyVersion: r.PartyVersion, ExpectedPremiseVersion: r.PremiseVersion, ExpectedLicenseVersion: r.LicenseVersion, EvidenceID: r.EvidenceID, Action: r.Action, Reason: r.Reason, Method: r.Method, Reference: r.Reference}
}

func (h *handlers) putLicenseReview(c *gin.Context) {
	p, partyID, licenceID, ok := reviewScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "review")
	if !ok {
		return
	}
	var in reviewInput
	if !decode(c, &in) {
		return
	}
	valid := cleanText(&in.Reason, &in.Reference) && binding.Validator.ValidateStruct(&in) == nil && in.EvidenceID.Version() == 7
	if in.Action == "verify" || in.Action == "reject" {
		valid = valid && in.Method != "not_applicable" && in.Reference != ""
	} else {
		valid = valid && in.Method == "not_applicable" && in.Reference == ""
	}
	if !valid {
		respond.Error(c, 400, "invalid_request", "Provide a valid review action, reason and independent authority-check reference.")
		return
	}
	roles := licenseWriters
	if in.Action != "submit" {
		if !has(c, PermLicenseReview) {
			respond.Forbidden(c, "Licence review permission required.")
			return
		}
		roles = []string{"compliance"}
	}
	var saved licenseReview
	created := false
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := requireRoles(tx, p, partyID, roles); err != nil {
			return err
		}
		if err := lockRegistry(tx, p, partyID, licenceID, "license"); err != nil {
			return err
		}
		err := tx.Select(reviewColumns).Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", p.TenantID, partyID, licenceID, id).First(&saved).Error
		if err == nil {
			if saved.ActorSubject != p.Subject || !reflect.DeepEqual(saved.input(), in) {
				return errConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		_, licence, _, err := h.reviewSources(tx, p, partyID, licenceID)
		if err != nil {
			return err
		}
		premiseID := licence.PremiseID
		if in.Action == "suspend" {
			var previous licenseReview
			if err := tx.Select(reviewColumns).Where("tenant_id=? AND party_id=? AND license_id=?", p.TenantID, partyID, licenceID).Order("version DESC").First(&previous).Error; err != nil {
				return err
			}
			premiseID = previous.PremiseID
		}
		status := map[string]string{"submit": "submitted", "verify": "verified", "reject": "rejected", "suspend": "suspended"}[in.Action]
		saved = licenseReview{ID: id, TenantID: uuid.MustParse(p.TenantID), PartyID: partyID, LicenseID: licenceID, Version: *in.ExpectedVersion + 1, PartyVersion: in.ExpectedPartyVersion, PremiseID: premiseID, PremiseVersion: in.ExpectedPremiseVersion, LicenseVersion: in.ExpectedLicenseVersion, EvidenceID: in.EvidenceID, Action: in.Action, Status: status, Reason: in.Reason, Method: in.Method, Reference: in.Reference, ActorSubject: p.Subject}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		if err := tx.Select(reviewColumns).Where("tenant_id=? AND id=?", p.TenantID, id).First(&saved).Error; err != nil {
			return err
		}
		data, err := json.Marshal(supplycontract.LicenseReviewChangedData{PartyID: partyID.String(), LicenseID: licenceID.String(), ReviewID: id.String(), Version: saved.Version})
		if err != nil {
			return err
		}
		created = true
		return h.bus.Publish(tx, supplycontract.SubjectLicenseReviewChanged, events.Event{Type: supplycontract.SubjectLicenseReviewChanged, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		reviewError(c, err)
		return
	}
	if created {
		respond.Created(c, saved)
	} else {
		respond.OK(c, saved)
	}
}

type reviewState struct {
	Latest          *licenseReview `json:"latest"`
	EffectiveStatus string         `json:"effective_status"`
	TradingApproved bool           `json:"trading_approved"`
	PartyVersion    int64          `json:"party_version"`
	PremiseVersion  int64          `json:"premise_version"`
	LicenseVersion  int64          `json:"license_version"`
	AllowedActions  []string       `json:"allowed_actions"`
}

func (h *handlers) getLicenseReview(c *gin.Context) {
	p, partyID, licenceID, ok := reviewScope(c)
	if !ok {
		return
	}
	state := reviewState{EffectiveStatus: "not_submitted", AllowedActions: []string{}}
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := requireRoles(tx, p, partyID, registryReaders); err != nil {
			return err
		}
		if err := lockRegistry(tx, p, partyID, licenceID, "license"); err != nil {
			return err
		}
		organisation, licence, location, err := h.reviewSources(tx, p, partyID, licenceID)
		if err != nil {
			return err
		}
		state.PartyVersion, state.PremiseVersion, state.LicenseVersion = organisation.Version, location.Version, licence.Version
		var today string
		if err := tx.Raw(`SELECT (clock_timestamp() AT TIME ZONE ?)::date::text`, location.TimeZone).Scan(&today).Error; err != nil {
			return err
		}
		prerequisite := ""
		switch {
		case location.Status != "active":
			prerequisite = "premise_inactive"
		case licence.IssuedOn > today:
			prerequisite = "not_yet_valid"
		case licence.ExpiresOn < today:
			prerequisite = "expired"
		}
		if prerequisite != "" {
			state.EffectiveStatus = prerequisite
		}
		var writers int64
		if err := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND subject=? AND role IN ?", p.TenantID, partyID, p.Subject, licenseWriters).Count(&writers).Error; err != nil {
			return err
		}
		if has(c, PermLicenseSubmit) && writers > 0 && prerequisite == "" {
			state.AllowedActions = append(state.AllowedActions, "submit")
		}
		var latest licenseReview
		err = tx.Select(reviewColumns).Where("tenant_id=? AND party_id=? AND license_id=?", p.TenantID, partyID, licenceID).Order("version DESC").First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return h.reviewReadAudit(tx, p, licenceID)
		}
		if err != nil {
			return err
		}
		state.Latest = &latest
		state.EffectiveStatus = latest.Status
		var proof evidence
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id,status,uploader_subject").Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", p.TenantID, partyID, licenceID, latest.EvidenceID).First(&proof).Error; err != nil {
			return err
		}
		// Uncertain or changed prerequisites never inherit an earlier verification.
		switch {
		case latest.PartyVersion != organisation.Version || latest.LicenseVersion != licence.Version || latest.PremiseID != location.ID || latest.PremiseVersion != location.Version:
			state.EffectiveStatus = "review_required"
		case proof.Status != "available":
			state.EffectiveStatus = "evidence_unavailable"
		case prerequisite != "":
			state.EffectiveStatus = prerequisite
		}
		if state.EffectiveStatus == "verified" || state.EffectiveStatus == "submitted" {
			state.AllowedActions = []string{}
		}
		var authors struct{ Known, Makers int64 }
		if err := tx.Raw(`SELECT count(*) AS known,count(*) FILTER(WHERE actor_subject=?) AS makers FROM (
SELECT actor_subject FROM supply_party_revisions WHERE tenant_id=? AND party_id=? AND version=?
UNION ALL SELECT actor_subject FROM supply_premise_revisions WHERE tenant_id=? AND party_id=? AND premise_id=? AND version=?
UNION ALL SELECT actor_subject FROM supply_license_revisions WHERE tenant_id=? AND party_id=? AND license_id=? AND version=?
) sources WHERE actor_subject IS NOT NULL AND btrim(actor_subject)<>''`, p.Subject, p.TenantID, partyID, latest.PartyVersion, p.TenantID, partyID, latest.PremiseID, latest.PremiseVersion, p.TenantID, partyID, licenceID, latest.LicenseVersion).Scan(&authors).Error; err != nil {
			return err
		}
		// Missing history cannot establish a safe review state or disclose its actions.
		if authors.Known != 3 {
			return gorm.ErrRecordNotFound
		}
		var compliance int64
		if err := tx.Table("supply_party_access").Where("tenant_id=? AND party_id=? AND subject=? AND role='compliance'", p.TenantID, partyID, p.Subject).Count(&compliance).Error; err != nil {
			return err
		}
		if has(c, PermLicenseReview) && compliance > 0 {
			if latest.Status == "verified" {
				state.AllowedActions = append(state.AllowedActions, "suspend")
			}
			if latest.Status == "submitted" && state.EffectiveStatus != "review_required" && authors.Makers == 0 && latest.ActorSubject != p.Subject && proof.UploaderSubject != p.Subject {
				state.AllowedActions = append(state.AllowedActions, "reject")
				if state.EffectiveStatus == "submitted" {
					state.AllowedActions = append(state.AllowedActions, "verify")
				}
			}
		}
		return h.reviewReadAudit(tx, p, licenceID)
	})
	if err != nil {
		reviewError(c, err)
		return
	}
	respond.OK(c, state)
}

func (h *handlers) reviewReadAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'review_read','supply_licenses',?)`, p.TenantID, p.Subject, id).Error
}

func (h *handlers) listLicenseReviews(c *gin.Context, params pagination.Params) ([]licenseReview, error) {
	p, partyID, licenceID, ok := reviewScope(c)
	if !ok {
		return nil, nil
	}
	var rows []licenseReview
	err := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if err := requireRoles(tx, p, partyID, registryReaders); err != nil {
			return err
		}
		var parent license
		if err := tx.Select("id").Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, licenceID).First(&parent).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(reviewColumns).Where("tenant_id=? AND party_id=? AND license_id=?", p.TenantID, partyID, licenceID).Find(&rows).Error; err != nil {
			return err
		}
		return h.reviewReadAudit(tx, p, licenceID)
	})
	if err != nil {
		reviewError(c, err)
	}
	return rows, err
}
