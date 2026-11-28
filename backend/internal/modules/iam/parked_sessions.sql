SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- #1778. A password-verified Zitadel session parked by a password-setup step
-- so /login can continue without a second password entry. It lives in
-- login_attempt (same 5-minute expiry, sweep, mutation guard and no-RLS
-- reasoning as 0004_iam), keyed by 'parked:' || sha256(handle) so it can never
-- collide with a Zitadel auth request id, at its own stage so no factor,
-- enrolment or password-change endpoint can read it.
ALTER TABLE login_attempt DROP CONSTRAINT login_attempt_stage_check,
  ADD CONSTRAINT login_attempt_stage_check CHECK (stage IN ('factor', 'password_change', 'parked'));
