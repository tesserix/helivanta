SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_login_review_context(t uuid,e uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND e=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_login_review_mode',true) IN('read','review')
 AND p->>'tenant_id'=t::text AND p->>'employer_id'=e::text
 AND p->>'capability'='workforce.account_link.review'
 AND p->>'employer_kind' IN('hospital','pharmacy','supplier')
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND (p->>'expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz IS NOT NULL
 AND (
  (p->>'kind'='owner' AND (p->>'owner_membership_id')::uuid IS NOT NULL
   AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
  OR (p->>'kind'='grant' AND (p->>'source_grant_id')::uuid IS NOT NULL
   AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
   AND p->>'template_key'='workforce.account_link_reviewer.v1' AND p->>'owner_membership_id' IS NULL)
 ),false)
 FROM (SELECT NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_login_review_staff(t uuid,e uuid,s uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_login_review_context(t,e)
 AND s::text=NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb->>'staff_id',false)
$$;
CREATE FUNCTION workforce_login_review_link_read(t uuid,e uuid,s uuid,account_subject text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_login_review_context(t,e) AND (
 workforce_login_review_staff(t,e,s)
 OR (current_setting('app.workforce_login_review_mode',true)='review'
 AND account_subject IN(NULLIF(current_setting('app.workforce_subject',true),''),NULLIF(current_setting('app.workforce_login_review_proposer',true),'')))
 ),false)
$$;
CREATE FUNCTION workforce_login_review_link_write(t uuid,e uuid,s uuid,l uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_login_review_staff(t,e,s)
 AND current_setting('app.workforce_login_review_mode',true)='review'
 AND l::text=NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb->>'link_id'
 AND current_setting('app.workforce_login_link_command',true)='review',false)
$$;
CREATE FUNCTION workforce_login_review_history(t uuid,e uuid,l uuid,writing boolean)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_login_review_context(t,e) AND EXISTS(
 SELECT 1 FROM workforce_login_links p WHERE p.tenant_id=t AND p.hospital_id=e AND p.id=l
 AND (NOT writing OR workforce_login_review_link_write(t,e,p.staff_id,l))
 ),false)
$$;

CREATE FUNCTION workforce_login_review_independent_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.workforce_login_review_mode',true)='review'
 AND workforce_login_review_staff(NEW.tenant_id,NEW.hospital_id,NEW.staff_id)
 AND NEW.status IN('active','rejected') AND (
 EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h
 ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id)
 WHERE l.tenant_id=NEW.tenant_id AND l.hospital_id=NEW.hospital_id AND l.staff_id=NEW.staff_id
 AND l.subject=NEW.actor_subject AND h.status='active')
 OR EXISTS(SELECT 1 FROM workforce_login_links a
 JOIN workforce_login_link_history ah ON(ah.tenant_id,ah.hospital_id,ah.id)=(a.tenant_id,a.hospital_id,a.id)
 JOIN workforce_login_links b ON(b.tenant_id,b.hospital_id,b.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_login_link_history bh ON(bh.tenant_id,bh.hospital_id,bh.id)=(b.tenant_id,b.hospital_id,b.id)
 WHERE a.tenant_id=NEW.tenant_id AND a.hospital_id=NEW.hospital_id
 AND a.subject=NEW.proposed_by_subject AND b.subject=NEW.actor_subject AND ah.status='active' AND bh.status='active')
 )THEN RAISE EXCEPTION 'a different independent person must review this account link'
 USING ERRCODE='23514',CONSTRAINT='workforce_login_review_independent';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_login_review_independent_guard BEFORE UPDATE ON workforce_login_links
 FOR EACH ROW EXECUTE FUNCTION workforce_login_review_independent_guard();

DO $migration$
DECLARE target record;prior record;guard_read text;guard_write text;read_extension text;write_extension text;
BEGIN
 FOR target IN SELECT * FROM (VALUES
 ('workforce_staff','tenant_isolation','workforce_login_review_staff(tenant_id,hospital_id,id)','false'),
 ('workforce_login_links','login_link_isolation','workforce_login_review_link_read(tenant_id,hospital_id,staff_id,subject)','workforce_login_review_link_write(tenant_id,hospital_id,staff_id,id)'),
 ('workforce_login_link_history','login_link_isolation','workforce_login_review_history(tenant_id,hospital_id,id,false)','workforce_login_review_history(tenant_id,hospital_id,id,true)'),
 ('workforce_independent_link_commands','tenant_isolation','workforce_login_review_staff(tenant_id,employer_id,staff_id)','workforce_login_review_link_write(tenant_id,employer_id,staff_id,link_id)')
 ) AS targets(tab,policy,reading,writing) LOOP
  SELECT pg_get_expr(polqual,polrelid) AS reading,pg_get_expr(polwithcheck,polrelid) AS writing INTO STRICT prior
   FROM pg_policy WHERE polrelid=('public.'||quote_ident(target.tab))::regclass AND polname=target.policy;
  IF prior.writing IS NULL THEN RAISE EXCEPTION 'account-link source requires explicit retained write checks';END IF;
  read_extension:=target.reading;write_extension:=target.writing;
  EXECUTE format('ALTER POLICY %I ON %I USING(hms_tenant_visible(tenant_id) AND ((%s) OR (%s))) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND ((%s) OR (%s)))',
   target.policy,target.tab,prior.reading,read_extension,prior.writing,write_extension);
  IF target.tab IN('workforce_staff','workforce_login_links') THEN
   SELECT pg_get_expr(polqual,polrelid),COALESCE(pg_get_expr(polwithcheck,polrelid),pg_get_expr(polqual,polrelid)) INTO STRICT guard_read,guard_write FROM pg_policy
    WHERE polrelid=('public.'||quote_ident(target.tab))::regclass AND polname='partner_candidate_update_guard';
   EXECUTE format('ALTER POLICY partner_candidate_update_guard ON %I USING(hms_tenant_visible(tenant_id) AND ((%s) OR (%s))) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND ((%s) OR (%s)))',
    target.tab,guard_read,read_extension,guard_write,write_extension);
  END IF;
 END LOOP;
END $migration$;

CREATE POLICY governance_link_staff_readonly ON workforce_staff AS RESTRICTIVE FOR UPDATE
 USING(hms_tenant_visible(tenant_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND COALESCE(current_setting('app.workforce_login_review_authority',true),'')='');
CREATE POLICY governance_link_insert_fence ON workforce_login_links AS RESTRICTIVE FOR INSERT
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND COALESCE(current_setting('app.workforce_login_review_authority',true),'')='');
CREATE POLICY governance_link_update_fence ON workforce_login_links AS RESTRICTIVE FOR UPDATE
 USING(hms_tenant_visible(tenant_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (COALESCE(current_setting('app.workforce_login_review_authority',true),'')=''
 OR (status IN('active','rejected') AND workforce_login_review_link_write(tenant_id,hospital_id,staff_id,id))));

CREATE FUNCTION workforce_login_review_audit(t uuid,e uuid,actor text,action_name text,resource uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_login_review_context(t,e)
 AND actor=NULLIF(current_setting('app.workforce_subject',true),'')
 AND action_name IN('workforce.login_link.read','workforce.login_link.changed','workforce.login_link.review_authority')
 AND ((action_name='workforce.login_link.read' AND resource::text=NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb->>'staff_id')
 OR EXISTS(SELECT 1 FROM workforce_login_links l WHERE l.tenant_id=t AND l.hospital_id=e AND l.id=resource
  AND workforce_login_review_staff(t,e,l.staff_id)
  AND (action_name='workforce.login_link.read' OR workforce_login_review_link_write(t,e,l.staff_id,l.id)))
 ),false)
$$;
DO $migration$
DECLARE prior text;policy_name text;
BEGIN
 SELECT polname,pg_get_expr(polwithcheck,polrelid) INTO STRICT policy_name,prior
 FROM pg_policy WHERE polrelid='public.workforce_audit'::regclass AND polpermissive;
 IF prior IS NULL THEN RAISE EXCEPTION 'account-link audit requires explicit retained write checks';END IF;
 EXECUTE format('ALTER POLICY %I ON workforce_audit WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND ((%s) OR workforce_login_review_audit(tenant_id,hospital_id,actor_subject,action,resource_id)))',policy_name,prior);
END $migration$;

CREATE TABLE workforce_login_link_authorities(
 tenant_id uuid NOT NULL,employer_id uuid NOT NULL,employer_kind text NOT NULL CHECK(employer_kind IN('hospital','pharmacy','supplier')),
 staff_id uuid NOT NULL,link_id uuid NOT NULL,link_version bigint NOT NULL CHECK(link_version BETWEEN 2 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(char_length(actor_subject) BETWEEN 1 AND 200),
 authority_kind text NOT NULL CHECK(authority_kind IN('owner','grant')),
 source_grant_id uuid,source_grant_version bigint,owner_membership_id uuid,template_key text,
 actor_membership_epoch bigint NOT NULL CHECK(actor_membership_epoch BETWEEN 1 AND 9007199254740991),
 owner_count bigint NOT NULL CHECK(owner_count BETWEEN 1 AND 1000),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740991),
 source_expires_at timestamptz NOT NULL,actor_authenticated_at timestamptz NOT NULL,actor_idle_deadline timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,employer_id,link_id,link_version),
 FOREIGN KEY(tenant_id,employer_id,staff_id)REFERENCES workforce_staff(tenant_id,hospital_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,employer_id,link_id,link_version)REFERENCES workforce_login_link_history(tenant_id,hospital_id,id,version)ON DELETE RESTRICT,
 CHECK(source_expires_at>created_at AND actor_idle_deadline>created_at),
 CHECK(COALESCE((authority_kind='owner' AND owner_membership_id IS NOT NULL AND owner_membership_id<>'00000000-0000-0000-0000-000000000000'::uuid AND source_grant_id IS NULL AND source_grant_version IS NULL AND template_key IS NULL)
 OR (authority_kind='grant' AND owner_membership_id IS NULL AND source_grant_id IS NOT NULL AND source_grant_id<>'00000000-0000-0000-0000-000000000000'::uuid AND source_grant_version BETWEEN 1 AND 9007199254740991 AND template_key='workforce.account_link_reviewer.v1'),false))
);
CREATE INDEX workforce_login_link_authorities_staff ON workforce_login_link_authorities(tenant_id,employer_id,staff_id,created_at DESC,link_id DESC);
ALTER TABLE workforce_login_link_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_login_link_authorities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_login_link_authorities
 USING(hms_tenant_visible(tenant_id) AND (workforce_staff_allowed(tenant_id,employer_id,true) OR workforce_login_review_staff(tenant_id,employer_id,staff_id)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.workforce_subject',true),'')
 AND workforce_login_review_link_write(tenant_id,employer_id,staff_id,link_id));
CREATE TRIGGER workforce_login_link_authorities_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_login_link_authorities
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_login_review_authority_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb;
BEGIN
 IF pg_trigger_depth()<2 OR NOT workforce_login_review_link_write(NEW.tenant_id,NEW.employer_id,NEW.staff_id,NEW.link_id)
 OR ROW(NEW.employer_kind,NEW.actor_subject,NEW.authority_kind,NEW.source_grant_id,NEW.source_grant_version,NEW.owner_membership_id,NEW.actor_membership_epoch,NEW.owner_count,NEW.template_key,NEW.source_expires_at,NEW.actor_authenticated_at,NEW.actor_idle_deadline)
 IS DISTINCT FROM ROW(proof->>'employer_kind',proof->>'actor_subject',proof->>'kind',(proof->>'source_grant_id')::uuid,(proof->>'source_grant_version')::bigint,(proof->>'owner_membership_id')::uuid,(proof->>'membership_epoch')::bigint,(proof->>'owner_count')::bigint,proof->>'template_key',(proof->>'expires_at')::timestamptz,(proof->>'authenticated_at')::timestamptz,(proof->>'idle_deadline')::timestamptz)
 OR NOT EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id,h.version)=(l.tenant_id,l.hospital_id,l.id,l.version)
 WHERE l.tenant_id=NEW.tenant_id AND l.hospital_id=NEW.employer_id AND l.staff_id=NEW.staff_id AND l.id=NEW.link_id
 AND l.version=NEW.link_version AND l.actor_subject=NEW.actor_subject AND l.status IN('active','rejected') AND l.updated_at=NEW.created_at)
 OR NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.employer_id AND s.id=NEW.staff_id AND s.version=NEW.staff_version)
 THEN RAISE EXCEPTION 'account-link authority must match the guarded retained review' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_login_review_authority_guard BEFORE INSERT ON workforce_login_link_authorities
 FOR EACH ROW EXECUTE FUNCTION workforce_login_review_authority_guard();

CREATE FUNCTION workforce_login_review_record()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_login_review_authority',true),'')::jsonb; basis bigint;retained workforce_login_link_authorities;
BEGIN
 IF proof IS NULL THEN RETURN NULL;END IF;
 IF NOT workforce_login_review_link_write(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) OR NEW.status NOT IN('active','rejected') THEN
 RAISE EXCEPTION 'account-link source cannot authorize another transition' USING ERRCODE='23514';END IF;
 SELECT version INTO STRICT basis FROM workforce_staff WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.staff_id;
 INSERT INTO workforce_login_link_authorities(tenant_id,employer_id,employer_kind,staff_id,link_id,link_version,actor_subject,authority_kind,source_grant_id,source_grant_version,owner_membership_id,template_key,actor_membership_epoch,owner_count,staff_version,source_expires_at,actor_authenticated_at,actor_idle_deadline,created_at)
 VALUES(NEW.tenant_id,NEW.hospital_id,proof->>'employer_kind',NEW.staff_id,NEW.id,NEW.version,NEW.actor_subject,proof->>'kind',(proof->>'source_grant_id')::uuid,(proof->>'source_grant_version')::bigint,(proof->>'owner_membership_id')::uuid,proof->>'template_key',(proof->>'membership_epoch')::bigint,(proof->>'owner_count')::bigint,basis,(proof->>'expires_at')::timestamptz,(proof->>'authenticated_at')::timestamptz,(proof->>'idle_deadline')::timestamptz,NEW.updated_at)
 RETURNING * INTO retained;
 INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.actor_subject,'workforce.login_link.review_authority',NEW.id,to_jsonb(retained));
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_login_review_record AFTER UPDATE ON workforce_login_links
 FOR EACH ROW EXECUTE FUNCTION workforce_login_review_record();
