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
)

// Outlet-declared schedule class (#1386 A2): the Drugs Rules class and NDPS
// control a product is handled under until the drug master is loaded.

var errDeclarationReviewed = errors.New("schedule declaration already reviewed")

type scheduleDeclaration struct {
	ID              uuid.UUID  `gorm:"column:id" json:"id"`
	ProductID       uuid.UUID  `gorm:"column:product_id" json:"product_id"`
	RegulatoryClass string     `gorm:"column:regulatory_class" json:"regulatory_class"`
	NDPS            bool       `gorm:"column:ndps" json:"ndps"`
	Source          string     `gorm:"column:source" json:"source"`
	DeclaredBy      string     `gorm:"column:declared_by" json:"declared_by"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"created_at"`
	Decision        *string    `gorm:"column:decision" json:"decision"`
	Reason          *string    `gorm:"column:reason" json:"decision_reason"`
	ReviewedBy      *string    `gorm:"column:reviewed_by" json:"reviewed_by"`
	ReviewedAt      *time.Time `gorm:"column:reviewed_at" json:"reviewed_at"`
}

const declarationQuery = `SELECT d.id, d.product_id, d.regulatory_class, d.ndps, d.source, d.declared_by, d.created_at,
	r.decision, r.reason, r.reviewed_by, r.created_at AS reviewed_at
	FROM pharmacy_product_schedule_declarations d LEFT JOIN pharmacy_product_schedule_reviews r
	ON r.tenant_id = d.tenant_id AND r.outlet_id = d.outlet_id AND r.declaration_id = d.id`

type scheduleInput struct {
	RegulatoryClass string `json:"regulatory_class" binding:"required,oneof=none G H H1 X"`
	// A pointer, so a missing NDPS flag is refused rather than read as false.
	NDPS   *bool  `json:"ndps" binding:"required"`
	Source string `json:"source" binding:"required,max=300"`
}

func scheduleRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/catalogue/products/:id/schedule/:declaration", PermScheduleDeclare, h.putScheduleDeclaration)
	s.POST("/catalogue/products/:id/schedule/:declaration/review", PermScheduleReview, h.reviewScheduleDeclaration)
	s.GET("/catalogue/products/:id/schedule", PermCatalogueRead, h.getSchedule)
}

func scheduleError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && strings.Contains(pg.Message, "reviewed by someone other than who declared it"):
		respond.Error(c, http.StatusConflict, "four_eyes", "a schedule class is reviewed by someone other than who declared it")
	case errors.Is(err, errDeclarationReviewed):
		respond.Conflict(c, "this declaration has already been reviewed")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this declaration id was already used with different details")
	default:
		catalogueError(c, err)
	}
}

func scheduleProduct(c *gin.Context) (uuid.UUID, bool) {
	product, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "product")
		return uuid.Nil, false
	}
	return product, true
}

func (h *catalogueHandlers) putScheduleDeclaration(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	id, ok := declarationID(c)
	if !ok {
		return
	}
	var in scheduleInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a declaration names its source, such as the pack label")
		return
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{product.String(), in.RegulatoryClass, boolText(*in.NDPS), in.Source}, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var result scheduleDeclaration
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_schedule_declarations (id, tenant_id, outlet_id, product_id, regulatory_class, ndps, source, intent_sha256, declared_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, id, p.TenantID, outlet, product, in.RegulatoryClass, *in.NDPS, in.Source, digest, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		created = insert.RowsAffected == 1
		if !created {
			var won []struct{ IntentSHA256, DeclaredBy string }
			if err := tx.Raw(`SELECT intent_sha256, declared_by FROM pharmacy_product_schedule_declarations WHERE tenant_id=? AND outlet_id=? AND id=?`,
				p.TenantID, outlet, id).Scan(&won).Error; err != nil {
				return err
			}
			if len(won) != 1 || won[0].IntentSHA256 != digest || won[0].DeclaredBy != p.Subject {
				return errStockIdentity
			}
		}
		return tx.Raw(declarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		scheduleError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
		return
	}
	respond.OK(c, result)
}

func declarationID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("declaration"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "schedule declaration")
		return uuid.Nil, false
	}
	return id, true
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (h *catalogueHandlers) reviewScheduleDeclaration(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	id, ok := declarationID(c)
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
	var result scheduleDeclaration
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_product_schedule_declarations WHERE tenant_id=? AND outlet_id=? AND product_id=? AND id=?`,
			p.TenantID, outlet, product, id).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id, outlet_id, declaration_id, decision, reason, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, id, in.Decision, in.Reason, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errDeclarationReviewed
		}
		return tx.Raw(declarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		scheduleError(c, err)
		return
	}
	respond.OK(c, result)
}

type scheduleState struct {
	// InForce is the class the product may be handled under, or nil: none
	// approved, or a newer declaration awaits review or was rejected.
	InForce *scheduleDeclaration `json:"in_force"`
	// Origin says where the class came from; until the drug master link
	// exists it is always the outlet's own declaration (decision 2).
	Origin  string                `json:"origin"`
	Reason  string                `json:"reason"`
	History []scheduleDeclaration `json:"history"`
}

// historyLimit bounds the declarations returned with the current state.
// Declarations change only when a product is reclassified.
const historyLimit = 50

func (h *catalogueHandlers) getSchedule(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	out := scheduleState{Origin: "declared_by_outlet", History: []scheduleDeclaration{}}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Raw(declarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.product_id=? ORDER BY d.seq DESC LIMIT ?`,
			p.TenantID, outlet, product, historyLimit).Scan(&out.History).Error; err != nil {
			return err
		}
		switch {
		case len(out.History) == 0:
			out.Reason = "no schedule class has been declared"
		case out.History[0].Decision == nil:
			out.Reason = "the latest declaration awaits review by a second person"
		case *out.History[0].Decision == "rejected":
			out.Reason = "the latest declaration was rejected"
		default:
			out.InForce = &out.History[0]
		}
		return nil
	})
	if err != nil {
		scheduleError(c, err)
		return
	}
	respond.OK(c, out)
}
