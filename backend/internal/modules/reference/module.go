// Package reference is the trivial module proving the platform wiring:
// authn → tenantdb (RLS) → outbox → JetStream → consumer (issue #2).
package reference

import (
	referencecontract "github.com/tesserix/helivanta/internal/modules/reference/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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
	}, {
		// #932. The proof table for directed cross-tenant writes.
		//
		// tenant_id is the DESTINATION — the tenant this row belongs to,
		// and the only one whose RLS context can read it. origin_* record
		// who disclosed it and which record it came from, NOT NULL so the
		// answer exists on every row (design D5); LintDirectedProvenance
		// fails the boot if either is dropped or made nullable.
		//
		// The policy is the ordinary shape: USING widens through
		// hms_tenant_visible, WITH CHECK stays pinned to strict equality.
		// A directed write needs NO policy exception — that it does not
		// is the evidence this is a copy, not a widening (design D6).
		ID: "0003_reference",
		SQL: `
			CREATE TABLE reference_forwarded_pings (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  origin_tenant_id uuid NOT NULL,
			  origin_record_id uuid NOT NULL,
			  message text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE reference_forwarded_pings ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_forwarded_pings FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_forwarded_pings
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_forwarded_pings (tenant_id, created_at DESC);
			CREATE INDEX ON reference_forwarded_pings (origin_tenant_id, origin_record_id);`,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/reference")
	pings := &pingHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/ping", authz.Public, pings.create)
	platform.ListRoute(g, "/pings", authz.Public, pings.list)
	g.GET("/pings/:id", authz.Public, pings.get)
}

// Publishes declares the two events reference emits. reference consumes
// both of its own events — see Consumers in consumers.go.
func (m *Module) Publishes() []string {
	return []string{
		referencecontract.SubjectPinged,
		referencecontract.SubjectPingForwarded,
	}
}

// DirectedSubjects: ping_forwarded is the one reference event that
// creates data in a tenant other than the publisher's (#932).
func (m *Module) DirectedSubjects() []string {
	return []string{referencecontract.SubjectPingForwarded}
}

// DirectedWriteTables: the table reference-forwarded writes into on
// behalf of the destination tenant.
func (m *Module) DirectedWriteTables() []string {
	return []string{"reference_forwarded_pings"}
}
