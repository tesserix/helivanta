SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_access_grants (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(subject ~ '^[A-Za-z0-9._:@|+\-]{1,200}$'),
 role text NOT NULL CHECK(role IN ('administrator','purchasing','finance','compliance','auditor','warehouse','sales','driver','inventory','picker','packer','dispatcher','regulator')),
 premise_id uuid,valid_from timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 5 AND 500),status text NOT NULL CHECK(status IN ('active','revoked')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,premise_id) REFERENCES supply_premises(tenant_id,party_id,id) ON DELETE RESTRICT,
 CHECK(expires_at>valid_from AND expires_at<=valid_from+interval '366 days'),
 CONSTRAINT supply_access_scope CHECK(status='revoked' OR ((premise_id IS NULL AND role<>'regulator') OR (premise_id IS NOT NULL AND role IN ('warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator'))))
);
CREATE UNIQUE INDEX supply_access_grants_global_unique ON supply_access_grants(tenant_id,party_id,subject,role) WHERE premise_id IS NULL AND status='active';
CREATE UNIQUE INDEX supply_access_grants_location_unique ON supply_access_grants(tenant_id,party_id,subject,role,premise_id) WHERE premise_id IS NOT NULL AND status='active';
CREATE INDEX supply_access_grants_subject ON supply_access_grants(tenant_id,subject,party_id);
CREATE INDEX supply_access_grants_page ON supply_access_grants(tenant_id,party_id,created_at DESC,id DESC);
ALTER TABLE supply_access_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_access_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_access_grants USING(
 hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (subject=NULLIF(current_setting('app.supply_subject',true),'')
  OR (party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND current_setting('app.supply_access_admin',true)='on')
  OR current_setting('app.supply_authority_lookup',true)='on')
) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND current_setting('app.supply_access_admin',true)='on');
CREATE TRIGGER supply_access_grants_audit AFTER INSERT OR UPDATE ON supply_access_grants FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_effective_role(row_tenant uuid,row_party uuid,grant_subject text,roles text[],row_premise uuid DEFAULT NULL) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_lookup text;allowed boolean;
BEGIN
 IF row_tenant IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false; END IF;
 previous_lookup:=current_setting('app.supply_authority_lookup',true);
 PERFORM set_config('app.supply_authority_lookup','on',true);
 BEGIN
  SELECT EXISTS(SELECT 1 FROM supply_party_access a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.subject=grant_subject AND a.role=ANY(roles))
   OR EXISTS(SELECT 1 FROM supply_access_grants a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.subject=grant_subject AND a.role=ANY(roles) AND a.status='active' AND a.valid_from<=clock_timestamp() AND a.expires_at>clock_timestamp() AND (a.premise_id IS NULL OR a.premise_id=row_premise)) INTO allowed;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.supply_authority_lookup',COALESCE(previous_lookup,''),true);
  RAISE;
 END;
 PERFORM set_config('app.supply_authority_lookup',COALESCE(previous_lookup,''),true);
 RETURN allowed;
END $$;
CREATE OR REPLACE FUNCTION supply_party_allowed(row_tenant uuid,row_party uuid,writing boolean) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT row_party=NULLIF(current_setting('app.supply_party',true),'')::uuid AND supply_effective_role(row_tenant,row_party,NULLIF(current_setting('app.supply_subject',true),''),CASE WHEN writing THEN ARRAY['administrator','purchasing'] ELSE ARRAY['administrator','purchasing','finance','compliance','auditor'] END,NULL)
$$;
CREATE OR REPLACE FUNCTION supply_roles_allowed(row_tenant uuid,row_party uuid,roles text[]) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT row_party=NULLIF(current_setting('app.supply_party',true),'')::uuid AND supply_effective_role(row_tenant,row_party,NULLIF(current_setting('app.supply_subject',true),''),roles,NULL)
$$;
CREATE FUNCTION supply_access_roles(row_tenant uuid,row_party uuid,grant_subject text) RETURNS jsonb LANGUAGE sql VOLATILE AS $$
 SELECT COALESCE(jsonb_agg(role ORDER BY role),'[]'::jsonb) FROM (SELECT DISTINCT role FROM (SELECT role FROM supply_party_access a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.subject=grant_subject UNION ALL SELECT role FROM supply_access_grants a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.subject=grant_subject AND a.premise_id IS NULL AND a.status='active' AND a.valid_from<=clock_timestamp() AND a.expires_at>clock_timestamp()) r) roles WHERE row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
$$;
ALTER POLICY tenant_isolation ON supply_parties USING(
 hms_tenant_visible(tenant_id)
 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (supply_party_allowed(tenant_id,id,false)
  OR (current_setting('app.supply_directory',true)='on'
   AND (current_setting('app.supply_manager',true)='on'
    OR supply_effective_role(tenant_id,id,NULLIF(current_setting('app.supply_subject',true),''),ARRAY['administrator','purchasing','finance','compliance','auditor','warehouse','sales','driver'],NULL)
   )
  )
 )
);
CREATE TABLE supply_access_grant_revisions (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,grant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,grant_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id,grant_id) REFERENCES supply_access_grants(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_access_grants ADD CONSTRAINT supply_access_current_revision FOREIGN KEY(tenant_id,party_id,id,version) REFERENCES supply_access_grant_revisions(tenant_id,party_id,grant_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_access_grant_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_access_grant_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_access_grant_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator'])) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND current_setting('app.supply_access_admin',true)='on');
CREATE INDEX supply_access_revisions_page ON supply_access_grant_revisions(tenant_id,party_id,grant_id,created_at DESC,id DESC);
CREATE TRIGGER supply_access_revisions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_access_grant_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_access_grants_retain BEFORE DELETE OR TRUNCATE ON supply_access_grants FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_access_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.party_id,NEW.id,NEW.subject,NEW.role,NEW.premise_id,NEW.valid_from,NEW.created_at) IS DISTINCT FROM(OLD.tenant_id,OLD.party_id,OLD.id,OLD.subject,OLD.role,OLD.premise_id,OLD.valid_from,OLD.created_at) OR NEW.version<>OLD.version+1 OR OLD.status='revoked') THEN RAISE EXCEPTION 'grant identity or revoked state immutable' USING ERRCODE='23514',CONSTRAINT='supply_access_identity'; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 IF NEW.premise_id IS NOT NULL AND NEW.status='active' AND NOT EXISTS(SELECT 1 FROM supply_premises p WHERE p.tenant_id=NEW.tenant_id AND p.party_id=NEW.party_id AND p.id=NEW.premise_id AND p.status='active' AND p.kind IN ('warehouse','depot')) THEN RAISE EXCEPTION 'location access requires active warehouse or depot' USING ERRCODE='23514',CONSTRAINT='supply_access_scope'; END IF;
 IF NEW.premise_id IS NULL AND NEW.status='active' AND EXISTS(SELECT 1 FROM supply_party_access WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND subject=NEW.subject AND role=NEW.role) THEN RAISE EXCEPTION 'duplicate global grant' USING ERRCODE='23505',CONSTRAINT='supply_access_global_duplicate'; END IF;
 IF TG_OP='INSERT' AND (NEW.version<>1 OR NEW.status<>'active') THEN RAISE EXCEPTION 'grant must start active at version one' USING ERRCODE='23514',CONSTRAINT='supply_access_identity'; END IF;
 NEW.updated_at:=clock_timestamp(); RETURN NEW;
 END $$;
CREATE TRIGGER supply_access_grant_guard BEFORE INSERT OR UPDATE ON supply_access_grants FOR EACH ROW EXECUTE FUNCTION supply_access_grant_guard();

CREATE FUNCTION supply_legacy_access_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 IF EXISTS(SELECT 1 FROM supply_access_grants WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND subject=NEW.subject AND role=NEW.role AND premise_id IS NULL AND status='active') THEN RAISE EXCEPTION 'duplicate global grant' USING ERRCODE='23505',CONSTRAINT='supply_access_global_duplicate'; END IF;
 RETURN NEW;
 END $$;
CREATE TRIGGER supply_legacy_access_guard BEFORE INSERT OR UPDATE ON supply_party_access FOR EACH ROW EXECUTE FUNCTION supply_legacy_access_guard();
