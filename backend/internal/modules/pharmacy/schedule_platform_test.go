package pharmacy_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1681: Helivanta decides a countersigned lowering, connected as the
// platform-only helivanta_drug_approver role.

func approverDB(t *testing.T, db *tenantdb.DB) *tenantdb.DB {
	t.Helper()
	var name string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_database()`).Scan(&name).Error
	}))
	approver, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugApproverDSN(t, name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = approver.Close() })
	return approver
}

// waitingLowering is an approved H1 product with a countersigned request
// to lower it to none.
func waitingLowering(t *testing.T) (r *gin.Engine, db *tenantdb.DB, product, lower string) {
	t.Helper()
	engine, db, product := loweringHarness(t)
	approved(t, engine, product, "H1", false)
	lower = declareWith(t, engine, product, "none", false, "pharmacist")
	require.Equal(t, http.StatusCreated, countersign(engine, product, lower, "owner", "label misread at intake").Code)
	return engine, db, product, lower
}

func TestHelivantaApprovesACountersignedLowering(t *testing.T) {
	_, db, product, lower := waitingLowering(t)
	approver := approverDB(t, db)

	waiting, err := pharmacy.ListScheduleLowerings(t.Context(), approver)
	require.NoError(t, err)
	require.Len(t, waiting, 1)
	got := waiting[0]
	require.Equal(t, lower, got.DeclarationID.String())
	require.Equal(t, product, got.ProductID.String())
	require.Equal(t, testutil.TenantA, got.TenantID.String())
	require.Equal(t, "TEST Amoxil", got.ProductName)
	require.Equal(t, "H1", got.CurrentClass)
	require.Equal(t, "none", got.RequestedClass)
	require.Equal(t, "user-pharmacist", got.DeclaredBy)
	require.Equal(t, "user-owner", got.CountersignedBy)
	require.Equal(t, "label misread at intake", got.CountersignReason)

	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approver, got.DeclarationID, "Mahesh Sangawar", true, "CDSCO approval shows no schedule"))
	require.Equal(t, []string{"none"}, scheduleInForce(t, db, product))
	waiting, err = pharmacy.ListScheduleLowerings(t.Context(), approver)
	require.NoError(t, err)
	require.Empty(t, waiting)

	var audit []struct{ ActorSubject, ResourceType string }
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT actor_subject, resource_type FROM audit_events WHERE resource_type='pharmacy_product_schedule_reviews' AND actor_subject LIKE 'helivanta:%'`).Scan(&audit).Error
	}))
	require.Equal(t, []struct{ ActorSubject, ResourceType string }{{"helivanta:Mahesh Sangawar", "pharmacy_product_schedule_reviews"}}, audit)
}

func TestTheScheduleScreenShowsAHelivantaDecision(t *testing.T) {
	r, db, product, lower := waitingLowering(t)
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approverDB(t, db), uuid.MustParse(lower), "Sam", false, "the pack label is H1"))
	w := testutil.Do(r, "GET", schedulePath(product, ""), "pharmacist", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := w.Body.String()
	require.Contains(t, body, `"decided_by_platform":true`)
	require.Contains(t, body, `"reason":"the lower class was rejected"`)
	require.Contains(t, body, `"regulatory_class":"H1"`)
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestHelivantaRejectionKeepsTheClass(t *testing.T) {
	_, db, product, lower := waitingLowering(t)
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approverDB(t, db), uuid.MustParse(lower), "Sam", false, "the pack label is H1"))
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestNoDecisionWithoutCountersign(t *testing.T) {
	r, db, product := loweringHarness(t)
	approved(t, r, product, "H1", false)
	lower := declareWith(t, r, product, "none", false, "pharmacist")
	approver := approverDB(t, db)
	waiting, err := pharmacy.ListScheduleLowerings(t.Context(), approver)
	require.NoError(t, err)
	require.Empty(t, waiting, "an uncountersigned request is not listed")
	err = pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.MustParse(lower), "Sam", true, "x")
	require.ErrorIs(t, err, pharmacy.ErrLoweringNotWaiting)

	// The database refuses it too, not only the function.
	err = approver.WithTenant(tenantdb.WithActor(t.Context(), "helivanta:Sam"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id,outlet_id,declaration_id,decision,reason,reviewed_by) VALUES (?,?,?,'approved','direct','Sam')`,
			testutil.TenantA, pharmaOutlet, lower).Error
	})
	requireConstraint(t, err, "pharmacy_schedule_platform_countersign")
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestTheReviewerIsNotTheDeclarerOrCountersigner(t *testing.T) {
	_, db, product, lower := waitingLowering(t)
	approver := approverDB(t, db)
	for _, by := range []string{"user-pharmacist", " USER-OWNER "} {
		err := pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.MustParse(lower), by, true, "x")
		requireConstraint(t, err, "pharmacy_schedule_four_eyes")
	}
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

func TestADecidedLoweringCannotBeDecidedAgain(t *testing.T) {
	_, db, _, lower := waitingLowering(t)
	approver := approverDB(t, db)
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.MustParse(lower), "Sam", false, "no"))
	require.ErrorIs(t, pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.MustParse(lower), "Sam", true, "yes"), pharmacy.ErrLoweringNotWaiting)
	var n int64
	require.NoError(t, scheduleSQL(t, db, "user-pharmacist", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pharmacy_product_schedule_reviews WHERE declaration_id=?`, lower).Scan(&n).Error
	}))
	require.Equal(t, int64(1), n)
}

func TestDecisionInputIsValidated(t *testing.T) {
	_, db, _, lower := waitingLowering(t)
	approver := approverDB(t, db)
	for _, c := range []struct{ by, reason string }{{" ", "x"}, {"Sam", " "}, {"Sam", strings.Repeat("x", 501)}} {
		require.Error(t, pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.MustParse(lower), c.by, true, c.reason))
	}
	require.ErrorIs(t, pharmacy.DecideScheduleLowering(t.Context(), approver, uuid.Must(uuid.NewV7()), "Sam", true, "x"), pharmacy.ErrLoweringNotWaiting)
}

func TestTheApproverSeesOnlyScheduleRows(t *testing.T) {
	r, db, product, _ := waitingLowering(t)
	approver := approverDB(t, db)
	err := approver.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		return tx.Raw(`SELECT count(*) FROM pharmacy_sales`).Scan(&n).Error
	})
	requireSQLState(t, err, "42501")

	// A raise is not Helivanta's to decide.
	raise := declareWith(t, r, product, "X", false, "pharmacist")
	err = approver.WithTenant(tenantdb.WithActor(t.Context(), "helivanta:Sam"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id,outlet_id,declaration_id,decision,reason,reviewed_by) VALUES (?,?,?,'approved','direct','Sam')`,
			testutil.TenantA, pharmaOutlet, raise).Error
	})
	requireConstraint(t, err, "pharmacy_schedule_platform_lowering_only")
}

// Review focus 2: an old lowering approved after a newer raise does not
// override the raise.
func TestALaterRaiseWinsOverAnOldLowering(t *testing.T) {
	r, db, product, lower := waitingLowering(t)
	raise := declareWith(t, r, product, "X", false, "editor")
	require.Equal(t, http.StatusOK, review(r, product, raise, "pharmacist", "approved"))
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approverDB(t, db), uuid.MustParse(lower), "Sam", true, "old request"))
	require.Equal(t, []string{"X"}, scheduleInForce(t, db, product))
}

func TestATenantReviewIsNeverMarkedPlatform(t *testing.T) {
	r, db, product := loweringHarness(t)
	approved(t, r, product, "H", false)
	raise := declareWith(t, r, product, "H1", false, "editor")
	require.NoError(t, scheduleSQL(t, db, "user-pharmacist", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id,outlet_id,declaration_id,decision,reason,reviewed_by,by_platform) VALUES (?,?,?,'approved','direct','user-pharmacist',true)`,
			testutil.TenantA, pharmaOutlet, raise).Error
	}))
	var platform []bool
	require.NoError(t, scheduleSQL(t, db, "user-pharmacist", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT by_platform FROM pharmacy_product_schedule_reviews WHERE declaration_id=?`, raise).Scan(&platform).Error
	}))
	require.Equal(t, []bool{false}, platform)
}

func TestTheAppRoleCannotActAsPlatform(t *testing.T) {
	_, db, product, lower := waitingLowering(t)
	err := pharmacy.DecideScheduleLowering(t.Context(), db, uuid.MustParse(lower), "Sam", true, "through the app pool")
	require.Error(t, err)
	require.Equal(t, []string{"H1"}, scheduleInForce(t, db, product))
}

// A lowering Helivanta approved is what the counter sells under.
func TestASaleFollowsAnApprovedLowering(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-PCM-H1", "H1")
	w.shelve(t, product, "2028-06-30", 13000, 10)
	code, _, out := w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "needs a prescription")

	lower := declareWith(t, w.r, product, "none", false, "pharmacist")
	require.NoError(t, scheduleSQL(t, w.db, "user-owner", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_schedule_countersigns (tenant_id,outlet_id,declaration_id,reason,countersigned_by) VALUES (?,?,?,'label misread','user-owner')`,
			testutil.TenantA, pharmaOutlet, lower).Error
	}))
	require.NoError(t, pharmacy.DecideScheduleLowering(t.Context(), approverDB(t, w.db), uuid.MustParse(lower), "Sam", true, "CDSCO approval shows no schedule"))
	code, _, out = w.sell(t, cashSale(product, 1, 1300, 1300))
	require.Equal(t, http.StatusCreated, code, out)
}
