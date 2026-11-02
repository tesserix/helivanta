// Package billing owns the hospital's billing ledger (#193). This slice
// keeps admission accounts (#197): what an admitted patient owes the
// hospital, posted by other modules through their published events and
// never by writing these tables.
package billing

import (
	_ "embed"

	billingcontract "github.com/tesserix/helivanta/internal/modules/billing/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	// PermAccountRead reads an admission's account at a hospital where the
	// caller holds a billing grant (ward staff answer a relative's question).
	PermAccountRead authz.Permission = "billing.account.read"
	// PermAccessManage grants and revokes billing access at a hospital.
	PermAccessManage authz.Permission = "billing.access.manage"
	// PermRateManage sets a hospital's bed-charging timezone and bed rates.
	PermRateManage authz.Permission = "billing.rate.manage"
	// PermBillIssue issues an interim bill on an open admission account.
	PermBillIssue authz.Permission = "billing.bill.issue"
	// PermCounterManage sets up a hospital's cash counters.
	PermCounterManage authz.Permission = "billing.counter.manage"
	// PermCashCollect opens and closes the caller's own shift and takes cash.
	PermCashCollect authz.Permission = "billing.cash.collect"
	// PermShiftAuthorise authorises a shift's cash variance and lists shifts.
	PermShiftAuthorise authz.Permission = "billing.shift.authorise"
	// PermCollectionRead reads and exports a hospital's daily cash collection.
	PermCollectionRead authz.Permission = "billing.collection.read"
	// PermTaxManage records a hospital's GST registration.
	PermTaxManage authz.Permission = "billing.tax.manage"
	// PermDiscountManage sets and retires a hospital's discount reasons.
	PermDiscountManage authz.Permission = "billing.discount.manage"
	// PermDiscountApprove decides discounts beyond the desk's limit.
	PermDiscountApprove authz.Permission = "billing.discount.approve"
)

//go:embed accounts.sql
var accountsSchema string

//go:embed bed_days.sql
var bedDaysSchema string

//go:embed interim_bills.sql
var interimBillsSchema string

//go:embed final_bills.sql
var finalBillsSchema string

//go:embed cash.sql
var cashSchema string

//go:embed deposits.sql
var depositsSchema string

//go:embed refunds.sql
var refundsSchema string

//go:embed positions.sql
var positionsSchema string

//go:embed gst.sql
var gstSchema string

//go:embed credit_notes.sql
var creditNotesSchema string

//go:embed upi.sql
var upiSchema string

//go:embed discounts.sql
var discountsSchema string

//go:embed pickups.sql
var pickupsSchema string

//go:embed handovers.sql
var handoversSchema string

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "billing" }

// Permissions: ward nurses and the front desk read accounts; only tenant
// administrators (implicitly) manage access and rates.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermAccountRead, Roles: []authz.Role{authz.RoleNurse, authz.RoleReceptionist}},
		{Permission: PermAccessManage},
		{Permission: PermRateManage},
		// The billing desk issues interim bills; ward nurses only read the account.
		{Permission: PermBillIssue, Roles: []authz.Role{authz.RoleReceptionist}},
		// The billing desk takes cash in its own shift; setting up counters and
		// authorising a cashier's variance stay with administrators.
		{Permission: PermCashCollect, Roles: []authz.Role{authz.RoleReceptionist}},
		{Permission: PermCounterManage},
		{Permission: PermShiftAuthorise},
		// No finance role exists on the hospital side: administrators only.
		{Permission: PermCollectionRead},
		// A GSTIN binds the hospital legally: administrators only.
		{Permission: PermTaxManage},
		// Concession policy and its approvals stay with administrators; the
		// desk asks under billing.bill.issue.
		{Permission: PermDiscountManage},
		{Permission: PermDiscountApprove},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_billing", SQL: accountsSchema}, {ID: "0002_billing", SQL: bedDaysSchema}, {ID: "0003_billing", SQL: interimBillsSchema}, {ID: "0004_billing", SQL: finalBillsSchema}, {ID: "0005_billing", SQL: cashSchema}, {ID: "0006_billing", SQL: depositsSchema}, {ID: "0007_billing", SQL: refundsSchema}, {ID: "0008_billing", SQL: positionsSchema}, {ID: "0009_billing", SQL: gstSchema}, {ID: "0010_billing", SQL: creditNotesSchema}, {ID: "0011_billing", SQL: upiSchema}, {ID: "0012_billing", SQL: discountsSchema}, {ID: "0013_billing", SQL: pickupsSchema}, {ID: "0014_billing", SQL: handoversSchema}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, bus: deps.Bus}
	g := r.Group("/billing")
	hp := "/hospitals/:hospital"
	g.PUT(hp+"/access/:subject", PermAccessManage, h.grantAccess)
	g.DELETE(hp+"/access/:subject", PermAccessManage, h.revokeAccess)
	g.GET(hp+"/access", PermAccountRead, h.myAccess)
	g.GET(hp+"/admissions/:admission/account", PermAccountRead, h.account)
	// The billing desk's list of accounts, by what they need (#197–#199).
	platform.ListRoute(g, hp+"/accounts", PermAccountRead, h.deskAccounts)
	platform.ListRoute(g, hp+"/admissions/:admission/entries", PermAccountRead, h.entries)
	g.PUT(hp+"/bed-charging", PermRateManage, h.setBedCharging)
	g.GET(hp+"/bed-charging", PermAccountRead, h.bedChargingSetup)
	g.PUT(hp+"/bed-rates/:class/:effective_from", PermRateManage, h.setBedRate)
	platform.ListRoute(g, hp+"/bed-rates", PermAccountRead, h.bedRates)
	bills := hp + "/admissions/:admission/interim-bills"
	g.POST(bills, PermBillIssue, h.issueInterimBill)
	platform.ListRoute(g, bills, PermAccountRead, h.bills("interim"))
	g.GET(bills+"/:number", PermAccountRead, h.bill("interim"))
	platform.ListRoute(g, bills+"/:number/lines", PermAccountRead, h.billLines("interim"))
	// Final bills, and every bill of either kind in one series (#197 slice 4).
	g.POST(hp+"/admissions/:admission/final-bills", PermBillIssue, h.issueFinalBill)
	// Cash counters (#198 slice 1).
	g.PUT(hp+"/counters/:counter", PermCounterManage, h.putCounter)
	platform.ListRoute(g, hp+"/counters", PermAccountRead, h.counters)
	g.PUT(hp+"/counters/:counter/shifts/:shift", PermCashCollect, h.openShift)
	g.GET(hp+"/cash-denominations", PermAccountRead, h.denominations)
	g.PUT(hp+"/cash-shifts/:shift/pickups/:pickup", PermShiftAuthorise, h.recordPickup)
	platform.ListRoute(g, hp+"/cash-shifts/:shift/pickups", PermShiftAuthorise, h.pickups)
	g.PUT(hp+"/cash-pickups/:pickup/corrections/:correction", PermShiftAuthorise, h.correctPickup)
	g.GET(hp+"/cash-shifts/mine", PermCashCollect, h.myShift)
	g.PUT(hp+"/cash-shifts/:shift/close", PermCashCollect, h.closeShift)
	g.PUT(hp+"/cash-shifts/:shift/authorisation", PermShiftAuthorise, h.authoriseShift)
	// Handover between cashiers (#198 slice 5).
	g.PUT(hp+"/cash-shifts/:shift/handovers/:handover", PermCashCollect, h.offerHandover)
	g.PUT(hp+"/cash-handovers/:handover/withdrawal", PermCashCollect, h.withdrawHandover)
	g.PUT(hp+"/cash-handovers/:handover/acceptance", PermCashCollect, h.acceptHandover)
	g.PUT(hp+"/cash-handovers/:handover/settlement", PermShiftAuthorise, h.settleHandover)
	platform.ListRoute(g, hp+"/cash-handovers/incoming", PermCashCollect, h.incomingHandovers)
	platform.ListRoute(g, hp+"/cash-handovers/disputed", PermShiftAuthorise, h.disputedHandovers)
	// The daily collection report (#198 slice 2).
	g.GET(hp+"/collections/:date", PermCollectionRead, h.dailyCollection)
	g.GET(hp+"/collections/:date/export.csv", PermCollectionRead, h.exportDailyCollection)
	platform.ListRoute(g, hp+"/cash-shifts", PermShiftAuthorise, h.shifts)
	g.PUT(hp+"/admissions/:admission/receipts/:receipt", PermCashCollect, h.takeCash)
	platform.ListRoute(g, hp+"/admissions/:admission/receipts", PermAccountRead, h.receipts)
	g.PUT(hp+"/receipts/:receipt/voids/:void", PermCashCollect, h.voidReceipt)
	// UPI at the desk (#198 slice 3, #211): asked in the cashier's shift,
	// confirmed by payments, recorded as a receipt.
	g.PUT(hp+"/admissions/:admission/upi-requests/:request", PermCashCollect, h.putUpiRequest)
	platform.ListRoute(g, hp+"/admissions/:admission/upi-requests", PermAccountRead, h.upiRequests)
	g.GET(hp+"/upi-requests/:request", PermCashCollect, h.upiRequest)
	g.POST(hp+"/upi-requests/:request/cancel", PermCashCollect, h.cancelUpiRequest)
	// Deposits (#199 slice 1).
	g.PUT(hp+"/admissions/:admission/deposits/:receipt", PermCashCollect, h.takeDeposit)
	g.PUT(hp+"/admissions/:admission/deposit-applications/:application", PermBillIssue, h.applyDeposits)
	platform.ListRoute(g, hp+"/admissions/:admission/deposit-applications", PermAccountRead, h.depositApplications)
	// Refunds of unused deposits (#199 slice 2).
	g.GET(hp+"/refund-policy", PermAccountRead, h.refundPolicy)
	g.PUT(hp+"/refund-policies/:policy", PermCounterManage, h.setRefundPolicy)
	g.PUT(hp+"/admissions/:admission/refund-approvals/:approval", PermShiftAuthorise, h.approveRefund)
	platform.ListRoute(g, hp+"/admissions/:admission/refund-approvals", PermAccountRead, h.refundApprovals)
	g.PUT(hp+"/admissions/:admission/refunds/:receipt", PermCashCollect, h.payRefund)
	// GST on rooms and the admission tax invoice (#193 slice 1).
	g.PUT(hp+"/gst-registration", PermTaxManage, h.setGstRegistration)
	g.GET(hp+"/gst-registration", PermAccountRead, h.gstRegistration)
	inv := hp + "/admissions/:admission/tax-invoices"
	g.PUT(inv+"/:invoice", PermBillIssue, h.issueTaxInvoice)
	platform.ListRoute(g, inv, PermAccountRead, h.taxInvoices)
	g.GET(inv+"/:invoice", PermAccountRead, h.taxInvoice)
	platform.ListRoute(g, inv+"/:invoice/lines", PermAccountRead, h.taxInvoiceLines)
	// Credit notes for an invoice's reversed room days (#193 slice 2).
	g.PUT(inv+"/:invoice/credit-notes/:note", PermBillIssue, h.issueCreditNote)
	cn := hp + "/admissions/:admission/credit-notes"
	platform.ListRoute(g, cn, PermAccountRead, h.creditNotes)
	g.GET(cn+"/:note", PermAccountRead, h.creditNote)
	platform.ListRoute(g, cn+"/:note/lines", PermAccountRead, h.creditNoteLines)
	// Discounts on room charges (#200 slice 1).
	g.PUT(hp+"/discount-reasons/:reason", PermDiscountManage, h.putDiscountReason)
	g.POST(hp+"/discount-reasons/:reason/retire", PermDiscountManage, h.retireDiscountReason)
	platform.ListRoute(g, hp+"/discount-reasons", PermAccountRead, h.discountReasons)
	g.PUT(hp+"/admissions/:admission/discounts/:discount", PermBillIssue, h.putDiscount)
	platform.ListRoute(g, hp+"/admissions/:admission/discounts", PermAccountRead, h.discounts)
	platform.ListRoute(g, hp+"/pending-discounts", PermDiscountApprove, h.pendingDiscounts)
	g.PUT(hp+"/discounts/:discount/decision", PermDiscountApprove, h.decideDiscount)
	all := hp + "/admissions/:admission/bills"
	platform.ListRoute(g, all, PermAccountRead, h.bills(""))
	g.GET(all+"/:number", PermAccountRead, h.bill(""))
	platform.ListRoute(g, all+"/:number/lines", PermAccountRead, h.billLines(""))
}

// Publishes the desk's UPI requests and cancels, for payments.
func (m *Module) Publishes() []string {
	return []string{billingcontract.SubjectPaymentRequested, billingcontract.SubjectPaymentCancelled}
}

// DirectedSubjects declares none: billing publishes nothing that creates
// data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: billing writes no table on behalf of
// another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

func (m *Module) Consumers(platform.Deps) []events.Consumer { return consumers() }

// Broadcasts declares none: billing has no per-replica cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
