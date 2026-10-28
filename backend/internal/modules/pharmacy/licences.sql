SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Outlet drug licences (#1386 A1). An outlet sells nothing unless a licence
-- reviewed by someone other than the person who entered it is valid on the
-- outlet's own date. Insert-only: a wrong licence is rejected, and a renewed
-- one is entered as a new licence.
CREATE TABLE pharmacy_outlet_licences (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 licence_kind text NOT NULL CHECK (licence_kind IN ('retail','wholesale')),
 -- The form the licence was issued on, as printed; no list of forms is
 -- encoded, since that is the issuing authority's, not ours.
 form_label text NOT NULL CHECK (length(btrim(form_label)) BETWEEN 1 AND 40),
 licence_number text NOT NULL CHECK (length(btrim(licence_number)) BETWEEN 1 AND 100),
 issuing_authority text NOT NULL CHECK (length(btrim(issuing_authority)) BETWEEN 1 AND 200),
 valid_from date NOT NULL CHECK (valid_from >= '1900-01-01'),
 valid_until date NOT NULL,
 pharmacist_name text NOT NULL CHECK (length(btrim(pharmacist_name)) BETWEEN 1 AND 200),
 pharmacist_registration text NOT NULL CHECK (length(btrim(pharmacist_registration)) BETWEEN 1 AND 100),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 entered_by text NOT NULL CHECK (length(btrim(entered_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 CHECK (valid_until >= valid_from),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_outlet_licences ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_licences FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlet_licences
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND entered_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_outlet_licence_page ON pharmacy_outlet_licences (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_outlet_licence_audit AFTER INSERT ON pharmacy_outlet_licences
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_outlet_licence_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlet_licences
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_outlet_licence_reviews (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 licence_id uuid NOT NULL,
 decision text NOT NULL CHECK (decision IN ('approved','rejected')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 reviewed_by text NOT NULL CHECK (length(btrim(reviewed_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 -- One decision per licence: a rejected licence is entered again.
 PRIMARY KEY (tenant_id,outlet_id,licence_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,licence_id) REFERENCES pharmacy_outlet_licences (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_outlet_licence_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_licence_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlet_licence_reviews
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND reviewed_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_outlet_licence_review_audit AFTER INSERT ON pharmacy_outlet_licence_reviews
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_outlet_licence_review_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlet_licence_reviews
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- Four-eyes in the database, so it holds for any writer (spec decision 1).
CREATE FUNCTION pharmacy_outlet_licence_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM pharmacy_outlet_licences l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id
    AND l.id=NEW.licence_id AND lower(btrim(l.entered_by))=lower(btrim(NEW.reviewed_by))) THEN
  RAISE EXCEPTION 'a licence is reviewed by someone other than who entered it' USING ERRCODE='23514',CONSTRAINT='pharmacy_licence_four_eyes';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_outlet_licence_review_guard BEFORE INSERT ON pharmacy_outlet_licence_reviews
 FOR EACH ROW EXECUTE FUNCTION pharmacy_outlet_licence_review_guard();

-- Whether the outlet holds an approved licence valid on day. Later slices
-- call it inside their own transaction, so the sale and the check agree.
CREATE FUNCTION pharmacy_outlet_licence_in_force(tenant uuid, outlet uuid, day date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM pharmacy_outlet_licences l JOIN pharmacy_outlet_licence_reviews r
  ON r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id AND r.licence_id=l.id
  WHERE l.tenant_id=tenant AND l.outlet_id=outlet AND r.decision='approved' AND day BETWEEN l.valid_from AND l.valid_until)
$$;
