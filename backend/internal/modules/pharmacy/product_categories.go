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
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

// A product's category (#988 slice 1): declared with the date it takes
// effect and a reason, and approved by someone else. An approval is in force
// from the later of that date and the outlet's date when it was approved, so
// it never reaches back over sales already made.

type productCategoryInput struct {
	CategoryID    uuid.UUID  `json:"category_id" binding:"required"`
	SubcategoryID *uuid.UUID `json:"subcategory_id"`
	EffectiveFrom string     `json:"effective_from" binding:"required"`
	Reason        string     `json:"reason" binding:"required"`
}

type productCategoryDeclaration struct {
	ID            uuid.UUID  `gorm:"column:id" json:"id"`
	CategoryID    uuid.UUID  `gorm:"column:category_id" json:"category_id"`
	SubcategoryID *uuid.UUID `gorm:"column:subcategory_id" json:"subcategory_id"`
	EffectiveFrom string     `gorm:"column:effective_from" json:"effective_from"`
	Reason        string     `gorm:"column:reason" json:"reason"`
	DeclaredBy    string     `gorm:"column:declared_by" json:"declared_by"`
	CreatedAt     time.Time  `gorm:"column:created_at" json:"created_at"`
	Decision      *string    `gorm:"column:decision" json:"decision"`
	ReviewReason  *string    `gorm:"column:review_reason" json:"decision_reason"`
	ReviewedBy    *string    `gorm:"column:reviewed_by" json:"reviewed_by"`
	InForceFrom   *string    `gorm:"column:in_force_from" json:"in_force_from"`
}

const productCategoryQuery = `SELECT d.id, d.category_id, d.subcategory_id, to_char(d.effective_from,'YYYY-MM-DD') AS effective_from, d.reason, d.declared_by, d.created_at,
 r.decision, r.reason AS review_reason, r.reviewed_by, to_char(r.in_force_from,'YYYY-MM-DD') AS in_force_from
 FROM pharmacy_product_category_declarations d LEFT JOIN pharmacy_product_category_reviews r
 ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id`

// maxCategoryHistory bounds the history a product shows; a product is
// re-categorised rarely, and the audit log keeps every change.
const maxCategoryHistory = 50

type productCategoryInForce struct {
	DeclarationID uuid.UUID  `gorm:"column:declaration_id" json:"declaration_id"`
	CategoryID    uuid.UUID  `gorm:"column:category_id" json:"category_id"`
	SubcategoryID *uuid.UUID `gorm:"column:subcategory_id" json:"subcategory_id"`
	InForceFrom   string     `gorm:"column:in_force_from" json:"in_force_from"`
}

func productCategoryRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/catalogue/products/:id/category/:declaration", PermCategoryDeclare, h.putProductCategory)
	s.POST("/catalogue/products/:id/category/:declaration/review", PermCategoryReview, h.reviewProductCategory)
	s.GET("/catalogue/products/:id/category", PermCatalogueRead, h.getProductCategory)
}

var (
	errUnknownCategory      = errors.New("unknown item category")
	errEffectiveBeforeToday = errors.New("effective_from before the outlet's today")
)

func productCategoryError(c *gin.Context, err error) {
	switch {
	case violated(err, "pharmacy_product_category_four_eyes"):
		respond.Error(c, http.StatusConflict, "four_eyes", "a product's category is reviewed by someone other than who declared it")
	case violated(err, "pharmacy_product_category_live"):
		respond.Conflict(c, "a product goes in a category in use, and a subcategory only of that category")
	case errors.Is(err, errUnknownCategory):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "category_id and subcategory_id are categories at this outlet")
	case errors.Is(err, errEffectiveBeforeToday):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "effective_from cannot be before today at the outlet")
	case errors.Is(err, errDeclarationReviewed):
		respond.Conflict(c, "this declaration has already been reviewed")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this declaration id was already used with different details")
	default:
		catalogueError(c, err)
	}
}

func (h *catalogueHandlers) putProductCategory(c *gin.Context) {
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
	var in productCategoryInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if _, err := time.Parse(time.DateOnly, in.EffectiveFrom); err != nil || len([]rune(in.Reason)) < 5 || !prescriptionText(in.Reason, 300) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a category declaration has an effective_from date (YYYY-MM-DD) and a reason of 5 to 300 characters")
		return
	}
	ids := []uuid.UUID{in.CategoryID}
	sub := ""
	if in.SubcategoryID != nil {
		ids, sub = append(ids, *in.SubcategoryID), in.SubcategoryID.String()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{product.String(), in.CategoryID.String(), sub, in.EffectiveFrom, in.Reason}, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var result productCategoryDeclaration
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		var known int64
		if err := tx.Raw(`SELECT count(DISTINCT id) FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=? AND id IN ?`, p.TenantID, outlet, ids).Scan(&known).Error; err != nil {
			return err
		}
		if known != int64(len(ids)) {
			return errUnknownCategory
		}
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		// A date already passed would re-file sales already made.
		if in.EffectiveFrom < today {
			return errEffectiveBeforeToday
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_category_declarations (id,tenant_id,outlet_id,product_id,category_id,subcategory_id,effective_from,reason,intent_sha256,declared_by)
 VALUES (?,?,?,?,?,?,?::date,?,?,?) ON CONFLICT DO NOTHING`, id, p.TenantID, outlet, product, in.CategoryID, in.SubcategoryID, in.EffectiveFrom, in.Reason, digest, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		created = insert.RowsAffected == 1
		if !created {
			var won []struct{ IntentSHA256, DeclaredBy string }
			if err := tx.Raw(`SELECT intent_sha256, declared_by FROM pharmacy_product_category_declarations WHERE tenant_id=? AND outlet_id=? AND id=?`,
				p.TenantID, outlet, id).Scan(&won).Error; err != nil {
				return err
			}
			if len(won) != 1 || won[0].IntentSHA256 != digest || won[0].DeclaredBy != p.Subject {
				return errStockIdentity
			}
		}
		return tx.Raw(productCategoryQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	switch {
	case err != nil:
		productCategoryError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) reviewProductCategory(c *gin.Context) {
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
	var result productCategoryDeclaration
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var effective []string
		if err := tx.Raw(`SELECT to_char(effective_from,'YYYY-MM-DD') FROM pharmacy_product_category_declarations WHERE tenant_id=? AND outlet_id=? AND product_id=? AND id=?`,
			p.TenantID, outlet, product, id).Scan(&effective).Error; err != nil {
			return err
		}
		if len(effective) == 0 {
			return gorm.ErrRecordNotFound
		}
		var inForce *string
		if in.Decision == "approved" {
			today, err := outletLocalDate(tx, p.TenantID, outlet)
			if err != nil {
				return err
			}
			// Never before the outlet's today: an approval does not reach back.
			from := max(effective[0], today)
			inForce = &from
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_category_reviews (tenant_id,outlet_id,declaration_id,decision,reason,in_force_from,reviewed_by)
 VALUES (?,?,?,?,?,?::date,?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, id, in.Decision, in.Reason, inForce, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errDeclarationReviewed
		}
		return tx.Raw(productCategoryQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		productCategoryError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getProductCategory(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	history := []productCategoryDeclaration{}
	var inForce []productCategoryInForce
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		if err := tx.Raw(`SELECT declaration_id, category_id, subcategory_id, to_char(in_force_from,'YYYY-MM-DD') AS in_force_from FROM pharmacy_product_category_on(?,?,?,?::date)`,
			p.TenantID, outlet, product, today).Scan(&inForce).Error; err != nil {
			return err
		}
		return tx.Raw(productCategoryQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.product_id=? ORDER BY d.created_at DESC, d.id DESC LIMIT ?`,
			p.TenantID, outlet, product, maxCategoryHistory).Scan(&history).Error
	})
	if err != nil {
		productCategoryError(c, err)
		return
	}
	var current *productCategoryInForce
	if len(inForce) == 1 {
		current = &inForce[0]
	}
	respond.OK(c, gin.H{"in_force": current, "history": history})
}
