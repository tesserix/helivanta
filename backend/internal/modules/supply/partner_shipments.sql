SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $prior$BEGIN
 IF NOT supply_operating_choice_integrity() THEN RAISE EXCEPTION 'supply policy inventory changed before partner projection migration';END IF;
END $prior$;
CREATE FUNCTION supply_partner_shipment_order(t uuid,row_side text,h uuid,outlet uuid,product text,org uuid,location uuid,relationship uuid,terms_hash text,buyer uuid,buyer_org uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND proof->>'permission'='partner.shipment.read'
 AND proof->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND (proof->>'valid_until')::timestamptz>clock_timestamp()
 AND proof->>'projection_hash'~'^[a-f0-9]{64}$'
 AND proof->>'grant_id'~'^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND proof-ARRAY['scope','grant_id','relationship_id','terms_sha256','buyer_organization_id','buyer_location_id','actor_subject','permission','projection_hash','valid_until']='{}'::jsonb
 AND (proof->'scope')-ARRAY['source_tenant_id','recipient_tenant_id','hospital_id','source_product','source_organization_id','source_location_id']='{}'::jsonb
 AND row_side='seller' AND t::text=proof#>>'{scope,source_tenant_id}'
 AND buyer::text=proof#>>'{scope,recipient_tenant_id}' AND t<>buyer
 AND h::text=proof#>>'{scope,hospital_id}'
 AND product=proof#>>'{scope,source_product}' AND product IN('pharmacy','supply')
 AND org::text=proof#>>'{scope,source_organization_id}'
 AND location::text=proof#>>'{scope,source_location_id}'
 AND relationship::text=proof->>'relationship_id'
 AND terms_hash=proof->>'terms_sha256'AND terms_hash~'^[a-f0-9]{64}$'
 AND outlet::text=proof->>'buyer_location_id'
 AND buyer_org::text=proof->>'buyer_organization_id',false)
 FROM(SELECT NULLIF(current_setting('app.supply_partner_shipments',true),'')::jsonb AS proof)frame
$$;
DO $purpose$
DECLARE target record;reading text;prior text;
BEGIN
 FOR target IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'supply_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
  reading:='false';
  IF target.relname='supply_purchase_orders'THEN
   reading:='supply_partner_shipment_order(tenant_id,side,hospital_id,buyer_outlet_id,source_product,source_organization_id,source_location_id,relationship_id,body#>>''{input,terms_sha256}'',origin_tenant_id,(body#>>''{terms,buyer,organization_id}'')::uuid)';
  ELSIF target.relname='supply_supplier_dispatched_shipments'THEN
   reading:='EXISTS(SELECT 1 FROM supply_purchase_orders o WHERE o.tenant_id=supply_supplier_dispatched_shipments.tenant_id AND o.id=supply_supplier_dispatched_shipments.order_id AND o.origin_tenant_id=supply_supplier_dispatched_shipments.buyer_tenant_id AND supply_partner_shipment_order(o.tenant_id,o.side,o.hospital_id,o.buyer_outlet_id,o.source_product,o.source_organization_id,o.source_location_id,o.relationship_id,o.body#>>''{input,terms_sha256}'',o.origin_tenant_id,(o.body#>>''{terms,buyer,organization_id}'')::uuid))';
  END IF;
  IF reading<>'false'THEN
   SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=('public.'||quote_ident(target.relname))::regclass AND polname='tenant_isolation'AND polpermissive AND polcmd='*'AND polqual IS NOT NULL AND polwithcheck IS NOT NULL;
   EXECUTE format('ALTER POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id)AND((%s)OR(%s)))',target.relname,prior,reading);
  END IF;
  EXECUTE format('CREATE POLICY partner_shipment_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND(COALESCE(current_setting(''app.supply_partner_shipments'',true),'''')=''''OR(%s)))WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND COALESCE(current_setting(''app.supply_partner_shipments'',true),'''')='''')',target.relname,reading);
  EXECUTE format('CREATE POLICY partner_shipment_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting(''app.supply_partner_shipments'',true),'''')='''')',target.relname);
 END LOOP;
END $purpose$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('supply_parties','supply_premises')OR starts_with(c.relname,'supply_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='supply_operating_choice_context';
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION supply_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('supply_parties','supply_premises')OR starts_with(c.relname,'supply_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',c.relname,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','ORDER BY c.relname,p.polname),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND starts_with(c.relname,'supply_')AND(p.polname IN('partner_shipment_purpose','partner_shipment_delete')OR c.relname IN('supply_purchase_orders','supply_supplier_dispatched_shipments')AND p.polname='tenant_isolation');
 SELECT format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid)))INTO STRICT functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=current_schema()AND p.proname='supply_partner_shipment_order';
 EXECUTE format($definition$CREATE FUNCTION supply_partner_shipment_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND starts_with(c.relname,'supply_')AND(p.polname IN('partner_shipment_purpose','partner_shipment_delete')OR c.relname IN('supply_purchase_orders','supply_supplier_dispatched_shipments')AND p.polname='tenant_isolation'))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
CREATE FUNCTION supply_partner_shipment_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT supply_partner_shipment_integrity()AND supply_operating_choice_integrity()AND NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND(rolsuper OR rolbypassrls))AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'supply_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='partner_shipment_purpose'AND NOT p.polpermissive AND p.polcmd='*'AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='partner_shipment_delete'AND NOT p.polpermissive AND p.polcmd='d'AND p.polqual IS NOT NULL)))
 AND EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.supply_partner_shipment_order_page')AND indrelid='public.supply_purchase_orders'::regclass AND indisvalid AND indisready)
$$;
