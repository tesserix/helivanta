SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_salary_assignments (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version >= 0), revision_id uuid, effective_on date,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((version = 0) = (revision_id IS NULL) AND (revision_id IS NULL) = (effective_on IS NULL)),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_salary_assignments_page ON workforce_salary_assignments(tenant_id,hospital_id,created_at DESC,staff_id DESC);

CREATE TABLE workforce_salary_assignment_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL, structure_id uuid NOT NULL,
 sequence integer NOT NULL CHECK (sequence > 0), base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_salary_assignment_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_salary_assignment_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_salary_assignment_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,sequence), UNIQUE(tenant_id,hospital_id,staff_id,approved_version),
 UNIQUE(tenant_id,hospital_id,staff_id,approved_version,id,effective_on),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_salary_assignments(tenant_id,hospital_id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,structure_id) REFERENCES workforce_salary_structures(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_salary_assignment_one_pending ON workforce_salary_assignment_revisions(tenant_id,hospital_id,staff_id) WHERE status = 'pending';
CREATE INDEX workforce_salary_assignment_history ON workforce_salary_assignment_revisions(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE INDEX workforce_salary_assignment_effective ON workforce_salary_assignment_revisions(tenant_id,hospital_id,effective_on) WHERE status = 'approved';
ALTER TABLE workforce_salary_assignments ADD CONSTRAINT workforce_salary_assignment_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,version,revision_id,effective_on) REFERENCES workforce_salary_assignment_revisions(tenant_id,hospital_id,staff_id,approved_version,id,effective_on) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_salary_assignment_amounts (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, head_id uuid NOT NULL,
 amount_paise bigint NOT NULL CHECK (amount_paise BETWEEN 0 AND 1000000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,head_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_salary_assignment_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,head_id) REFERENCES workforce_salary_heads(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

-- Approved revisions advance in effective date, so the highest approved version on or before the day is in force.
CREATE FUNCTION workforce_salary_assignment_on(t uuid, h uuid, staff uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT r.id FROM workforce_salary_assignment_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.staff_id=staff AND r.status='approved' AND r.effective_on<=day
 ORDER BY r.approved_version DESC LIMIT 1
$$;

-- Bound of each line at full pay with the person's amounts in place of the structure's, as workforce_salary_structure_bound.
CREATE FUNCTION workforce_salary_assignment_bound(t uuid, h uuid, structure_revision uuid, assignment_revision uuid, day date) RETURNS numeric LANGUAGE plpgsql STABLE AS $$
DECLARE bounds jsonb := '{}'; progressed boolean; rec record; v numeric; worst numeric := 0;
BEGIN
 LOOP
  progressed := false;
  FOR rec IN SELECT l.head_id, coalesce(o.amount_paise,l.amount_paise) AS amount_paise, a.id AS revision_id, a.basis, a.rate_ppm, a.rounding_unit_paise AS unit, a.min_amount_paise AS floor_paise, a.max_amount_paise AS cap_paise
    FROM workforce_salary_structure_lines l JOIN workforce_salary_heads_at(t,h,day) a ON a.head_id=l.head_id
    LEFT JOIN workforce_salary_assignment_amounts o ON (o.tenant_id,o.hospital_id,o.revision_id,o.head_id)=(t,h,assignment_revision,l.head_id)
    WHERE l.tenant_id=t AND l.hospital_id=h AND l.revision_id=structure_revision AND NOT bounds ? l.head_id::text LOOP
   IF rec.basis='fixed' THEN
    v := rec.amount_paise;
   ELSIF NOT EXISTS(SELECT 1 FROM workforce_salary_head_bases b WHERE b.tenant_id=t AND b.hospital_id=h AND b.revision_id=rec.revision_id AND NOT bounds ? b.base_head_id::text) THEN
    SELECT rec.rate_ppm * coalesce(sum((bounds->>b.base_head_id::text)::numeric),0) / 1000000 INTO v FROM workforce_salary_head_bases b WHERE b.tenant_id=t AND b.hospital_id=h AND b.revision_id=rec.revision_id;
   ELSE
    CONTINUE;
   END IF;
   v := least(greatest(ceil(v / rec.unit) * rec.unit, coalesce(rec.floor_paise,0)), coalesce(rec.cap_paise, ceil(v / rec.unit) * rec.unit));
   bounds := bounds || jsonb_build_object(rec.head_id::text, v);
   worst := greatest(worst, v);
   progressed := true;
  END LOOP;
  EXIT WHEN NOT progressed;
 END LOOP;
 RETURN worst;
END $$;

-- An assignment revision fits a day when its structure is active then, each personal amount is a fixed line, and full pay stays within the calculation limit.
CREATE FUNCTION workforce_salary_assignment_fits(t uuid, h uuid, assignment_revision uuid, day date) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE s record;
BEGIN
 SELECT sr.id, sr.active INTO s FROM workforce_salary_assignment_revisions r
  JOIN workforce_salary_structure_revisions sr ON sr.id=workforce_salary_structure_on(t,h,r.structure_id,day) AND (sr.tenant_id,sr.hospital_id)=(t,h)
  WHERE (r.tenant_id,r.hospital_id,r.id)=(t,h,assignment_revision);
 IF s.id IS NULL OR NOT s.active THEN
  RETURN false;
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_salary_assignment_amounts o
   LEFT JOIN workforce_salary_structure_lines l ON (l.tenant_id,l.hospital_id,l.revision_id,l.head_id)=(t,h,s.id,o.head_id)
   LEFT JOIN workforce_salary_heads_at(t,h,day) a ON a.head_id=o.head_id
   WHERE (o.tenant_id,o.hospital_id,o.revision_id)=(t,h,assignment_revision) AND (l.head_id IS NULL OR a.basis IS DISTINCT FROM 'fixed')) THEN
  RETURN false;
 END IF;
 RETURN workforce_salary_assignment_bound(t,h,s.id,assignment_revision,day) <= 100000000000;
END $$;

-- Proves every date from a change onwards: each approved assignment in force still fits its structure and heads.
CREATE FUNCTION workforce_salary_assignment_check(t uuid, h uuid, from_day date) RETURNS void LANGUAGE plpgsql AS $$
DECLARE d date; bad uuid;
BEGIN
 FOR d IN SELECT from_day UNION SELECT r.effective_on FROM workforce_salary_head_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on>from_day
   UNION SELECT r.effective_on FROM workforce_salary_structure_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on>from_day
   UNION SELECT r.effective_on FROM workforce_salary_assignment_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.status='approved' AND r.effective_on>from_day ORDER BY 1 LOOP
  SELECT a.staff_id INTO bad FROM (SELECT DISTINCT ON (r.staff_id) r.staff_id, r.id FROM workforce_salary_assignment_revisions r
    WHERE r.tenant_id=t AND r.hospital_id=h AND r.status='approved' AND r.effective_on<=d ORDER BY r.staff_id, r.approved_version DESC) a
   WHERE NOT workforce_salary_assignment_fits(t,h,a.id,d) ORDER BY a.staff_id LIMIT 1;
  IF bad IS NOT NULL THEN
   RAISE EXCEPTION 'an approved salary assignment would no longer fit its structure' USING ERRCODE='23514', CONSTRAINT='workforce_salary_assignment_incomplete', DETAIL=bad::text, HINT=d::text;
  END IF;
 END LOOP;
END $$;

-- Head changes need no guard here: a head that stops being an active fixed head already fails the structure check for every active structure using it.
CREATE FUNCTION workforce_salary_assignment_config_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM workforce_salary_assignment_check(NEW.tenant_id,NEW.hospital_id,NEW.effective_on);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_salary_assignment_structure_guard AFTER INSERT ON workforce_salary_structure_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_config_guard();

-- A proposal starts pending on the current version for an active staff member and never dates itself before the revision in force.
CREATE FUNCTION workforce_salary_assignment_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent workforce_salary_assignments;
BEGIN
 SELECT * INTO parent FROM workforce_salary_assignments s WHERE (s.tenant_id,s.hospital_id,s.staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id);
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL OR NEW.base_version IS DISTINCT FROM parent.version THEN
  RAISE EXCEPTION 'a salary assignment proposal starts pending on the current version' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on < parent.effective_on THEN
  RAISE EXCEPTION 'a salary assignment cannot take effect before the one in force' USING ERRCODE='23514', CONSTRAINT='workforce_salary_assignment_effective';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND s.employment_status='active') THEN
  RAISE EXCEPTION 'pay can only be proposed for an active staff member' USING ERRCODE='23514', CONSTRAINT='workforce_salary_assignment_staff_inactive';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_salary_assignment_proposal BEFORE INSERT ON workforce_salary_assignment_revisions FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_proposal();

-- Deferred so the amounts inserted after their revision are part of the check.
CREATE FUNCTION workforce_salary_assignment_proposed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_salary_assignment_fits(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.effective_on) THEN
  RAISE EXCEPTION 'a salary assignment must fit its structure on its effective date' USING ERRCODE='23514', CONSTRAINT='workforce_salary_assignment_incomplete', DETAIL=NEW.staff_id::text, HINT=NEW.effective_on::text;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_salary_assignment_proposed AFTER INSERT ON workforce_salary_assignment_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_proposed();

-- The only change a proposal ever takes is its single decision; its content stays as proposed.
CREATE FUNCTION workforce_salary_assignment_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE decision text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
BEGIN
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - decision) IS DISTINCT FROM (to_jsonb(OLD) - decision) THEN
  RAISE EXCEPTION 'a salary assignment proposal is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_salary_assignment_decision BEFORE UPDATE ON workforce_salary_assignment_revisions FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_decision();

CREATE FUNCTION workforce_salary_assignment_approved() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' THEN
  IF NOT EXISTS(SELECT 1 FROM workforce_salary_assignments s WHERE (s.tenant_id,s.hospital_id,s.staff_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id)) THEN
   RAISE EXCEPTION 'an approved salary assignment must become the one in force' USING ERRCODE='23514';
  END IF;
  PERFORM workforce_salary_assignment_check(NEW.tenant_id,NEW.hospital_id,NEW.effective_on);
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_salary_assignment_approved AFTER UPDATE ON workforce_salary_assignment_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_approved();

CREATE FUNCTION workforce_salary_assignment_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a salary assignment keeps its staff member and advances one approved version at a time' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on < OLD.effective_on THEN
  RAISE EXCEPTION 'a salary assignment cannot take effect before the one in force' USING ERRCODE='23514', CONSTRAINT='workforce_salary_assignment_effective';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_salary_assignment_advance BEFORE UPDATE ON workforce_salary_assignments FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_advance();

CREATE FUNCTION workforce_salary_assignment_amount_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_salary_assignment_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.created_tx=txid_current()) THEN
  RAISE EXCEPTION 'salary assignment amounts must be inserted in their revision creating transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Tax years run 1 April to 31 March and are written YYYY-YY.
CREATE FUNCTION workforce_valid_tax_year(y text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT y ~ '^[0-9]{4}-[0-9]{2}$' AND left(y,4)::int BETWEEN 2000 AND 2199 AND right(y,2)::int = (left(y,4)::int + 1) % 100
$$;

CREATE TABLE workforce_staff_tax_regimes (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 tax_year text NOT NULL CHECK (workforce_valid_tax_year(tax_year)),
 regime text NOT NULL CHECK (regime IN ('new','old')), intimated_on date NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,tax_year), UNIQUE(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_tax_regimes_page ON workforce_staff_tax_regimes(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE TABLE workforce_staff_tax_regime_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL, tax_year text NOT NULL, version bigint NOT NULL CHECK (version > 0),
 regime text NOT NULL CHECK (regime IN ('new','old')),
 -- An intimation can precede the year but never follow it.
 intimated_on date NOT NULL CHECK (intimated_on >= DATE '2000-01-01'),
 note text NOT NULL CHECK (length(note) <= 500),
 recorded_by_subject text NOT NULL CHECK (length(recorded_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_tax_regime_intimated CHECK (intimated_on <= make_date(left(tax_year,4)::int + 1, 3, 31)),
 CONSTRAINT workforce_tax_regime_change_note CHECK (version = 1 OR length(btrim(note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,tax_year,version), UNIQUE(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,tax_year,version,regime,intimated_on),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,tax_year) REFERENCES workforce_staff_tax_regimes(tenant_id,hospital_id,staff_id,tax_year) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_tax_regime_history ON workforce_staff_tax_regime_revisions(tenant_id,hospital_id,staff_id,tax_year,created_at DESC,id DESC);
ALTER TABLE workforce_staff_tax_regimes ADD CONSTRAINT workforce_staff_tax_regime_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,tax_year,version,regime,intimated_on) REFERENCES workforce_staff_tax_regime_revisions(tenant_id,hospital_id,staff_id,tax_year,version,regime,intimated_on) DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION workforce_staff_tax_regime_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.tax_year,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.tax_year,OLD.created_at) OR NEW.id<>OLD.id OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a tax-regime election keeps its staff member and year and advances one version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_tax_regime_advance BEFORE UPDATE ON workforce_staff_tax_regimes FOR EACH ROW EXECUTE FUNCTION workforce_staff_tax_regime_advance();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_salary_assignments','workforce_salary_assignment_revisions','workforce_salary_assignment_amounts','workforce_staff_tax_regimes','workforce_staff_tax_regime_revisions'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  IF tbl IN ('workforce_salary_assignments','workforce_salary_assignment_revisions','workforce_staff_tax_regimes') THEN
   EXECUTE format('CREATE TRIGGER workforce_payroll_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_payroll_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
CREATE TRIGGER workforce_salary_assignment_amount_creation BEFORE INSERT ON workforce_salary_assignment_amounts FOR EACH ROW EXECUTE FUNCTION workforce_salary_assignment_amount_creation();
