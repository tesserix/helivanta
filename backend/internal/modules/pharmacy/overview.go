package pharmacy

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/countryprofile"
)

type overviewStock struct {
	Expired int64 `json:"expired"`
	Due30   int64 `json:"due_30"`
	Due90   int64 `json:"due_90"`
}

type overviewSafety struct {
	Recent int64 `json:"recent"`
}

type overviewBills struct {
	Overdue int64 `json:"overdue"`
	Week    int64 `json:"week"`
}

// overviewSetup is present only where the outlet's country has a tax scheme:
// elsewhere nothing can be classified or registered, so no gap is real.
type overviewSetup struct {
	Unclassified        int64 `json:"unclassified"`
	RegistrationInForce *bool `json:"registration_in_force,omitempty"`
}

type outletOverview struct {
	AsOf     string          `json:"as_of"`
	TimeZone string          `json:"time_zone"`
	Stock    *overviewStock  `json:"stock,omitempty"`
	Safety   *overviewSafety `json:"safety,omitempty"`
	Bills    *overviewBills  `json:"bills,omitempty"`
	Setup    *overviewSetup  `json:"setup,omitempty"`
}

// getOverview answers the PharmaConnect overview's outlet counts (#576).
//
// Each count applies the same scope as the list its tile links to
// (stockExpiryScope, recentSafetyScope, payableDueScope, unclassifiedScope),
// so a number and the rows behind it cannot disagree. A section is returned
// only when the person holds its permission: absent means not permitted, a
// present zero means zero. It fails closed: no permission set is the 503 the
// router already answers before this runs.
func (h *catalogueHandlers) getOverview(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	perms, _ := authz.PermissionsFrom(c)
	var result outletOverview
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var o pharmacyOutlet
		if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&o).Error; err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Timezone)
		if err != nil {
			return err
		}
		now := clock()
		today := now.In(loc).Format(time.DateOnly)
		result.AsOf = now.In(loc).Format(time.RFC3339)
		result.TimeZone = o.Timezone

		count := func(q *gorm.DB) (int64, error) {
			var n int64
			return n, q.Count(&n).Error
		}

		if perms.Has(PermStockRead) {
			// The outlet's stock lines are aggregated once per request into a
			// transaction-local table; the three bands then apply the register's
			// own scope to it, so they still match the list and the history is
			// not re-aggregated per band on every refresh. Read at the current
			// database time so committed movements cannot fall beyond a slower host clock.
			var stockAsOf time.Time
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&stockAsOf).Error; err != nil {
				return err
			}
			source := stockPositionsSource(tx, p.TenantID, outlet, stockAsOf)
			if err := tx.Exec("CREATE TEMP TABLE overview_positions ON COMMIT DROP AS ?", source).Error; err != nil {
				return err
			}
			positions := func(expiry string) (int64, error) {
				return count(stockExpiryScope(expiry, "on_hand")(tx.Table("overview_positions")))
			}
			var stock overviewStock
			if stock.Expired, err = positions("expired"); err != nil {
				return err
			}
			if stock.Due30, err = positions("due_30"); err != nil {
				return err
			}
			if stock.Due90, err = positions("due_90"); err != nil {
				return err
			}
			result.Stock = &stock
			recent, err := count(recentSafetyScope(today, o.Timezone)(tx.Model(&batchSafetyIncident{}).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)))
			if err != nil {
				return err
			}
			result.Safety = &overviewSafety{Recent: recent}
		}

		if perms.Has(PermAccountsRead) {
			bills := func(due string) (int64, error) {
				return count(payableDueScope(due, today)(tx.Table("pharmacy_payable_balances").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)))
			}
			var b overviewBills
			if b.Overdue, err = bills("overdue"); err != nil {
				return err
			}
			if b.Week, err = bills("week"); err != nil {
				return err
			}
			result.Bills = &b
		}

		if _, ok := countryprofile.Tax(o.CountryCode); !ok {
			return nil
		}
		unclassified, err := count(unclassifiedScope(today)(tx.Model(&catalogueProduct{}).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)))
		if err != nil {
			return err
		}
		result.Setup = &overviewSetup{Unclassified: unclassified}
		// Only an owner sees registration.
		if perms.Has(PermTaxManage) {
			var inForce bool
			if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM pharmacy_outlet_tax_registrations
 WHERE tenant_id=? AND outlet_id=? AND effective_from <= ?::date)`, p.TenantID, outlet, today).Scan(&inForce).Error; err != nil {
				return err
			}
			result.Setup.RegistrationInForce = &inForce
		}
		return nil
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, result)
}
