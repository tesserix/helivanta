package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #988 slice 1: an outlet's item categories, and each product's reviewed,
// dated category.

var categoryPerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.category.declare", "pharmacy.category.review"}

type categoryWorld struct {
	r       *gin.Engine
	db      *tenantdb.DB
	product string
}

func newCategoryWorld(t *testing.T) categoryWorld {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "clerk": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     categoryPerms,
			"pharmacist": categoryPerms,
			"clerk":      {"pharmacy.catalogue.read", "pharmacy.catalogue.write"},
			"foreign":    categoryPerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, s := range []string{"user-editor", "user-pharmacist", "user-clerk"} {
		grantCatalogue(t, r, s, "editor")
	}
	product := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", productPath(product), "editor", productBody(0, "TEST Amoxil"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return categoryWorld{r: r, db: db, product: product}
}

func categoryPath(suffix string) string { return licencePath("/catalogue/categories" + suffix) }

type categoryResp struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	RetiredAt *string `json:"retired_at"`
}

func (w categoryWorld) putCategory(t *testing.T, who, id, body string) (int, categoryResp, string) {
	t.Helper()
	res := do(w.r, "PUT", categoryPath("/"+id), who, body)
	var c categoryResp
	if res.Code < 300 {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &c))
	}
	return res.Code, c, res.Body.String()
}

func category(code, name string) string {
	return fmt.Sprintf(`{"code":%q,"name":%q}`, code, name)
}

func subcategory(parent, code, name string) string {
	return fmt.Sprintf(`{"parent_id":%q,"code":%q,"name":%q}`, parent, code, name)
}

func TestAnOutletKeepsItsItemCategories(t *testing.T) {
	w := newCategoryWorld(t)
	antibiotics := uuid.Must(uuid.NewV7()).String()
	code, c, out := w.putCategory(t, "editor", antibiotics, category("ANTI", "Antibiotics"))
	require.Equal(t, http.StatusCreated, code, out)
	require.Nil(t, c.ParentID)
	require.Equal(t, "ANTI", c.Code)

	// A replay is idempotent; the same id with a new name renames it.
	code, _, out = w.putCategory(t, "editor", antibiotics, category("ANTI", "Antibiotics"))
	require.Equal(t, http.StatusOK, code, out)
	code, c, out = w.putCategory(t, "editor", antibiotics, category("ANTI", "Anti-infectives"))
	require.Equal(t, http.StatusOK, code, out)
	require.Equal(t, "Anti-infectives", c.Name)
	// Its code and level are fixed.
	code, _, out = w.putCategory(t, "editor", antibiotics, category("ABX", "Anti-infectives"))
	require.Equal(t, http.StatusConflict, code, out)

	oral := uuid.Must(uuid.NewV7()).String()
	code, c, out = w.putCategory(t, "editor", oral, subcategory(antibiotics, "ORAL", "Oral"))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, antibiotics, *c.ParentID)
	code, _, out = w.putCategory(t, "editor", oral, category("ORAL", "Oral"))
	require.Equal(t, http.StatusConflict, code, "a subcategory stays under its category: "+out)
	code, _, out = w.putCategory(t, "editor", uuid.Must(uuid.NewV7()).String(), subcategory(oral, "TAB", "Tablets"))
	require.Equal(t, http.StatusBadRequest, code, "two levels only: "+out)
	code, _, out = w.putCategory(t, "editor", uuid.Must(uuid.NewV7()).String(), category("ANTI", "Again"))
	require.Equal(t, http.StatusConflict, code, "a code is unique among its siblings: "+out)
	code, _, out = w.putCategory(t, "editor", uuid.Must(uuid.NewV7()).String(), subcategory(uuid.Must(uuid.NewV7()).String(), "X", "Orphan"))
	require.Equal(t, http.StatusBadRequest, code, "a subcategory's category exists at this outlet: "+out)
	for _, bad := range []string{`{"code":"","name":"X"}`, `{"code":"lower case","name":"X"}`, `{"code":"OK","name":""}`} {
		code, _, out = w.putCategory(t, "editor", uuid.Must(uuid.NewV7()).String(), bad)
		require.Equal(t, http.StatusBadRequest, code, bad+out)
	}

	// Retiring keeps the category; a retired one takes no new subcategory.
	res := do(w.r, "POST", categoryPath("/"+antibiotics+"/retire"), "editor", `{}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"retired_at":"`)
	res = do(w.r, "POST", categoryPath("/"+antibiotics+"/retire"), "editor", `{}`)
	require.Equal(t, http.StatusOK, res.Code, "retiring again changes nothing")
	code, _, out = w.putCategory(t, "editor", uuid.Must(uuid.NewV7()).String(), subcategory(antibiotics, "INJ", "Injections"))
	require.Equal(t, http.StatusConflict, code, out)

	res = do(w.r, "GET", categoryPath(""), "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var list struct{ Data []categoryResp }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list.Data, 2, "a retired category is still listed")

	require.Error(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM pharmacy_item_categories WHERE id=?`, antibiotics).Error
	}), "a category is never deleted")
}

func TestOnlyAnOutletEditorWithTheGrantKeepsCategories(t *testing.T) {
	w := newCategoryWorld(t)
	code, _, _ := w.putCategory(t, "clerk", uuid.Must(uuid.NewV7()).String(), category("ANTI", "Antibiotics"))
	require.Equal(t, http.StatusForbidden, code)
	code, _, _ = w.putCategory(t, "foreign", uuid.Must(uuid.NewV7()).String(), category("ANTI", "Antibiotics"))
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = w.putCategory(t, "editor", "nope", category("ANTI", "Antibiotics"))
	require.Equal(t, http.StatusNotFound, code)
}
