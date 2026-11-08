SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE workforce_login_links ADD COLUMN independent_staff_version bigint CHECK(independent_staff_version BETWEEN 1 AND 9007199254740991);
CREATE FUNCTION workforce_login_establishment_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE kind text;current_version bigint;
BEGIN
 SELECT establishment_kind INTO kind FROM workforce_hospitals WHERE tenant_id=NEW.tenant_id AND id=NEW.hospital_id FOR KEY SHARE;
 IF kind IS NULL OR kind NOT IN('hospital','pharmacy','supplier')THEN RAISE EXCEPTION 'account establishment is unavailable' USING ERRCODE='42501';END IF;
 IF kind='hospital' THEN
  IF NEW.independent_staff_version IS NOT NULL THEN RAISE EXCEPTION 'hospital identity uses employment generation' USING ERRCODE='23514';END IF;
 ELSE
  IF NEW.independent_staff_version IS NULL OR NEW.employment_generation_id IS NOT NULL THEN RAISE EXCEPTION 'independent identity requires its own basis' USING ERRCODE='23514';END IF;
  IF TG_OP='INSERT' OR (TG_OP='UPDATE' AND NEW.status='active')THEN
   SELECT version INTO current_version FROM workforce_staff WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.staff_id FOR SHARE;
   IF current_version IS DISTINCT FROM NEW.independent_staff_version THEN RAISE EXCEPTION 'Staff identity changed' USING ERRCODE='23514',CONSTRAINT='workforce_independent_identity_changed';END IF;
  END IF;
  IF TG_OP='UPDATE' AND NEW.status IN('active','rejected') AND (
   EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id) WHERE l.tenant_id=NEW.tenant_id AND l.hospital_id=NEW.hospital_id AND l.staff_id=NEW.staff_id AND l.subject=NEW.actor_subject AND h.status='active')
   OR EXISTS(SELECT 1 FROM workforce_login_links a JOIN workforce_login_link_history ah ON(ah.tenant_id,ah.hospital_id,ah.id)=(a.tenant_id,a.hospital_id,a.id) JOIN workforce_login_links b ON(b.tenant_id,b.hospital_id,b.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id) JOIN workforce_login_link_history bh ON(bh.tenant_id,bh.hospital_id,bh.id)=(b.tenant_id,b.hospital_id,b.id) WHERE a.tenant_id=NEW.tenant_id AND a.hospital_id=NEW.hospital_id AND a.subject=NEW.proposed_by_subject AND b.subject=NEW.actor_subject AND ah.status='active' AND bh.status='active')
  )THEN RAISE EXCEPTION 'independent person required' USING ERRCODE='23514',CONSTRAINT='workforce_independent_identity_changed';END IF;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER workforce_hospital_establishment ON workforce_login_links;
CREATE TRIGGER workforce_login_establishment_guard BEFORE INSERT OR UPDATE ON workforce_login_links FOR EACH ROW EXECUTE FUNCTION workforce_login_establishment_guard();

CREATE TABLE workforce_independent_link_commands(
 tenant_id uuid NOT NULL,employer_id uuid NOT NULL,employer_kind text NOT NULL CHECK(employer_kind IN('pharmacy','supplier')),staff_id uuid NOT NULL,link_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN('8','9','a','b')),
 actor_subject text NOT NULL CHECK(char_length(actor_subject) BETWEEN 1 AND 200),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 9007199254740990),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,employer_id,link_id,expected_version),
 FOREIGN KEY(tenant_id,employer_id,staff_id)REFERENCES workforce_staff(tenant_id,hospital_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,employer_id,link_id)REFERENCES workforce_login_links(tenant_id,hospital_id,id)ON DELETE RESTRICT,
 CHECK(receipt->>'id' IS NOT DISTINCT FROM id::text AND receipt->>'link_id' IS NOT DISTINCT FROM link_id::text
 AND receipt#>>'{scope,owner_tenant_id}' IS NOT DISTINCT FROM tenant_id::text AND receipt#>>'{scope,employer_id}' IS NOT DISTINCT FROM employer_id::text
 AND receipt#>>'{scope,employer_kind}' IS NOT DISTINCT FROM employer_kind AND receipt#>>'{scope,staff_id}' IS NOT DISTINCT FROM staff_id::text
 AND receipt->>'actor_subject' IS NOT DISTINCT FROM actor_subject AND receipt#>>'{input,expected_version}' IS NOT DISTINCT FROM expected_version::text)
);
CREATE INDEX workforce_independent_link_commands_staff ON workforce_independent_link_commands(tenant_id,employer_id,staff_id);
ALTER TABLE workforce_independent_link_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_independent_link_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_independent_link_commands
 USING(hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,employer_id,true) AND current_setting('app.workforce_login_link_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=NULLIF(current_setting('app.workforce_subject',true),'') AND workforce_staff_allowed(tenant_id,employer_id,true) AND current_setting('app.workforce_login_link_access',true)='authorized');
CREATE TRIGGER workforce_independent_link_commands_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_independent_link_commands FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE FUNCTION workforce_independent_link_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l workforce_login_links;
BEGIN
 SELECT * INTO l FROM workforce_login_links WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.employer_id AND staff_id=NEW.staff_id AND id=NEW.link_id FOR SHARE;
 IF NOT FOUND OR l.version<>NEW.expected_version+1 OR NEW.receipt#>>'{result,link,version}' IS DISTINCT FROM l.version::text OR NEW.receipt#>>'{result,link,status}' IS DISTINCT FROM l.status OR NEW.receipt#>>'{result,link,id}' IS DISTINCT FROM l.id::text OR NEW.receipt->>'actor_subject' IS DISTINCT FROM l.actor_subject
 OR NOT EXISTS(SELECT 1 FROM workforce_hospitals WHERE tenant_id=NEW.tenant_id AND id=NEW.employer_id AND establishment_kind=NEW.employer_kind)
 THEN RAISE EXCEPTION 'identity command evidence differs' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_independent_link_command_guard BEFORE INSERT ON workforce_independent_link_commands FOR EACH ROW EXECUTE FUNCTION workforce_independent_link_command_guard();

CREATE FUNCTION workforce_independent_identity_source(t uuid,e uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND e=NULLIF(current_setting('app.workforce_independent_employer',true),'')::uuid
 AND current_setting('app.workforce_independent_identity',true)='source'
 AND EXISTS(SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=t AND h.id=e
 AND h.establishment_kind IN('pharmacy','supplier') AND h.establishment_kind=current_setting('app.workforce_independent_kind',true))
$$;
DO $$
DECLARE original text;
BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO STRICT original FROM pg_policy WHERE polrelid='workforce_staff'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON workforce_staff USING((%s) OR (workforce_independent_identity_source(tenant_id,hospital_id) AND id=NULLIF(current_setting(''app.workforce_independent_staff'',true),'''')::uuid))',original);
 SELECT pg_get_expr(polqual,polrelid) INTO STRICT original FROM pg_policy WHERE polrelid='workforce_login_links'::regclass AND polname='login_link_isolation';
 EXECUTE format('ALTER POLICY login_link_isolation ON workforce_login_links USING((%s) OR (workforce_independent_identity_source(tenant_id,hospital_id) AND subject=NULLIF(current_setting(''app.workforce_independent_subject'',true),'''')))',original);
END $$;

-- Row locks need UPDATE visibility, while retained checks continue to forbid source-purpose writes.
DO $$
DECLARE original text;tab text;source_expression text;
BEGIN
 FOREACH tab IN ARRAY ARRAY['workforce_staff','workforce_login_links']LOOP
  SELECT pg_get_expr(polqual,polrelid) INTO STRICT original FROM pg_policy WHERE polrelid=('public.'||quote_ident(tab))::regclass AND polname='partner_candidate_update_guard';
  source_expression:=CASE tab WHEN 'workforce_staff' THEN 'workforce_independent_identity_source(tenant_id,hospital_id) AND id=NULLIF(current_setting(''app.workforce_independent_staff'',true),'''')::uuid'
  ELSE 'workforce_independent_identity_source(tenant_id,hospital_id) AND subject=NULLIF(current_setting(''app.workforce_independent_subject'',true),'''')' END;
  EXECUTE format('ALTER POLICY partner_candidate_update_guard ON %I USING((%s) OR (%s)) WITH CHECK(%s)',tab,original,source_expression,original);
 END LOOP;
END $$;

DO $$
DECLARE original text;tab text;source_expression text;
BEGIN
 FOREACH tab IN ARRAY ARRAY['workforce_login_links','workforce_login_link_history']LOOP
  SELECT pg_get_expr(polqual,polrelid) INTO STRICT original FROM pg_policy WHERE polrelid=('public.'||quote_ident(tab))::regclass AND polname='login_link_isolation';
  IF tab='workforce_login_links' THEN
   source_expression:=$predicate$subject IN(NULLIF(current_setting('app.workforce_independent_subject',true),''),NULLIF(current_setting('app.workforce_independent_actor',true),''),NULLIF(current_setting('app.workforce_independent_proposer',true),''))$predicate$;
  ELSE
   source_expression:=$predicate$EXISTS(SELECT 1 FROM workforce_login_links l WHERE(l.tenant_id,l.hospital_id,l.id)=(workforce_login_link_history.tenant_id,workforce_login_link_history.hospital_id,workforce_login_link_history.id)AND l.subject IN(NULLIF(current_setting('app.workforce_independent_subject',true),''),NULLIF(current_setting('app.workforce_independent_actor',true),''),NULLIF(current_setting('app.workforce_independent_proposer',true),'')))$predicate$;
  END IF;
  EXECUTE format('ALTER POLICY login_link_isolation ON %I USING((%s) OR (workforce_independent_identity_source(tenant_id,hospital_id) AND (%s)))',tab,original,source_expression);
 END LOOP;
END $$;
