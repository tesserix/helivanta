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

func productCategoryPath(product, suffix string) string {
	return productPath(product) + "/category" + suffix
}

func declareCategory(categoryID, subcategoryID, effective string) string {
	sub := ""
	if subcategoryID != "" {
		sub = fmt.Sprintf(`,"subcategory_id":%q`, subcategoryID)
	}
	return fmt.Sprintf(`{"category_id":%q%s,"effective_from":%q,"reason":"TEST filed with the antibiotics"}`, categoryID, sub, effective)
}

type productCategoryResp struct {
	InForce *struct {
		DeclarationID string  `json:"declaration_id"`
		CategoryID    string  `json:"category_id"`
		SubcategoryID *string `json:"subcategory_id"`
		InForceFrom   string  `json:"in_force_from"`
	} `json:"in_force"`
	History []struct {
		ID            string  `json:"id"`
		CategoryID    string  `json:"category_id"`
		EffectiveFrom string  `json:"effective_from"`
		DeclaredBy    string  `json:"declared_by"`
		Decision      *string `json:"decision"`
		InForceFrom   *string `json:"in_force_from"`
	} `json:"history"`
}

func (w categoryWorld) productCategory(t *testing.T) productCategoryResp {
	t.Helper()
	res := do(w.r, "GET", productCategoryPath(w.product, ""), "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var out productCategoryResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	return out
}

func (w categoryWorld) declare(t *testing.T, who, body string) (string, int, string) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", productCategoryPath(w.product, "/"+id), who, body)
	return id, res.Code, res.Body.String()
}

func (w categoryWorld) review(t *testing.T, who, declaration, decision string) (int, string) {
	t.Helper()
	res := do(w.r, "POST", productCategoryPath(w.product, "/"+declaration+"/review"), who, fmt.Sprintf(`{"decision":%q,"reason":"checked the pack"}`, decision))
	return res.Code, res.Body.String()
}

func (w categoryWorld) categories(t *testing.T) (antibiotics, oral, analgesics string) {
	t.Helper()
	antibiotics, oral, analgesics = uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	for id, body := range map[string]string{antibiotics: category("ANTI", "Antibiotics"), analgesics: category("ANAL", "Analgesics")} {
		code, _, out := w.putCategory(t, "editor", id, body)
		require.Equal(t, http.StatusCreated, code, out)
	}
	code, _, out := w.putCategory(t, "editor", oral, subcategory(antibiotics, "ORAL", "Oral"))
	require.Equal(t, http.StatusCreated, code, out)
	return antibiotics, oral, analgesics
}

func TestAProductsCategoryIsDeclaredThenReviewed(t *testing.T) {
	w := newCategoryWorld(t)
	antibiotics, oral, analgesics := w.categories(t)
	require.Nil(t, w.productCategory(t).InForce, "no category until one is approved")

	first, code, out := w.declare(t, "editor", declareCategory(antibiotics, oral, "2026-11-01"))
	require.Equal(t, http.StatusCreated, code, out)
	require.Nil(t, w.productCategory(t).InForce, "a declaration awaiting review is not in force")
	code, out = w.review(t, "editor", first, "approved")
	require.Equal(t, http.StatusConflict, code, "four eyes: "+out)
	require.Contains(t, out, "four_eyes")
	code, out = w.review(t, "pharmacist", first, "approved")
	require.Equal(t, http.StatusOK, code, out)
	got := w.productCategory(t)
	require.Equal(t, antibiotics, got.InForce.CategoryID)
	require.Equal(t, oral, *got.InForce.SubcategoryID)
	require.Equal(t, "2026-11-01", got.InForce.InForceFrom)
	code, out = w.review(t, "pharmacist", first, "rejected")
	require.Equal(t, http.StatusConflict, code, "reviewed once: "+out)

	// A later declaration approved two days after its date takes effect on
	// approval, not before: the sales of those two days keep antibiotics.
	second, code, out := w.declare(t, "editor", declareCategory(analgesics, "", "2026-11-02"))
	require.Equal(t, http.StatusCreated, code, out)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 4, 6, 30, 0, 0, time.UTC) })
	code, out = w.review(t, "pharmacist", second, "approved")
	require.Equal(t, http.StatusOK, code, out)
	got = w.productCategory(t)
	require.Equal(t, analgesics, got.InForce.CategoryID)
	require.Equal(t, "2026-11-04", got.InForce.InForceFrom)
	require.Nil(t, got.InForce.SubcategoryID)
	var onDay string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT category_id::text FROM pharmacy_product_category_on(?,?,?,'2026-11-03')`, testutil.TenantA, pharmaOutlet, w.product).Scan(&onDay).Error
	}))
	require.Equal(t, antibiotics, onDay, "an approval never reaches back over earlier days")

	// A rejected declaration is history, never in force.
	third, code, out := w.declare(t, "editor", declareCategory(antibiotics, "", "2026-11-04"))
	require.Equal(t, http.StatusCreated, code, out)
	code, out = w.review(t, "pharmacist", third, "rejected")
	require.Equal(t, http.StatusOK, code, out)
	got = w.productCategory(t)
	require.Equal(t, analgesics, got.InForce.CategoryID)
	require.Len(t, got.History, 3)
	require.Equal(t, third, got.History[0].ID, "newest first")
	require.Equal(t, "rejected", *got.History[0].Decision)
	require.Nil(t, got.History[0].InForceFrom)

	// A retired category keeps its products but takes no new one.
	res := do(w.r, "POST", categoryPath("/"+analgesics+"/retire"), "editor", `{}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, analgesics, w.productCategory(t).InForce.CategoryID)
	_, code, out = w.declare(t, "editor", declareCategory(analgesics, "", "2026-11-05"))
	require.Equal(t, http.StatusConflict, code, out)
}

func TestAProductCategoryDeclarationIsChecked(t *testing.T) {
	w := newCategoryWorld(t)
	antibiotics, oral, analgesics := w.categories(t)
	for why, c := range map[string]struct {
		body string
		code int
	}{
		"a subcategory as the category":  {declareCategory(oral, "", "2026-11-01"), http.StatusConflict},
		"another category's subcategory": {declareCategory(analgesics, oral, "2026-11-01"), http.StatusConflict},
		"before today at the outlet":     {declareCategory(antibiotics, "", "2026-10-31"), http.StatusBadRequest},
		"an unknown category":            {declareCategory(uuid.Must(uuid.NewV7()).String(), "", "2026-11-01"), http.StatusBadRequest},
		"no reason":                      {fmt.Sprintf(`{"category_id":%q,"effective_from":"2026-11-01","reason":""}`, antibiotics), http.StatusBadRequest},
		"a malformed date":               {fmt.Sprintf(`{"category_id":%q,"effective_from":"1 Nov","reason":"TEST reason"}`, antibiotics), http.StatusBadRequest},
	} {
		_, code, out := w.declare(t, "editor", c.body)
		require.Equal(t, c.code, code, why+": "+out)
	}
	// A replay is idempotent; the same id with another category is refused.
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", productCategoryPath(w.product, "/"+id), "editor", declareCategory(antibiotics, "", "2026-11-01"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	res = do(w.r, "PUT", productCategoryPath(w.product, "/"+id), "editor", declareCategory(antibiotics, "", "2026-11-01"))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	res = do(w.r, "PUT", productCategoryPath(w.product, "/"+id), "editor", declareCategory(analgesics, "", "2026-11-01"))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())

	_, code, _ := w.declare(t, "clerk", declareCategory(antibiotics, "", "2026-11-01"))
	require.Equal(t, http.StatusForbidden, code)
	code, _ = w.review(t, "clerk", id, "approved")
	require.Equal(t, http.StatusForbidden, code)
	_, code, _ = w.declare(t, "foreign", declareCategory(antibiotics, "", "2026-11-01"))
	require.Equal(t, http.StatusNotFound, code)
	res = do(w.r, "PUT", productCategoryPath(uuid.Must(uuid.NewV7()).String(), "/"+uuid.Must(uuid.NewV7()).String()), "editor", declareCategory(antibiotics, "", "2026-11-01"))
	require.Equal(t, http.StatusNotFound, res.Code, "an unknown product")
}
