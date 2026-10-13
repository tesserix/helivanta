\set ON_ERROR_STOP on
BEGIN;
SET LOCAL ROLE hms_app;
DO $$
DECLARE
  tenant_a uuid := gen_random_uuid();
  tenant_b uuid := gen_random_uuid();
  hospital_a uuid := gen_random_uuid();
  hospital_b uuid := gen_random_uuid();
  n integer;
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
    RAISE EXCEPTION 'test role bypasses RLS';
  END IF;
  PERFORM set_config('app.tenant_id', tenant_a::text, true);
  PERFORM set_config('app.workforce_subject', 'synthetic-live-rls-check', true);
  PERFORM set_config('app.workforce_hospital', hospital_a::text, true);
  INSERT INTO workforce_hospitals(id,tenant_id,name) VALUES
    (hospital_a,tenant_a,'Synthetic rollback hospital A'),(hospital_b,tenant_a,'Synthetic rollback hospital B');
  INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES
    (tenant_a,hospital_a,'synthetic-live-rls-check','manager'),(tenant_a,hospital_b,'synthetic-live-rls-check','manager');
  INSERT INTO workforce_staff(id,tenant_id,hospital_id,employee_code,display_name,job_category,employment_status)
    VALUES(gen_random_uuid(),tenant_a,hospital_a,'SYN-A','Synthetic rollback staff A','nurse','active');
  PERFORM set_config('app.workforce_hospital', hospital_b::text, true);
  INSERT INTO workforce_staff(id,tenant_id,hospital_id,employee_code,display_name,job_category,employment_status)
    VALUES(gen_random_uuid(),tenant_a,hospital_b,'SYN-B','Synthetic rollback staff B','doctor','active');
  SELECT count(*) INTO n FROM workforce_staff;
  IF n <> 1 THEN RAISE EXCEPTION 'hospital isolation failed'; END IF;
  PERFORM set_config('app.workforce_subject', 'synthetic-outsider', true);
  SELECT count(*) INTO n FROM workforce_staff;
  IF n <> 0 THEN RAISE EXCEPTION 'grant enforcement failed'; END IF;
  PERFORM set_config('app.workforce_subject', 'synthetic-live-rls-check', true);
  PERFORM set_config('app.tenant_id', tenant_b::text, true);
  SELECT count(*) INTO n FROM workforce_staff;
  IF n <> 0 THEN RAISE EXCEPTION 'tenant isolation failed'; END IF;
  PERFORM set_config('app.tenant_id', tenant_a::text, true);
  UPDATE workforce_hospital_access SET level='reader' WHERE hospital_id=hospital_b;
  BEGIN
    INSERT INTO workforce_staff(id,tenant_id,hospital_id,employee_code,display_name,job_category,employment_status)
      VALUES(gen_random_uuid(),tenant_a,hospital_b,'SYN-DENIED','Synthetic denied','nurse','active');
    RAISE EXCEPTION 'reader write was accepted';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    UPDATE workforce_audit SET action='synthetic-tampering';
    RAISE EXCEPTION 'audit modification was accepted';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
END $$;
ROLLBACK;
SELECT 'live_rls_check_passed_all_fixture_writes_rolled_back';
