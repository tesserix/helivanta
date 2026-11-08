SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE OR REPLACE FUNCTION supply_stock_scope(t uuid,p uuid,l uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p=NULLIF(current_setting('app.supply_stock_party',true),'')::uuid AND l=NULLIF(current_setting('app.supply_stock_premise',true),'')::uuid
 AND current_setting('app.supply_stock_access',true)='authorized' AND NULLIF(current_setting('app.supply_subject',true),'')IS NOT NULL
 AND current_setting('app.supply_subject',true)=current_setting('app.actor_subject',true)
 AND ((operation='read'AND current_setting('app.supply_stock_permission',true)IN('supply.operations.read','supply.operations.write','supply.stock.review'))
 OR(operation='write'AND current_setting('app.supply_stock_permission',true)='supply.operations.write')
 OR(operation='review'AND current_setting('app.supply_stock_permission',true)='supply.stock.review')),false)
$$;
CREATE TABLE supply_stock_quality_decisions (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,receipt_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1)IN('8','9','a','b')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 identity_check_id uuid NOT NULL,identity_version bigint NOT NULL CHECK(identity_version BETWEEN 1 AND 9007199254740991),
 decision text NOT NULL CHECK(decision IN('release','hold')),
 released_base_units bigint NOT NULL CHECK(released_base_units BETWEEN 0 AND 9007199254740991),
 reason text NOT NULL CHECK(char_length(reason)BETWEEN 1 AND 500 AND reason=btrim(reason)AND reason!~'[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(char_length(actor_subject)BETWEEN 1 AND 200),
 input_hash text NOT NULL CHECK(input_hash~'^[a-f0-9]{64}$'),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'
 AND document->>'id' IS NOT DISTINCT FROM id::text
 AND document->>'owner_tenant_id' IS NOT DISTINCT FROM tenant_id::text
 AND document->>'party_id' IS NOT DISTINCT FROM party_id::text
 AND document->>'premise_id' IS NOT DISTINCT FROM premise_id::text
 AND document->>'receipt_id' IS NOT DISTINCT FROM receipt_id::text
 AND document->>'version' IS NOT DISTINCT FROM version::text
 AND document->>'actor_subject' IS NOT DISTINCT FROM actor_subject
 AND document->>'released_base_units' IS NOT DISTINCT FROM released_base_units::text
 AND document#>>'{input,identity_check_id}' IS NOT DISTINCT FROM identity_check_id::text
 AND document#>>'{input,identity_version}' IS NOT DISTINCT FROM identity_version::text
 AND document#>>'{input,decision}' IS NOT DISTINCT FROM decision
 AND document#>>'{input,reason}' IS NOT DISTINCT FROM reason
 AND document->>'status' IS NOT DISTINCT FROM CASE decision WHEN 'release' THEN 'released' ELSE 'held' END),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,party_id,premise_id,receipt_id,id),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,party_id,premise_id,receipt_id,version),
 FOREIGN KEY(tenant_id,party_id,premise_id,receipt_id)REFERENCES supply_stock_receipts(tenant_id,party_id,premise_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,premise_id,receipt_id,identity_check_id)REFERENCES supply_stock_identity_checks(tenant_id,party_id,premise_id,receipt_id,id)ON DELETE RESTRICT
);
ALTER TABLE supply_stock_quality_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_stock_quality_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_stock_quality_decisions
 USING(hms_tenant_visible(tenant_id)AND(supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','auditor','regulator'])OR supply_stock_scope(tenant_id,party_id,premise_id,'read')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')AND actor_subject=NULLIF(current_setting('app.supply_subject',true),'')
 AND(supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','inventory'])OR supply_stock_scope(tenant_id,party_id,premise_id,'review')));
CREATE INDEX ON supply_stock_quality_decisions(tenant_id,party_id,premise_id,receipt_id,created_at DESC,id DESC);
CREATE INDEX ON supply_stock_quality_decisions(tenant_id,party_id,premise_id,receipt_id,identity_check_id);
CREATE TRIGGER supply_stock_quality_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_stock_quality_decisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_stock_quality_audit AFTER INSERT ON supply_stock_quality_decisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_stock_quality_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r supply_stock_receipts;i supply_stock_identity_checks;head bigint;identity_head bigint;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.premise_id AND kind IN('warehouse','depot')AND status='active'AND country_code='IN' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'stock source unavailable' USING ERRCODE='42501';END IF;
 SELECT * INTO r FROM supply_stock_receipts WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND id=NEW.receipt_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'stock receipt unavailable' USING ERRCODE='42501';END IF;
 SELECT * INTO i FROM supply_stock_identity_checks WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND receipt_id=NEW.receipt_id AND id=NEW.identity_check_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'stock identity unavailable' USING ERRCODE='42501';END IF;
 SELECT COALESCE(max(version),0)INTO head FROM supply_stock_quality_decisions WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND receipt_id=NEW.receipt_id;
 SELECT max(version)INTO identity_head FROM supply_stock_identity_checks WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND receipt_id=NEW.receipt_id;
 IF NEW.version<>head+1 OR(NEW.document#>>'{input,expected_version}')::bigint IS DISTINCT FROM head OR NEW.identity_version<>identity_head OR NEW.identity_version<>i.version
 OR(NEW.decision='hold'AND NEW.released_base_units<>0)
 OR(NEW.decision='release'AND(NEW.released_base_units<>i.quantity_base_units OR NEW.actor_subject IN(r.actor_subject,i.actor_subject)
 OR r.expires_on<=(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date
 OR COALESCE(i.document#>>'{source,regulatory_class}','') NOT IN('none','G','H','H1') OR COALESCE((i.document#>>'{source,ndps}')::boolean,true)))THEN
 RAISE EXCEPTION 'stock quality basis changed' USING ERRCODE='23514',CONSTRAINT='supply_stock_quality_guard';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_stock_quality_guard BEFORE INSERT ON supply_stock_quality_decisions FOR EACH ROW EXECUTE FUNCTION supply_stock_quality_guard();
