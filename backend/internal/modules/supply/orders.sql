SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION supply_order_scope(t uuid,row_id uuid,side text,h uuid,d uuid,o uuid,product text,org uuid,location uuid,write_access boolean)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND((current_setting('app.supply_order_mode',true)='lookup'AND NOT write_access AND row_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid)
 OR(current_setting('app.supply_order_mode',true)IN('buyer_read','buyer_write')AND side='buyer'
 AND h=NULLIF(current_setting('app.supply_order_hospital',true),'')::uuid AND d=NULLIF(current_setting('app.supply_order_department',true),'')::uuid AND o=NULLIF(current_setting('app.supply_order_outlet',true),'')::uuid
 AND(NOT write_access OR current_setting('app.supply_order_mode',true)='buyer_write'))
 OR(current_setting('app.supply_order_mode',true)IN('seller_read','seller_write')AND side='seller'
 AND product=current_setting('app.supply_order_product',true)AND org=NULLIF(current_setting('app.supply_order_organization',true),'')::uuid AND location=NULLIF(current_setting('app.supply_order_location',true),'')::uuid
 AND(NOT write_access OR current_setting('app.supply_order_mode',true)='seller_write'))
 OR(current_setting('app.supply_order_mode',true)='exchange'AND row_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid)),false)
$$;
CREATE TABLE supply_purchase_orders(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'AND substring(id::text,20,1)IN('8','9','a','b')),
 origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,
 side text NOT NULL CHECK(side IN('buyer','seller')),
 hospital_id uuid NOT NULL,buyer_outlet_id uuid NOT NULL,department_id uuid,
 source_product text NOT NULL CHECK(source_product IN('supply','pharmacy')),source_organization_id uuid NOT NULL,source_location_id uuid NOT NULL,
 relationship_id uuid NOT NULL,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),body jsonb NOT NULL,
 proposal_input jsonb,proposal_receipt jsonb,
 actor_subject text NOT NULL CHECK(length(actor_subject)BETWEEN 1 AND 200),actor_staff_id uuid,
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL CHECK(updated_at>=created_at),
 PRIMARY KEY(tenant_id,id),
 CHECK(origin_record_id=id AND origin_tenant_id::text IS NOT DISTINCT FROM body#>>'{terms,buyer,tenant_id}'),
 CHECK(jsonb_typeof(body)='object'AND body->>'id'IS NOT DISTINCT FROM id::text AND body->>'owner_tenant_id'IS NOT DISTINCT FROM tenant_id::text AND body->>'side'IS NOT DISTINCT FROM side AND body->>'version'IS NOT DISTINCT FROM version::text),
 CHECK(body->>'status'IS NOT NULL AND body->>'status'IN('pending_review','offered','incoming','accepted','rejected','cancelled','cancellation_requested')),
 CHECK(body#>>'{terms,buyer,hospital_id}'IS NOT DISTINCT FROM hospital_id::text AND body#>>'{terms,buyer,location_id}'IS NOT DISTINCT FROM buyer_outlet_id::text),
 CHECK(body#>>'{terms,seller,product}'IS NOT DISTINCT FROM source_product AND body#>>'{terms,seller,organization_id}'IS NOT DISTINCT FROM source_organization_id::text AND body#>>'{terms,seller,location_id}'IS NOT DISTINCT FROM source_location_id::text),
 CHECK(body#>>'{input,relationship_id}'IS NOT DISTINCT FROM relationship_id::text AND body#>>'{terms,seller,tenant_id}'<>body#>>'{terms,buyer,tenant_id}'),
 CHECK((side='buyer'AND tenant_id=origin_tenant_id AND department_id IS NOT NULL AND body->>'buyer_department_id'IS NOT DISTINCT FROM department_id::text AND proposal_input IS NOT NULL AND proposal_receipt IS NOT NULL)OR(side='seller'AND tenant_id::text=body#>>'{terms,seller,tenant_id}'AND department_id IS NULL AND body->>'buyer_department_id'IS NULL AND proposal_input IS NULL AND proposal_receipt IS NULL)),
 CHECK(body->>'authority_epoch'IS NOT NULL AND body->>'remote_epoch'IS NOT NULL AND(body->>'authority_epoch')::bigint BETWEEN 1 AND version AND(body->>'remote_epoch')::bigint BETWEEN 0 AND 9007199254740991),
 FOREIGN KEY(tenant_id,relationship_id)REFERENCES supply_trade_relationships(tenant_id,id)ON DELETE RESTRICT
);
CREATE UNIQUE INDEX supply_purchase_order_document ON supply_purchase_orders(tenant_id,hospital_id,buyer_outlet_id,(body#>>'{input,purchase_order_reference}'))WHERE side='buyer';
CREATE INDEX supply_purchase_order_buyer_page ON supply_purchase_orders(tenant_id,hospital_id,department_id,buyer_outlet_id,created_at DESC,id DESC)WHERE side='buyer';
CREATE INDEX supply_purchase_order_seller_page ON supply_purchase_orders(tenant_id,source_product,source_organization_id,source_location_id,created_at DESC,id DESC)WHERE side='seller';
CREATE INDEX ON supply_purchase_orders(tenant_id,relationship_id);
ALTER TABLE supply_purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_purchase_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_purchase_orders USING(hms_tenant_visible(tenant_id)AND supply_order_scope(tenant_id,id,side,hospital_id,department_id,buyer_outlet_id,source_product,source_organization_id,source_location_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND supply_order_scope(tenant_id,id,side,hospital_id,department_id,buyer_outlet_id,source_product,source_organization_id,source_location_id,true)
 AND actor_subject=current_setting('app.actor_subject',true)
 AND((current_setting('app.supply_order_mode',true)='exchange'AND actor_subject='system:supplier-orders'AND actor_staff_id IS NULL
 AND((side='buyer'AND body#>>'{terms,seller,tenant_id}'=current_setting('app.supply_order_origin',true))OR(side='seller'AND origin_tenant_id::text=current_setting('app.supply_order_origin',true))))OR(current_setting('app.supply_order_mode',true)IN('buyer_write','seller_write')AND actor_staff_id=NULLIF(current_setting('app.supply_order_actor_staff',true),'')::uuid AND actor_staff_id<>'00000000-0000-0000-0000-000000000000')));
CREATE TABLE supply_purchase_order_commands(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'AND substring(id::text,20,1)IN('8','9','a','b')),
 order_id uuid NOT NULL,actor_subject text NOT NULL,actor_staff_id uuid NOT NULL,input jsonb NOT NULL,receipt jsonb NOT NULL,
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,order_id)REFERENCES supply_purchase_orders(tenant_id,id)ON DELETE RESTRICT,
 CHECK(jsonb_typeof(input)='object'AND jsonb_typeof(receipt)='object'AND receipt->>'command_id'IS NOT DISTINCT FROM id::text AND receipt#>>'{order,id}'IS NOT DISTINCT FROM order_id::text AND receipt->>'actor_subject'IS NOT DISTINCT FROM actor_subject AND receipt->>'actor_staff_id'IS NOT DISTINCT FROM actor_staff_id::text)
);
ALTER TABLE supply_purchase_order_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_purchase_order_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_purchase_order_commands USING(hms_tenant_visible(tenant_id)AND EXISTS(SELECT 1 FROM supply_purchase_orders o WHERE o.tenant_id=supply_purchase_order_commands.tenant_id AND o.id=order_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.actor_subject',true)AND actor_staff_id=NULLIF(current_setting('app.supply_order_actor_staff',true),'')::uuid AND current_setting('app.supply_order_mode',true)IN('buyer_write','seller_write')AND EXISTS(SELECT 1 FROM supply_purchase_orders o WHERE o.tenant_id=supply_purchase_order_commands.tenant_id AND o.id=order_id));
CREATE INDEX ON supply_purchase_order_commands(tenant_id,order_id,created_at DESC,id DESC);
CREATE TABLE supply_purchase_order_history(
 tenant_id uuid NOT NULL,origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,order_id uuid NOT NULL,version bigint NOT NULL,
 body jsonb NOT NULL,actor_subject text NOT NULL,source_event_id uuid,
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(tenant_id,order_id,version),
 FOREIGN KEY(tenant_id,order_id)REFERENCES supply_purchase_orders(tenant_id,id)ON DELETE RESTRICT,
 CHECK(jsonb_typeof(body)='object'AND body->>'id'IS NOT DISTINCT FROM order_id::text AND body->>'version'IS NOT DISTINCT FROM version::text AND origin_record_id=order_id AND body#>>'{terms,buyer,tenant_id}'IS NOT DISTINCT FROM origin_tenant_id::text)
);
ALTER TABLE supply_purchase_order_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_purchase_order_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_purchase_order_history USING(hms_tenant_visible(tenant_id)AND EXISTS(SELECT 1 FROM supply_purchase_orders o WHERE o.tenant_id=supply_purchase_order_history.tenant_id AND o.id=order_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.actor_subject',true)AND current_setting('app.supply_order_mode',true)IN('buyer_write','seller_write','exchange')AND EXISTS(SELECT 1 FROM supply_purchase_orders o WHERE o.tenant_id=supply_purchase_order_history.tenant_id AND o.id=order_id));
CREATE INDEX ON supply_purchase_order_history(tenant_id,order_id,created_at DESC);
CREATE TRIGGER supply_purchase_order_commands_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_purchase_order_commands FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_purchase_order_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_purchase_order_history FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_purchase_order_audit AFTER INSERT OR UPDATE ON supply_purchase_orders FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_purchase_order_command_audit AFTER INSERT ON supply_purchase_order_commands FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_purchase_order_preserve()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE'THEN RAISE EXCEPTION 'purchase orders are retained' USING ERRCODE='23514';END IF;
 IF(NEW.tenant_id,NEW.id,NEW.origin_tenant_id,NEW.origin_record_id,NEW.side,NEW.hospital_id,NEW.department_id,NEW.buyer_outlet_id,NEW.source_product,NEW.source_organization_id,NEW.source_location_id,NEW.relationship_id,NEW.proposal_input,NEW.proposal_receipt,NEW.created_at)IS DISTINCT FROM(OLD.tenant_id,OLD.id,OLD.origin_tenant_id,OLD.origin_record_id,OLD.side,OLD.hospital_id,OLD.department_id,OLD.buyer_outlet_id,OLD.source_product,OLD.source_organization_id,OLD.source_location_id,OLD.relationship_id,OLD.proposal_input,OLD.proposal_receipt,OLD.created_at)
 OR NEW.version<>OLD.version+1 OR NEW.body->'terms'IS DISTINCT FROM OLD.body->'terms'OR NEW.body->'input'IS DISTINCT FROM OLD.body->'input'OR NEW.body->>'input_sha256'IS DISTINCT FROM OLD.body->>'input_sha256'OR NEW.body->>'prepared_by'IS DISTINCT FROM OLD.body->>'prepared_by'OR NEW.body->>'prepared_staff_id'IS DISTINCT FROM OLD.body->>'prepared_staff_id'
 OR(NEW.body->>'authority_epoch')::bigint<(OLD.body->>'authority_epoch')::bigint OR(NEW.body->>'remote_epoch')::bigint<(OLD.body->>'remote_epoch')::bigint
 OR(OLD.body->>'status'IN('rejected','cancelled')AND NEW.body->>'status'IS DISTINCT FROM OLD.body->>'status')
 OR(OLD.body->>'signed_by'IS NOT NULL AND NEW.body->>'status'='cancellation_requested'AND(NEW.body->>'signed_by',NEW.body->>'signed_staff_id')IS DISTINCT FROM(OLD.body->>'signed_by',OLD.body->>'signed_staff_id'))
 THEN RAISE EXCEPTION 'purchase order evidence changed' USING ERRCODE='23514',CONSTRAINT='supply_purchase_order_preserve';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_purchase_order_preserve BEFORE UPDATE OR DELETE ON supply_purchase_orders FOR EACH ROW EXECUTE FUNCTION supply_purchase_order_preserve();
CREATE TRIGGER supply_purchase_order_no_truncate BEFORE TRUNCATE ON supply_purchase_orders FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
