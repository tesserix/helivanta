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
	return newService(deps.Transit, newRazorpay(deps.PaymentsProviderURL))
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	accountRoutes(r.Group("/payments"), &accountHandlers{db: deps.DB, svc: serviceFor(deps), webhookBase: deps.PaymentsWebhookBaseURL})
}

func (m *Module) Publishes() []string {
	return []string{paymentscontract.SubjectIntentOpened, paymentscontract.SubjectIntentFailed, paymentscontract.SubjectIntentPaid,
		paymentscontract.SubjectIntentExpired, paymentscontract.SubjectIntentCancelled}
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	svc := serviceFor(deps)
	return []events.Consumer{{
		Name:    "payments-pharmacy-request",
		Subject: pharmacycontract.SubjectPaymentRequested,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d pharmacycontract.PaymentRequestedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return svc.open(ctx, tx, evt.TenantID, openRequest{Requester: paymentscontract.RequesterPharmacy, RequestID: d.RequestID,
				AmountMinor: d.AmountMinor, Currency: d.Currency, ExpiresInSeconds: d.ExpiresInSeconds})
		},
	}, {
		Name:    "payments-pharmacy-cancel",
		Subject: pharmacycontract.SubjectPaymentCancelled,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d pharmacycontract.PaymentCancelledData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return svc.cancel(ctx, tx, paymentscontract.RequesterPharmacy, d.RequestID)
		},
	}}
}

// Broadcasts declares none: payments has no per-replica cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// DirectedSubjects declares none: every outcome stays in its tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none.
func (m *Module) DirectedWriteTables() []string { return nil }
