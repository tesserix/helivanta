// Package patientmaster owns the platform's single patient registry:
// one row per human being, shared by every clinical module that needs
// to know who a visit, order or dispense is for. Registration logic and
// routes land in Task 5 (#70); this file is the module skeleton, its
// four tables, and their tenant isolation.
package patientmaster

import (
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	PermPatientRead     authz.Permission = "patient.read"
	PermPatientRegister authz.Permission = "patient.register"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "patientmaster" }

// Permissions: the receptionist registers and reads; clinical staff read.
// RoleTenantAdmin is never listed — the reconciler grants it everything.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermPatientRead, Roles: []authz.Role{
			authz.RoleReceptionist, authz.RoleDoctor, authz.RoleNurse,
			authz.RolePharmacist, authz.RoleLabTech}},
		{Permission: PermPatientRegister, Roles: []authz.Role{authz.RoleReceptionist}},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_patientmaster",
		SQL: `
			CREATE TABLE patients (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  mrn text NOT NULL,
			  given_name text NOT NULL,
			  family_name text NOT NULL DEFAULT '',
			  -- Normalised at write time so matching never re-derives it
			  -- per query, and so the index below is over the compared form.
			  normalized_name text NOT NULL DEFAULT '',
			  phonetic_key text NOT NULL DEFAULT '',
			  sex text NOT NULL DEFAULT '' CHECK (sex IN ('', 'male', 'female', 'other')),
			  dob date NOT NULL,
			  -- Dates of birth are routinely approximated to 1 January.
			  -- Recording that the date is an estimate is what stops the
			  -- matcher treating a guess as evidence.
			  dob_estimated boolean NOT NULL DEFAULT false,
			  mobile text NOT NULL DEFAULT '',
			  address_line text NOT NULL DEFAULT '',
			  -- Aadhaar: masked tail and a keyed hash ONLY. The raw value is
			  -- never written here, to logs, or to any event (spec D2).
			  aadhaar_last4 char(4),
			  aadhaar_hash text,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  updated_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, mrn),
			  -- Composite target for every child table's tenant-scoped FK
			  -- below: a plain REFERENCES patients(id) would let a session
			  -- pinned to tenant A insert a child row pointing at tenant B's
			  -- patient, because Postgres FK checks are not subject to row
			  -- security policies. RLS stops A from READING that row; only
			  -- this composite FK stops A from LINKING to it.
			  UNIQUE (tenant_id, id)
			);
			ALTER TABLE patients ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patients FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patients
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patients (tenant_id, created_at DESC);
			CREATE INDEX ON patients (tenant_id, phonetic_key);
			CREATE INDEX ON patients (tenant_id, mobile);

			-- Identifiers get their own table so ABHA linkage (#74) adds
			-- rows rather than a migration on patients.
			CREATE TABLE patient_identifiers (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_id uuid NOT NULL,
			  kind text NOT NULL CHECK (kind IN ('abha_number','abha_address','mrn_external')),
			  value text NOT NULL,
			  verified_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, kind, value),
			  FOREIGN KEY (tenant_id, patient_id) REFERENCES patients(tenant_id, id)
			);
			ALTER TABLE patient_identifiers ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_identifiers FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_identifiers
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_identifiers (tenant_id, patient_id);

			-- Registration is the first point of processing under DPDP, so
			-- this row is written in the SAME transaction as the patient
			-- (spec D5). A patient without a receipt is unreachable, not
			-- merely discouraged.
			CREATE TABLE patient_consents (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_id uuid NOT NULL,
			  notice_version text NOT NULL,
			  consented_by text NOT NULL CHECK (consented_by IN ('patient','guardian')),
			  guardian_name text NOT NULL DEFAULT '',
			  guardian_relationship text NOT NULL DEFAULT '',
			  recorded_by_subject text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  FOREIGN KEY (tenant_id, patient_id) REFERENCES patients(tenant_id, id)
			);
			ALTER TABLE patient_consents ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_consents FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_consents
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_consents (tenant_id, patient_id);

			-- Merge is out of slice. This table is what makes that honest:
			-- every duplicate deliberately created is recorded with who did
			-- it and why, and it becomes merge's worklist when merge ships.
			CREATE TABLE patient_duplicate_overrides (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  created_patient_id uuid NOT NULL,
			  matched_patient_id uuid NOT NULL,
			  score numeric(4,3) NOT NULL,
			  reason text NOT NULL,
			  actor_subject text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  FOREIGN KEY (tenant_id, created_patient_id) REFERENCES patients(tenant_id, id),
			  FOREIGN KEY (tenant_id, matched_patient_id) REFERENCES patients(tenant_id, id)
			);
			ALTER TABLE patient_duplicate_overrides ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_duplicate_overrides FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_duplicate_overrides
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_duplicate_overrides (tenant_id, created_at DESC);`,
	}}
}

// Routes mounts registration (spec D4/D5), single lookup, and the
// tenant's paginated patient list.
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/patients")
	patients := &patientHandlers{db: deps.DB, bus: deps.Bus}
	g.POST("", PermPatientRegister, patients.register)
	g.GET("/:id", PermPatientRead, patients.get)
	platform.ListRoute(g, "", PermPatientRead, patients.list)
}

// Publishes declares none: patientmaster emits nothing yet. Task 5 adds
// the registration event and its contract package alongside the route
// that publishes it.
func (m *Module) Publishes() []string { return nil }

// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: this module writes no table on
// behalf of another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

// Consumers declares none: patientmaster does not react to other
// modules' events.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }

// Broadcasts declares none: patientmaster has no per-replica cache to
// invalidate. See platform.Module.Broadcasts's doc comment (iam's
// revocation-cache invalidation is the one module that needs this).
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
