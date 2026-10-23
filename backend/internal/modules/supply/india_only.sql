-- #1287: Pakistan is no longer a supported country. Existing Pakistan
-- supply records are refused, not migrated: moving a party, premises or
-- licence to India would change its registrations, identifiers and
-- regulatory scope, a decision no migration can make. An operator follows
-- docs/deployment/remove-pakistan-release.md.
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $$
DECLARE n bigint; found text := ''; c record;
BEGIN
  -- Every supply table that records a country, found rather than listed, so
  -- none is missed. The owner sees no tenant's rows under FORCE ROW LEVEL
  -- SECURITY, so a plain count would always be 0: lift it inside this
  -- transaction only. ALTER TABLE holds ACCESS EXCLUSIVE until commit, so no
  -- other session sees it unforced, and a raise rolls it back.
  FOR c IN
    SELECT col.table_name AS tbl,
      CASE WHEN col.table_name = 'supply_party_tax_registrations'
        THEN 'country_code = ''PK'' OR kind IN (''ntn'',''stn'')'
        ELSE 'country_code = ''PK''' END AS pakistan
    FROM information_schema.columns col
    JOIN information_schema.tables t ON t.table_schema = col.table_schema AND t.table_name = col.table_name
    WHERE col.table_schema = current_schema() AND col.column_name = 'country_code'
      AND t.table_type = 'BASE TABLE' AND col.table_name LIKE 'supply\_%'
    ORDER BY col.table_name
  LOOP
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

  -- The checks that allowed Pakistan become India only.
  FOR c IN
    SELECT conrelid::regclass::text AS tbl, conname FROM pg_constraint
    WHERE contype = 'c' AND conrelid::regclass::text LIKE 'supply\_%'
      AND pg_get_constraintdef(oid) LIKE '%''PK''%'
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', c.tbl, c.conname);
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = c.tbl::regclass AND conname = c.tbl || '_india') THEN
      IF c.tbl = 'supply_party_tax_registrations' THEN
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (country_code = ''IN'' AND kind IN (''gstin'',''pan'',''other''))', c.tbl, c.tbl || '_india');
      ELSE
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (country_code = ''IN'')', c.tbl, c.tbl || '_india');
      END IF;
    END IF;
  END LOOP;
END $$;
