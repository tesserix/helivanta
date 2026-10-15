SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pharmacy_organizations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'), tenant_id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
 ownership_type text NOT NULL CHECK (ownership_type IN ('independent','chain','hospital_owned')),
 onboarding_status text NOT NULL DEFAULT 'draft' CHECK (onboarding_status='draft'),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id), UNIQUE (tenant_id,code)
);
ALTER TABLE pharmacy_organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_organizations USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX pharmacy_organization_page ON pharmacy_organizations (tenant_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_organization_audit AFTER INSERT ON pharmacy_organizations FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE pharmacy_outlets (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,pharmacy_id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
 country_code text NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),timezone text NOT NULL CHECK (length(timezone) BETWEEN 1 AND 64),
 regulatory_status text NOT NULL DEFAULT 'pending_review' CHECK (regulatory_status='pending_review'),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY (tenant_id,id),UNIQUE (tenant_id,pharmacy_id,code),
 FOREIGN KEY (tenant_id,pharmacy_id) REFERENCES pharmacy_organizations (tenant_id,id)
);
ALTER TABLE pharmacy_outlets ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlets USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX pharmacy_outlet_page ON pharmacy_outlets (tenant_id,pharmacy_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_outlet_audit AFTER INSERT ON pharmacy_outlets FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE pharmacy_outlet_access (
 id uuid NOT NULL CHECK (substring(id::text,15,1) = '7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL CHECK (outlet_id <> '00000000-0000-0000-0000-000000000000'),
 subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
 level text NOT NULL CHECK (level IN ('reader','editor')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,subject),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_outlet_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlet_access
 USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TRIGGER pharmacy_outlet_access_audit AFTER INSERT OR UPDATE OR DELETE ON pharmacy_outlet_access
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION pharmacy_outlet_allowed(row_tenant uuid,row_outlet uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant = NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_outlet = NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
 AND EXISTS (SELECT 1 FROM pharmacy_outlet_access a JOIN pharmacy_outlets o ON o.tenant_id=a.tenant_id AND o.id=a.outlet_id WHERE a.tenant_id=row_tenant
 AND o.pharmacy_id=NULLIF(current_setting('app.pharmacy_id',true),'')::uuid
 AND a.outlet_id=row_outlet AND a.subject=NULLIF(current_setting('app.pharmacy_subject',true),'')
 AND (NOT writing OR a.level='editor'))
$$;

CREATE TABLE pharmacy_catalogue_products (
 id uuid NOT NULL CHECK (substring(id::text,15,1) = '7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL CHECK (outlet_id <> '00000000-0000-0000-0000-000000000000'),
 sku text NOT NULL CHECK (sku ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),
 brand_name text NOT NULL CHECK (length(btrim(brand_name)) BETWEEN 1 AND 200),
 generic_name text NOT NULL CHECK (length(btrim(generic_name)) BETWEEN 1 AND 200),
 dosage_form text NOT NULL CHECK (length(btrim(dosage_form)) BETWEEN 1 AND 80),
 route text NOT NULL CHECK (length(btrim(route)) BETWEEN 1 AND 80),
 manufacturer text NOT NULL CHECK (length(btrim(manufacturer)) BETWEEN 1 AND 200),
 marketing_company text NOT NULL CHECK (length(marketing_company) <= 200),
 ingredients jsonb NOT NULL CHECK (jsonb_typeof(ingredients)='array' AND jsonb_array_length(ingredients) BETWEEN 1 AND 20),
 pack_size integer NOT NULL CHECK (pack_size BETWEEN 1 AND 1000000),
 unit_of_issue text NOT NULL CHECK (length(btrim(unit_of_issue)) BETWEEN 1 AND 40),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 mrp_minor bigint NOT NULL CHECK (mrp_minor BETWEEN 0 AND 1000000000000),
 status text NOT NULL CHECK (status IN ('active','discontinued','withdrawn')),
 regulatory_status text NOT NULL DEFAULT 'pending_review' CHECK (regulatory_status = 'pending_review'),
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,sku),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_catalogue_products ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_catalogue_products FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_catalogue_products
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_catalogue_page ON pharmacy_catalogue_products (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_catalogue_brand ON pharmacy_catalogue_products (tenant_id,outlet_id,lower(brand_name) text_pattern_ops);
CREATE INDEX pharmacy_catalogue_generic ON pharmacy_catalogue_products (tenant_id,outlet_id,lower(generic_name) text_pattern_ops);
CREATE TRIGGER pharmacy_catalogue_audit AFTER INSERT OR UPDATE ON pharmacy_catalogue_products
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE pharmacy_catalogue_revisions (
 id uuid NOT NULL CHECK (substring(id::text,15,1) = '7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 product_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,product_id,version),
 UNIQUE (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_catalogue_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_catalogue_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_catalogue_revisions
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_catalogue_revision_page ON pharmacy_catalogue_revisions (tenant_id,outlet_id,product_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_catalogue_revision_audit AFTER INSERT ON pharmacy_catalogue_revisions
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION pharmacy_catalogue_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'pharmacy catalogue history cannot be removed or changed' USING ERRCODE='23514';
END $$;
CREATE TRIGGER pharmacy_catalogue_no_delete BEFORE DELETE OR TRUNCATE ON pharmacy_catalogue_products
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_catalogue_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_catalogue_revisions
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_catalogue_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='withdrawn' OR NEW.version<>OLD.version+1
 OR (NEW.id,NEW.tenant_id,NEW.outlet_id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.outlet_id,OLD.created_at) THEN
  RAISE EXCEPTION 'invalid pharmacy catalogue revision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_catalogue_version_guard BEFORE UPDATE ON pharmacy_catalogue_products
 FOR EACH ROW EXECUTE FUNCTION pharmacy_catalogue_version_guard();
ALTER TABLE pharmacy_catalogue_revisions ADD CONSTRAINT pharmacy_revision_snapshot_identity CHECK (
 snapshot->>'id'=product_id::text AND (snapshot->>'version')::bigint=version
 AND snapshot->>'outlet_id'=outlet_id::text
);

CREATE TRIGGER pharmacy_organization_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_organizations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_outlet_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlets FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_hospital_affiliations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,pharmacy_id uuid NOT NULL,hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'),
 relationship text NOT NULL CHECK (relationship IN ('owner','serving','partner')),
 verification_status text NOT NULL DEFAULT 'pending_validation' CHECK (verification_status='pending_validation'),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY (tenant_id,id),UNIQUE (tenant_id,pharmacy_id,hospital_id,relationship),
 FOREIGN KEY (tenant_id,pharmacy_id) REFERENCES pharmacy_organizations (tenant_id,id)
);
ALTER TABLE pharmacy_hospital_affiliations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_hospital_affiliations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_hospital_affiliations USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX pharmacy_affiliation_page ON pharmacy_hospital_affiliations (tenant_id,pharmacy_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_affiliation_audit AFTER INSERT ON pharmacy_hospital_affiliations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_affiliation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_hospital_affiliations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
