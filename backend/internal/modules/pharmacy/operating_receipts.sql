SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- ADR-0064: only the IAM-admitted operating command sets these transaction-local settings (archtest-enforced).
CREATE FUNCTION pharmacy_operating_stock_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_stock_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_stock_access',true)='authorized'
 AND NULLIF(current_setting('app.pharmacy_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND((operation='read'AND current_setting('app.pharmacy_stock_permission',true)IN('pharmacy.stock.read','pharmacy.stock.receive'))
  OR(operation='write'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stock.receive'))
 AND EXISTS(SELECT 1 FROM pharmacy_outlets x WHERE x.tenant_id=t AND x.id=o AND x.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid),false)
$$;
DO $$DECLARE tab text;prior text;BEGIN
 FOREACH tab IN ARRAY ARRAY['pharmacy_stock_locations','pharmacy_stock_batches','pharmacy_stock_movements','pharmacy_catalogue_products']LOOP
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=tab::regclass AND polname='tenant_isolation';
  EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)OR pharmacy_operating_stock_scope(tenant_id,outlet_id,''read''))',tab,prior);
 END LOOP;
 SELECT pg_get_expr(polwithcheck,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_stock_batches'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON pharmacy_stock_batches WITH CHECK((%s)OR pharmacy_operating_stock_scope(tenant_id,outlet_id,''write''))',prior);
 SELECT pg_get_expr(polwithcheck,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_stock_movements'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON pharmacy_stock_movements WITH CHECK((%s)OR(pharmacy_operating_stock_scope(tenant_id,outlet_id,''write'')AND kind=''receipt''AND actor_subject=NULLIF(current_setting(''app.actor_subject'',true),'''')))',prior);
END $$;
CREATE TABLE pharmacy_operating_stock_audit(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 pharmacy_id uuid NOT NULL,outlet_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('pharmacy.operating_stock.read','pharmacy.operating_stock.batch_recorded','pharmacy.operating_stock.receipt_recorded')),
 resource_kind text NOT NULL CHECK(resource_kind IN('product','location','batch','receipt')),resource_id uuid,
 permission text NOT NULL CHECK(permission IN('pharmacy.stock.read','pharmacy.stock.receive')),
 duty_id uuid NOT NULL CHECK(duty_id<>'00000000-0000-0000-0000-000000000000'),
 snapshot_version bigint NOT NULL CHECK(snapshot_version BETWEEN 1 AND 9007199254740991),
 authority_epoch bigint NOT NULL CHECK(authority_epoch BETWEEN 1 AND 9007199254740991),
 staff_id uuid NOT NULL CHECK(staff_id<>'00000000-0000-0000-0000-000000000000'),staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740991),
 link_id uuid NOT NULL CHECK(link_id<>'00000000-0000-0000-0000-000000000000'),link_version bigint NOT NULL CHECK(link_version BETWEEN 1 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id),
 CHECK(CASE action WHEN 'pharmacy.operating_stock.read' THEN permission='pharmacy.stock.read'
  WHEN 'pharmacy.operating_stock.batch_recorded' THEN permission='pharmacy.stock.receive'AND resource_kind='batch'AND resource_id IS NOT NULL
  ELSE permission='pharmacy.stock.receive'AND resource_kind='receipt'AND resource_id IS NOT NULL END)
);
ALTER TABLE pharmacy_operating_stock_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_operating_stock_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_operating_stock_audit
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,true))
 WITH CHECK(pharmacy_operating_stock_scope(tenant_id,outlet_id,'read')
  AND pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid
  AND permission=current_setting('app.pharmacy_stock_permission',true)
  AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX pharmacy_operating_stock_audit_page ON pharmacy_operating_stock_audit(tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_operating_stock_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_operating_stock_audit
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
