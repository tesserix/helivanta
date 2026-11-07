SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE pharmacy_licence_revocations(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,licence_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500 AND reason!~'[[:cntrl:]]'),
 revoked_at timestamptz NOT NULL,PRIMARY KEY(tenant_id,id),CONSTRAINT pharmacy_licence_revocation_one_licence UNIQUE(tenant_id,outlet_id,licence_id),FOREIGN KEY(tenant_id,outlet_id,licence_id)REFERENCES pharmacy_outlet_licences(tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_licence_revocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_licence_revocations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_licence_revocations USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=current_setting('app.pharmacy_subject',true));
CREATE TRIGGER pharmacy_licence_revocation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_licence_revocations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_licence_revocation_audit AFTER INSERT ON pharmacy_licence_revocations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION pharmacy_licence_revocation_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':pharmacy-licence:'||NEW.outlet_id::text||':'||NEW.licence_id::text,0));
 PERFORM id FROM pharmacy_outlet_licences l WHERE(l.tenant_id,l.outlet_id,l.id)=(NEW.tenant_id,NEW.outlet_id,NEW.licence_id)FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'unknown licence' USING ERRCODE='23503';END IF;
 RETURN NEW;END $$;
CREATE TRIGGER pharmacy_licence_revocation_parent BEFORE INSERT ON pharmacy_licence_revocations FOR EACH ROW EXECUTE FUNCTION pharmacy_licence_revocation_guard();
CREATE OR REPLACE FUNCTION pharmacy_trading_licence_source(t uuid,o uuid,l uuid)RETURNS TABLE(review_id uuid)LANGUAGE plpgsql VOLATILE AS $$BEGIN
 PERFORM pg_advisory_xact_lock_shared(hashtextextended(t::text||':pharmacy-licence:'||o::text||':'||l::text,0));
 RETURN QUERY SELECT r.id FROM pharmacy_outlet_licences x JOIN pharmacy_outlet_licence_reviews r ON(r.tenant_id,r.outlet_id,r.licence_id)=(x.tenant_id,x.outlet_id,x.id)JOIN pharmacy_licence_evidence e ON(e.tenant_id,e.outlet_id,e.licence_id)=(x.tenant_id,x.outlet_id,x.id)JOIN pharmacy_outlets a ON(a.tenant_id,a.id)=(x.tenant_id,x.outlet_id)
 WHERE x.tenant_id=t AND x.outlet_id=o AND x.id=l AND a.country_code='IN'AND r.decision='approved'AND e.status='available'AND(timezone(a.timezone,CURRENT_TIMESTAMP))::date BETWEEN x.valid_from AND x.valid_until
 AND NOT EXISTS(SELECT 1 FROM pharmacy_licence_revocations v WHERE(v.tenant_id,v.outlet_id,v.licence_id)=(x.tenant_id,x.outlet_id,x.id))FOR SHARE OF x,r,e,a;
END $$;
CREATE OR REPLACE FUNCTION pharmacy_outlet_licence_in_force(tenant uuid,outlet uuid,day date)RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$DECLARE ids uuid[];item uuid;BEGIN
 SELECT array_agg(l.id ORDER BY l.id)INTO ids FROM(SELECT id FROM pharmacy_outlet_licences WHERE tenant_id=tenant AND outlet_id=outlet AND day BETWEEN valid_from AND valid_until ORDER BY id LIMIT 129)l;
 IF cardinality(ids)>128 THEN RAISE EXCEPTION 'licence verification capacity exceeded' USING ERRCODE='54000';END IF;
 FOREACH item IN ARRAY COALESCE(ids,ARRAY[]::uuid[])LOOP
  PERFORM pg_advisory_xact_lock_shared(hashtextextended(tenant::text||':pharmacy-licence:'||outlet::text||':'||item::text,0));
 END LOOP;
 RETURN EXISTS(SELECT 1 FROM pharmacy_outlet_licences l JOIN pharmacy_outlet_licence_reviews r ON(r.tenant_id,r.outlet_id,r.licence_id)=(l.tenant_id,l.outlet_id,l.id)JOIN pharmacy_outlets o ON(o.tenant_id,o.id)=(l.tenant_id,l.outlet_id)WHERE l.tenant_id=tenant AND l.outlet_id=outlet AND l.id=ANY(ids)AND r.decision='approved'AND day BETWEEN l.valid_from AND l.valid_until AND NOT EXISTS(SELECT 1 FROM pharmacy_licence_revocations v WHERE(v.tenant_id,v.outlet_id,v.licence_id)=(l.tenant_id,l.outlet_id,l.id)AND(timezone(o.timezone,v.revoked_at))::date<=day));
END $$;
CREATE FUNCTION pharmacy_trade_source_scope(t uuid,o uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND o=NULLIF(current_setting('app.pharmacy_trade_outlet',true),'')::uuid AND current_setting('app.pharmacy_trade_source',true)='current',false)
$$;
DO $$DECLARE tab text;prior text;key_column text;BEGIN
 FOREACH tab IN ARRAY ARRAY['pharmacy_outlet_licences','pharmacy_outlet_licence_reviews','pharmacy_licence_evidence','pharmacy_licence_revocations']LOOP
  key_column:=CASE WHEN tab='pharmacy_outlet_licences' THEN 'id' ELSE 'licence_id' END;
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=tab::regclass AND polname='tenant_isolation';
  EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)OR(pharmacy_trade_source_scope(tenant_id,outlet_id)AND %I=NULLIF(current_setting(''app.pharmacy_trade_licence'',true),'''')::uuid))',tab,prior,key_column);
 END LOOP;
END $$;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_trading_scopes'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON pharmacy_trading_scopes USING((%s)OR(pharmacy_trade_source_scope(tenant_id,outlet_id)AND id=NULLIF(current_setting(''app.pharmacy_trade_registration'',true),'''')::uuid))',prior);
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_staff_department_bindings'::regclass AND polname='pharmacy_staff_binding_scope';
 EXECUTE format('ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING((%s)OR(pharmacy_trade_source_scope(tenant_id,outlet_id)AND id=NULLIF(current_setting(''app.pharmacy_trade_binding'',true),'''')::uuid AND hospital_id=NULLIF(current_setting(''app.pharmacy_trade_hospital'',true),'''')::uuid))',prior);
END $$;
