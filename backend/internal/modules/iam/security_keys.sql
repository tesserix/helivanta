SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- #1898. The security keys a person registered while signing in to Helivanta.
-- A key belongs to the person's Zitadel identity, not to a tenant: sign-in
-- has not chosen a tenant yet, so this follows login_attempt's non-tenant,
-- no-RLS rule (0004_iam) and is on tenantdb's lintAllowlist. subject is the
-- Zitadel user id from the verified session, never request input.
CREATE TABLE iam_security_keys (
  subject       text        NOT NULL CHECK (subject <> ''),
  key_id        text        NOT NULL CHECK (key_id <> ''),
  rp_id         text        NOT NULL CHECK (rp_id <> ''),
  registered_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (subject, key_id)
);

-- The key registered at the password step for this attempt; NULL when none
-- was offered (no relying party configured, or not an enrolment).
ALTER TABLE login_attempt
  ADD COLUMN security_key_id text CONSTRAINT login_attempt_security_key_id_check CHECK (security_key_id <> '');
