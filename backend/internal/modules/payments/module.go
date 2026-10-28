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

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "payments" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermAccountManage}}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_payments", SQL: schema}}
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
// so no consumer holds a database connection while Razorpay answers.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	svc := serviceFor(deps)
	requested := func(evt events.Event) (openRequest, error) {
		var d pharmacycontract.PaymentRequestedData
		err := json.Unmarshal(evt.Data, &d)
		return openRequest{Requester: paymentscontract.RequesterPharmacy, RequestID: d.RequestID, AmountMinor: d.AmountMinor,
			Currency: d.Currency, ExpiresInSeconds: d.ExpiresInSeconds}, err
	}
	cancelled := func(evt events.Event) (openRequest, error) {
		var d pharmacycontract.PaymentCancelledData
		err := json.Unmarshal(evt.Data, &d)
		return openRequest{Requester: paymentscontract.RequesterPharmacy, RequestID: d.RequestID, AmountMinor: d.AmountMinor, Currency: d.Currency}, err
	}
	return []events.Consumer{{
		Name:    "payments-pharmacy-request",
		Subject: pharmacycontract.SubjectPaymentRequested,
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
			return svc.open(tenantdb.WithActor(ctx, "system:payments-pharmacy-request"), evt.TenantID, r.Requester, r.RequestID)
		},
	}, {
		Name:    "payments-pharmacy-cancel",
		Subject: pharmacycontract.SubjectPaymentCancelled,
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
			return svc.cancelNow(tenantdb.WithActor(ctx, "system:payments-pharmacy-cancel"), evt.TenantID, r.Requester, r.RequestID)
		},
	}}
}

// Broadcasts declares none: payments has no per-replica cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// DirectedSubjects declares none: every outcome stays in its tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none.
func (m *Module) DirectedWriteTables() []string { return nil }
