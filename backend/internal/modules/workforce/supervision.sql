SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION workforce_lock_supervision(p_tenant uuid, p_hospital uuid)
RETURNS void LANGUAGE plpgsql
SET search_path = pg_catalog, public
SET lock_timeout = '5s'
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(
    'workforce.supervision:' || p_tenant::text || ':' || p_hospital::text, 0));
END
$$;

CREATE OR REPLACE FUNCTION workforce_assert_supervision_acyclic()
RETURNS trigger LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
  circular boolean;
BEGIN
  IF NEW.supervisor_id IS NULL THEN
    RETURN NEW;
  END IF;
  IF current_setting('transaction_isolation') NOT IN ('read committed', 'read uncommitted') THEN
    RAISE EXCEPTION 'Staff supervision requires a current read-committed snapshot.'
      USING ERRCODE = '25001', CONSTRAINT = 'workforce_supervision_isolation';
  END IF;
  -- RLS-hidden ancestors must not look like a terminal reporting node.
  PERFORM 1 FROM workforce_hospital_access a
    WHERE a.tenant_id = NEW.tenant_id AND a.hospital_id = NEW.hospital_id
      AND a.subject = NULLIF(current_setting('app.workforce_subject', true), '')
      AND a.level = 'manager'
    FOR SHARE;
  IF NOT FOUND OR workforce_enrollment_allowed(NEW.tenant_id, NEW.hospital_id) IS NOT TRUE THEN
    RAISE EXCEPTION 'Staff supervision authority is unavailable.'
      USING ERRCODE = '42501', CONSTRAINT = 'workforce_supervision_authority';
  END IF;
  PERFORM workforce_lock_supervision(NEW.tenant_id, NEW.hospital_id);
  -- Bound each hop to its composite primary key; a plain join can rescan the hospital.
  WITH RECURSIVE chain(staff_id, supervisor_id) AS (
    SELECT seed.staff_id, parent.supervisor_id
    FROM (VALUES (NEW.supervisor_id)) seed(staff_id)
    LEFT JOIN LATERAL (
      SELECT p.supervisor_id FROM workforce_enrollment_profile p
      WHERE p.tenant_id = NEW.tenant_id AND p.hospital_id = NEW.hospital_id
        AND p.staff_id = seed.staff_id
      LIMIT 1
    ) parent ON true
    UNION
    SELECT c.supervisor_id, parent.supervisor_id
    FROM chain c
    LEFT JOIN LATERAL (
      SELECT p.supervisor_id FROM workforce_enrollment_profile p
      WHERE p.tenant_id = NEW.tenant_id AND p.hospital_id = NEW.hospital_id
        AND p.staff_id = c.supervisor_id
      LIMIT 1
    ) parent ON true
    WHERE c.supervisor_id IS NOT NULL
  )
  SELECT bool_or(staff_id = NEW.staff_id)
    OR NOT bool_or(supervisor_id IS NULL)
  FROM chain
  INTO circular;
  IF circular THEN
    RAISE EXCEPTION 'Supervisor would create a circular reporting chain.'
      USING ERRCODE = '23514', CONSTRAINT = 'workforce_enrollment_supervisor_acyclic';
  END IF;
  RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER workforce_enrollment_supervisor_acyclic
AFTER INSERT OR UPDATE ON workforce_enrollment_profile
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION workforce_assert_supervision_acyclic();
