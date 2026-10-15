SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_parties (
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),
 record_kind text NOT NULL CHECK(record_kind IN ('owned_organisation','counterparty')),
 legal_name text NOT NULL CHECK(length(btrim(legal_name)) BETWEEN 1 AND 200),
 trading_name text NOT NULL CHECK(length(trading_name)<=200),
 business_kind text NOT NULL CHECK(business_kind IN ('pharmacy','distributor','wholesaler','manufacturer','importer','other')),
 country_code text NOT NULL CHECK(country_code IN ('IN','PK')),
 jurisdiction text NOT NULL CHECK(length(btrim(jurisdiction)) BETWEEN 1 AND 100),
 registration_number text NOT NULL CHECK(length(btrim(registration_number)) BETWEEN 1 AND 100),
 corporate_group text NOT NULL CHECK(length(corporate_group)<=200),
 website text NOT NULL CHECK(length(website)<=500),
 default_credit_days integer NOT NULL CHECK(default_credit_days BETWEEN 0 AND 365),
 onboarding_status text NOT NULL DEFAULT 'draft' CHECK(onboarding_status='draft'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,code),
 UNIQUE(tenant_id,country_code,jurisdiction,registration_number),UNIQUE(tenant_id,id,country_code)
);
CREATE TABLE supply_party_access (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 role text NOT NULL CHECK(role IN ('administrator','purchasing','finance','compliance','auditor','warehouse','sales','driver')),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,party_id,subject,role),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE supply_party_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_party_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_party_access USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TRIGGER supply_party_access_audit AFTER INSERT OR UPDATE OR DELETE ON supply_party_access FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_party_allowed(row_tenant uuid,row_party uuid,writing boolean) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_party=NULLIF(current_setting('app.supply_party',true),'')::uuid
 AND EXISTS(SELECT 1 FROM supply_party_access a WHERE a.tenant_id=row_tenant AND a.party_id=row_party
 AND a.subject=NULLIF(current_setting('app.supply_subject',true),'')
 AND CASE WHEN writing THEN a.role IN ('administrator','purchasing') ELSE a.role IN ('administrator','purchasing','finance','compliance','auditor') END)
$$;
ALTER TABLE supply_parties ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_parties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_parties USING(
 hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND
 (supply_party_allowed(tenant_id,id,false) OR
 (current_setting('app.supply_directory',true)='on' AND (current_setting('app.supply_manager',true)='on' OR EXISTS(
 SELECT 1 FROM supply_party_access a WHERE a.tenant_id=supply_parties.tenant_id AND a.party_id=supply_parties.id AND a.subject=NULLIF(current_setting('app.supply_subject',true),''))))))
 WITH CHECK(supply_party_allowed(tenant_id,id,true));
CREATE INDEX supply_party_page ON supply_parties(tenant_id,created_at DESC,id DESC);
CREATE INDEX supply_party_access_subject ON supply_party_access(tenant_id,subject,party_id);
CREATE TABLE supply_party_revisions (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,version bigint NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,version),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_parties ADD CONSTRAINT supply_current_revision FOREIGN KEY(tenant_id,id,version) REFERENCES supply_party_revisions(tenant_id,party_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_party_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_party_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_party_revisions USING(hms_tenant_visible(tenant_id) AND supply_party_allowed(tenant_id,party_id,false)) WITH CHECK(supply_party_allowed(tenant_id,party_id,true));
CREATE INDEX supply_party_revision_page ON supply_party_revisions(tenant_id,party_id,created_at DESC,id DESC);
CREATE FUNCTION supply_history_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'supply history is immutable' USING ERRCODE='23514',CONSTRAINT='supply_history_immutable'; END $$;
CREATE TRIGGER supply_party_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_party_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_party_history_required BEFORE DELETE OR TRUNCATE ON supply_parties FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_party_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.id,NEW.record_kind,NEW.country_code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.record_kind,OLD.country_code,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
 RAISE EXCEPTION 'party identity or version is immutable' USING ERRCODE='23514',CONSTRAINT='supply_party_identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_party_version_guard BEFORE UPDATE ON supply_parties FOR EACH ROW EXECUTE FUNCTION supply_party_version_guard();
CREATE TRIGGER supply_party_audit AFTER INSERT OR UPDATE ON supply_parties FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE supply_party_contacts (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),position integer NOT NULL CHECK(position BETWEEN 0 AND 19),
 name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 200),role text NOT NULL CHECK(role IN ('general','purchasing','finance','pharmacist','signatory','emergency')),email text NOT NULL CHECK(length(email)<=254),phone text NOT NULL CHECK(phone ~ '^(|[+][1-9][0-9]{7,14})$'),alternate_phone text NOT NULL CHECK(alternate_phone ~ '^(|[+][1-9][0-9]{7,14})$'),PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,party_id,position) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT
 );
 ALTER TABLE supply_party_contacts ENABLE ROW LEVEL SECURITY;
 ALTER TABLE supply_party_contacts FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON supply_party_contacts USING(hms_tenant_visible(tenant_id) AND supply_party_allowed(tenant_id,party_id,false)) WITH CHECK(supply_party_allowed(tenant_id,party_id,true));
 CREATE TRIGGER supply_party_contacts_audit AFTER INSERT OR UPDATE OR DELETE ON supply_party_contacts FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE supply_party_addresses (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),position integer NOT NULL CHECK(position BETWEEN 0 AND 19),
 kind text NOT NULL CHECK(kind IN ('registered','correspondence','billing')),line1 text NOT NULL CHECK(length(btrim(line1)) BETWEEN 1 AND 200),line2 text NOT NULL CHECK(length(line2)<=200),city text NOT NULL CHECK(length(btrim(city)) BETWEEN 1 AND 100),region text NOT NULL CHECK(length(btrim(region)) BETWEEN 1 AND 100),postal_code text NOT NULL CHECK(length(btrim(postal_code)) BETWEEN 1 AND 20),country_code text NOT NULL CHECK(country_code IN ('IN','PK')),PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,party_id,position) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT
 );
 ALTER TABLE supply_party_addresses ENABLE ROW LEVEL SECURITY;
 ALTER TABLE supply_party_addresses FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON supply_party_addresses USING(hms_tenant_visible(tenant_id) AND supply_party_allowed(tenant_id,party_id,false)) WITH CHECK(supply_party_allowed(tenant_id,party_id,true));
 CREATE TRIGGER supply_party_addresses_audit AFTER INSERT OR UPDATE OR DELETE ON supply_party_addresses FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE supply_party_tax_registrations (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),position integer NOT NULL CHECK(position BETWEEN 0 AND 19),
 kind text NOT NULL CHECK(kind IN ('gstin','pan','ntn','stn','other')),number text NOT NULL CHECK(length(btrim(number)) BETWEEN 1 AND 100),jurisdiction text NOT NULL CHECK(length(btrim(jurisdiction)) BETWEEN 1 AND 100),country_code text NOT NULL CHECK((country_code='IN' AND kind IN ('gstin','pan','other')) OR (country_code='PK' AND kind IN ('ntn','stn','other'))),PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,party_id,position) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT,UNIQUE(tenant_id,country_code,kind,number),FOREIGN KEY(tenant_id,party_id,country_code) REFERENCES supply_parties(tenant_id,id,country_code) ON DELETE RESTRICT
 );
 ALTER TABLE supply_party_tax_registrations ENABLE ROW LEVEL SECURITY;
 ALTER TABLE supply_party_tax_registrations FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON supply_party_tax_registrations USING(hms_tenant_visible(tenant_id) AND supply_party_allowed(tenant_id,party_id,false)) WITH CHECK(supply_party_allowed(tenant_id,party_id,true));
 CREATE TRIGGER supply_party_tax_registrations_audit AFTER INSERT OR UPDATE OR DELETE ON supply_party_tax_registrations FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE INDEX supply_party_code_prefix ON supply_parties(tenant_id,lower(code) text_pattern_ops);

CREATE INDEX supply_party_legal_name_prefix ON supply_parties(tenant_id,lower(legal_name) text_pattern_ops);

CREATE INDEX supply_party_trading_name_prefix ON supply_parties(tenant_id,lower(trading_name) text_pattern_ops);
