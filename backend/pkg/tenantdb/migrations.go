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
	}, {
		// 0002_platform_rls fixes a latent defect in 0001's function,
		// found while adding RLS to outbox_events (#835 Task 1): on a
		// pooled connection, current_setting('app.tenant_id', true)
		// does NOT reliably return NULL when the GUC is "unset".
		// Postgres's custom-GUC placeholder mechanism means that once
		// app.tenant_id has been SET LOCAL on a session (as WithTenant
		// does on every call) and the transaction ends, the value
		// reverts to '' (empty string) for the rest of that session, not
		// NULL — NULL is only returned the very first time a session
		// ever references the name. A later WithSystem transaction
		// reusing that same pooled connection then hits
		// ''::uuid, which is a hard Postgres ERROR ("invalid input
		// syntax for type uuid"), not the intended "no match" — turning
		// an ordinary zero-rows read into a 500. This was never observed
		// before Task 1 because no WithSystem transaction had ever
		// queried a table whose policy calls hms_tenant_visible; outbox_events
		// (0002_events_outbox_tenant) is the first, and
		// TestOutboxRowIsInvisibleUnderWithSystem
		// (pkg/events/outbox_rls_test.go) is what caught it — it failed
		// deterministically on every run, not intermittently.
		// NULLIF collapses both the "never set" (NULL) and "reverted to
		// unset" ('') cases to NULL before the cast, so both compare as
		// NULL (no match) rather than one of them erroring.
		ID: "0002_platform_rls",
		SQL: `
			CREATE OR REPLACE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean
			  LANGUAGE sql STABLE PARALLEL SAFE AS
			$$ SELECT row_tenant = NULLIF(current_setting('app.tenant_id', true), '')::uuid $$;`,
	}}
}
