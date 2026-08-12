package tenantdb

// Migrations returns the platform-owned schema this package requires.
//
// The tenancy predicate lives in a function rather than inlined into every
// policy so that widening it later — a hospital-group tenant seeing its
// member hospitals — is one function replacement instead of one ALTER
// POLICY per table across every module, with no way to verify completeness.
// Migrations are append-only, so that retrofit is exactly the kind of
// change that cannot be made cheaply after the fact.
//
// pkg/events owns 0001_events_outbox on the same principle: a pkg package
// may own a namespaced migration.
func Migrations() []Migration {
	return []Migration{{
		ID: "0001_platform_rls",
		SQL: `
			CREATE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean
			  LANGUAGE sql STABLE PARALLEL SAFE AS
			$$ SELECT row_tenant = current_setting('app.tenant_id', true)::uuid $$;`,
	}}
}
