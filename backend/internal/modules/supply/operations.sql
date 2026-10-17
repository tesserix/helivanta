SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION supply_operating_allowed(row_tenant uuid,row_party uuid,row_premise uuid,roles text[]) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE ordinary_roles text[];
BEGIN
 IF row_tenant IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid OR row_party IS DISTINCT FROM NULLIF(current_setting('app.supply_party',true),'')::uuid THEN RETURN false; END IF;
 SELECT COALESCE(array_agg(role),'{}'::text[]) INTO ordinary_roles FROM unnest(roles) AS r(role) WHERE role<>'regulator';
 RETURN supply_effective_role(row_tenant,row_party,NULLIF(current_setting('app.supply_subject',true),''),ordinary_roles,row_premise)
  OR ('regulator'=ANY(roles) AND row_premise IS NOT NULL AND EXISTS(SELECT 1 FROM supply_access_grants a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.premise_id=row_premise AND a.subject=NULLIF(current_setting('app.supply_subject',true),'') AND a.role='regulator' AND a.status='active' AND a.valid_from<=clock_timestamp() AND a.expires_at>clock_timestamp()));
END $$;
CREATE FUNCTION supply_operating_owned(row_tenant uuid,row_party uuid,row_premise uuid,roles text[]) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_directory text;previous_manager text;owned boolean;
BEGIN
 IF NOT supply_operating_allowed(row_tenant,row_party,row_premise,roles) THEN RETURN false; END IF;
 previous_directory:=current_setting('app.supply_directory',true);previous_manager:=current_setting('app.supply_manager',true);
 PERFORM set_config('app.supply_directory','on',true),set_config('app.supply_manager','on',true);
 BEGIN
  SELECT EXISTS(SELECT 1 FROM supply_parties WHERE tenant_id=row_tenant AND id=row_party AND record_kind='owned_organisation') INTO owned;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.supply_directory',COALESCE(previous_directory,''),true),set_config('app.supply_manager',COALESCE(previous_manager,''),true);
  RAISE;
 END;
 PERFORM set_config('app.supply_directory',COALESCE(previous_directory,''),true),set_config('app.supply_manager',COALESCE(previous_manager,''),true);
 RETURN owned;
END $$;
ALTER POLICY tenant_isolation ON supply_premises USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']) OR (kind IN ('warehouse','depot') AND supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator'])))) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']));
ALTER POLICY tenant_isolation ON supply_bins USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance','auditor']) OR supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','auditor','regulator']))) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']) OR supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','inventory']));
ALTER POLICY tenant_isolation ON supply_bin_revisions USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance','auditor']) OR supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','auditor','regulator']))) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']) OR supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','inventory']));
CREATE FUNCTION supply_operating_directory_allowed(row_tenant uuid,row_party uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_party text;allowed boolean;
BEGIN
 IF row_tenant IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid OR current_setting('app.supply_manager',true)='on' THEN RETURN false; END IF;
 previous_party:=current_setting('app.supply_party',true);PERFORM set_config('app.supply_party',row_party::text,true);
 BEGIN
  SELECT EXISTS(SELECT 1 FROM supply_premises p WHERE p.tenant_id=row_tenant AND p.party_id=row_party AND p.kind IN ('warehouse','depot') AND p.status='active' AND supply_operating_owned(row_tenant,row_party,p.id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator'])) INTO allowed;
 EXCEPTION WHEN OTHERS THEN PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);RAISE;
 END;
 PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);RETURN allowed;
END $$;
CREATE FUNCTION supply_operating_roles(row_tenant uuid,row_party uuid,grant_subject text) RETURNS jsonb LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_party text;roles_snapshot jsonb;
BEGIN
 IF row_tenant IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid OR grant_subject IS DISTINCT FROM NULLIF(current_setting('app.supply_subject',true),'') THEN RETURN '[]'::jsonb; END IF;
 previous_party:=current_setting('app.supply_party',true);
 PERFORM set_config('app.supply_party',row_party::text,true);
 BEGIN
  SELECT COALESCE(jsonb_agg(role ORDER BY role),'[]'::jsonb) INTO roles_snapshot FROM (
   SELECT DISTINCT a.role FROM (
    SELECT role,NULL::uuid AS premise_id FROM supply_party_access WHERE tenant_id=row_tenant AND party_id=row_party AND subject=grant_subject
    UNION ALL
    SELECT role,premise_id FROM supply_access_grants WHERE tenant_id=row_tenant AND party_id=row_party AND subject=grant_subject AND status='active' AND valid_from<=clock_timestamp() AND expires_at>clock_timestamp()
   ) a WHERE a.role IN ('administrator','warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator') AND EXISTS(
    SELECT 1 FROM supply_premises p WHERE p.tenant_id=row_tenant AND p.party_id=row_party AND p.kind IN ('warehouse','depot') AND p.status='active' AND (a.premise_id IS NULL OR a.premise_id=p.id) AND supply_operating_owned(row_tenant,row_party,p.id,ARRAY[a.role])
   )
  ) effective_roles;
 EXCEPTION WHEN OTHERS THEN PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);RAISE;END;
 PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);
 RETURN roles_snapshot;
END $$;
