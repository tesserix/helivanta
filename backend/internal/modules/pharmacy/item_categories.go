package pharmacy

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Item categories (#988 slice 1): an outlet's categories and subcategories,
// and each product's reviewed, dated category. See item_categories.sql.

var categoryCode = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,19}$`)

var errCategoryFixed = errors.New("item category code or level differs")

type itemCategory struct {
	ID        uuid.UUID  `gorm:"column:id" json:"id"`
	ParentID  *uuid.UUID `gorm:"column:parent_id" json:"parent_id"`
	Code      string     `gorm:"column:code" json:"code"`
	Name      string     `gorm:"column:name" json:"name"`
	RetiredAt *time.Time `gorm:"column:retired_at" json:"retired_at"`
	CreatedBy string     `gorm:"column:created_by" json:"created_by"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (c itemCategory) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

const itemCategoryColumns = "id,parent_id,code,name,retired_at,created_by,created_at"

type itemCategoryInput struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Code     string     `json:"code" binding:"required"`
	Name     string     `json:"name" binding:"required"`
}

func itemCategoryRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/catalogue/categories/:id", PermCategoryDeclare, h.putItemCategory)
	s.POST("/catalogue/categories/:id/retire", PermCategoryDeclare, h.retireItemCategory)
	platform.ListRoute(s, "/catalogue/categories", PermCatalogueRead, h.listItemCategories)
	productCategoryRoutes(s, h)
}

func itemCategoryError(c *gin.Context, err error) {
	switch {
	case violated(err, "pharmacy_item_category_code"):
		respond.Conflict(c, "another category here already has that code")
	case violated(err, "pharmacy_item_category_levels"):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a subcategory belongs to a category, not to another subcategory")
	case violated(err, "pharmacy_item_category_retired"):
		respond.Conflict(c, "a retired category takes no new subcategory")
	case errors.Is(err, errCategoryFixed), violated(err, "pharmacy_item_category_fixed"):
		respond.Conflict(c, "a category's code and level are fixed; create another category instead")
	case errors.Is(err, errUnknownParent):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "parent_id is a category at this outlet")
	default:
		catalogueError(c, err)
	}
}

var errUnknownParent = errors.New("unknown parent category")

func (h *catalogueHandlers) putItemCategory(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in itemCategoryInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !categoryCode.MatchString(in.Code) || in.Name == "" || len([]rune(in.Name)) > 100 || !prescriptionText(in.Name, 100) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a category has a code of capital letters, digits, - or _ (up to 20) and a name of up to 100 characters")
		return
	}
	var result itemCategory
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if in.ParentID != nil {
			var parents int64
			if err := tx.Raw(`SELECT count(*) FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, *in.ParentID).Scan(&parents).Error; err != nil {
				return err
			}
			if parents == 0 {
				return errUnknownParent
			}
		}
		var held []itemCategory
		if err := tx.Raw(`SELECT `+itemCategoryColumns+` FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=? AND id=? FOR UPDATE`, p.TenantID, outlet, id).Scan(&held).Error; err != nil {
			return err
		}
		if len(held) == 1 {
			// The same id renames; it never moves or recodes a category.
			if held[0].Code != in.Code || !sameParent(held[0].ParentID, in.ParentID) {
				return errCategoryFixed
			}
			if held[0].Name != in.Name {
				if err := tx.Exec(`UPDATE pharmacy_item_categories SET name=? WHERE tenant_id=? AND outlet_id=? AND id=?`, in.Name, p.TenantID, outlet, id).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.Exec(`INSERT INTO pharmacy_item_categories (id,tenant_id,outlet_id,parent_id,code,name,created_by) VALUES (?,?,?,?,?,?,?)`,
				id, p.TenantID, outlet, in.ParentID, in.Code, in.Name, p.Subject).Error; err != nil {
				return err
			}
			created = true
		}
		return tx.Raw(`SELECT `+itemCategoryColumns+` FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	switch {
	case err != nil:
		itemCategoryError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func sameParent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// retireItemCategory keeps the category and its history; it can no longer be
// newly assigned or take a subcategory. Retiring again changes nothing.
func (h *catalogueHandlers) retireItemCategory(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var result itemCategory
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE pharmacy_item_categories SET retired_at=now() WHERE tenant_id=? AND outlet_id=? AND id=? AND retired_at IS NULL`, p.TenantID, outlet, id)
		if res.Error != nil {
			return res.Error
		}
		found := tx.Raw(`SELECT `+itemCategoryColumns+` FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&result)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		itemCategoryError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) listItemCategories(c *gin.Context, params pagination.Params) ([]itemCategory, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []itemCategory
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyOrderedKeyset(tx.Table(`(SELECT `+itemCategoryColumns+` FROM pharmacy_item_categories WHERE tenant_id=? AND outlet_id=?) e`, p.TenantID, outlet),
			params, tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
