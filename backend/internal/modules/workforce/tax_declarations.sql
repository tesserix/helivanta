SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_staff_tax_declarations (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 tax_year text NOT NULL CHECK (workforce_valid_tax_year(tax_year)),
 version bigint NOT NULL CHECK (version >= 0), revision_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((version = 0) = (revision_id IS NULL)),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,tax_year),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

CREATE TABLE workforce_staff_tax_declaration_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL, tax_year text NOT NULL,
 sequence integer NOT NULL CHECK (sequence > 0), base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 hra_metro boolean,
 home_loan_interest_paise bigint CHECK (home_loan_interest_paise BETWEEN 1 AND 1000000000),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_tax_declaration_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_tax_declaration_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_tax_declaration_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,tax_year,sequence), UNIQUE(tenant_id,hospital_id,staff_id,tax_year,approved_version),
 UNIQUE(tenant_id,hospital_id,staff_id,tax_year,approved_version,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,tax_year) REFERENCES workforce_staff_tax_declarations(tenant_id,hospital_id,staff_id,tax_year) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_tax_declaration_one_pending ON workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,staff_id,tax_year) WHERE status = 'pending';
CREATE INDEX workforce_tax_declaration_history ON workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,staff_id,tax_year,created_at DESC,id DESC);
ALTER TABLE workforce_staff_tax_declarations ADD CONSTRAINT workforce_tax_declaration_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,tax_year,version,revision_id) REFERENCES workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,staff_id,tax_year,approved_version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_staff_tax_declaration_claims (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 position smallint NOT NULL CHECK (position BETWEEN 1 AND 24),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 amount_paise bigint NOT NULL CHECK (amount_paise BETWEEN 1 AND 100000000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 CONSTRAINT workforce_tax_declaration_claim_code UNIQUE(tenant_id,hospital_id,revision_id,code),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

CREATE TABLE workforce_staff_tax_declaration_parties (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 role text NOT NULL CHECK (role IN ('landlord','lender')),
 position smallint NOT NULL CHECK (position BETWEEN 1 AND 4),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 150),
 address text NOT NULL CHECK (length(btrim(address)) BETWEEN 1 AND 300),
 rent_paise bigint CHECK (rent_paise BETWEEN 1 AND 1000000000),
 pan_ciphertext text CHECK (pan_ciphertext ~ '^vault:v[1-9][0-9]*:[A-Za-z0-9+/]+={0,2}$' AND length(pan_ciphertext) <= 512),
 pan_masked text CHECK (pan_masked ~ '^XXXXXX[0-9]{3}[A-Z]$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_tax_declaration_party_rent CHECK ((role = 'landlord') = (rent_paise IS NOT NULL)),
 -- Erasing a decided-against proposal drops the ciphertext and keeps the mask, so the reverse never holds.
 CONSTRAINT workforce_tax_declaration_party_mask CHECK (pan_ciphertext IS NULL OR pan_masked IS NOT NULL),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,role,position),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION workforce_tax_declaration_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent workforce_staff_tax_declarations;
BEGIN
 SELECT * INTO parent FROM workforce_staff_tax_declarations d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.tax_year)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.tax_year);
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL OR NEW.base_version IS DISTINCT FROM parent.version THEN
  RAISE EXCEPTION 'a tax declaration proposal starts pending on the current version' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND s.employment_status='active') THEN
  RAISE EXCEPTION 'tax declarations can only be proposed for an active staff member' USING ERRCODE='23514', CONSTRAINT='workforce_tax_declaration_staff_inactive';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_declaration_proposal BEFORE INSERT ON workforce_staff_tax_declaration_revisions FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_proposal();

CREATE FUNCTION workforce_tax_declaration_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allowed text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
BEGIN
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed) THEN
  RAISE EXCEPTION 'a tax declaration proposal is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_declaration_decision BEFORE UPDATE ON workforce_staff_tax_declaration_revisions FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_decision();

CREATE FUNCTION workforce_tax_declaration_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.tax_year,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.tax_year,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a tax declaration keeps its staff member and year and advances one approved version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_declaration_advance BEFORE UPDATE ON workforce_staff_tax_declarations FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_advance();

-- Lines belong to the transaction that proposed their revision, so an approved or decided-against declaration never gains one.
CREATE FUNCTION workforce_tax_declaration_line() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.status='pending' AND r.created_tx=txid_current()) THEN
  RAISE EXCEPTION 'tax declaration lines are written only with their pending proposal' USING ERRCODE='23514';
 END IF;
 -- Nested so the claims row type never resolves pan_ciphertext.
 IF TG_TABLE_NAME='workforce_staff_tax_declaration_parties' THEN
  IF (NEW.pan_ciphertext IS NULL) <> (NEW.pan_masked IS NULL) THEN
   RAISE EXCEPTION 'a proposed party PAN is stored as ciphertext and mask together' USING ERRCODE='23514', CONSTRAINT='workforce_tax_declaration_party_sealed';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_declaration_claim_line BEFORE INSERT ON workforce_staff_tax_declaration_claims FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_line();
CREATE TRIGGER workforce_tax_declaration_party_line BEFORE INSERT ON workforce_staff_tax_declaration_parties FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_line();

CREATE FUNCTION workforce_tax_declaration_claim_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'tax declaration claims are immutable' USING ERRCODE='23514';
END $$;
CREATE TRIGGER workforce_tax_declaration_claim_frozen BEFORE UPDATE ON workforce_staff_tax_declaration_claims FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_claim_frozen();

-- The only change to a party is erasing its PAN ciphertext once the proposal was rejected or withdrawn.
CREATE FUNCTION workforce_tax_declaration_party_erase() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.pan_ciphertext IS NOT NULL OR (to_jsonb(NEW) - 'pan_ciphertext') IS DISTINCT FROM (to_jsonb(OLD) - 'pan_ciphertext')
   OR NOT EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.status IN ('rejected','withdrawn')) THEN
  RAISE EXCEPTION 'a tax declaration party only loses its PAN ciphertext after its proposal is decided against' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_declaration_party_erase BEFORE UPDATE ON workforce_staff_tax_declaration_parties FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_party_erase();

-- Tax years are Indian, so the decision date is the India calendar date, clamped into the declared year.
CREATE FUNCTION workforce_tax_declaration_landlord_threshold(t uuid, h uuid, staff uuid, y text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT c.landlord_pan_rent_above_paise FROM workforce_staff_tax_regimes e
 JOIN workforce_income_tax_claims c ON (c.tenant_id,c.hospital_id)=(e.tenant_id,e.hospital_id) AND c.kind='hra'
  AND c.revision_id=workforce_statutory_rule_on(e.tenant_id,e.hospital_id,'income_tax_'||e.regime,
   greatest(make_date(left(y,4)::int,4,1),least(make_date(left(y,4)::int+1,3,31),(clock_timestamp() AT TIME ZONE 'Asia/Kolkata')::date)))
 WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.tax_year)=(t,h,staff,y)
$$;

-- Checked when a proposal commits and again when it is approved; an unknown threshold demands every landlord's PAN.
CREATE FUNCTION workforce_tax_declaration_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE threshold bigint; rent bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.status<>'approved' THEN
  IF NEW.status IN ('rejected','withdrawn') AND EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_parties p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) AND p.pan_ciphertext IS NOT NULL) THEN
   RAISE EXCEPTION 'a decided-against tax declaration keeps no party PAN ciphertext' USING ERRCODE='23514', CONSTRAINT='workforce_tax_declaration_erased';
  END IF;
  RETURN NULL;
 END IF;
 IF TG_OP='UPDATE' AND NOT EXISTS(SELECT 1 FROM workforce_staff_tax_declarations d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.tax_year,d.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.tax_year,NEW.id)) THEN
  RAISE EXCEPTION 'an approved tax declaration must become the one in force' USING ERRCODE='23514';
 END IF;
 IF (NEW.hra_metro IS NOT NULL) <> EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_parties p WHERE (p.tenant_id,p.hospital_id,p.revision_id,p.role)=(NEW.tenant_id,NEW.hospital_id,NEW.id,'landlord'))
   OR (NEW.home_loan_interest_paise IS NOT NULL) <> EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_parties p WHERE (p.tenant_id,p.hospital_id,p.revision_id,p.role)=(NEW.tenant_id,NEW.hospital_id,NEW.id,'lender')) THEN
  RAISE EXCEPTION 'a tax declaration names landlords exactly when it claims rent and lenders exactly when it claims home loan interest' USING ERRCODE='23514', CONSTRAINT='workforce_tax_declaration_shape';
 END IF;
 SELECT sum(p.rent_paise) INTO rent FROM workforce_staff_tax_declaration_parties p WHERE (p.tenant_id,p.hospital_id,p.revision_id,p.role)=(NEW.tenant_id,NEW.hospital_id,NEW.id,'landlord');
 threshold := workforce_tax_declaration_landlord_threshold(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.tax_year);
 IF (threshold IS NULL OR rent > threshold) AND EXISTS(SELECT 1 FROM workforce_staff_tax_declaration_parties p WHERE (p.tenant_id,p.hospital_id,p.revision_id,p.role)=(NEW.tenant_id,NEW.hospital_id,NEW.id,'landlord') AND p.pan_masked IS NULL) THEN
  RAISE EXCEPTION 'every landlord needs a PAN once rent paid crosses the threshold in force' USING ERRCODE='23514', CONSTRAINT='workforce_tax_declaration_landlord_pan_required';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_tax_declaration_complete AFTER INSERT OR UPDATE ON workforce_staff_tax_declaration_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_tax_declaration_complete();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_staff_tax_declarations','workforce_staff_tax_declaration_revisions','workforce_staff_tax_declaration_claims','workforce_staff_tax_declaration_parties'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  EXECUTE format('CREATE TRIGGER workforce_payroll_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
 END LOOP;
END $$;
