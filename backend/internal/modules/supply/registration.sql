SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION supply_roles_allowed(row_tenant uuid,row_party uuid,roles text[]) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_party=NULLIF(current_setting('app.supply_party',true),'')::uuid
 AND EXISTS(SELECT 1 FROM supply_party_access a WHERE a.tenant_id=row_tenant AND a.party_id=row_party
 AND a.subject=NULLIF(current_setting('app.supply_subject',true),'') AND a.role=ANY(roles))
$$;
CREATE FUNCTION supply_registry_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.party_id,NEW.id,NEW.country_code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.id,OLD.country_code,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
 RAISE EXCEPTION 'registry identity or version is immutable' USING ERRCODE='23514',CONSTRAINT='supply_registry_identity'; END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE FUNCTION supply_categories_valid(value jsonb) RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE WHEN jsonb_typeof(value)='array' THEN
 (SELECT count(*) BETWEEN 1 AND 4 AND count(*)=count(DISTINCT v) AND bool_and(v IN ('otc','prescription','controlled','cold_chain')) FROM jsonb_array_elements_text(value) AS t(v)) ELSE false END
$$;
CREATE TABLE supply_premises (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 200),
 kind text NOT NULL CHECK(kind IN ('registered','branch','warehouse','depot','pharmacy')),
 line1 text NOT NULL CHECK(length(btrim(line1)) BETWEEN 1 AND 200),line2 text NOT NULL CHECK(length(line2)<=200),city text NOT NULL CHECK(length(btrim(city)) BETWEEN 1 AND 100),region text NOT NULL CHECK(length(btrim(region)) BETWEEN 1 AND 100),postal_code text NOT NULL CHECK(length(btrim(postal_code)) BETWEEN 1 AND 20),country_code text NOT NULL CHECK(country_code IN ('IN','PK')),
 time_zone text NOT NULL CHECK(length(btrim(time_zone)) BETWEEN 1 AND 100),status text NOT NULL CHECK(status IN ('active','inactive')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,party_id,code),UNIQUE(tenant_id,party_id,id,country_code),
 FOREIGN KEY(tenant_id,party_id,country_code) REFERENCES supply_parties(tenant_id,id,country_code) ON DELETE RESTRICT
);
CREATE TABLE supply_licenses (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),country_code text NOT NULL,
 kind text NOT NULL CHECK(kind ~ '^[A-Z0-9][A-Z0-9._-]{0,59}$'),number text NOT NULL CHECK(length(btrim(number)) BETWEEN 1 AND 100),issuing_authority text NOT NULL CHECK(length(btrim(issuing_authority)) BETWEEN 1 AND 200),jurisdiction text NOT NULL CHECK(length(btrim(jurisdiction)) BETWEEN 1 AND 100),premise_id uuid NOT NULL,
 issued_on date NOT NULL CHECK(issued_on>='1900-01-01'),expires_on date NOT NULL CHECK(expires_on>=issued_on),product_categories jsonb NOT NULL CHECK(supply_categories_valid(product_categories)),
 status text NOT NULL DEFAULT 'declared' CHECK(status='declared'),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,id),FOREIGN KEY(tenant_id,party_id,country_code) REFERENCES supply_parties(tenant_id,id,country_code) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,premise_id,country_code) REFERENCES supply_premises(tenant_id,party_id,id,country_code) ON DELETE RESTRICT
);
CREATE INDEX supply_licenses_premise ON supply_licenses(tenant_id,party_id,premise_id);
CREATE UNIQUE INDEX supply_license_identity ON supply_licenses(tenant_id,country_code,kind,lower(issuing_authority),lower(jurisdiction),number);

ALTER TABLE supply_premises ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_premises FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_premises USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']));
CREATE INDEX supply_premises_page ON supply_premises(tenant_id,party_id,created_at DESC,id DESC);
CREATE TRIGGER supply_premises_audit AFTER INSERT OR UPDATE ON supply_premises FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_premises_version_guard BEFORE UPDATE ON supply_premises FOR EACH ROW EXECUTE FUNCTION supply_registry_version_guard();
CREATE TRIGGER supply_premises_retain BEFORE DELETE OR TRUNCATE ON supply_premises FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_premise_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id,premise_id) REFERENCES supply_premises(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_premises ADD CONSTRAINT supply_premises_revision_head FOREIGN KEY(tenant_id,party_id,id,version) REFERENCES supply_premise_revisions(tenant_id,party_id,premise_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_premise_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_premise_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_premise_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']));
CREATE INDEX supply_premise_revisions_page ON supply_premise_revisions(tenant_id,party_id,premise_id,created_at DESC,id DESC);
CREATE TRIGGER supply_premise_revisions_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_premise_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();

ALTER TABLE supply_licenses ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_licenses FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_licenses USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance']));
CREATE INDEX supply_licenses_page ON supply_licenses(tenant_id,party_id,created_at DESC,id DESC);
CREATE TRIGGER supply_licenses_audit AFTER INSERT OR UPDATE ON supply_licenses FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_licenses_version_guard BEFORE UPDATE ON supply_licenses FOR EACH ROW EXECUTE FUNCTION supply_registry_version_guard();
CREATE TRIGGER supply_licenses_retain BEFORE DELETE OR TRUNCATE ON supply_licenses FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_license_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,license_id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,license_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id,license_id) REFERENCES supply_licenses(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_licenses ADD CONSTRAINT supply_licenses_revision_head FOREIGN KEY(tenant_id,party_id,id,version) REFERENCES supply_license_revisions(tenant_id,party_id,license_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_license_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_license_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_license_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance']));
CREATE INDEX supply_license_revisions_page ON supply_license_revisions(tenant_id,party_id,license_id,created_at DESC,id DESC);
CREATE TRIGGER supply_license_revisions_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_license_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
