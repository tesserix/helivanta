package pharmacy_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1681: inside the tenant a schedule class can only be made stricter. A
// lower class needs a Helivanta decision, never a second tenant member.

func declareWith(t *testing.T, r *gin.Engine, product, class string, ndps bool, token string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", schedulePath(product, "/"+id), token, `{"regulatory_class":"`+class+`","ndps":`+strconv.FormatBool(ndps)+`,"source":"TEST pack label"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return id
}

func approved(t *testing.T, r *gin.Engine, product, class string, ndps bool) string {
	t.Helper()
	id := declareWith(t, r, product, class, ndps, "editor")
	require.Equal(t, http.StatusOK, review(r, product, id, "pharmacist", "approved"))
	return id
}

type loweringHistory struct {
	InForce *struct {
		ID              string `json:"id"`
		RegulatoryClass string `json:"regulatory_class"`
	} `json:"in_force"`
	Reason  string `json:"reason"`
	History []struct {
		ID     string `json:"id"`
		Lowers bool   `json:"lowers"`
	} `json:"history"`
}

func loweringState(t *testing.T, r *gin.Engine, product string) loweringHistory {
	t.Helper()
	w := do(r, "GET", schedulePath(product, ""), "pharmacist", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s loweringHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func lowersOf(t *testing.T, db *tenantdb.DB, declaration string) bool {
	t.Helper()
	var lowers []bool
	require.NoError(t, scheduleSQL(t, db, "user-pharmacist", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT lowers FROM pharmacy_product_schedule_declarations WHERE id=?`, declaration).Scan(&lowers).Error
	}))
	require.Len(t, lowers, 1)
	return lowers[0]
}

// scheduleSQL runs fn as subject inside the outlet's tenant session, so a
// test can reach the database the way a future route or a direct write would.
func scheduleSQL(t *testing.T, db *tenantdb.DB, subject string, fn func(*gorm.DB) error) error {
	t.Helper()
	return db.WithTenant(tenantdb.WithActor(t.Context(), subject), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true), set_config('app.pharmacy_outlet',?,true), set_config('app.pharmacy_id',?,true)`, subject, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func requireConstraint(t *testing.T, err error, constraint string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, constraint, pg.ConstraintName, pg.Message)
}

func TestPharmacistAndOwnerCannotMoveH1ToNone(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H1", false)

	lower := declareWith(t, r, product, "none", false, "pharmacist")
	w := do(r, "POST", schedulePath(product, "/"+lower+"/review"), "editor", `{"decision":"approved","reason":"checked the pack"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"platform_review_required"`)
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))

	s := loweringState(t, r, product)
	require.True(t, s.History[0].Lowers)
	require.NotNil(t, s.InForce)
	require.Equal(t, "H1", s.InForce.RegulatoryClass)
}

func TestTheDatabaseRefusesATenantApprovalOfALowering(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H1", false)
	lower := declareWith(t, r, product, "none", false, "pharmacist")
	err := scheduleSQL(t, db, "user-editor", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id,outlet_id,declaration_id,decision,reason,reviewed_by) VALUES (?,?,?,'approved','direct','user-editor')`,
			testutil.TenantA, pharmaOutlet, lower).Error
	})
	requireConstraint(t, err, "pharmacy_schedule_platform_review")
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestLoweringIsDecidedByTheDatabase(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H1", false)
	insert := func(class string, lowers bool) string {
		id := uuid.Must(uuid.NewV7()).String()
		require.NoError(t, scheduleSQL(t, db, "user-editor", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_product_schedule_declarations (id,tenant_id,outlet_id,product_id,regulatory_class,ndps,source,intent_sha256,declared_by,lowers)
				VALUES (?,?,?,?,?,false,'direct',repeat('a',64),'user-editor',?)`, id, testutil.TenantA, pharmaOutlet, product, class, lowers).Error
		}))
		return id
	}
	require.True(t, lowersOf(t, db, insert("none", false)), "a client cannot hide a lowering")
	require.False(t, lowersOf(t, db, insert("X", true)), "a client cannot mark a raise as a lowering")
}

func TestDroppingNDPSIsALowering(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H", true)
	require.True(t, lowersOf(t, db, declareWith(t, r, product, "H1", false, "pharmacist")), "a stricter class without the NDPS control is still lower")
	require.False(t, lowersOf(t, db, declareWith(t, r, product, "X", true, "pharmacist")))
}

func TestAWaitingLoweringKeepsTheOldClass(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H1", false)
	declareWith(t, r, product, "none", false, "pharmacist")
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
	s := loweringState(t, r, product)
	require.NotNil(t, s.InForce)
	require.Equal(t, "H1", s.InForce.RegulatoryClass)
	require.Equal(t, "a lower class waits for Helivanta review", s.Reason)

	// A second lowering while the first waits is compared with the approved
	// class, not with the waiting one.
	require.True(t, lowersOf(t, db, declareWith(t, r, product, "H", false, "pharmacist")))
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestAPendingRaiseStillBlocks(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H", false)
	raise := declareWith(t, r, product, "H1", false, "pharmacist")
	require.False(t, lowersOf(t, db, raise))
	require.Empty(t, scheduleInForce(t, db, product))
	require.Nil(t, loweringState(t, r, product).InForce)

	// A lowering filed behind a waiting raise still compares with the
	// approved H, and the waiting raise keeps the product blocked.
	require.True(t, lowersOf(t, db, declareWith(t, r, product, "none", false, "pharmacist")))
	require.Empty(t, scheduleInForce(t, db, product))
}

// Two declarations filed before anything was approved are both first
// declarations at insert. Approving the stricter one must not let the
// tenant then approve the other and lower the class.
func TestALaterTenantApprovalCannotLowerAnApprovedClass(t *testing.T) {
	r, db, product := scheduleHarness(t)
	strict := declareWith(t, r, product, "H1", false, "editor")
	lax := declareWith(t, r, product, "none", false, "editor")
	require.False(t, lowersOf(t, db, lax))
	require.Equal(t, http.StatusOK, review(r, product, strict, "pharmacist", "approved"))
	w := do(r, "POST", schedulePath(product, "/"+lax+"/review"), "pharmacist", `{"decision":"approved","reason":"checked the pack"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"platform_review_required"`)
	require.Equal(t, http.StatusOK, review(r, product, lax, "pharmacist", "rejected"), "a lowering can still be rejected inside the tenant")
}

func TestConcurrentDeclarationsAreClassifiedAgainstTheClassInForce(t *testing.T) {
	r, db, product := scheduleHarness(t)
	approved(t, r, product, "H1", false)
	ids := make([]string, 8)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i] = uuid.Must(uuid.NewV7()).String()
			w := do(r, "PUT", schedulePath(product, "/"+ids[i]), "pharmacist", `{"regulatory_class":"none","ndps":false,"source":"TEST pack label"}`)
			if w.Code != http.StatusCreated {
				ids[i] = ""
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		require.NotEmpty(t, id, "every concurrent declaration is recorded")
		require.True(t, lowersOf(t, db, id))
	}
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

// Not covered by #1681: a product's first declaration has no class to
// compare with, so tenant four-eyes still decides it (#1683 closes this).
func TestAFirstDeclarationIsNotALowering(t *testing.T) {
	r, db, product := scheduleHarness(t)
	first := declareWith(t, r, product, "none", false, "editor")
	require.False(t, lowersOf(t, db, first))
	require.Equal(t, http.StatusOK, review(r, product, first, "pharmacist", "approved"))
	require.Equal(t, []string{"none"}, scheduleInForce(t, db, product))
}
