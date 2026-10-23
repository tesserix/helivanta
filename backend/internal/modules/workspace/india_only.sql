-- #1287: Pakistan is no longer a supported country. Existing Pakistan
-- policies are refused, not migrated: moving one to India would change its
-- currency and regulatory scope, a decision no migration can make. An
-- operator deletes or corrects them, then re-runs.
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $$
DECLARE n bigint; c record;
BEGIN
  -- The owner sees no tenant's rows under FORCE ROW LEVEL SECURITY, so a
  -- plain count would always be 0. Lift it inside this transaction only;
  -- ALTER TABLE holds ACCESS EXCLUSIVE until commit, so no other session
  -- sees it unforced, and a raise rolls it back.
  ALTER TABLE workspace_country_policy_scopes NO FORCE ROW LEVEL SECURITY;
  SELECT count(*) INTO n FROM workspace_country_policy_scopes WHERE country <> 'IN' OR currency <> 'INR';
  ALTER TABLE workspace_country_policy_scopes FORCE ROW LEVEL SECURITY;
  IF n > 0 THEN
    RAISE EXCEPTION '#1287: % non-India rows in workspace_country_policy_scopes; delete or correct them, then re-run', n;
  END IF;
  FOR c IN SELECT conname FROM pg_constraint
    WHERE conrelid = 'workspace_country_policy_scopes'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%''PK''%'
  LOOP
    EXECUTE format('ALTER TABLE workspace_country_policy_scopes DROP CONSTRAINT %I', c.conname);
  END LOOP;
END $$;

ALTER TABLE workspace_country_policy_scopes
  ADD CONSTRAINT workspace_country_policy_india CHECK (country = 'IN'),
  ADD CONSTRAINT workspace_country_policy_currency CHECK (currency = 'INR');
