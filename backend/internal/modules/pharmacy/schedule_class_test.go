package pharmacy_test

import (
	"encoding/json"
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

// #1386 A2: a product is handled under the class of its latest declaration,
// and only once a second person approved it.

var schedulePerms = []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.schedule.declare", "pharmacy.schedule.review"}

func scheduleHarness(t *testing.T) (*gin.Engine, *tenantdb.DB, string) {
	t.Helper()
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "pharmacist": testutil.TenantA, "clerk": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor":     schedulePerms,
			"pharmacist": schedulePerms,
			"clerk":      {"pharmacy.catalogue.read", "pharmacy.catalogue.write"},
			"foreign":    schedulePerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	for _, s := range []string{"user-editor", "user-pharmacist", "user-clerk"} {
		grantCatalogue(t, r, s, "editor")
	}
	product := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", productPath(product), "editor", productBody(0, "TEST Amoxil"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return r, db, product
}

func schedulePath(product, suffix string) string {
	return productPath(product) + "/schedule" + suffix
}

type scheduleResp struct {
	InForce *struct {
		ID              string `json:"id"`
		RegulatoryClass string `json:"regulatory_class"`
		NDPS            bool   `json:"ndps"`
	} `json:"in_force"`
	Origin  string `json:"origin"`
	Reason  string `json:"reason"`
	History []struct {
		ID string `json:"id"`
	} `json:"history"`
}

func scheduleOf(t *testing.T, r *gin.Engine, product string) scheduleResp {
	t.Helper()
	w := do(r, "GET", schedulePath(product, ""), "pharmacist", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s scheduleResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func declare(t *testing.T, r *gin.Engine, product, class, token string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", schedulePath(product, "/"+id), token, `{"regulatory_class":"`+class+`","ndps":false,"source":"TEST pack label"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return id
}

func review(r *gin.Engine, product, id, token, decision string) int {
	return do(r, "POST", schedulePath(product, "/"+id+"/review"), token, `{"decision":"`+decision+`","reason":"checked the pack"}`).Code
}

func scheduleInForce(t *testing.T, db *tenantdb.DB, product string) []string {
	var classes []string
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-pharmacist',true), set_config('app.pharmacy_outlet',?,true), set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT regulatory_class FROM pharmacy_product_schedule_in_force(?, ?, ?)`, testutil.TenantA, pharmaOutlet, product).Scan(&classes).Error
	}))
	return classes
}

func TestAScheduleClassNeedsASecondPerson(t *testing.T) {
	r, db, product := scheduleHarness(t)
	s := scheduleOf(t, r, product)
	require.Nil(t, s.InForce)
	require.Equal(t, "no schedule class has been declared", s.Reason)
	require.Equal(t, "declared_by_outlet", s.Origin)

	id := declare(t, r, product, "H", "editor")
	require.Equal(t, "the latest declaration awaits review by a second person", scheduleOf(t, r, product).Reason)
	require.Empty(t, scheduleInForce(t, db, product))

	require.Equal(t, http.StatusConflict, review(r, product, id, "editor", "approved"), "self-review")
	require.Empty(t, scheduleInForce(t, db, product))
	require.Equal(t, http.StatusForbidden, review(r, product, id, "clerk", "approved"))
	require.Equal(t, http.StatusOK, review(r, product, id, "pharmacist", "approved"))
	require.Equal(t, http.StatusConflict, review(r, product, id, "pharmacist", "rejected"), "one decision")

	s = scheduleOf(t, r, product)
	require.NotNil(t, s.InForce)
	require.Equal(t, "H", s.InForce.RegulatoryClass)
	require.Equal(t, []string{"H"}, scheduleInForce(t, db, product))
}

// Review focus 3: a newer declaration awaiting review, or rejected, blocks
// the product even though an older one was approved.
func TestANewerDeclarationBlocksAnOlderApproval(t *testing.T) {
	r, db, product := scheduleHarness(t)
	older := declare(t, r, product, "H", "editor")
	require.Equal(t, http.StatusOK, review(r, product, older, "pharmacist", "approved"))

	newer := declare(t, r, product, "H1", "editor")
	require.Nil(t, scheduleOf(t, r, product).InForce)
	require.Empty(t, scheduleInForce(t, db, product), "a pending stricter class is not bypassed by an older approval")

	require.Equal(t, http.StatusOK, review(r, product, newer, "pharmacist", "rejected"))
	s := scheduleOf(t, r, product)
	require.Nil(t, s.InForce)
	require.Equal(t, "the latest declaration was rejected", s.Reason)
	require.Len(t, s.History, 2)
	require.Empty(t, scheduleInForce(t, db, product), "a rejected latest declaration leaves no class")

	fixed := declare(t, r, product, "H1", "pharmacist")
	require.Equal(t, http.StatusOK, review(r, product, fixed, "editor", "approved"))
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestScheduleDeclarationInputAndScope(t *testing.T) {
	r, _, product := scheduleHarness(t)
	for _, body := range []string{
		`{"regulatory_class":"Z","ndps":false,"source":"label"}`,
		`{"regulatory_class":"H","source":"label"}`,
		`{"regulatory_class":"H","ndps":false,"source":"  "}`,
	} {
		require.Equal(t, http.StatusBadRequest, do(r, "PUT", schedulePath(product, "/"+uuid.Must(uuid.NewV7()).String()), "editor", body).Code, body)
	}
	id := uuid.Must(uuid.NewV7()).String()
	body := `{"regulatory_class":"X","ndps":true,"source":"TEST label"}`
	require.Equal(t, http.StatusCreated, do(r, "PUT", schedulePath(product, "/"+id), "editor", body).Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", schedulePath(product, "/"+id), "editor", body).Code, "replay")
	require.Equal(t, http.StatusConflict, do(r, "PUT", schedulePath(product, "/"+id), "editor", `{"regulatory_class":"X","ndps":false,"source":"TEST label"}`).Code)
	require.Equal(t, http.StatusNotFound, do(r, "PUT", schedulePath(uuid.Must(uuid.NewV7()).String(), "/"+uuid.Must(uuid.NewV7()).String()), "editor", body).Code, "no such product")
	require.Equal(t, http.StatusNotFound, review(r, uuid.Must(uuid.NewV7()).String(), id, "pharmacist", "approved"), "a declaration under another product")
	require.Equal(t, http.StatusNotFound, do(r, "GET", schedulePath(product, ""), "foreign", "").Code)
}
