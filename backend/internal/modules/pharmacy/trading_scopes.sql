SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION pharmacy_trading_licence_source(t uuid,o uuid,l uuid)RETURNS TABLE(review_id uuid)LANGUAGE sql VOLATILE AS $$
 SELECT r.id FROM pharmacy_outlet_licences x JOIN pharmacy_outlet_licence_reviews r ON(r.tenant_id,r.outlet_id,r.licence_id)=(x.tenant_id,x.outlet_id,x.id)JOIN pharmacy_licence_evidence e ON(e.tenant_id,e.outlet_id,e.licence_id)=(x.tenant_id,x.outlet_id,x.id)JOIN pharmacy_outlets a ON(a.tenant_id,a.id)=(x.tenant_id,x.outlet_id)
 WHERE x.tenant_id=t AND x.outlet_id=o AND x.id=l AND a.country_code='IN'AND r.decision='approved'AND e.status='available'AND(timezone(a.timezone,CURRENT_TIMESTAMP))::date BETWEEN x.valid_from AND x.valid_until FOR SHARE OF x,r,e,a
$$;
CREATE TABLE pharmacy_trading_scopes(
 tenant_id uuid NOT NULL,pharmacy_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 input jsonb NOT NULL CHECK(pharmacy_trading_scope_input_valid(input)IS TRUE),
 status text NOT NULL CHECK(status IN('pending_review','approved','rejected','revoked')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 prepared_by text NOT NULL CHECK(length(btrim(prepared_by))BETWEEN 1 AND 200),
 proposal_receipt jsonb NOT NULL CHECK(jsonb_typeof(proposal_receipt)='object'),
 decided_by text,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL CHECK(updated_at>=created_at),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id),
 CHECK((status='pending_review'AND version=1 AND decided_by IS NULL)OR(status IN('approved','rejected')AND version=2 AND decided_by IS NOT NULL AND decided_by<>prepared_by)OR(status='revoked'AND version=3 AND decided_by IS NOT NULL))
);
CREATE UNIQUE INDEX pharmacy_trading_scope_active_category ON pharmacy_trading_scopes(tenant_id,outlet_id,(input->>'category'))WHERE status='approved';
CREATE INDEX pharmacy_trading_scope_page ON pharmacy_trading_scopes(tenant_id,outlet_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_trading_scopes ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_trading_scopes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_trading_scopes USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TABLE pharmacy_trading_scope_commands(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,scope_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,outlet_id,scope_id)REFERENCES pharmacy_trading_scopes(tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_trading_scope_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_trading_scope_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_trading_scope_commands USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=current_setting('app.pharmacy_subject',true));
CREATE TRIGGER pharmacy_trading_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_trading_scope_commands FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_trading_scope_audit AFTER INSERT OR UPDATE ON pharmacy_trading_scopes FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_trading_scope_command_audit AFTER INSERT ON pharmacy_trading_scope_commands FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION pharmacy_trading_scope_state_guard()RETURNS trigger LANGUAGE plpgsql AS $$DECLARE actor text:=current_setting('app.pharmacy_subject',true);BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'trade scope evidence is retained' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending_review'OR NEW.version<>1 OR NEW.prepared_by IS DISTINCT FROM actor OR NOT EXISTS(SELECT 1 FROM pharmacy_outlets o WHERE o.tenant_id=NEW.tenant_id AND o.id=NEW.outlet_id AND o.pharmacy_id=NEW.pharmacy_id)THEN RAISE EXCEPTION 'prepare an exact outlet scope' USING ERRCODE='23514';END IF;
 ELSE
  IF(NEW.tenant_id,NEW.pharmacy_id,NEW.outlet_id,NEW.id,NEW.input,NEW.prepared_by,NEW.proposal_receipt,NEW.created_at)IS DISTINCT FROM(OLD.tenant_id,OLD.pharmacy_id,OLD.outlet_id,OLD.id,OLD.input,OLD.prepared_by,OLD.proposal_receipt,OLD.created_at)OR NEW.version<>OLD.version+1 OR NEW.decided_by IS DISTINCT FROM actor OR NOT((OLD.status='pending_review'AND NEW.status IN('approved','rejected')AND actor<>OLD.prepared_by)OR(OLD.status='approved'AND NEW.status='revoked'))THEN RAISE EXCEPTION 'invalid trade scope transition' USING ERRCODE='23514';END IF;
 END IF;
 IF NEW.status='approved' THEN
  PERFORM review_id FROM pharmacy_trading_licence_source(NEW.tenant_id,NEW.outlet_id,(NEW.input->>'licence_id')::uuid);IF NOT FOUND THEN RAISE EXCEPTION 'current reviewed licence required' USING ERRCODE='23514';END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':pharmacy-trade-scope:'||NEW.outlet_id::text,0));
  IF EXISTS(SELECT 1 FROM pharmacy_trading_scopes s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id<>NEW.id AND s.status='approved'AND s.input->>'jurisdiction'IS DISTINCT FROM NEW.input->>'jurisdiction')THEN RAISE EXCEPTION 'current outlet jurisdiction conflicts' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;END $$;
CREATE TRIGGER pharmacy_trading_scope_state BEFORE INSERT OR UPDATE OR DELETE ON pharmacy_trading_scopes FOR EACH ROW EXECUTE FUNCTION pharmacy_trading_scope_state_guard();
