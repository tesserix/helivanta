SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_posting_inbox_context(t uuid,h uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_scoped_posting_context(t,h,NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid,false)
 AND current_setting('app.workforce_posting_inbox',true)='read'
 AND current_setting('app.workforce_posting',true)='read'
 AND current_setting('app.workforce_posting_scoped',true)='read'
 AND current_setting('app.workforce_posting_review_mode',true)='read'
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.proposal.read',false)
$$;

DO $migration$
DECLARE item record; prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('workforce_posting_proposals',$scope$workforce_posting_inbox_context(tenant_id,hospital_id)$scope$),
  ('workforce_posting_proposal_revisions',$scope$workforce_posting_inbox_context(tenant_id,hospital_id)
   AND EXISTS(SELECT 1 FROM workforce_posting_proposals p
   WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id)=
   (workforce_posting_proposal_revisions.tenant_id,workforce_posting_proposal_revisions.hospital_id,
   workforce_posting_proposal_revisions.staff_id,workforce_posting_proposal_revisions.proposal_id)
   AND (workforce_posting_proposal_revisions.version=1 OR workforce_posting_proposal_revisions.id=p.revision_id))$scope$)
 ) AS scopes(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
   pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  IF prior.write_expression IS NULL THEN
   RAISE EXCEPTION 'posting inbox requires an explicit unchanged write policy';
  END IF;
  EXECUTE format('CREATE POLICY posting_inbox_update_guard ON %I AS RESTRICTIVE FOR UPDATE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('CREATE POLICY posting_inbox_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
END $migration$;
