SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_operational_access_limits (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid GENERATED ALWAYS AS(party_id) STORED,
 max_grant_hours bigint NOT NULL CHECK(max_grant_hours BETWEEN 1 AND 8784),max_support_hours bigint NOT NULL CHECK(max_support_hours BETWEEN 1 AND 24),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 5 AND 500),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id),FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_operational_access_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_operational_access_limits FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_operational_access_limits USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE FUNCTION supply_operational_access_limits_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 IF NOT supply_roles_allowed(NEW.tenant_id,NEW.party_id,ARRAY['administrator']) THEN RAISE EXCEPTION USING ERRCODE='42501',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_authority'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.party_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.created_at) OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION USING ERRCODE='23514',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_identity'; END IF;
 ELSIF NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_identity'; END IF;
 PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id AND record_kind='owned_organisation' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_owner'; END IF;
 NEW.updated_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER supply_operational_access_limits_guard BEFORE INSERT OR UPDATE ON supply_operational_access_limits FOR EACH ROW EXECUTE FUNCTION supply_operational_access_limits_guard();
CREATE TRIGGER supply_operational_access_limits_audit AFTER INSERT OR UPDATE ON supply_operational_access_limits FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_operational_access_limits_retain BEFORE DELETE OR TRUNCATE ON supply_operational_access_limits FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_operational_access_limit_revisions (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id) REFERENCES supply_operational_access_limits(tenant_id,party_id) ON DELETE RESTRICT
);
ALTER TABLE supply_operational_access_limits ADD CONSTRAINT supply_operational_access_limit_head FOREIGN KEY(tenant_id,party_id,version) REFERENCES supply_operational_access_limit_revisions(tenant_id,party_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_operational_access_limit_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_operational_access_limit_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_operational_access_limit_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE INDEX supply_operational_access_limit_revision_page ON supply_operational_access_limit_revisions(tenant_id,party_id,created_at DESC,id DESC);
CREATE TRIGGER supply_operational_access_limit_revision_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_operational_access_limit_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_operational_access_limit(row_tenant uuid,row_party uuid,kind text) RETURNS bigint LANGUAGE plpgsql VOLATILE AS $$
DECLARE limit_hours bigint;
BEGIN
 IF NOT supply_roles_allowed(row_tenant,row_party,ARRAY['administrator']) THEN RAISE EXCEPTION USING ERRCODE='42501',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_authority'; END IF;
 IF kind='grant' THEN SELECT max_grant_hours INTO limit_hours FROM supply_operational_access_limits WHERE tenant_id=row_tenant AND party_id=row_party;
 ELSIF kind='support' THEN SELECT max_support_hours INTO limit_hours FROM supply_operational_access_limits WHERE tenant_id=row_tenant AND party_id=row_party;
 ELSE RAISE EXCEPTION USING ERRCODE='23514',TABLE='supply_operational_access_limits',CONSTRAINT='supply_operational_access_limits_kind'; END IF;
 RETURN COALESCE(limit_hours,CASE WHEN kind='grant' THEN 8784 ELSE 24 END);
END $$;
CREATE FUNCTION supply_operational_access_duration_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE check_duration boolean;limit_hours bigint;
BEGIN
 IF TG_TABLE_NAME='supply_access_grants' THEN
  check_duration:=NEW.status='active' AND NEW.role<>'administrator' AND (TG_OP='INSERT' OR NEW.expires_at>OLD.expires_at);
 ELSE
  check_duration:=TG_OP='INSERT' OR (NEW.status='approved' AND OLD.status<>'approved');
 END IF;
 IF check_duration THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
  limit_hours:=supply_operational_access_limit(NEW.tenant_id,NEW.party_id,CASE WHEN TG_TABLE_NAME='supply_access_grants' THEN 'grant' ELSE 'support' END);
  IF NEW.expires_at-NEW.valid_from>limit_hours*interval '1 hour' THEN RAISE EXCEPTION USING ERRCODE='23514',TABLE=TG_TABLE_NAME,CONSTRAINT='supply_operational_access_duration'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_operational_access_duration BEFORE INSERT OR UPDATE ON supply_access_grants FOR EACH ROW EXECUTE FUNCTION supply_operational_access_duration_guard();
CREATE TRIGGER supply_operational_access_duration BEFORE INSERT OR UPDATE ON supply_support_access FOR EACH ROW EXECUTE FUNCTION supply_operational_access_duration_guard();
CREATE FUNCTION supply_operational_permanent_access_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 IF NEW.role<>'administrator' AND EXISTS(SELECT 1 FROM supply_operational_access_limits WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',TABLE='supply_party_access',CONSTRAINT='supply_operational_access_permanent';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_operational_permanent_access_guard BEFORE INSERT ON supply_party_access FOR EACH ROW EXECUTE FUNCTION supply_operational_permanent_access_guard();
