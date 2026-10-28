SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- These transaction-local gates are populated only from request-bound authority
-- after locking the actor's current staff record and reviewed login link.
CREATE FUNCTION workforce_leave_review_context(row_tenant uuid,row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT hms_tenant_visible(row_tenant)
 AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_leave_review',true)='authorized'
 AND NULLIF(current_setting('app.workforce_leave_review_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_leave_review_staff',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_leave_review_request',true),'') IS NOT NULL
$$;

CREATE FUNCTION workforce_leave_review_request()
RETURNS TABLE(tenant_id uuid,hospital_id uuid,staff_id uuid,request_id uuid,
 leave_type_id uuid,leave_type_revision_id uuid,assignment_revision_id uuid,
 leave_year smallint,start_on date,end_on date)
LANGUAGE sql STABLE AS $$
 SELECT r.tenant_id,r.hospital_id,r.staff_id,r.id,r.leave_type_id,
 r.leave_type_revision_id,r.assignment_revision_id,r.leave_year,r.start_on,r.end_on
 FROM workforce_leave_requests r
 WHERE workforce_leave_review_context(r.tenant_id,r.hospital_id)
 AND r.id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid
 AND r.staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid
$$;

-- Preserve the single permissive owner policy required by the live RLS gate.
-- The restrictive DELETE policy keeps every pre-existing removal restriction
-- intact when a scoped reader is admitted through that owner policy.
DO $migration$
DECLARE item record; prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
('workforce_login_links',$scope$(hms_tenant_visible(tenant_id)
 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND hospital_id=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_leave_actor_check',true)='authorized'
 AND subject=NULLIF(current_setting('app.workforce_subject',true),'')
 AND status='active') OR (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.workforce_leave_discovery',true)='authorized' AND subject=NULLIF(current_setting('app.workforce_subject',true),'') AND status='active')$scope$,$scope$false$scope$),
('workforce_staff',$scope$(hms_tenant_visible(tenant_id)
 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND hospital_id=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_leave_actor_check',true)='authorized'
 AND id=NULLIF(current_setting('app.workforce_leave_review_actor',true),'')::uuid) OR (workforce_leave_review_context(tenant_id,hospital_id)
 AND id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid)$scope$,$scope$false$scope$),
('workforce_leave_requests',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid) OR (workforce_leave_review_context(tenant_id,hospital_id)
 AND id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid)$scope$,$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid
 AND requested_by_subject<>NULLIF(current_setting('app.workforce_subject',true),'')
 AND status IN ('approved','rejected'))$scope$),
('workforce_leave_request_days',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND request_id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid)$scope$,$scope$false$scope$),
('workforce_leave_request_events',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND request_id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid)$scope$,$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND request_id=NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.workforce_subject',true),'')
 AND status IN ('approved','rejected'))$scope$),
('workforce_staff_leave_assignment_revisions',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid
 AND id IN (SELECT r.assignment_revision_id FROM workforce_leave_review_request() r))$scope$,$scope$false$scope$),
('workforce_leave_years',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND leave_year IN (SELECT r.leave_year FROM workforce_leave_review_request() r))$scope$,$scope$false$scope$),
('workforce_leave_structure_entries',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND revision_id IN (
  SELECT a.structure_revision_id FROM workforce_leave_review_request() r
  JOIN workforce_staff_leave_assignment_revisions a
  ON (a.tenant_id,a.hospital_id,a.id)=(r.tenant_id,r.hospital_id,r.assignment_revision_id)
 ))$scope$,$scope$false$scope$),
('workforce_leave_config_revisions',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND id IN (
  SELECT r.leave_type_revision_id FROM workforce_leave_review_request() r
  UNION
  SELECT a.structure_revision_id FROM workforce_leave_review_request() r
  JOIN workforce_staff_leave_assignment_revisions a
  ON (a.tenant_id,a.hospital_id,a.id)=(r.tenant_id,r.hospital_id,r.assignment_revision_id)
  UNION
  SELECT e.leave_type_revision_id FROM workforce_leave_review_request() r
  JOIN workforce_staff_leave_assignment_revisions a
  ON (a.tenant_id,a.hospital_id,a.id)=(r.tenant_id,r.hospital_id,r.assignment_revision_id)
  JOIN workforce_leave_structure_entries e
  ON (e.tenant_id,e.hospital_id,e.revision_id)=(a.tenant_id,a.hospital_id,a.structure_revision_id)
 ))$scope$,$scope$false$scope$),
('workforce_leave_type_rules',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND revision_id IN (SELECT r.leave_type_revision_id FROM workforce_leave_review_request() r))$scope$,$scope$false$scope$),
('workforce_leave_ledger',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND (staff_id,leave_type_id,leave_year) IN (
  SELECT r.staff_id,r.leave_type_id,r.leave_year FROM workforce_leave_review_request() r
 ))$scope$,$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND kind='consumption'
 AND actor_subject=NULLIF(current_setting('app.workforce_subject',true),'')
 AND (staff_id,leave_type_id,leave_year,request_id) IN (
  SELECT r.staff_id,r.leave_type_id,r.leave_year,r.request_id FROM workforce_leave_review_request() r
 ))$scope$),
('workforce_enrollment_profile',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid)$scope$,$scope$false$scope$),
('workforce_roster_allocations',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND removed_version IS NULL
 AND EXISTS (
  SELECT 1 FROM workforce_leave_review_request() r
  WHERE r.staff_id=workforce_roster_allocations.staff_id
  AND workforce_roster_allocations.roster_date BETWEEN r.start_on AND r.end_on
 ))$scope$,$scope$false$scope$),
('workforce_rosters',$scope$(workforce_leave_review_context(tenant_id,hospital_id)
 AND id IN (
  SELECT a.roster_id FROM workforce_leave_review_request() r
  JOIN workforce_roster_allocations a
  ON (a.tenant_id,a.hospital_id,a.staff_id)=(r.tenant_id,r.hospital_id,r.staff_id)
  WHERE a.removed_version IS NULL AND a.roster_date BETWEEN r.start_on AND r.end_on
 ))$scope$,$scope$false$scope$)
 ) AS scoped(table_name,read_expression,write_expression)
 LOOP
  SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
   COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),pg_get_expr(p.polqual,p.polrelid)) AS write_expression
  INTO STRICT prior FROM pg_policy p
  WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  IF prior.polcmd<>'*' OR prior.read_expression IS NULL OR prior.write_expression IS NULL THEN
   RAISE EXCEPTION 'reviewed leave requires one complete owner policy on %',item.table_name;
  END IF;
  EXECUTE format('CREATE POLICY reviewed_leave_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s)) WITH CHECK ((%s) OR (%s))',
   prior.polname,item.table_name,prior.read_expression,item.read_expression,prior.write_expression,item.write_expression);
 END LOOP;
END
$migration$;

CREATE FUNCTION workforce_leave_review_read_context(row_tenant uuid,row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT hms_tenant_visible(row_tenant)
 AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_leave_review_read',true)='authorized'
 AND current_setting('app.workforce_leave_review_read_kind',true) IN ('queue','detail')
 AND NULLIF(current_setting('app.workforce_leave_review_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL
$$;
CREATE FUNCTION workforce_leave_review_readable_request(row_tenant uuid,row_hospital uuid,request_id uuid,staff_id uuid,filer text,request_status text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_leave_review_read_context(row_tenant,row_hospital)
 AND request_status='pending'
 AND staff_id<>NULLIF(current_setting('app.workforce_leave_review_actor',true),'')::uuid
 AND filer<>NULLIF(current_setting('app.workforce_subject',true),'')
 AND (current_setting('app.workforce_leave_review_read_kind',true)='queue' OR (
 request_id=NULLIF(current_setting('app.workforce_leave_review_read_request',true),'')::uuid
 AND staff_id=NULLIF(current_setting('app.workforce_leave_review_read_staff',true),'')::uuid))
$$;
CREATE FUNCTION workforce_leave_review_read_requests()
RETURNS TABLE(tenant_id uuid,hospital_id uuid,staff_id uuid,request_id uuid,leave_type_revision_id uuid,start_on date,end_on date)
LANGUAGE sql STABLE AS $$
 SELECT r.tenant_id,r.hospital_id,r.staff_id,r.id,r.leave_type_revision_id,r.start_on,r.end_on
 FROM workforce_leave_requests r
 WHERE workforce_leave_review_readable_request(r.tenant_id,r.hospital_id,r.id,r.staff_id,r.requested_by_subject,r.status)
$$;
DO $read_migration$
DECLARE item record; prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('workforce_leave_requests',$read$workforce_leave_review_readable_request(tenant_id,hospital_id,id,staff_id,requested_by_subject,status)$read$),
 ('workforce_staff',$read$(workforce_leave_review_read_context(tenant_id,hospital_id) AND id IN (SELECT r.staff_id FROM workforce_leave_review_read_requests() r))$read$),
 ('workforce_leave_config_revisions',$read$(workforce_leave_review_read_context(tenant_id,hospital_id) AND id IN (SELECT r.leave_type_revision_id FROM workforce_leave_review_read_requests() r))$read$),
 ('workforce_leave_request_days',$read$(workforce_leave_review_read_context(tenant_id,hospital_id) AND current_setting('app.workforce_leave_review_read_kind',true)='detail' AND request_id IN (SELECT r.request_id FROM workforce_leave_review_read_requests() r))$read$),
 ('workforce_roster_allocations',$read$(workforce_leave_review_read_context(tenant_id,hospital_id) AND current_setting('app.workforce_leave_review_read_kind',true)='detail' AND removed_version IS NULL AND EXISTS (SELECT 1 FROM workforce_leave_review_read_requests() r WHERE r.staff_id=workforce_roster_allocations.staff_id AND workforce_roster_allocations.roster_date BETWEEN r.start_on AND r.end_on))$read$),
 ('workforce_rosters',$read$(workforce_leave_review_read_context(tenant_id,hospital_id) AND current_setting('app.workforce_leave_review_read_kind',true)='detail' AND id IN (SELECT a.roster_id FROM workforce_leave_review_read_requests() r JOIN workforce_roster_allocations a ON (a.tenant_id,a.hospital_id,a.staff_id)=(r.tenant_id,r.hospital_id,r.staff_id) WHERE a.removed_version IS NULL AND a.roster_date BETWEEN r.start_on AND r.end_on))$read$)
 ) AS reads(table_name,read_expression) LOOP
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression
 INTO STRICT prior FROM pg_policy p
 WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
 EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_expression);
 END LOOP;
END $read_migration$;
