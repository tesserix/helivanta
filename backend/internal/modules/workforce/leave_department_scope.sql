SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- ADR-0055 slice 2: a department leave session reaches only requesters posted in its locked subtree.
CREATE FUNCTION workforce_leave_department_session() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(current_setting('app.workforce_leave_department',true)='authorized' AND workforce_leave_review_session(),false)
$$;

CREATE FUNCTION workforce_leave_department_subtree() RETURNS uuid[] LANGUAGE sql STABLE AS $$
 SELECT string_to_array(NULLIF(current_setting('app.workforce_leave_department_subtree',true),''),',')::uuid[]
$$;

CREATE FUNCTION workforce_leave_department_reads(t uuid,h uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_leave_department_session() AND hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid,false)
$$;

-- Necessary condition only; the owner live check in Go also applies cutoffs, generations and the all-postings rule.
CREATE FUNCTION workforce_leave_department_candidate(t uuid,h uuid,s uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_posting_proposals p
 JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.id)=(p.tenant_id,p.revision_id)
 WHERE (p.tenant_id,p.hospital_id,p.staff_id)=(t,h,s) AND p.status='approved'
 AND r.starts_at<=now() AND (r.ends_at IS NULL OR r.ends_at>now()) AND r.eligibility_until>now()
 AND r.department_id=ANY(workforce_leave_department_subtree()))
$$;

-- Read-only approved branches span the hospital so Go can prove no live posting lies outside the subtree.
DO $migration$
DECLARE item record; prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('workforce_posting_proposals',$scope$workforce_leave_department_reads(tenant_id,hospital_id) AND status='approved'$scope$),
  ('workforce_posting_proposal_revisions',$scope$workforce_leave_department_reads(tenant_id,hospital_id) AND status='approved'
   AND EXISTS(SELECT 1 FROM workforce_posting_proposals p
   WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.status)=
   (workforce_posting_proposal_revisions.tenant_id,workforce_posting_proposal_revisions.hospital_id,
   workforce_posting_proposal_revisions.staff_id,workforce_posting_proposal_revisions.proposal_id,
   workforce_posting_proposal_revisions.id,workforce_posting_proposal_revisions.version,'approved'))$scope$)
 ) AS scopes(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
   pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  IF prior.write_expression IS NULL THEN
   RAISE EXCEPTION 'department leave reads require an unchanged explicit write policy';
  END IF;
  EXECUTE format('CREATE POLICY leave_department_update_guard ON %I AS RESTRICTIVE FOR UPDATE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('CREATE POLICY leave_department_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
END $migration$;

CREATE OR REPLACE FUNCTION workforce_posting_proposal_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_scoped_posting_context(t,h,s,writing)
 AND ((current_setting('app.workforce_posting_review_mode',true) IN ('read','submit','approve','reject','withdraw')
 AND ((NOT writing AND current_setting('app.workforce_posting_permission',true) IN ('workforce.posting.proposal.read','workforce.posting.submit','workforce.posting.review'))
 OR (writing AND ((current_setting('app.workforce_posting_review_mode',true) IN ('submit','withdraw')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.submit')
 OR (current_setting('app.workforce_posting_review_mode',true) IN ('approve','reject')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.review')))))
 OR (NOT writing AND current_setting('app.workforce_posting_review_mode',true)='source_use'
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting_source.use')
 OR (NOT writing AND current_setting('app.workforce_posting_review_mode',true)='leave_department'
 AND current_setting('app.workforce_posting_permission',true)='workforce.leave.approve'
 AND workforce_leave_department_session())),false)
$$;

CREATE POLICY leave_department_review_guard ON workforce_leave_requests AS RESTRICTIVE
 USING (NOT workforce_leave_department_session() OR workforce_leave_department_candidate(tenant_id,hospital_id,staff_id));
