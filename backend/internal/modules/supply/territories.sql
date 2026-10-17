CREATE TABLE supply_territories (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 200),
 country_code text NOT NULL CHECK(country_code IN ('IN','PK')),region text NOT NULL CHECK(length(btrim(region)) BETWEEN 1 AND 100),
 city text NOT NULL CHECK(length(city)<=100),description text NOT NULL CHECK(length(description)<=1000),status text NOT NULL CHECK(status IN ('active','inactive')),
 branch_premise_id uuid CHECK(branch_premise_id IS NULL OR substring(branch_premise_id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,party_id,code),
 FOREIGN KEY(tenant_id,party_id,country_code) REFERENCES supply_parties(tenant_id,id,country_code) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,branch_premise_id,country_code) REFERENCES supply_premises(tenant_id,party_id,id,country_code) ON DELETE RESTRICT
);
CREATE INDEX supply_territories_page ON supply_territories(tenant_id,party_id,created_at DESC,id DESC);
CREATE INDEX supply_territory_branch ON supply_territories(tenant_id,party_id,branch_premise_id,country_code);
CREATE TABLE supply_territory_representatives (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,territory_id uuid NOT NULL,id uuid GENERATED ALWAYS AS (territory_id) STORED,subject text NOT NULL CHECK(subject ~ '^[A-Za-z0-9._:@|+\-]{1,200}$'),
 PRIMARY KEY(tenant_id,party_id,territory_id,subject),
 FOREIGN KEY(tenant_id,party_id,territory_id) REFERENCES supply_territories(tenant_id,party_id,id) ON DELETE RESTRICT
);
CREATE INDEX supply_territory_representative_subject ON supply_territory_representatives(tenant_id,party_id,subject,territory_id);
ALTER TABLE supply_territory_representatives ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_territory_representatives FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_territory_representatives USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']) OR (subject=current_setting('app.supply_subject',true) AND supply_roles_allowed(tenant_id,party_id,ARRAY['sales'])))) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
ALTER TABLE supply_territories ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_territories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_territories USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']) OR (status='active' AND supply_roles_allowed(tenant_id,party_id,ARRAY['sales']) AND EXISTS(SELECT 1 FROM supply_territory_representatives a WHERE a.tenant_id=supply_territories.tenant_id AND a.party_id=supply_territories.party_id AND a.territory_id=supply_territories.id AND a.subject=current_setting('app.supply_subject',true))))) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE FUNCTION supply_territory_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE changed_branch boolean:=true;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.party_id,NEW.id,NEW.country_code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.id,OLD.country_code,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_territory_identity';
  END IF;
  changed_branch:=NEW.branch_premise_id IS DISTINCT FROM OLD.branch_premise_id;
 ELSIF NEW.version<>1 THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_territory_identity';
 END IF;
 PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id AND country_code=NEW.country_code AND record_kind='owned_organisation' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_territory_owner'; END IF;
 IF NEW.branch_premise_id IS NOT NULL THEN
  PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.branch_premise_id AND country_code=NEW.country_code AND kind='branch' AND (NOT changed_branch OR status='active') FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_territory_branch'; END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE FUNCTION supply_territory_representative_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 IF NOT supply_effective_role(NEW.tenant_id,NEW.party_id,NEW.subject,ARRAY['sales'],NULL) THEN
  RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_territory_sales_grant';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_territory_guard BEFORE INSERT OR UPDATE ON supply_territories FOR EACH ROW EXECUTE FUNCTION supply_territory_guard();
CREATE TRIGGER supply_territory_audit AFTER INSERT OR UPDATE ON supply_territories FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_territory_retain BEFORE DELETE OR TRUNCATE ON supply_territories FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_territory_representative_guard BEFORE INSERT OR UPDATE ON supply_territory_representatives FOR EACH ROW EXECUTE FUNCTION supply_territory_representative_guard();
CREATE TRIGGER supply_territory_representative_audit AFTER INSERT OR UPDATE OR DELETE ON supply_territory_representatives FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE supply_territory_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,territory_id uuid NOT NULL,version bigint NOT NULL,
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,territory_id,version),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id,territory_id) REFERENCES supply_territories(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_territories ADD CONSTRAINT supply_territory_revision_head FOREIGN KEY(tenant_id,party_id,id,version) REFERENCES supply_territory_revisions(tenant_id,party_id,territory_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_territory_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_territory_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_territory_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE INDEX supply_territory_revision_page ON supply_territory_revisions(tenant_id,party_id,territory_id,created_at DESC,id DESC);
CREATE TRIGGER supply_territory_revision_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_territory_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_premise_territory_kind_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind<>'branch' AND EXISTS(SELECT 1 FROM supply_territories WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND branch_premise_id=NEW.id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_premise_territory_kind';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_premise_territory_kind_guard BEFORE UPDATE OF kind ON supply_premises FOR EACH ROW EXECUTE FUNCTION supply_premise_territory_kind_guard();
