SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_hr_masters ADD CONSTRAINT workforce_hr_master_kind UNIQUE (tenant_id,hospital_id,id,kind);

CREATE TABLE workforce_salary_heads (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('earning','deduction','employer_contribution')),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80), active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,code), UNIQUE(tenant_id,hospital_id,id,kind), UNIQUE(tenant_id,hospital_id,id,kind,code),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_salary_head_name_unique ON workforce_salary_heads(tenant_id,hospital_id,lower(name));
CREATE INDEX workforce_salary_heads_page ON workforce_salary_heads(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX workforce_salary_heads_kind_page ON workforce_salary_heads(tenant_id,hospital_id,kind,created_at DESC,id DESC);

CREATE TABLE workforce_salary_head_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, head_id uuid NOT NULL,
 kind text NOT NULL, code text NOT NULL, name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80), active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 basis text NOT NULL CHECK (basis IN ('fixed','percent')),
 rate_ppm integer CHECK (rate_ppm BETWEEN 1 AND 1000000),
 proration text NOT NULL CHECK (proration IN ('none','calendar_days','fixed_days')),
 proration_days smallint CHECK (proration_days BETWEEN 1 AND 31),
 minimum_payable_half_days smallint NOT NULL CHECK (minimum_payable_half_days BETWEEN 0 AND 62),
 month_mask smallint NOT NULL CHECK (month_mask BETWEEN 1 AND 4095),
 rounding_mode text NOT NULL CHECK (rounding_mode IN ('nearest','up','down')),
 rounding_unit_paise smallint NOT NULL CHECK (rounding_unit_paise IN (1,50,100,500,1000)),
 min_amount_paise bigint CHECK (min_amount_paise BETWEEN 0 AND 1000000000),
 max_amount_paise bigint CHECK (max_amount_paise BETWEEN 0 AND 1000000000),
 in_gross boolean NOT NULL, in_ctc boolean NOT NULL, arrears_eligible boolean NOT NULL,
 taxability text NOT NULL CHECK (taxability IN ('taxable','exempt','not_applicable')),
 tax_note text CHECK (length(btrim(tax_note)) BETWEEN 1 AND 300),
 wage_class text NOT NULL CHECK (wage_class IN ('wages','excluded','outside')),
 payslip_label text NOT NULL CHECK (length(btrim(payslip_label)) BETWEEN 1 AND 40),
 payslip_order smallint NOT NULL CHECK (payslip_order BETWEEN 0 AND 999), show_when_zero boolean NOT NULL,
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((basis='percent') = (rate_ppm IS NOT NULL)),
 CHECK (basis='fixed' OR proration='none'),
 CHECK ((proration='fixed_days') = (proration_days IS NOT NULL)),
 CHECK (min_amount_paise % rounding_unit_paise = 0 AND max_amount_paise % rounding_unit_paise = 0 AND min_amount_paise <= max_amount_paise),
 CHECK (kind='earning' OR NOT in_gross),
 CHECK (kind<>'deduction' OR (NOT in_ctc AND taxability='not_applicable' AND wage_class='outside')),
 CHECK (kind='deduction' OR taxability<>'not_applicable'),
 CHECK ((taxability='exempt') = (tax_note IS NOT NULL)),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,id,basis),
 UNIQUE(tenant_id,hospital_id,head_id,version,id), UNIQUE(tenant_id,hospital_id,head_id,version),
 FOREIGN KEY(tenant_id,hospital_id,head_id,kind,code) REFERENCES workforce_salary_heads(tenant_id,hospital_id,id,kind,code) ON DELETE RESTRICT
);
CREATE INDEX workforce_salary_head_history ON workforce_salary_head_revisions(tenant_id,hospital_id,head_id,created_at DESC,id DESC);
CREATE INDEX workforce_salary_head_effective ON workforce_salary_head_revisions(tenant_id,hospital_id,effective_on);
ALTER TABLE workforce_salary_heads ADD CONSTRAINT workforce_salary_head_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_salary_head_revisions(tenant_id,hospital_id,head_id,version,id) DEFERRABLE INITIALLY DEFERRED;

-- Percentage bases are earnings by key; whether a base is active on a date is the validator's job.
CREATE TABLE workforce_salary_head_bases (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 basis text GENERATED ALWAYS AS ('percent') STORED,
 base_head_id uuid NOT NULL, base_kind text GENERATED ALWAYS AS ('earning') STORED,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,base_head_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,basis) REFERENCES workforce_salary_head_revisions(tenant_id,hospital_id,id,basis) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,base_head_id,base_kind) REFERENCES workforce_salary_heads(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_salary_head_base_lookup ON workforce_salary_head_bases(tenant_id,hospital_id,base_head_id);

CREATE TABLE workforce_salary_structures (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80), active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,code), UNIQUE(tenant_id,hospital_id,id,code),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_salary_structure_name_unique ON workforce_salary_structures(tenant_id,hospital_id,lower(name));
CREATE INDEX workforce_salary_structures_page ON workforce_salary_structures(tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE workforce_salary_structure_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, structure_id uuid NOT NULL,
 code text NOT NULL, name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 description text NOT NULL CHECK (length(description) <= 500), active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 department_id uuid, department_kind text GENERATED ALWAYS AS ('department') STORED,
 designation_id uuid, designation_kind text GENERATED ALWAYS AS ('designation') STORED,
 grade_id uuid, grade_kind text GENERATED ALWAYS AS ('grade') STORED,
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,structure_id,version,id), UNIQUE(tenant_id,hospital_id,structure_id,version),
 FOREIGN KEY(tenant_id,hospital_id,structure_id,code) REFERENCES workforce_salary_structures(tenant_id,hospital_id,id,code) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,department_id,department_kind) REFERENCES workforce_hr_masters(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,designation_id,designation_kind) REFERENCES workforce_hr_masters(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,grade_id,grade_kind) REFERENCES workforce_hr_masters(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_salary_structure_history ON workforce_salary_structure_revisions(tenant_id,hospital_id,structure_id,created_at DESC,id DESC);
CREATE INDEX workforce_salary_structure_effective ON workforce_salary_structure_revisions(tenant_id,hospital_id,effective_on);
ALTER TABLE workforce_salary_structures ADD CONSTRAINT workforce_salary_structure_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_salary_structure_revisions(tenant_id,hospital_id,structure_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_salary_structure_lines (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 position smallint NOT NULL CHECK (position BETWEEN 1 AND 60), head_id uuid NOT NULL,
 amount_paise bigint CHECK (amount_paise BETWEEN 0 AND 1000000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position), UNIQUE(tenant_id,hospital_id,revision_id,head_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_salary_structure_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,head_id) REFERENCES workforce_salary_heads(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_salary_structure_line_head ON workforce_salary_structure_lines(tenant_id,hospital_id,head_id);

-- Revisions advance in effective date, so the highest version on or before the day is the one in force.
CREATE FUNCTION workforce_salary_head_on(t uuid, h uuid, head uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT r.id FROM workforce_salary_head_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.head_id=head AND r.effective_on<=day ORDER BY r.version DESC LIMIT 1
$$;
CREATE FUNCTION workforce_salary_structure_on(t uuid, h uuid, structure uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT r.id FROM workforce_salary_structure_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.structure_id=structure AND r.effective_on<=day ORDER BY r.version DESC LIMIT 1
$$;
CREATE FUNCTION workforce_salary_heads_at(t uuid, h uuid, day date) RETURNS SETOF workforce_salary_head_revisions LANGUAGE sql STABLE AS $$
 SELECT DISTINCT ON (r.head_id) r.* FROM workforce_salary_head_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on<=day ORDER BY r.head_id, r.version DESC
$$;

-- Upper bound of each line at full pay: proration never raises an amount, so rounding up then clamping bounds every run.
CREATE FUNCTION workforce_salary_structure_bound(t uuid, h uuid, structure_revision uuid, day date) RETURNS numeric LANGUAGE plpgsql STABLE AS $$
DECLARE bounds jsonb := '{}'; progressed boolean; rec record; v numeric; worst numeric := 0;
BEGIN
 LOOP
  progressed := false;
  FOR rec IN SELECT l.head_id, l.amount_paise, a.id AS revision_id, a.basis, a.rate_ppm, a.rounding_unit_paise AS unit, a.min_amount_paise AS floor_paise, a.max_amount_paise AS cap_paise
    FROM workforce_salary_structure_lines l JOIN workforce_salary_heads_at(t,h,day) a ON a.head_id=l.head_id
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

-- Proves every date from a change onwards: active bases, acyclic heads, and every active structure computable within the line limit.
CREATE FUNCTION workforce_salary_config_check(t uuid, h uuid, from_day date) RETURNS void LANGUAGE plpgsql AS $$
DECLARE d date; bad uuid;
BEGIN
 FOR d IN SELECT from_day UNION SELECT r.effective_on FROM workforce_salary_head_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on>from_day
   UNION SELECT r.effective_on FROM workforce_salary_structure_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on>from_day ORDER BY 1 LOOP
  SELECT a.head_id INTO bad FROM workforce_salary_heads_at(t,h,d) a
   JOIN workforce_salary_head_bases b ON (b.tenant_id,b.hospital_id,b.revision_id)=(t,h,a.id)
   LEFT JOIN workforce_salary_heads_at(t,h,d) x ON x.head_id=b.base_head_id
   WHERE a.active AND (x.head_id IS NULL OR NOT x.active) ORDER BY a.head_id LIMIT 1;
  IF bad IS NOT NULL THEN
   RAISE EXCEPTION 'a percentage head uses a base that is not an active earning' USING ERRCODE='23514', CONSTRAINT='workforce_salary_base_unavailable', DETAIL=bad::text, HINT=d::text;
  END IF;
  WITH RECURSIVE a AS (SELECT * FROM workforce_salary_heads_at(t,h,d) WHERE active),
   e AS (SELECT a.head_id AS src, b.base_head_id AS dst FROM a JOIN workforce_salary_head_bases b ON (b.tenant_id,b.hospital_id,b.revision_id)=(t,h,a.id)),
   walk(origin,node) AS (SELECT src,dst FROM e UNION SELECT w.origin,e.dst FROM walk w JOIN e ON e.src=w.node)
  SELECT origin INTO bad FROM walk WHERE origin=node ORDER BY origin LIMIT 1;
  IF bad IS NOT NULL THEN
   RAISE EXCEPTION 'salary heads form a dependency cycle' USING ERRCODE='23514', CONSTRAINT='workforce_salary_cycle', DETAIL=bad::text, HINT=d::text;
  END IF;
  WITH a AS (SELECT * FROM workforce_salary_heads_at(t,h,d)),
   s AS (SELECT DISTINCT ON (r.structure_id) r.structure_id, r.id, r.active FROM workforce_salary_structure_revisions r
    WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on<=d ORDER BY r.structure_id, r.version DESC)
  SELECT s.structure_id INTO bad FROM s JOIN workforce_salary_structure_lines l ON (l.tenant_id,l.hospital_id,l.revision_id)=(t,h,s.id)
   LEFT JOIN a ON a.head_id=l.head_id
   WHERE s.active AND (a.head_id IS NULL OR NOT a.active OR (a.basis='fixed') <> (l.amount_paise IS NOT NULL)
    OR EXISTS(SELECT 1 FROM workforce_salary_head_bases b WHERE (b.tenant_id,b.hospital_id,b.revision_id)=(t,h,a.id)
     AND NOT EXISTS(SELECT 1 FROM workforce_salary_structure_lines o WHERE (o.tenant_id,o.hospital_id,o.revision_id,o.head_id)=(t,h,s.id,b.base_head_id))))
   ORDER BY s.structure_id LIMIT 1;
  IF bad IS NULL THEN
   SELECT s.structure_id INTO bad FROM (SELECT DISTINCT ON (r.structure_id) r.structure_id, r.id, r.active FROM workforce_salary_structure_revisions r
     WHERE r.tenant_id=t AND r.hospital_id=h AND r.effective_on<=d ORDER BY r.structure_id, r.version DESC) s
    WHERE s.active AND workforce_salary_structure_bound(t,h,s.id,d) > 100000000000 ORDER BY s.structure_id LIMIT 1;
  END IF;
  IF bad IS NOT NULL THEN
   RAISE EXCEPTION 'an active salary structure cannot be computed' USING ERRCODE='23514', CONSTRAINT='workforce_salary_structure_incomplete', DETAIL=bad::text, HINT=d::text;
  END IF;
 END LOOP;
END $$;

CREATE FUNCTION workforce_salary_child_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_TABLE_NAME='workforce_salary_head_bases' AND NOT EXISTS(SELECT 1 FROM workforce_salary_head_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.created_tx=txid_current()))
  OR (TG_TABLE_NAME='workforce_salary_structure_lines' AND NOT EXISTS(SELECT 1 FROM workforce_salary_structure_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.created_tx=txid_current())) THEN
  RAISE EXCEPTION 'salary configuration children must be inserted in their revision creating transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Deferred so a revision, its children and its parent row are checked together at commit.
CREATE FUNCTION workforce_salary_head_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n int;
BEGIN
 SELECT count(*) INTO n FROM workforce_salary_head_bases b WHERE (b.tenant_id,b.hospital_id,b.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF NEW.basis='percent' AND n NOT BETWEEN 1 AND 20 THEN
  RAISE EXCEPTION 'a percentage head needs 1 to 20 bases' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_salary_heads s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.head_id)
   AND (s.version<>NEW.version OR (s.revision_id,s.name,s.active,s.effective_on,s.reviewed_by_subject)=(NEW.id,NEW.name,NEW.active,NEW.effective_on,NEW.reviewed_by_subject))) THEN
  RAISE EXCEPTION 'a salary head must mirror its current revision' USING ERRCODE='23514';
 END IF;
 PERFORM workforce_salary_config_check(NEW.tenant_id,NEW.hospital_id,NEW.effective_on);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_salary_head_valid AFTER INSERT ON workforce_salary_head_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_salary_head_valid();

CREATE FUNCTION workforce_salary_structure_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n int; top int;
BEGIN
 SELECT count(*), coalesce(max(l.position),0) INTO n, top FROM workforce_salary_structure_lines l WHERE (l.tenant_id,l.hospital_id,l.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF n=0 OR top<>n THEN
  RAISE EXCEPTION 'a salary structure needs lines numbered 1 upwards without gaps' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_hr_masters m WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.id IN (NEW.department_id,NEW.designation_id,NEW.grade_id) AND NOT m.active) THEN
  RAISE EXCEPTION 'a salary structure scope must use active masters' USING ERRCODE='23514', CONSTRAINT='workforce_salary_scope_inactive';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_salary_structures s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.structure_id)
   AND (s.version<>NEW.version OR (s.revision_id,s.name,s.active,s.effective_on,s.reviewed_by_subject)=(NEW.id,NEW.name,NEW.active,NEW.effective_on,NEW.reviewed_by_subject))) THEN
  RAISE EXCEPTION 'a salary structure must mirror its current revision' USING ERRCODE='23514';
 END IF;
 PERFORM workforce_salary_config_check(NEW.tenant_id,NEW.hospital_id,NEW.effective_on);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_salary_structure_valid AFTER INSERT ON workforce_salary_structure_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_salary_structure_valid();

CREATE FUNCTION workforce_salary_stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.code,OLD.created_at)
   OR (TG_TABLE_NAME='workforce_salary_heads' AND to_jsonb(NEW)->>'kind' IS DISTINCT FROM to_jsonb(OLD)->>'kind') OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'salary configuration identity is immutable and versions must advance' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on<OLD.effective_on THEN
  RAISE EXCEPTION 'salary configuration cannot become effective before its current revision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_salary_heads','workforce_salary_head_revisions','workforce_salary_head_bases','workforce_salary_structures','workforce_salary_structure_revisions','workforce_salary_structure_lines'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  IF tbl IN ('workforce_salary_heads','workforce_salary_structures') THEN
   EXECUTE format('CREATE TRIGGER workforce_salary_stable_identity BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION workforce_salary_stable_identity()',tbl);
   EXECUTE format('CREATE TRIGGER workforce_salary_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_salary_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
  IF tbl IN ('workforce_salary_head_bases','workforce_salary_structure_lines') THEN
   EXECUTE format('CREATE TRIGGER workforce_salary_child_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_salary_child_creation()',tbl);
  END IF;
 END LOOP;
END $$;
