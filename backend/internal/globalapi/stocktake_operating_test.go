package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStocktakeReviewedIndependentCounterFreezesReceivedStock(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	batch := v7()
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-COUNT-LOT"), 200)
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/receipts/"+v7()), "receiver", f.receipt(batch, f.quarantine, "TEST-COUNT-RECEIPT"), 200)
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
	path := root + "/" + v7()
	input := map[string]any{
		"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine,
		"reason": "TEST reviewed outlet count",
	}
	f.call("PUT", path, "receiver", input, 404)
	f.call("PUT", path, "owner", input, 404)
	f.call("PUT", path, "foreign", input, 404)

	// The new duty uses the same reviewed identity, without a legacy inventory role or outlet editor grant.
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.input.Reason = "TEST reviewed physical counting duty"
	f.activate(t)
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/operating-locations/"+f.sibling+"/stocktakes/"+v7(), "receiver", input, 404)
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.hospitalPharmacy+"/operating-locations/"+f.hospitalOutlet+"/stocktakes/"+v7(), "receiver", input, 404)
	first := f.call("PUT", path, "receiver", input, 200)
	require.JSONEq(t, first, f.call("PUT", path, "receiver", input, 200))
	changed := map[string]any{
		"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine,
		"reason": "TEST changed command identity",
	}
	f.call("PUT", path, "receiver", changed, 409)
	f.call("PUT", root+"/"+v7(), "receiver", input, 409)
	var page struct {
		Data []struct {
			ID        string `json:"id"`
			BatchID   string `json:"batch_id"`
			BookUnits int64  `json:"book_units"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, batch, page.Data[0].BatchID)
	require.EqualValues(t, 30, page.Data[0].BookUnits)
	f.call("GET", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/stocktakes", "receiver", nil, 403)
	f.checks.down.Store(true)
	unavailable := f.call("GET", path+"/lines", "receiver", nil, 503)
	require.NotContains(t, unavailable, "TEST-COUNT-LOT")
	f.checks.down.Store(false)
	f.call("GET", path+"/lines", "receiver", nil, 200)
	countCommand := v7()
	countPath := path + "/counts/" + countCommand
	count := map[string]any{
		"expected_version": 0, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 27}},
		"reason": "TEST reviewed physical shelf count",
	}
	f.call("PUT", countPath, "owner", count, 404)
	f.call("PUT", countPath, "foreign", count, 404)
	recorded := f.call("PUT", countPath, "receiver", count, 200)
	require.JSONEq(t, recorded, f.call("PUT", countPath, "receiver", count, 200))
	require.JSONEq(t, recorded, f.call("GET", countPath, "receiver", nil, 200))
	changedCount := map[string]any{
		"expected_version": 0, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 26}},
		"reason": "TEST changed physical shelf count",
	}
	f.call("PUT", countPath, "receiver", changedCount, 409)
	stalePath := path + "/counts/" + v7()
	f.call("PUT", stalePath, "receiver", count, 409)
	f.call("GET", stalePath, "receiver", nil, 404)
	var receipt struct {
		Version    int64  `json:"version"`
		RecordedBy string `json:"recorded_by"`
		Counts     []struct {
			CountedUnits int64 `json:"counted_units"`
		} `json:"counts"`
	}
	require.NoError(t, json.Unmarshal([]byte(recorded), &receipt))
	require.EqualValues(t, 1, receipt.Version)
	require.Len(t, receipt.Counts, 1)
	require.EqualValues(t, 27, receipt.Counts[0].CountedUnits)
	f.checks.down.Store(true)
	f.call("GET", countPath, "receiver", nil, 503)
	f.checks.down.Store(false)
	require.Equal(t, "user-receiver", receipt.RecordedBy)
	legacyCount := map[string]any{
		"expected_version": 1, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 25}},
		"reason": "TEST legacy editor cannot enter reviewed history",
	}
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/stocktakes/"+path[len(root)+1:]+"/counts/"+v7(), "owner", legacyCount, 409)
	submitPath := path + "/decisions/" + v7()
	submit := map[string]any{"decision": "submit", "expected_version": 1, "reason": "TEST physical observations ready for independent review"}
	f.call("PUT", submitPath, "owner", submit, 404)
	f.call("PUT", path+"/decisions/"+countCommand, "receiver", submit, 409)
	submitted := f.call("PUT", submitPath, "receiver", submit, 200)
	require.JSONEq(t, submitted, f.call("PUT", submitPath, "receiver", submit, 200))
	require.JSONEq(t, submitted, f.call("GET", submitPath, "receiver", nil, 200))
	var decision struct {
		State           string `json:"state"`
		Version         int64  `json:"version"`
		AdjustmentCount int64  `json:"adjustment_count"`
	}
	require.NoError(t, json.Unmarshal([]byte(submitted), &decision))
	require.Equal(t, "submitted", decision.State)
	require.EqualValues(t, 2, decision.Version)
	require.Zero(t, decision.AdjustmentCount)
	afterSubmissionPath := path + "/counts/" + v7()
	afterSubmission := map[string]any{"expected_version": 2, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 26}}, "reason": "TEST submitted count cannot be edited"}
	f.call("PUT", afterSubmissionPath, "receiver", afterSubmission, 409)
	f.call("GET", afterSubmissionPath, "receiver", nil, 404)
	f.call("PUT", path+"/decisions/"+v7(), "receiver", map[string]any{
		"decision": "approve", "expected_version": 2, "reason": "TEST counter cannot independently approve", "extra_units_disposition": "no_extra_units",
	}, 404)
	counterDuty := f.duty
	f.call("POST", "/v1/iam/members", "owner", map[string]any{"subject": "user-stock-reviewer", "role_key": "staff_member"}, 202)
	require.Eventually(t, func() bool {
		member, err := f.graph.IsMember(t.Context(), "user-stock-reviewer", testutil.TenantA)
		return err == nil && member
	}, 20*time.Second, 50*time.Millisecond)
	reviewStaff, reviewLink := v7(), v7()
	staffRoot := "/v1/workforce/pharmacies/" + f.pharmacy + "/staff"
	f.call("PUT", staffRoot+"/"+reviewStaff+"?directory_context=v1", "owner", map[string]any{
		"employee_code": "TEST-PH-REVIEW", "expected_version": 0, "display_name": "TEST Independent Stock Reviewer", "job_category": "pharmacist", "employment_status": "active", "directory_context_version": "v1",
	}, 200)
	reviewStaffRoot := staffRoot + "/" + reviewStaff
	f.call("PUT", reviewStaffRoot+"/account-links/"+reviewLink, "owner", map[string]any{
		"subject": "user-stock-reviewer", "expected_staff_version": 1, "reason": "TEST distinct approver account",
	}, 201)
	f.call("PUT", reviewStaffRoot+"/account-links/"+reviewLink+"/commands/"+v7(), "reviewer", map[string]any{
		"decision": "approve", "expected_version": 1, "reason": "TEST approve distinct account proof",
	}, 201)
	f.input.TemplateKey = "pharmacy.stocktake_reviewer.v1"
	f.input.Subject = "user-stock-reviewer"
	f.input.Identity.StaffID, f.input.Identity.LinkID = uuid.MustParse(reviewStaff), uuid.MustParse(reviewLink)
	f.input.Identity.StaffVersion, f.input.Identity.LinkVersion = 1, 2
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.activate(t)
	approvalPath := path + "/decisions/" + v7()
	approval := map[string]any{"decision": "approve", "expected_version": 2, "reason": "TEST independently approve observed shortage", "extra_units_disposition": "no_extra_units"}
	posted := f.call("PUT", approvalPath, "stock-reviewer", approval, 200)
	require.JSONEq(t, posted, f.call("PUT", approvalPath, "stock-reviewer", approval, 200))
	require.JSONEq(t, posted, f.call("GET", approvalPath, "stock-reviewer", nil, 200))
	require.NoError(t, json.Unmarshal([]byte(posted), &decision))
	require.Equal(t, "posted", decision.State)
	require.EqualValues(t, 3, decision.Version)
	require.EqualValues(t, 1, decision.AdjustmentCount)
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections, COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, ledger.Corrections)
		require.EqualValues(t, 27, ledger.Units)
		return nil
	})
	f.duty = counterDuty
	f.call("PUT", f.duty+"/commands/"+v7(), "owner", map[string]any{
		"expected_version": 3, "decision": "revoke", "reason": "TEST end the reviewed counting duty",
	}, 201)
	f.call("GET", path+"/lines", "receiver", nil, 404)
	f.call("GET", countPath, "receiver", nil, 404)
	f.call("GET", submitPath, "receiver", nil, 404)
	f.call("PUT", path+"/counts/"+v7(), "receiver", count, 404)
}

func newSubmittedReviewedStocktake(t *testing.T) (pharmacyReceiverFixture, string) {
	t.Helper()
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	stockRoot := f.stock(f.pharmacy, f.outlet, "")
	batch := v7()
	f.call("PUT", stockRoot+"/batches/"+batch, "receiver", f.batch("TEST-REVIEW-PARTICIPANT"), 200)
	f.call("PUT", stockRoot+"/receipts/"+v7(), "receiver", f.receipt(batch, f.quarantine, "TEST reviewed participant receipt"), 200)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	path := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes/" + v7()
	f.call("PUT", path, "receiver", map[string]any{"scope": "all", "product_ids": []string{}, "reason": "TEST reviewed participant count"}, 200)
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &page))
	require.Len(t, page.Data, 1)
	f.call("PUT", path+"/counts/"+v7(), "receiver", map[string]any{"expected_version": 0, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 27}}, "reason": "TEST observed shortage"}, 200)
	f.call("PUT", path+"/decisions/"+v7(), "receiver", map[string]any{"decision": "submit", "expected_version": 1, "reason": "TEST submit for independent review"}, 200)
	return f, path
}

func TestStocktakeReviewedCounterAliasCannotApprove(t *testing.T) {
	f, path := newSubmittedReviewedStocktake(t)
	f.call("POST", "/v1/iam/members", "owner", map[string]any{"subject": "user-stock-reviewer", "role_key": "staff_member"}, 202)
	require.Eventually(t, func() bool {
		member, err := f.graph.IsMember(t.Context(), "user-stock-reviewer", testutil.TenantA)
		return err == nil && member
	}, 20*time.Second, 50*time.Millisecond)
	staffPath := "/v1/workforce/pharmacies/" + f.pharmacy + "/staff/" + f.staff
	f.call("PUT", staffPath+"/account-links/"+f.input.Identity.LinkID.String()+"/commands/"+v7(), "owner", map[string]any{"decision": "revoke", "expected_version": 2, "reason": "TEST rotate counter account"}, 201)
	link := v7()
	f.call("PUT", staffPath+"/account-links/"+link, "owner", map[string]any{"subject": "user-stock-reviewer", "expected_staff_version": 1, "reason": "TEST relink the same Staff under a new account"}, 201)
	f.call("PUT", staffPath+"/account-links/"+link+"/commands/"+v7(), "reviewer", map[string]any{"decision": "approve", "expected_version": 1, "reason": "TEST verify rotated account"}, 201)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.Subject = "user-stock-reviewer"
	f.input.TemplateKey = "pharmacy.stocktake_reviewer.v1"
	f.input.Identity.LinkID = uuid.MustParse(link)
	f.input.Identity.LinkVersion = 2
	f.activate(t)
	approval := path + "/decisions/" + v7()
	f.call("PUT", approval, "stock-reviewer", map[string]any{"decision": "approve", "expected_version": 2, "reason": "TEST same Staff is not independent", "extra_units_disposition": "no_extra_units"}, 409)
	f.call("GET", approval, "stock-reviewer", nil, 404)
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections,COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.Zero(t, ledger.Corrections)
		require.EqualValues(t, 30, ledger.Units)
		return nil
	})
}
