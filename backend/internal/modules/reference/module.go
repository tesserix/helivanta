// Package reference is the trivial module proving the platform wiring:
// authn → tenantdb (RLS) → outbox → JetStream → consumer (issue #2).
package reference

import (
	referencecontract "github.com/tesserix/hms/internal/modules/reference/contract"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "reference" }

// Permissions is empty: reference exposes no tenant data, so every
// route is deliberately public (authz.Public).
func (m *Module) Permissions() []authz.Grant { return nil }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_reference",
		SQL: `
			CREATE TABLE reference_pings (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  message text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE reference_pings ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_pings FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_pings
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_pings (tenant_id, created_at DESC);

			CREATE TABLE reference_ping_receipts (
			  event_id uuid PRIMARY KEY,
			  ping_id uuid NOT NULL,
			  processed_at timestamptz NOT NULL DEFAULT now()
			);`,
	}, {
		ID: "0002_reference",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON reference_pings
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			ALTER TABLE reference_ping_receipts ADD COLUMN tenant_id uuid;

			-- Derive the tenant from the ping the receipt refers to. Receipts whose
			-- ping no longer exists cannot be attributed to a tenant and are dropped;
			-- they are consumer bookkeeping, not clinical data.
			UPDATE reference_ping_receipts r
			   SET tenant_id = p.tenant_id
			  FROM reference_pings p
			 WHERE p.id = r.ping_id;
			DELETE FROM reference_ping_receipts WHERE tenant_id IS NULL;

			ALTER TABLE reference_ping_receipts ALTER COLUMN tenant_id SET NOT NULL;
			ALTER TABLE reference_ping_receipts ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_ping_receipts FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_ping_receipts
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_ping_receipts (tenant_id, processed_at DESC);`,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/reference")
	pings := &pingHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/ping", authz.Public, pings.create)
	platform.ListRoute(g, "/pings", authz.Public, pings.list)
	g.GET("/pings/:id", authz.Public, pings.get)
}

// Publishes declares pinged, the one event reference emits. reference
// consumes its own event — see Consumers in consumers.go.
func (m *Module) Publishes() []string {
	return []string{referencecontract.SubjectPinged}
}
