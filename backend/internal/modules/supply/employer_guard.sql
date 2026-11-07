SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- A source-owned structural lookup for Workforce; no contacts, financial or licence tables use this purpose.
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO STRICT prior FROM pg_policy
 WHERE polrelid='supply_parties'::regclass AND polname='tenant_isolation' AND polpermissive;
 EXECUTE format($p$ALTER POLICY tenant_isolation ON supply_parties USING ((%s) OR
 (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.supply_employer_check',true)='true' AND record_kind='owned_organisation' AND country_code='IN'))$p$,prior);
END $$;

DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO STRICT prior FROM pg_policy WHERE polrelid='supply_premises'::regclass AND polname='tenant_isolation' AND polpermissive;
 EXECUTE format($p$ALTER POLICY tenant_isolation ON supply_premises USING((%s)OR
 (hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.supply_operating_reference',true)='true'
 AND party_id=NULLIF(current_setting('app.supply_reference_party',true),'')::uuid
 AND id=NULLIF(current_setting('app.supply_reference_premise',true),'')::uuid
 AND country_code='IN' AND kind IN('warehouse','depot')AND status='active'))$p$,prior);
END $$;
