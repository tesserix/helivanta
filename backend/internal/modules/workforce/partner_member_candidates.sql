SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION workforce_partner_candidate_context(t uuid,h uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_partner_hospital',true),'')::uuid
 AND current_setting('app.workforce_partner_candidates',true)='read'
 AND NULLIF(current_setting('app.workforce_partner_subject',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_partner_actor',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_partner_link',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_partner_generation',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_partner_assignment',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_partner_assignment_version',true),'')::bigint>0,false)
$$;
DO $migration$
DECLARE tab text;prior record;
BEGIN
 FOREACH tab IN ARRAY ARRAY['workforce_staff','workforce_login_links','workforce_posting_proposals','workforce_posting_proposal_revisions']LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,pg_get_expr(p.polwithcheck,p.polrelid)AS write_expression INTO STRICT prior
  FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(tab))::regclass AND p.polpermissive;
  IF prior.write_expression IS NULL THEN RAISE EXCEPTION 'candidate browsing requires unchanged explicit write checks';END IF;
  EXECUTE format('CREATE POLICY partner_candidate_update_guard ON %I AS RESTRICTIVE FOR UPDATE USING(%s)',tab,prior.read_expression);
  EXECUTE format('CREATE POLICY partner_candidate_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING(%s)',tab,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING((%s) OR workforce_partner_candidate_context(tenant_id,hospital_id))',prior.polname,tab,prior.read_expression);
 END LOOP;
END $migration$;
