SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $source$
DECLARE retained text; needle text; replacement text;
BEGIN
 retained:=pg_get_functiondef('public.workforce_governance_draft_source(uuid,uuid,uuid)'::regprocedure);
 needle:=$old$AND p->>'staff_id'=s::text AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid$old$;
 replacement:=$new$AND ((p->>'operation'='targets' AND s IS NULL AND p->>'staff_id' IS NULL
 AND NULLIF(current_setting('app.workforce_identity_staff',true),'') IS NULL)
 OR(p->>'operation'<>'targets' AND p->>'staff_id'=s::text AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid))$new$;
 IF (length(retained)-length(replace(retained,needle,'')))/length(needle)<>1
 OR strpos(retained,$old$p->>'operation' IN('source','list')$old$)=0 THEN
 RAISE EXCEPTION 'employment targets require the retained selected Staff source';END IF;
 retained:=replace(retained,needle,replacement);
 retained:=replace(retained,$old$p->>'operation' IN('source','list')$old$,$new$p->>'operation' IN('targets','source','list')$new$);
 EXECUTE retained;
END $source$;

CREATE OR REPLACE FUNCTION workforce_governance_draft_staff_read(t uuid,h uuid,s uuid,state text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_draft_source(t,h,s)
 OR(workforce_governance_draft_source(t,h,NULL)
 AND NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb->>'operation'='targets')
 OR(state='active' AND s=(NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb->>'sponsor_id')::uuid
 AND workforce_governance_draft_source(t,h,NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid))
$$;

CREATE FUNCTION workforce_governance_target_audit(t uuid,h uuid,actor text,action text,resource uuid,changes jsonb)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_governance_draft_source(t,h,NULL)
 AND p->>'operation'='targets' AND actor=p->>'actor_subject'
 AND action='workforce.employment_preparation.target.read'
 AND changes=jsonb_build_object('authority',p,'version',(changes->>'version')::bigint)
 AND (changes->>'version')::bigint BETWEEN 1 AND 9007199254740990
 AND EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=t AND s.hospital_id=h AND s.id=resource AND s.version=(changes->>'version')::bigint),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb AS p) proof
$$;

DO $audit$
DECLARE prior record;
BEGIN
 FOR prior IN SELECT polname,pg_get_expr(polwithcheck,polrelid) AS writing FROM pg_policy
 WHERE polrelid='public.workforce_audit'::regclass AND (polpermissive OR polname='governance_draft_purpose') LOOP
 IF prior.writing IS NULL THEN RAISE EXCEPTION 'employment targets require the retained audit write boundary';END IF;
 EXECUTE format('ALTER POLICY %I ON workforce_audit WITH CHECK((%s) OR workforce_governance_target_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes))',prior.polname,prior.writing);
 END LOOP;
END $audit$;
