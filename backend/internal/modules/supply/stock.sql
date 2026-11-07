SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION supply_stock_scope(t uuid,p uuid,l uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p=NULLIF(current_setting('app.supply_stock_party',true),'')::uuid AND l=NULLIF(current_setting('app.supply_stock_premise',true),'')::uuid
 AND current_setting('app.supply_stock_access',true)='authorized' AND NULLIF(current_setting('app.supply_subject',true),'')IS NOT NULL
 AND current_setting('app.supply_subject',true)=current_setting('app.actor_subject',true)
 AND ((operation='read'AND current_setting('app.supply_stock_permission',true)IN('supply.operations.read','supply.operations.write'))OR(operation='write'AND current_setting('app.supply_stock_permission',true)='supply.operations.write')),false)
$$;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='supply_premises'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON supply_premises USING((%s)OR supply_stock_scope(tenant_id,party_id,id,''read''))',prior);
END $$;
CREATE TABLE supply_stock_receipts (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN('8','9','a','b')),
 product_version_id uuid NOT NULL CHECK(product_version_id<>'00000000-0000-0000-0000-000000000000'),
 lot_number text NOT NULL CHECK(char_length(lot_number) BETWEEN 1 AND 100 AND lot_number=btrim(lot_number) AND lot_number !~ '[[:cntrl:]]'),
 expires_on date NOT NULL CHECK(expires_on>='1900-01-01'),
 unit_code text NOT NULL CHECK(unit_code ~ '^[a-z][a-z0-9._-]{0,39}$'),
 quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 9007199254740991),
 source_reference text NOT NULL CHECK(char_length(source_reference) BETWEEN 1 AND 200 AND source_reference=btrim(source_reference) AND source_reference !~ '[[:cntrl:]]'),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(char_length(actor_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,party_id,premise_id,id),
 UNIQUE(tenant_id,party_id,premise_id,source_reference),
 FOREIGN KEY(tenant_id,party_id,premise_id) REFERENCES supply_premises(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_stock_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_stock_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_stock_receipts
 USING(hms_tenant_visible(tenant_id) AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','auditor','regulator'])OR supply_stock_scope(tenant_id,party_id,premise_id,'read')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
 AND actor_subject=NULLIF(current_setting('app.supply_subject',true),'')
 AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','inventory'])OR supply_stock_scope(tenant_id,party_id,premise_id,'write')));
CREATE INDEX ON supply_stock_receipts(tenant_id,party_id,premise_id,created_at DESC,id DESC);
CREATE TRIGGER supply_stock_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_stock_receipts FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_stock_receipt_audit AFTER INSERT ON supply_stock_receipts FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_stock_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.premise_id AND kind IN('warehouse','depot') AND status='active' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'stock destination is unavailable' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_stock_receipt_guard BEFORE INSERT ON supply_stock_receipts FOR EACH ROW EXECUTE FUNCTION supply_stock_receipt_guard();

CREATE TABLE supply_stock_identity_checks (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,receipt_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN('8','9','a','b')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 product_version_id uuid NOT NULL CHECK(product_version_id<>'00000000-0000-0000-0000-000000000000'),
 quantity_base_units bigint NOT NULL CHECK(quantity_base_units BETWEEN 1 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(char_length(actor_subject) BETWEEN 1 AND 200),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'
  AND document->>'id' IS NOT DISTINCT FROM id::text
  AND document->>'owner_tenant_id' IS NOT DISTINCT FROM tenant_id::text
  AND document->>'party_id' IS NOT DISTINCT FROM party_id::text
  AND document->>'premise_id' IS NOT DISTINCT FROM premise_id::text
  AND document->>'receipt_id' IS NOT DISTINCT FROM receipt_id::text
  AND document->>'version' IS NOT DISTINCT FROM version::text
  AND document->>'actor_subject' IS NOT DISTINCT FROM actor_subject
  AND document->>'quantity_base_units' IS NOT DISTINCT FROM quantity_base_units::text
  AND document#>>'{source,version_id}' IS NOT DISTINCT FROM product_version_id::text
  AND document#>>'{input,product_version_id}' IS NOT DISTINCT FROM product_version_id::text
  AND document->>'status' IS NOT DISTINCT FROM 'matched'),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,party_id,premise_id,receipt_id,id),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,party_id,premise_id,receipt_id,version),
 FOREIGN KEY(tenant_id,party_id,premise_id,receipt_id)REFERENCES supply_stock_receipts(tenant_id,party_id,premise_id,id)ON DELETE RESTRICT
);
ALTER TABLE supply_stock_identity_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_stock_identity_checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_stock_identity_checks
 USING(hms_tenant_visible(tenant_id) AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','auditor','regulator'])OR supply_stock_scope(tenant_id,party_id,premise_id,'read')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
 AND actor_subject=NULLIF(current_setting('app.supply_subject',true),'')
 AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','inventory'])OR supply_stock_scope(tenant_id,party_id,premise_id,'write')));
CREATE INDEX ON supply_stock_identity_checks(tenant_id,party_id,premise_id,receipt_id,created_at DESC,id DESC);
CREATE TRIGGER supply_stock_identity_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_stock_identity_checks FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_stock_identity_audit AFTER INSERT ON supply_stock_identity_checks FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_stock_identity_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r supply_stock_receipts;head bigint;factor bigint;unit_kind text;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.premise_id AND kind IN('warehouse','depot') AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'stock destination is unavailable' USING ERRCODE='42501';END IF;
 SELECT * INTO r FROM supply_stock_receipts WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND id=NEW.receipt_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'stock receipt is unavailable' USING ERRCODE='42501';END IF;
 SELECT COALESCE(max(version),0) INTO head FROM supply_stock_identity_checks WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND receipt_id=NEW.receipt_id;
 unit_kind:=NEW.document#>>'{input,unit_kind}';
 factor:=CASE unit_kind WHEN 'pack' THEN (NEW.document#>>'{source,pack_size}')::bigint WHEN 'unit' THEN 1 ELSE 0 END;
 IF NEW.version<>head+1 OR (NEW.document#>>'{input,expected_version}')::bigint IS DISTINCT FROM head
 OR factor IS NULL OR factor<1 OR r.quantity_units>9007199254740991/factor
 OR NEW.quantity_base_units IS DISTINCT FROM r.quantity_units*factor
 OR NEW.quantity_base_units IS DISTINCT FROM (NEW.document#>>'{input,expected_base_units}')::bigint
 OR NEW.document#>>'{source,pack_size}' IS DISTINCT FROM NEW.document#>>'{input,expected_pack_size}'
 OR (unit_kind='pack' AND r.unit_code<>'pack')
 OR (unit_kind='unit' AND r.unit_code IS DISTINCT FROM NEW.document#>>'{source,unit_of_issue}')THEN
  RAISE EXCEPTION 'stock identity or units changed' USING ERRCODE='23514',CONSTRAINT='supply_stock_identity_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_stock_identity_guard BEFORE INSERT ON supply_stock_identity_checks FOR EACH ROW EXECUTE FUNCTION supply_stock_identity_guard();
