SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION supply_trading_source_scope(t uuid,p uuid,l uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p=NULLIF(current_setting('app.supply_trade_party',true),'')::uuid
 AND l=NULLIF(current_setting('app.supply_trade_licence',true),'')::uuid
 AND current_setting('app.supply_trade_source',true)='current',false)
$$;
DO $$DECLARE tab text;prior text;licence text;BEGIN
 FOREACH tab IN ARRAY ARRAY['supply_licenses','supply_license_reviews','supply_license_evidence'] LOOP
  licence:=CASE WHEN tab='supply_licenses' THEN 'id' ELSE 'license_id' END;
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=tab::regclass AND polname='tenant_isolation';
  EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)OR supply_trading_source_scope(tenant_id,party_id,%I))',tab,prior,licence);
 END LOOP;
END $$;
