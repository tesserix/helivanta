SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Service charges on pharmacy bills (#1388). An outlet's charge (delivery to
-- a ward, a counter handling fee) is versioned and each version is reviewed
-- by a second person. Its tax treatment is the tenant accountant's decision,
-- recorded with its basis: a service of its own with its SAC and rate, or
-- part of a composite supply taxed as the medicines (CGST Act s.8(a)). A
-- charge is its own invoice line and never touches a medicine line's price.

CREATE TABLE pharmacy_service_charges (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z][A-Z0-9_]{1,19}$'),
 created_by text NOT NULL CHECK (length(btrim(created_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 CONSTRAINT pharmacy_service_charge_code UNIQUE (tenant_id,outlet_id,code),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_service_charges ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_service_charges FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_service_charges
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND created_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_service_charge_page ON pharmacy_service_charges (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_service_charge_audit AFTER INSERT ON pharmacy_service_charges
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_service_charge_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_service_charges
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
-- At most 50 charges an outlet, so the counter reads them all in one bounded
-- list. The outlet lock makes the count exact under concurrent creation.
CREATE FUNCTION pharmacy_service_charge_limit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':pharmacy-service-charges:'||NEW.outlet_id::text, 0));
 -- A retried creation conflicts on its id and inserts nothing, so it is not counted.
 IF NOT EXISTS (SELECT 1 FROM pharmacy_service_charges WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.id)
    AND (SELECT count(*) FROM pharmacy_service_charges WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id) >= 50 THEN
  RAISE EXCEPTION 'an outlet has at most 50 service charges' USING ERRCODE='23514',CONSTRAINT='pharmacy_service_charge_limit';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_service_charge_limit BEFORE INSERT ON pharmacy_service_charges
 FOR EACH ROW EXECUTE FUNCTION pharmacy_service_charge_limit();

CREATE TABLE pharmacy_service_charge_versions (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 charge_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
 channel text NOT NULL CHECK (channel IN ('counter','ward_delivery')),
 -- What the buyer pays, tax inside it, as for a medicine line.
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 100000000),
 available boolean NOT NULL,
 tax_treatment text NOT NULL CHECK (tax_treatment IN ('own_service','principal_supply')),
 sac_code text CHECK (sac_code ~ '^99[0-9]{4}$'),
 supply_nature text CHECK (supply_nature IN ('taxable','nil_rated','exempt')),
 rate_ppm bigint CHECK (rate_ppm % 100 = 0),
 exemption_basis text CHECK (length(exemption_basis) BETWEEN 1 AND 500 AND exemption_basis=btrim(exemption_basis) AND exemption_basis !~ '[[:cntrl:]]'),
 -- Who decided the treatment and on what authority (IN-RC-10).
 tax_basis text NOT NULL CHECK (length(tax_basis) BETWEEN 5 AND 500 AND tax_basis=btrim(tax_basis) AND tax_basis !~ '[[:cntrl:]]'),
 effective_from date NOT NULL,
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 declared_by text NOT NULL CHECK (length(btrim(declared_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (seq),
 FOREIGN KEY (tenant_id,outlet_id,charge_id) REFERENCES pharmacy_service_charges (tenant_id,outlet_id,id),
 -- A principal-supply charge takes its tax from the medicines, so it carries
 -- none of its own; a service of its own carries all of it.
 CONSTRAINT pharmacy_service_charge_version_form CHECK (
  (tax_treatment='principal_supply' AND num_nonnulls(sac_code,supply_nature,rate_ppm,exemption_basis)=0)
  OR (tax_treatment='own_service' AND num_nonnulls(sac_code,supply_nature,rate_ppm)=3
   AND (CASE supply_nature WHEN 'taxable' THEN rate_ppm BETWEEN 100 AND 1000000 ELSE rate_ppm=0 END)
   AND (supply_nature='exempt') = (exemption_basis IS NOT NULL)))
);
ALTER TABLE pharmacy_service_charge_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_service_charge_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_service_charge_versions
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND declared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_service_charge_version_current ON pharmacy_service_charge_versions (tenant_id,outlet_id,charge_id,seq DESC);
CREATE TRIGGER pharmacy_service_charge_version_audit AFTER INSERT ON pharmacy_service_charge_versions
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_service_charge_version_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_service_charge_versions
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_service_charge_reviews (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 version_id uuid NOT NULL,
 decision text NOT NULL CHECK (decision IN ('approved','rejected')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 reviewed_by text NOT NULL CHECK (length(btrim(reviewed_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,version_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,version_id) REFERENCES pharmacy_service_charge_versions (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_service_charge_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_service_charge_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_service_charge_reviews
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND reviewed_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_service_charge_review_audit AFTER INSERT ON pharmacy_service_charge_reviews
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_service_charge_review_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_service_charge_reviews
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- Versions and reviews of one charge take one lock, so a review and a newer
-- version never both pass the "latest" check below.
CREATE FUNCTION pharmacy_service_charge_lock(tenant uuid, outlet uuid, charge uuid) RETURNS void LANGUAGE sql AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(tenant::text||':pharmacy-service-charge:'||outlet::text||':'||charge::text, 0))
$$;
CREATE FUNCTION pharmacy_service_charge_version_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pharmacy_service_charge_lock(NEW.tenant_id,NEW.outlet_id,NEW.charge_id);
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_service_charge_version_lock BEFORE INSERT ON pharmacy_service_charge_versions
 FOR EACH ROW EXECUTE FUNCTION pharmacy_service_charge_version_lock();

-- Reviewed by someone other than the declarer, and only while it is the
-- charge's latest version: a replaced one keeps its state.
CREATE FUNCTION pharmacy_service_charge_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ver pharmacy_service_charge_versions;
BEGIN
 SELECT * INTO ver FROM pharmacy_service_charge_versions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.version_id;
 IF FOUND THEN
  PERFORM pharmacy_service_charge_lock(ver.tenant_id,ver.outlet_id,ver.charge_id);
  IF EXISTS (SELECT 1 FROM pharmacy_service_charge_versions n WHERE n.tenant_id=ver.tenant_id AND n.outlet_id=ver.outlet_id
     AND n.charge_id=ver.charge_id AND n.seq>ver.seq) THEN
   RAISE EXCEPTION 'a newer version replaced this one' USING ERRCODE='23514',CONSTRAINT='pharmacy_service_charge_review_latest';
  END IF;
  IF lower(btrim(ver.declared_by))=lower(btrim(NEW.reviewed_by)) THEN
   RAISE EXCEPTION 'a service charge is reviewed by someone other than who declared it' USING ERRCODE='23514',CONSTRAINT='pharmacy_service_charge_four_eyes';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_service_charge_review_guard BEFORE INSERT ON pharmacy_service_charge_reviews
 FOR EACH ROW EXECUTE FUNCTION pharmacy_service_charge_review_guard();

-- The version a charge is billed under on a day: the approved version with
-- the latest start on or before the day that no newer approved version
-- starting on or before its own replaced (the #1371 rule). None while the
-- latest version awaits review or was rejected: the counter never charges
-- terms a second person has not seen, and a withdrawal stops it at once.
CREATE FUNCTION pharmacy_service_charge_in_force(tenant uuid, outlet uuid, charge uuid, day date)
RETURNS TABLE (version_id uuid, available boolean) LANGUAGE plpgsql STABLE AS $$
DECLARE latest_approved boolean;
BEGIN
 SELECT r.decision='approved' INTO latest_approved FROM (
  SELECT * FROM pharmacy_service_charge_versions WHERE tenant_id=tenant AND outlet_id=outlet AND charge_id=charge ORDER BY seq DESC LIMIT 1) v
  LEFT JOIN pharmacy_service_charge_reviews r ON r.tenant_id=v.tenant_id AND r.outlet_id=v.outlet_id AND r.version_id=v.id;
 IF NOT COALESCE(latest_approved,false) THEN
  RETURN;
 END IF;
 RETURN QUERY SELECT v.id, v.available FROM pharmacy_service_charge_versions v JOIN pharmacy_service_charge_reviews r
  ON r.tenant_id=v.tenant_id AND r.outlet_id=v.outlet_id AND r.version_id=v.id
  WHERE v.tenant_id=tenant AND v.outlet_id=outlet AND v.charge_id=charge AND r.decision='approved' AND v.effective_from<=day
   AND NOT EXISTS (SELECT 1 FROM pharmacy_service_charge_versions n JOIN pharmacy_service_charge_reviews nr
    ON nr.tenant_id=n.tenant_id AND nr.outlet_id=n.outlet_id AND nr.version_id=n.id AND nr.decision='approved'
    WHERE n.tenant_id=v.tenant_id AND n.outlet_id=v.outlet_id AND n.charge_id=v.charge_id AND n.seq>v.seq AND n.effective_from<=v.effective_from)
  ORDER BY v.effective_from DESC, v.seq DESC LIMIT 1;
END $$;

-- A charge on an invoice: its own line, copied from the version in force, so
-- a later version never alters an issued invoice.
CREATE TABLE pharmacy_sale_charge_lines (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 sale_id uuid NOT NULL,
 charge_number integer NOT NULL CHECK (charge_number BETWEEN 1 AND 5),
 charge_id uuid NOT NULL,
 version_id uuid NOT NULL,
 code text NOT NULL,
 name text NOT NULL,
 tax_treatment text NOT NULL CHECK (tax_treatment IN ('own_service','principal_supply')),
 sac_code text,
 supply_nature text NOT NULL CHECK (supply_nature IN ('taxable','nil_rated','exempt')),
 rate_ppm bigint NOT NULL CHECK (rate_ppm BETWEEN 0 AND 1000000),
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 PRIMARY KEY (tenant_id,outlet_id,sale_id,charge_number),
 UNIQUE (tenant_id,id),
 CONSTRAINT pharmacy_sale_charge_once UNIQUE (tenant_id,outlet_id,sale_id,charge_id),
 FOREIGN KEY (tenant_id,outlet_id,sale_id) REFERENCES pharmacy_sales (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,charge_id) REFERENCES pharmacy_service_charges (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,version_id) REFERENCES pharmacy_service_charge_versions (tenant_id,outlet_id,id),
 CHECK (amount_minor = taxable_minor + cgst_minor + sgst_minor),
 CHECK (cgst_minor = sgst_minor),
 CHECK ((tax_treatment='own_service') = (sac_code IS NOT NULL)),
 -- The counter's tax-inside split, halves rounded half up (spec B4 of #1386).
 CONSTRAINT pharmacy_sale_charge_tax CHECK (CASE WHEN supply_nature='taxable'
  THEN rate_ppm >= 100 AND cgst_minor = div(2*rate_ppm::numeric*amount_minor + 2*(1000000+rate_ppm), 4*(1000000+rate_ppm::numeric))
  ELSE rate_ppm = 0 AND cgst_minor = 0 END)
);
ALTER TABLE pharmacy_sale_charge_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_sale_charge_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_sale_charge_lines
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_sale_charge_line_audit AFTER INSERT ON pharmacy_sale_charge_lines
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_sale_charge_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_sale_charge_lines
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_sale_charge_line_sealed BEFORE INSERT ON pharmacy_sale_charge_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_sealed();

-- A charge line is the version in force on the sale date, available, for the
-- sale's channel (billed to an admission is ward delivery), with every value
-- the version's. A handler bug cannot bill a pending, withdrawn or edited charge.
CREATE FUNCTION pharmacy_sale_charge_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sold record; f record; ver pharmacy_service_charge_versions; code text;
BEGIN
 SELECT s.sale_date, s.admission_id INTO sold FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.sale_id;
 SELECT * INTO f FROM pharmacy_service_charge_in_force(NEW.tenant_id,NEW.outlet_id,NEW.charge_id,sold.sale_date);
 SELECT * INTO ver FROM pharmacy_service_charge_versions v WHERE v.tenant_id=NEW.tenant_id AND v.outlet_id=NEW.outlet_id AND v.id=NEW.version_id AND v.charge_id=NEW.charge_id;
 SELECT c.code INTO code FROM pharmacy_service_charges c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.id=NEW.charge_id;
 IF f.version_id IS DISTINCT FROM NEW.version_id OR NOT COALESCE(f.available,false)
    OR ver.channel IS DISTINCT FROM (CASE WHEN sold.admission_id IS NULL THEN 'counter' ELSE 'ward_delivery' END)
    OR (NEW.code,NEW.name,NEW.tax_treatment,NEW.amount_minor) IS DISTINCT FROM (code,ver.name,ver.tax_treatment,ver.amount_minor)
    OR (ver.tax_treatment='own_service' AND (NEW.sac_code,NEW.supply_nature,NEW.rate_ppm) IS DISTINCT FROM (ver.sac_code,ver.supply_nature,ver.rate_ppm)) THEN
  RAISE EXCEPTION 'a service charge line is the version in force for the sale''s channel' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_charge_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_charge_line_guard BEFORE INSERT ON pharmacy_sale_charge_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_charge_line_guard();

-- At commit, a principal-supply charge is taxed as every medicine on the
-- invoice: one supply nature and rate. Mixed rates have no rule yet (fail
-- closed until the accountant gives one).
CREATE FUNCTION pharmacy_sale_charge_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM pharmacy_sale_charge_lines c JOIN pharmacy_sale_lines l
    ON l.tenant_id=c.tenant_id AND l.outlet_id=c.outlet_id AND l.sale_id=c.sale_id
    WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.sale_id=NEW.id AND c.tax_treatment='principal_supply'
    AND (l.supply_nature,l.rate_ppm) IS DISTINCT FROM (c.supply_nature,c.rate_ppm)) THEN
  RAISE EXCEPTION 'a charge taxed as the medicines needs them all at one rate' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_charge_principal';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_sale_charge_principal AFTER INSERT ON pharmacy_sales
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_charge_principal();

-- An invoice is its medicine lines and its charge lines, paid by its
-- tenders, with each medicine line's stock moved. It still needs a medicine:
-- a charge alone is not a sale.
CREATE OR REPLACE FUNCTION pharmacy_sale_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; charges record; paid bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO lines
  FROM pharmacy_sale_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO charges
  FROM pharmacy_sale_charge_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO paid FROM pharmacy_sale_tenders
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 -- Equality on source_reference, so the unique index serves it.
 SELECT count(*) INTO moved FROM pharmacy_sale_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='sale:'||l.sale_id||':'||l.line_number AND m.kind='sale'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.sale_id=NEW.id;
 IF lines.n = 0 OR lines.amount + charges.amount <> NEW.total_minor OR lines.taxable + charges.taxable <> NEW.taxable_minor
    OR lines.cgst + charges.cgst <> NEW.cgst_minor OR lines.sgst + charges.sgst <> NEW.sgst_minor
    OR paid <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a sale is its lines, paid by its tenders, with its stock moved' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_balanced';
 END IF;
 RETURN NULL;
END $$;

-- A credited charge reverses its sale's charge line whole, once: a service
-- is not returned in part, and no stock moves for it.
CREATE TABLE pharmacy_credit_note_charge_lines (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 credit_note_id uuid NOT NULL,
 charge_number integer NOT NULL CHECK (charge_number BETWEEN 1 AND 5),
 sale_id uuid NOT NULL,
 sale_charge integer NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 PRIMARY KEY (tenant_id,outlet_id,credit_note_id,charge_number),
 UNIQUE (tenant_id,id),
 CONSTRAINT pharmacy_credit_note_charge_once UNIQUE (tenant_id,outlet_id,sale_id,sale_charge),
 FOREIGN KEY (tenant_id,outlet_id,credit_note_id) REFERENCES pharmacy_credit_notes (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,sale_id,sale_charge) REFERENCES pharmacy_sale_charge_lines (tenant_id,outlet_id,sale_id,charge_number),
 CHECK (amount_minor = taxable_minor + cgst_minor + sgst_minor),
 CHECK (cgst_minor = sgst_minor)
);
ALTER TABLE pharmacy_credit_note_charge_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_credit_note_charge_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_credit_note_charge_lines
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_credit_note_charge_line_audit AFTER INSERT ON pharmacy_credit_note_charge_lines
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_credit_note_charge_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_credit_note_charge_lines
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_credit_note_charge_line_sealed BEFORE INSERT ON pharmacy_credit_note_charge_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_sealed();

CREATE FUNCTION pharmacy_credit_note_charge_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_sale_charge_lines c JOIN pharmacy_credit_notes n
    ON n.tenant_id=c.tenant_id AND n.outlet_id=c.outlet_id AND n.id=NEW.credit_note_id AND n.sale_id=c.sale_id
    WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.sale_id=NEW.sale_id AND c.charge_number=NEW.sale_charge
    AND (c.amount_minor,c.taxable_minor,c.cgst_minor,c.sgst_minor) = (NEW.amount_minor,NEW.taxable_minor,NEW.cgst_minor,NEW.sgst_minor)) THEN
  RAISE EXCEPTION 'a credited charge reverses a charge of its credit note''s sale, whole' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_charge_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_credit_note_charge_line_guard BEFORE INSERT ON pharmacy_credit_note_charge_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_charge_line_guard();

-- A credit note is its returned lines and credited charges, at least one of
-- them, refunded in full, with each returned line's stock received.
CREATE OR REPLACE FUNCTION pharmacy_credit_note_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; charges record; refunded bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO lines
  FROM pharmacy_credit_note_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO charges
  FROM pharmacy_credit_note_charge_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO refunded FROM pharmacy_credit_note_refunds
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT count(*) INTO moved FROM pharmacy_credit_note_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='return:'||l.credit_note_id||':'||l.line_number AND m.kind='return'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.credit_note_id=NEW.id;
 IF lines.n + charges.n = 0 OR lines.amount + charges.amount <> NEW.total_minor OR lines.taxable + charges.taxable <> NEW.taxable_minor
    OR lines.cgst + charges.cgst <> NEW.cgst_minor OR lines.sgst + charges.sgst <> NEW.sgst_minor
    OR refunded <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a credit note is its lines, refunded in full, with its stock received' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_balanced';
 END IF;
 RETURN NULL;
END $$;
