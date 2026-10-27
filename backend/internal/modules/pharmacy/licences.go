package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Outlet drug licences (#1386 A1): an outlet sells only while a licence
// reviewed by a second person is valid on the outlet's own date.

var errLicenceReviewed = errors.New("licence already reviewed")

type outletLicence struct {
	ID                     uuid.UUID `gorm:"column:id" json:"id"`
	TenantID               uuid.UUID `gorm:"column:tenant_id" json:"-"`
	OutletID               uuid.UUID `gorm:"column:outlet_id" json:"outlet_id"`
	LicenceKind            string    `gorm:"column:licence_kind" json:"licence_kind"`
	FormLabel              string    `gorm:"column:form_label" json:"form_label"`
	LicenceNumber          string    `gorm:"column:licence_number" json:"licence_number"`
	IssuingAuthority       string    `gorm:"column:issuing_authority" json:"issuing_authority"`
	ValidFrom              string    `gorm:"column:valid_from" json:"valid_from"`
	ValidUntil             string    `gorm:"column:valid_until" json:"valid_until"`
	PharmacistName         string    `gorm:"column:pharmacist_name" json:"pharmacist_name"`
	PharmacistRegistration string    `gorm:"column:pharmacist_registration" json:"pharmacist_registration"`
	EnteredBy              string    `gorm:"column:entered_by" json:"entered_by"`
	CreatedAt              time.Time `gorm:"column:created_at" json:"created_at"`
	// Review is the decision, or nil while the licence awaits one.
	Decision   *string    `gorm:"column:decision" json:"decision"`
	Reason     *string    `gorm:"column:reason" json:"decision_reason"`
	ReviewedBy *string    `gorm:"column:reviewed_by" json:"reviewed_by"`
	ReviewedAt *time.Time `gorm:"column:reviewed_at" json:"reviewed_at"`
}

func (r outletLicence) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const licenceQuery = `SELECT l.id, l.tenant_id, l.outlet_id, l.licence_kind, l.form_label, l.licence_number, l.issuing_authority,
	l.valid_from::text AS valid_from, l.valid_until::text AS valid_until, l.pharmacist_name, l.pharmacist_registration, l.entered_by, l.created_at,
	r.decision, r.reason, r.reviewed_by, r.created_at AS reviewed_at
	FROM pharmacy_outlet_licences l LEFT JOIN pharmacy_outlet_licence_reviews r
	ON r.tenant_id = l.tenant_id AND r.outlet_id = l.outlet_id AND r.licence_id = l.id`

type licenceInput struct {
	LicenceKind            string `json:"licence_kind" binding:"required,oneof=retail wholesale"`
	FormLabel              string `json:"form_label" binding:"required,max=40"`
	LicenceNumber          string `json:"licence_number" binding:"required,max=100"`
	IssuingAuthority       string `json:"issuing_authority" binding:"required,max=200"`
	ValidFrom              string `json:"valid_from" binding:"required"`
	ValidUntil             string `json:"valid_until" binding:"required"`
	PharmacistName         string `json:"pharmacist_name" binding:"required,max=200"`
	PharmacistRegistration string `json:"pharmacist_registration" binding:"required,max=100"`
}

func (in *licenceInput) normalise() bool {
	for _, f := range []*string{&in.FormLabel, &in.LicenceNumber, &in.IssuingAuthority, &in.PharmacistName, &in.PharmacistRegistration} {
		*f = strings.TrimSpace(*f)
		if *f == "" {
			return false
		}
	}
	from, err1 := time.Parse(time.DateOnly, in.ValidFrom)
	until, err2 := time.Parse(time.DateOnly, in.ValidUntil)
	return err1 == nil && err2 == nil && from.Year() >= 1900 && !until.Before(from) && until.Year() <= 9999
}

func (in licenceInput) digest() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{in.LicenceKind, in.FormLabel, in.LicenceNumber, in.IssuingAuthority,
		in.ValidFrom, in.ValidUntil, in.PharmacistName, in.PharmacistRegistration}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func licenceRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/licences/:id", PermLicenceManage, h.putLicence)
	s.POST("/licences/:id/review", PermLicenceReview, h.reviewLicence)
	platform.ListRoute(s, "/licences", PermCatalogueRead, h.listLicences)
	s.GET("/licence-status", PermCatalogueRead, h.licenceStatus)
}

func licenceError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_licence_four_eyes":
		respond.Error(c, http.StatusConflict, "four_eyes", "a licence is reviewed by someone other than who entered it")
	case errors.Is(err, errLicenceReviewed):
		respond.Conflict(c, "this licence has already been reviewed")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this licence id was already used with different details")
	default:
		catalogueError(c, err)
	}
}

func (h *catalogueHandlers) putLicence(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in licenceInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !in.normalise() {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid licence: every field is required and valid_until is on or after valid_from")
		return
	}
	digest := in.digest()
	var result outletLicence
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		insert := tx.Exec(`INSERT INTO pharmacy_outlet_licences (id, tenant_id, outlet_id, licence_kind, form_label, licence_number, issuing_authority,
			valid_from, valid_until, pharmacist_name, pharmacist_registration, intent_sha256, entered_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, id, p.TenantID, outlet, in.LicenceKind, in.FormLabel, in.LicenceNumber,
			in.IssuingAuthority, in.ValidFrom, in.ValidUntil, in.PharmacistName, in.PharmacistRegistration, digest, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		created = insert.RowsAffected == 1
		if !created {
			// A retry of the same licence replays; anything else is a conflict.
			var won []struct{ IntentSHA256, EnteredBy string }
			if err := tx.Raw(`SELECT intent_sha256, entered_by FROM pharmacy_outlet_licences WHERE tenant_id=? AND outlet_id=? AND id=?`,
				p.TenantID, outlet, id).Scan(&won).Error; err != nil {
				return err
			}
			if len(won) != 1 || won[0].IntentSHA256 != digest || won[0].EnteredBy != p.Subject {
				return errStockIdentity
			}
		}
		return tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND l.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		licenceError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
		return
	}
	respond.OK(c, result)
}

type licenceReviewInput struct {
	Decision string `json:"decision" binding:"required,oneof=approved rejected"`
	Reason   string `json:"reason" binding:"required,max=500"`
}

func (h *catalogueHandlers) reviewLicence(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in licenceReviewInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a review needs a reason")
		return
	}
	var result outletLicence
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_outlet_licences WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_outlet_licence_reviews (tenant_id, outlet_id, licence_id, decision, reason, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, id, in.Decision, in.Reason, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errLicenceReviewed
		}
		return tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND l.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		licenceError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) listLicences(c *gin.Context, params pagination.Params) ([]outletLicence, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []outletLicence
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx.Table("(?) AS licences", tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=?`, p.TenantID, outlet)), params)
		return q.Find(&rows).Error
	})
	if err != nil {
		licenceError(c, err)
		return nil, nil
	}
	return rows, nil
}

type licenceStatusResponse struct {
	InForce bool `json:"in_force"`
	// Today is the outlet's date the answer is for.
	Today   string         `json:"today"`
	Licence *outletLicence `json:"licence"`
	Reason  string         `json:"reason"`
}

// licenceStatus says whether the outlet may sell today, and why not.
func (h *catalogueHandlers) licenceStatus(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var out licenceStatusResponse
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var o pharmacyOutlet
		if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&o).Error; err != nil {
			return err
		}
		today, err := outletToday(o.Timezone)
		if err != nil {
			return err
		}
		out.Today = today
		var rows []outletLicence
		if err := tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND r.decision='approved' AND ? BETWEEN l.valid_from AND l.valid_until
			ORDER BY l.valid_until DESC, l.id DESC LIMIT 1`, p.TenantID, outlet, today).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 1 {
			out.InForce, out.Licence = true, &rows[0]
			return nil
		}
		var pending, approved int64
		if err := tx.Raw(`SELECT
			count(*) FILTER (WHERE r.licence_id IS NULL) AS pending,
			count(*) FILTER (WHERE r.decision='approved') AS approved
			FROM pharmacy_outlet_licences l LEFT JOIN pharmacy_outlet_licence_reviews r
			ON r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id AND r.licence_id=l.id
			WHERE l.tenant_id=? AND l.outlet_id=?`, p.TenantID, outlet).Row().Scan(&pending, &approved); err != nil {
			return err
		}
		switch {
		case pending > 0:
			out.Reason = "a licence is awaiting review by a second person"
		case approved > 0:
			out.Reason = "no approved licence is valid today"
		default:
			out.Reason = "no licence has been recorded"
		}
		return nil
	})
	if err != nil {
		licenceError(c, err)
		return
	}
	respond.OK(c, out)
}
