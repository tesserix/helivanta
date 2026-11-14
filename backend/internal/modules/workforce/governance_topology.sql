SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE FUNCTION workforce_lock_governance_topology(exclusive boolean) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE tenant uuid := NULLIF(current_setting('app.tenant_id',true),'')::uuid;
BEGIN
 IF tenant IS NULL THEN
  RAISE EXCEPTION 'tenant context required' USING ERRCODE='42501';
 END IF;
 IF exclusive THEN
  PERFORM pg_advisory_xact_lock(hashtextextended('workforce.governance_topology:'||tenant::text,0));
 ELSE
  PERFORM pg_advisory_xact_lock_shared(hashtextextended('workforce.governance_topology:'||tenant::text,0));
 END IF;
END $$;

CREATE FUNCTION workforce_governance_topology_write() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 PERFORM workforce_lock_governance_topology(true);
 RETURN NULL;
END $$;

-- Statement admission precedes row locks, preserving IAM -> topology -> root order.
CREATE TRIGGER workforce_governance_topology_write BEFORE INSERT OR UPDATE OR DELETE
 ON workforce_hospitals FOR EACH STATEMENT EXECUTE FUNCTION workforce_governance_topology_write();
