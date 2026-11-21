SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514'; END IF;
END $$;

CREATE TABLE pharmacy_supplier_credit_matches(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,credit_id uuid NOT NULL,return_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 999999999999),
 expected_return_matched_minor bigint NOT NULL CHECK(expected_return_matched_minor BETWEEN 0 AND 1000000000000),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),overage_reason text CHECK(length(btrim(overage_reason)) BETWEEN 1 AND 500),
 return_number bigint NOT NULL CHECK(return_number BETWEEN 1 AND 1000000000000),expected_credit_minor bigint NOT NULL CHECK(expected_credit_minor BETWEEN 0 AND 1000000000000),
 return_matched_minor bigint GENERATED ALWAYS AS(expected_return_matched_minor+amount_minor)STORED CHECK(return_matched_minor BETWEEN 0 AND 1000000000000),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,credit_id,id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_supplier_credit_match_page ON pharmacy_supplier_credit_matches(tenant_id,outlet_id,credit_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_supplier_credit_match_return ON pharmacy_supplier_credit_matches(tenant_id,outlet_id,return_id,id)INCLUDE(amount_minor);

CREATE TABLE pharmacy_supplier_credit_match_reversals(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,credit_id uuid NOT NULL,match_id uuid NOT NULL,return_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 999999999999),
 expected_return_matched_minor bigint NOT NULL CHECK(expected_return_matched_minor BETWEEN 0 AND 1000000000000),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),return_number bigint NOT NULL CHECK(return_number BETWEEN 1 AND 1000000000000),
 return_matched_minor bigint GENERATED ALWAYS AS(expected_return_matched_minor-amount_minor)STORED CHECK(return_matched_minor BETWEEN 0 AND 1000000000000),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,match_id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id,match_id)REFERENCES pharmacy_supplier_credit_matches(tenant_id,outlet_id,credit_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_supplier_credit_match_reversal_credit ON pharmacy_supplier_credit_match_reversals(tenant_id,outlet_id,credit_id,expected_version);
CREATE INDEX pharmacy_supplier_credit_match_reversal_return ON pharmacy_supplier_credit_match_reversals(tenant_id,outlet_id,return_id);

DO $policies$DECLARE target text;BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_credit_matches','pharmacy_supplier_credit_match_reversals'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',target);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',target);
  EXECUTE format($policy$CREATE POLICY tenant_isolation ON %I
   USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''))$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable()',target||'_immutable',target);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()',target||'_audit',target);
 END LOOP;
END $policies$;

CREATE FUNCTION pharmacy_supplier_credit_return_lock(t uuid,o uuid,r uuid)RETURNS void LANGUAGE sql VOLATILE AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(t::text||':'||o::text||':supplier-credit-return:'||r::text,0))
$$;
CREATE FUNCTION pharmacy_supplier_return_matched(t uuid,o uuid,r uuid)RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(m.amount_minor),0)FROM pharmacy_supplier_credit_matches m
 WHERE m.tenant_id=t AND m.outlet_id=o AND m.return_id=r
 AND NOT EXISTS(SELECT 1 FROM pharmacy_supplier_credit_match_reversals v WHERE v.tenant_id=m.tenant_id AND v.outlet_id=m.outlet_id AND v.match_id=m.id)
$$;

CREATE FUNCTION pharmacy_supplier_credit_match_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE position pharmacy_supplier_credit_positions;document pharmacy_supplier_credit_documents;source pharmacy_supplier_returns;matched numeric;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.credit_id);
 PERFORM pharmacy_supplier_credit_return_lock(NEW.tenant_id,NEW.outlet_id,NEW.return_id);
 SELECT * INTO position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id FOR UPDATE;
 SELECT * INTO document FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 SELECT r.* INTO source FROM pharmacy_supplier_returns r JOIN pharmacy_supplier_return_dispatches d ON d.tenant_id=r.tenant_id AND d.outlet_id=r.outlet_id AND d.return_id=r.id
 WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.id=NEW.return_id
 AND r.line_count=(SELECT count(*)FROM pharmacy_supplier_return_dispatch_lines l JOIN pharmacy_stock_movements m ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.movement_id
 WHERE l.tenant_id=d.tenant_id AND l.outlet_id=d.outlet_id AND l.dispatch_id=d.id AND pharmacy_supplier_return_movement_matches(m));
 IF position.id IS NULL OR position.state<>'active' OR position.version<>NEW.expected_version OR
  source.id IS NULL OR source.payee_reference<>document.payee_reference OR source.currency<>document.currency OR
  NEW.return_number<>source.number OR NEW.expected_credit_minor<>source.expected_credit_minor THEN
  RAISE EXCEPTION 'match requires current credit and exact dispatched supplier return' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_match_source';
 END IF;
 matched:=pharmacy_supplier_return_matched(NEW.tenant_id,NEW.outlet_id,NEW.return_id);
 IF matched<>NEW.expected_return_matched_minor OR matched+NEW.amount_minor>1000000000000 OR
  position.matched_minor+NEW.amount_minor>document.amount_minor OR
  (matched+NEW.amount_minor>source.expected_credit_minor AND NEW.overage_reason IS NULL) THEN
  RAISE EXCEPTION 'match requires observed totals, document capacity and explicit excess reason' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_match_amount';
 END IF;
 UPDATE pharmacy_supplier_credit_positions SET matched_minor=matched_minor+NEW.amount_minor,version=version+1,actor_subject=NEW.actor_subject
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_match_guard BEFORE INSERT ON pharmacy_supplier_credit_matches FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_match_guard();

CREATE FUNCTION pharmacy_supplier_credit_match_reversal_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE position pharmacy_supplier_credit_positions;original pharmacy_supplier_credit_matches;matched numeric;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.credit_id);
 SELECT * INTO original FROM pharmacy_supplier_credit_matches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=NEW.credit_id AND id=NEW.match_id;
 IF original.id IS NULL OR NEW.return_id<>original.return_id OR NEW.return_number<>original.return_number OR NEW.amount_minor<>original.amount_minor THEN
  RAISE EXCEPTION 'reversal requires the exact original match' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_match_reversal_source';
 END IF;
 PERFORM pharmacy_supplier_credit_return_lock(NEW.tenant_id,NEW.outlet_id,original.return_id);
 SELECT * INTO position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id FOR UPDATE;
 matched:=pharmacy_supplier_return_matched(NEW.tenant_id,NEW.outlet_id,original.return_id);
 IF position.id IS NULL OR position.state<>'active' OR position.version<>NEW.expected_version OR matched<>NEW.expected_return_matched_minor OR
  matched<original.amount_minor OR position.matched_minor<original.amount_minor OR
  EXISTS(SELECT 1 FROM pharmacy_supplier_credit_match_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND match_id=NEW.match_id) THEN
  RAISE EXCEPTION 'reversal requires current unreversed attribution' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_match_reversal_state';
 END IF;
 UPDATE pharmacy_supplier_credit_positions SET matched_minor=matched_minor-original.amount_minor,version=version+1,actor_subject=NEW.actor_subject
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_match_reversal_guard BEFORE INSERT ON pharmacy_supplier_credit_match_reversals FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_match_reversal_guard();

CREATE OR REPLACE FUNCTION pharmacy_supplier_credit_projection_complete()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE credit uuid;position pharmacy_supplier_credit_positions;voids bigint;corrections bigint;matches bigint;reversals bigint;matched numeric;expected_actor text;expected_state text;
BEGIN
 IF TG_TABLE_NAME IN('pharmacy_supplier_credit_voids','pharmacy_supplier_credit_matches','pharmacy_supplier_credit_match_reversals')THEN credit:=NEW.credit_id;ELSE credit:=NEW.id;END IF;
 SELECT * INTO STRICT position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit;
 SELECT count(*)INTO voids FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*)INTO corrections FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 SELECT count(*),COALESCE(sum(amount_minor),0)INTO matches,matched FROM pharmacy_supplier_credit_matches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*),matched-COALESCE(sum(amount_minor),0)INTO reversals,matched FROM pharmacy_supplier_credit_match_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 IF corrections>0 THEN
  expected_state:='corrected';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 ELSIF voids>0 THEN
  expected_state:='voided';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 ELSE
  expected_state:='active';
  SELECT actor_subject INTO expected_actor FROM(
   SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_matches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_match_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,0 FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit
  )latest ORDER BY expected_version DESC LIMIT 1;
 END IF;
 IF position.version<>1+voids+corrections+matches+reversals OR position.state<>expected_state OR position.actor_subject<>expected_actor OR position.matched_minor<>matched OR position.allocated_minor<>0 THEN
  RAISE EXCEPTION 'credit position requires complete immutable evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_projection_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_match_complete AFTER INSERT ON pharmacy_supplier_credit_matches DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_match_reversal_complete AFTER INSERT ON pharmacy_supplier_credit_match_reversals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();

-- Extend the checked purpose inventory only after validating its existing fingerprint.
DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='pharmacy_operating_choice_context';
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION pharmacy_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
