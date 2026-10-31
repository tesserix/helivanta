// Package payments is the payment orchestration core (#210, #1386 C-4a):
// the tenant's provider account, payment intents and what the provider
// reported about them. Requesters ask for a payment and hear its outcome
// by events; they never read these tables, and this module never reads
// theirs.
package payments

import (
	"context"
	_ "embed"
	"encoding/json"

	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	billingcontract "github.com/tesserix/helivanta/internal/modules/billing/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// PermAccountManage reads and replaces the tenant's provider account. No
// role holds it: provider keys move money, so only tenant administrators
// may set them.
const PermAccountManage authz.Permission = "payments.account.manage"

//go:embed payments.sql
var schema string

//go:embed requesters.sql
var requestersSchema string

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "payments" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermAccountManage}}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_payments", SQL: schema}, {ID: "0002_payments", SQL: requestersSchema}}
}

func serviceFor(deps platform.Deps) *service {
	return newService(deps.DB, deps.Transit, newRazorpay(deps.PaymentsProviderURL))
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	accountRoutes(r.Group("/payments"), &accountHandlers{db: deps.DB, svc: serviceFor(deps), webhookBase: deps.PaymentsWebhookBaseURL})
}

func (m *Module) Publishes() []string {
	return []string{paymentscontract.SubjectIntentOpened, paymentscontract.SubjectIntentFailed, paymentscontract.SubjectIntentPaid,
		paymentscontract.SubjectIntentExpired, paymentscontract.SubjectIntentCancelled}
}

// Consumers record each request and cancel in the bus transaction and do
// the provider calls after it commits (AfterCommit, run on every delivery),
// so no consumer holds a database connection while Razorpay answers. Each
// requester publishes its own subjects; the payload shapes match.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	svc := serviceFor(deps)
	pharmacy := requesterConsumers(svc, paymentscontract.RequesterPharmacy, pharmacycontract.SubjectPaymentRequested, pharmacycontract.SubjectPaymentCancelled,
		func(evt events.Event) (openRequest, error) {
			var d pharmacycontract.PaymentRequestedData
			err := json.Unmarshal(evt.Data, &d)
			return openRequest{RequestID: d.RequestID, AmountMinor: d.AmountMinor, Currency: d.Currency, ExpiresInSeconds: d.ExpiresInSeconds}, err
		},
		func(evt events.Event) (openRequest, error) {
			var d pharmacycontract.PaymentCancelledData
			err := json.Unmarshal(evt.Data, &d)
			return openRequest{RequestID: d.RequestID, AmountMinor: d.AmountMinor, Currency: d.Currency}, err
		})
	billing := requesterConsumers(svc, paymentscontract.RequesterBilling, billingcontract.SubjectPaymentRequested, billingcontract.SubjectPaymentCancelled,
		func(evt events.Event) (openRequest, error) {
			var d billingcontract.PaymentRequestedData
			err := json.Unmarshal(evt.Data, &d)
			return openRequest{RequestID: d.RequestID, AmountMinor: d.AmountMinor, Currency: d.Currency, ExpiresInSeconds: d.ExpiresInSeconds}, err
		},
		func(evt events.Event) (openRequest, error) {
			var d billingcontract.PaymentCancelledData
			err := json.Unmarshal(evt.Data, &d)
			return openRequest{RequestID: d.RequestID, AmountMinor: d.AmountMinor, Currency: d.Currency}, err
		})
	return append(pharmacy, billing...)
}

// requesterConsumers serve one requester's request and cancel subjects. The
// requester is set here, never taken from the payload.
func requesterConsumers(svc *service, requester, requestSubject, cancelSubject string, requested, cancelled func(events.Event) (openRequest, error)) []events.Consumer {
	as := func(decode func(events.Event) (openRequest, error)) func(events.Event) (openRequest, error) {
		return func(evt events.Event) (openRequest, error) {
			r, err := decode(evt)
			r.Requester = requester
			return r, err
		}
	}
	requested, cancelled = as(requested), as(cancelled)
	requestName, cancelName := "payments-"+requester+"-request", "payments-"+requester+"-cancel"
	return []events.Consumer{{
		Name:    requestName,
		Subject: requestSubject,
		Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			r, err := requested(evt)
			if err != nil {
				return err
			}
			return svc.record(tx, evt.TenantID, r)
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			r, err := requested(evt)
			if err != nil {
				return err
			}
			return svc.open(tenantdb.WithActor(ctx, "system:"+requestName), evt.TenantID, r.Requester, r.RequestID)
		},
	}, {
		Name:    cancelName,
		Subject: cancelSubject,
		Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			r, err := cancelled(evt)
			if err != nil {
				return err
			}
			return svc.requestCancel(tx, evt.TenantID, r)
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			r, err := cancelled(evt)
			if err != nil {
				return err
			}
			return svc.cancelNow(tenantdb.WithActor(ctx, "system:"+cancelName), evt.TenantID, r.Requester, r.RequestID)
		},
	}}
}

// Broadcasts declares none: payments has no per-replica cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// DirectedSubjects declares none: every outcome stays in its tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none.
func (m *Module) DirectedWriteTables() []string { return nil }
