-- #1287: Pakistan is no longer a supported country. Existing Pakistan
-- outlets and rupee amounts are refused, not migrated: moving them to India
-- would change their currency, time zone and tax rules, a decision no
-- migration can make. An operator follows docs/deployment/remove-pakistan-release.md.
-- The outlet check stays a two-letter pattern: pharmacy takes its rules from
-- the country profile, and the API refuses a code without one.
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $$
DECLARE n bigint; found text := ''; c record;
BEGIN
  FOR c IN SELECT * FROM (VALUES
    ('pharmacy_outlets', 'country_code = ''PK'''),
    ('pharmacy_catalogue_products', 'currency = ''PKR'''),
    ('pharmacy_payables', 'currency = ''PKR'''),
    ('pharmacy_settlements', 'currency = ''PKR''')
  ) AS t(tbl, pakistan)
  LOOP
    -- The owner sees no tenant's rows under FORCE ROW LEVEL SECURITY, so a
    -- plain count would always be 0. Lift it inside this transaction only;
    -- ALTER TABLE holds ACCESS EXCLUSIVE until commit, so no other session
    -- sees it unforced, and a raise rolls it back.
    EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', c.tbl);
    EXECUTE format('SELECT count(*) FROM %I WHERE %s', c.tbl, c.pakistan) INTO n;
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', c.tbl);
    IF n > 0 THEN
      found := found || format(' %s %s', c.tbl, n);
    END IF;
  END LOOP;
  IF found <> '' THEN
    RAISE EXCEPTION '#1287: Pakistan rows:%; see docs/deployment/remove-pakistan-release.md', found;
  END IF;
END $$;
