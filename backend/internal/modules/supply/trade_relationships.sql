SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_trade_relationships(
 tenant_id uuid NOT NULL,origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 body jsonb NOT NULL CHECK(jsonb_typeof(body)='object'),proposal_input jsonb,proposal_receipt jsonb,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL CHECK(updated_at>=created_at),
 PRIMARY KEY(tenant_id,id),
 CHECK(origin_tenant_id::text=body#>>'{terms,seller,tenant_id}'AND origin_record_id=id),
 CHECK(body->>'owner_tenant_id'=tenant_id::text AND body->>'id'=id::text AND(body->>'version')::bigint=version),
 CHECK(body->>'side'IN('seller','buyer')AND body->>'status'IN('pending_review','offered','incoming','accepted_pending_ack','active','rejected','suspended')),
 CHECK((body->>'side'='seller'AND body#>>'{terms,seller,tenant_id}'=tenant_id::text)OR(body->>'side'='buyer'AND body#>>'{terms,buyer,tenant_id}'=tenant_id::text)),
 CHECK(body#>>'{terms,seller,tenant_id}'<>body#>>'{terms,buyer,tenant_id}'),
 CHECK((body->>'authority_epoch')::bigint BETWEEN 1 AND 9007199254740991 AND(body->>'remote_epoch')::bigint BETWEEN 0 AND 9007199254740991),
 CHECK((body->>'side'='seller'AND proposal_input IS NOT NULL AND proposal_receipt IS NOT NULL)OR(body->>'side'='buyer'AND proposal_input IS NULL AND proposal_receipt IS NULL))
);
CREATE INDEX supply_trade_relationships_seller_page ON supply_trade_relationships(tenant_id,(body#>>'{terms,seller,product}'),(body#>>'{terms,seller,organization_id}'),(body#>>'{terms,seller,location_id}'),created_at DESC,id DESC)WHERE body->>'side'='seller';
CREATE INDEX supply_trade_relationships_buyer_page ON supply_trade_relationships(tenant_id,(body#>>'{terms,buyer,product}'),(body#>>'{terms,buyer,organization_id}'),(body#>>'{terms,buyer,location_id}'),created_at DESC,id DESC)WHERE body->>'side'='buyer';
CREATE INDEX supply_trade_relationships_page ON supply_trade_relationships(tenant_id,created_at DESC,id DESC);
ALTER TABLE supply_trade_relationships ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_trade_relationships FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_trade_relationships USING(hms_tenant_visible(tenant_id))WITH CHECK(hms_tenant_visible(tenant_id));
CREATE TABLE supply_trade_relationship_commands(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),relationship_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),input jsonb NOT NULL,receipt jsonb NOT NULL,
 created_at timestamptz NOT NULL,PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,relationship_id)REFERENCES supply_trade_relationships(tenant_id,id)
);
ALTER TABLE supply_trade_relationship_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_trade_relationship_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_trade_relationship_commands USING(hms_tenant_visible(tenant_id))WITH CHECK(hms_tenant_visible(tenant_id));
CREATE TABLE supply_trade_relationship_history(
 tenant_id uuid NOT NULL,origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,relationship_id uuid NOT NULL,version bigint NOT NULL,body jsonb NOT NULL,
 actor_subject text NOT NULL,source_event_id uuid,created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,relationship_id,version),FOREIGN KEY(tenant_id,relationship_id)REFERENCES supply_trade_relationships(tenant_id,id)
);
ALTER TABLE supply_trade_relationship_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_trade_relationship_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_trade_relationship_history USING(hms_tenant_visible(tenant_id))WITH CHECK(hms_tenant_visible(tenant_id));
CREATE TRIGGER supply_trade_commands_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_trade_relationship_commands FOR EACH STATEMENT EXECUTE FUNCTION supply_stock_immutable();
CREATE TRIGGER supply_trade_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_trade_relationship_history FOR EACH STATEMENT EXECUTE FUNCTION supply_stock_immutable();
CREATE TRIGGER supply_trade_relationship_audit AFTER INSERT OR UPDATE ON supply_trade_relationships FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_trade_command_audit AFTER INSERT ON supply_trade_relationship_commands FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_trade_relationship_preserve()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE'THEN RAISE EXCEPTION 'relationship evidence is retained' USING ERRCODE='23514';END IF;
 IF(NEW.tenant_id,NEW.origin_tenant_id,NEW.origin_record_id,NEW.id,NEW.proposal_input,NEW.proposal_receipt,NEW.created_at)IS DISTINCT FROM(OLD.tenant_id,OLD.origin_tenant_id,OLD.origin_record_id,OLD.id,OLD.proposal_input,OLD.proposal_receipt,OLD.created_at)OR NEW.version<>OLD.version+1 OR NEW.body->'terms'IS DISTINCT FROM OLD.body->'terms'OR NEW.body->>'terms_sha256'IS DISTINCT FROM OLD.body->>'terms_sha256'OR NEW.body->>'side'IS DISTINCT FROM OLD.body->>'side'OR NEW.body->>'prepared_by'IS DISTINCT FROM OLD.body->>'prepared_by'OR(NEW.body->>'authority_epoch')::bigint<(OLD.body->>'authority_epoch')::bigint OR(NEW.body->>'remote_epoch')::bigint<(OLD.body->>'remote_epoch')::bigint OR(OLD.body->>'status'IN('rejected','suspended')AND NEW.body->>'status'IS DISTINCT FROM OLD.body->>'status')THEN RAISE EXCEPTION 'immutable relationship or invalid version' USING ERRCODE='23514';END IF;
 RETURN NEW;END $$;
CREATE TRIGGER supply_trade_relationship_preserve BEFORE UPDATE OR DELETE ON supply_trade_relationships FOR EACH ROW EXECUTE FUNCTION supply_trade_relationship_preserve();
