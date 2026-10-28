SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Tax setup (#1283). Both tables are append-only and effective-dated: the
-- row in force on a date is the latest effective_from on or before it, and
-- the highest seq among equal dates, so a same-day mistake is corrected by
-- recording again. A correction must say why.

CREATE TABLE pharmacy_outlet_tax_registrations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 jurisdiction_code text NOT NULL CHECK (jurisdiction_code ~ '^[0-9]{2}$'),
 identifier_kind text NOT NULL CHECK (identifier_kind ~ '^[a-z][a-z_]{0,39}$'),
 identifier text NOT NULL CHECK (identifier ~ '^[0-9A-Z]{1,40}$'),
 effective_from date NOT NULL,
 reason text CHECK (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500),
 recorded_by text NOT NULL CHECK (length(btrim(recorded_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),UNIQUE (seq),
 CHECK (identifier_kind<>'gstin' OR substring(identifier,1,2)=jurisdiction_code),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE pharmacy_outlet_tax_registrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_tax_registrations FORCE ROW LEVEL SECURITY;
-- Outlet administration: tenant isolation, as pharmacy_outlets; the handler
-- scopes to visible pharmacies (spec D2).
CREATE POLICY tenant_isolation ON pharmacy_outlet_tax_registrations USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX pharmacy_tax_registration_in_force ON pharmacy_outlet_tax_registrations (tenant_id,outlet_id,effective_from DESC,seq DESC);
CREATE TRIGGER pharmacy_tax_registration_audit AFTER INSERT ON pharmacy_outlet_tax_registrations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_tax_registration_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlet_tax_registrations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_product_tax_classifications (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,product_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 classification_code text NOT NULL CHECK (classification_code ~ '^[0-9A-Z]{1,20}$'),
 supply_nature text NOT NULL CHECK (supply_nature IN ('taxable','nil_rated','exempt')),
 rate_ppm bigint NOT NULL CHECK (rate_ppm % 100 = 0),
 exemption_basis text CHECK (exemption_basis IS NULL OR length(btrim(exemption_basis)) BETWEEN 1 AND 500),
 effective_from date NOT NULL,
 reason text CHECK (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500),
 recorded_by text NOT NULL CHECK (length(btrim(recorded_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),UNIQUE (seq),
 CHECK (CASE supply_nature WHEN 'taxable' THEN rate_ppm BETWEEN 100 AND 1000000 ELSE rate_ppm = 0 END),
 -- An exemption without a recorded basis cannot be defended (IN-RC-10).
 CHECK ((supply_nature='exempt') = (exemption_basis IS NOT NULL)),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id) ON DELETE RESTRICT
);
ALTER TABLE pharmacy_product_tax_classifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_tax_classifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_tax_classifications USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false)) WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_tax_classification_in_force ON pharmacy_product_tax_classifications (tenant_id,outlet_id,product_id,effective_from DESC,seq DESC);
CREATE TRIGGER pharmacy_tax_classification_audit AFTER INSERT ON pharmacy_product_tax_classifications FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_tax_classification_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_tax_classifications FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- A row sharing its effective_from with an earlier one replaces it, so it
-- must carry a reason. The advisory lock serialises writers per scope, so
-- two unexplained rows for one date cannot both pass the check, and seq is
-- drawn under it so it follows the order rows are recorded.
CREATE FUNCTION pharmacy_tax_correction_reason() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE replaces boolean;
BEGIN
 -- Branches, not one expression: NEW has no product_id on registrations.
 IF TG_TABLE_NAME='pharmacy_product_tax_classifications' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_NAME||NEW.tenant_id::text||NEW.product_id::text,0));
  SELECT EXISTS (SELECT 1 FROM pharmacy_product_tax_classifications c WHERE c.tenant_id=NEW.tenant_id
   AND c.outlet_id=NEW.outlet_id AND c.product_id=NEW.product_id AND c.effective_from=NEW.effective_from) INTO replaces;
 ELSE
  PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_NAME||NEW.tenant_id::text||NEW.outlet_id::text,0));
  SELECT EXISTS (SELECT 1 FROM pharmacy_outlet_tax_registrations r WHERE r.tenant_id=NEW.tenant_id
   AND r.outlet_id=NEW.outlet_id AND r.effective_from=NEW.effective_from) INTO replaces;
 END IF;
 -- seq is the order of recording. The identity value was drawn before the
 -- lock, so a writer that waited could hold a lower seq than the row it
 -- waited behind; draw it again now that writers are serialised.
 NEW.seq := nextval(pg_get_serial_sequence(TG_TABLE_NAME,'seq'));
 IF replaces AND NEW.reason IS NULL THEN
  RAISE EXCEPTION 'a tax correction for the same date needs a reason' USING ERRCODE='23514', CONSTRAINT='pharmacy_tax_correction_reason';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_tax_registration_reason BEFORE INSERT ON pharmacy_outlet_tax_registrations FOR EACH ROW EXECUTE FUNCTION pharmacy_tax_correction_reason();
CREATE TRIGGER pharmacy_tax_classification_reason BEFORE INSERT ON pharmacy_product_tax_classifications FOR EACH ROW EXECUTE FUNCTION pharmacy_tax_correction_reason();
