SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_statutory_rules DROP CONSTRAINT workforce_statutory_rules_kind_check;
ALTER TABLE workforce_statutory_rules ADD CONSTRAINT workforce_statutory_rules_kind_check CHECK (kind IN ('pf','esi','pt','lwf'));

CREATE TABLE workforce_statutory_pt_params (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, kind text GENERATED ALWAYS AS ('pt') STORED,
 state_code text NOT NULL CHECK (state_code ~ '^IN-[A-Z]{2}$'),
 wage_basis text NOT NULL CHECK (wage_basis IN ('statutory_wages','gross')),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

CREATE TABLE workforce_statutory_pt_slabs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 96),
 months_mask smallint NOT NULL CHECK (months_mask BETWEEN 1 AND 4095),
 genders_mask smallint NOT NULL CHECK (genders_mask BETWEEN 1 AND 7),
 from_paise bigint NOT NULL CHECK (from_paise BETWEEN 0 AND 1000000000),
 to_paise bigint CHECK (to_paise > from_paise AND to_paise <= 1000000000),
 amount_paise bigint NOT NULL CHECK (amount_paise BETWEEN 0 AND 10000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_statutory_pt_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT
);

CREATE TABLE workforce_statutory_lwf_params (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, kind text GENERATED ALWAYS AS ('lwf') STORED,
 state_code text NOT NULL CHECK (state_code ~ '^IN-[A-Z]{2}$'),
 wage_basis text NOT NULL CHECK (wage_basis IN ('statutory_wages','gross')),
 deduction_mask smallint NOT NULL CHECK (deduction_mask BETWEEN 1 AND 4095),
 rounding_mode text NOT NULL CHECK (rounding_mode IN ('nearest','up','down')),
 rounding_unit_paise smallint NOT NULL CHECK (rounding_unit_paise IN (1,50,100,500,1000)),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

CREATE TABLE workforce_statutory_lwf_slabs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 24),
 from_paise bigint NOT NULL CHECK (from_paise BETWEEN 0 AND 1000000000),
 to_paise bigint CHECK (to_paise > from_paise AND to_paise <= 1000000000),
 employee_fixed_paise bigint NOT NULL CHECK (employee_fixed_paise BETWEEN 0 AND 10000000),
 employee_rate_ppm integer NOT NULL CHECK (employee_rate_ppm BETWEEN 0 AND 1000000),
 employee_cap_paise bigint CHECK (employee_cap_paise BETWEEN 0 AND 10000000),
 employer_fixed_paise bigint NOT NULL CHECK (employer_fixed_paise BETWEEN 0 AND 10000000),
 employer_rate_ppm integer NOT NULL CHECK (employer_rate_ppm BETWEEN 0 AND 1000000),
 employer_cap_paise bigint CHECK (employer_cap_paise BETWEEN 0 AND 10000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_statutory_lwf_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION workforce_statutory_parameters() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n int;
BEGIN
 SELECT (SELECT count(*) FROM workforce_statutory_pf_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_esi_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_pt_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_lwf_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)) INTO n;
 IF n <> NEW.covered::int THEN
  RAISE EXCEPTION 'a covered statutory revision needs its parameters and an uncovered one none' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_parameters';
 END IF;
 RETURN NULL;
END $$;

-- Every named month and gender (PT) or every wage (LWF) falls in exactly one slab, so no calculation can find none.
CREATE FUNCTION workforce_statutory_slabs() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE broken boolean;
BEGIN
 WITH slabs AS (
  SELECT s.months_mask, s.genders_mask, s.from_paise, s.to_paise FROM workforce_statutory_pt_slabs s WHERE NEW.kind='pt' AND (s.tenant_id,s.hospital_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
  UNION ALL
  SELECT 4095, 7, s.from_paise, s.to_paise FROM workforce_statutory_lwf_slabs s WHERE NEW.kind='lwf' AND (s.tenant_id,s.hospital_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
 ), cells AS (
  SELECT m, g FROM generate_series(0,11) m, unnest(ARRAY[1,2,4]) g WHERE EXISTS (SELECT 1 FROM slabs s WHERE s.months_mask & (1<<m) <> 0)
 ), applying AS (
  SELECT s.from_paise, s.to_paise, lag(s.to_paise) OVER w AS prev, row_number() OVER w AS rn, count(*) OVER (PARTITION BY c.m,c.g) AS n
  FROM cells c LEFT JOIN slabs s ON s.months_mask & (1<<c.m) <> 0 AND s.genders_mask & c.g <> 0
  WINDOW w AS (PARTITION BY c.m,c.g ORDER BY s.from_paise)
 )
 SELECT NOT EXISTS (SELECT 1 FROM slabs) OR EXISTS (SELECT 1 FROM applying a WHERE a.from_paise IS NULL OR (a.rn=1 AND a.from_paise<>0)
  OR (a.rn>1 AND a.from_paise IS DISTINCT FROM a.prev) OR ((a.rn=a.n) <> (a.to_paise IS NULL))) INTO broken;
 IF broken THEN
  RAISE EXCEPTION 'statutory slabs must cover every wage once from zero' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_slabs';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_statutory_slabs AFTER INSERT ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW WHEN (NEW.kind IN ('pt','lwf') AND NEW.covered) EXECUTE FUNCTION workforce_statutory_slabs();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_statutory_pt_params','workforce_statutory_pt_slabs','workforce_statutory_lwf_params','workforce_statutory_lwf_slabs'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_parameter_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_statutory_parameter_creation()',tbl);
 END LOOP;
END $$;
