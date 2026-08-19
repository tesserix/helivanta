-- Three roles, because there are three genuinely different privileges and
-- conflating any two of them is how #894 happened.
--
--   helivanta        the schema OWNER (created by the Postgres image as its
--                    POSTGRES_USER). Runs migrations. FORCE ROW LEVEL
--                    SECURITY binds it like any other role.
--   hms_app          the runtime role. No superuser, no RLS bypass.
--   helivanta_system the ONLY role that may cross tenants.
--
-- This file previously constrained hms_app and said nothing about the
-- owner. That omission was the bug: in dev the owner is the image's
-- superuser and in production it is a plain CNPG role, so a whole-system
-- read that returned every row here returned zero rows there — silently,
-- because a policy that hides every row returns no rows, not an error.
-- The owner's attributes could not be stated here (Postgres refuses to
-- strip SUPERUSER from the bootstrap role), which is precisely why the
-- system role below exists rather than the owner being granted BYPASSRLS.

-- Runtime role: no superuser, no RLS bypass. Migrations run as `helivanta`.
CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;

-- System role: BYPASSRLS and nothing else. Used by exactly three callers
-- — the authorization reconciler, the outbox dispatcher and the retention
-- pruner — each of which enumerates every tenant in one pass and so has no
-- single tenant GUC to scope by. internal/archtest's
-- TestWithAllTenantsIsOnlyCalledFromTheAllowlist keeps that set closed.
--
-- It is deliberately NOT the owner and deliberately has no DDL: the
-- alternative considered was `ALTER ROLE helivanta BYPASSRLS`, which is one
-- statement and no code change, but removes FORCE's binding on the owner
-- across every forced table for every admin-pool connection — including
-- migrations — to fix three reads.
CREATE ROLE helivanta_system LOGIN PASSWORD 'helivanta_system' NOSUPERUSER BYPASSRLS;

GRANT USAGE ON SCHEMA public TO hms_app, helivanta_system;
-- BYPASSRLS decides whether the row POLICIES apply; it grants no table
-- privileges of its own. Without these grants the system pool connects
-- fine and then fails every statement with "permission denied for table".
ALTER DEFAULT PRIVILEGES FOR ROLE helivanta IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app, helivanta_system;
ALTER DEFAULT PRIVILEGES FOR ROLE helivanta IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO hms_app, helivanta_system;
