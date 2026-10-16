SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_statutory_rules DROP CONSTRAINT workforce_statutory_rules_kind_check;
ALTER TABLE workforce_statutory_rules ADD CONSTRAINT workforce_statutory_rules_kind_check CHECK (kind IN ('pf','esi','pt','lwf','income_tax_new','income_tax_old'));

-- The tax year is the Act's 1 April to 31 March, written YYYY-YY.
CREATE FUNCTION workforce_tax_year(day date) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT to_char(y,'FM0000')||'-'||to_char((y+1)%100,'FM00') FROM (SELECT extract(year FROM day)::int - (extract(month FROM day)<4)::int AS y) t
$$;

CREATE TABLE workforce_income_tax_params (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('income_tax_new','income_tax_old')),
 tax_year text NOT NULL CHECK (tax_year ~ '^2[01][0-9]{2}-[0-9]{2}$' AND right(tax_year,2)::int = (left(tax_year,4)::int+1)%100),
 standard_deduction_paise bigint NOT NULL CHECK (standard_deduction_paise BETWEEN 0 AND 100000000),
 taxable_rounding_mode text NOT NULL CHECK (taxable_rounding_mode IN ('nearest','up','down')),
 taxable_rounding_unit_paise smallint NOT NULL CHECK (taxable_rounding_unit_paise IN (1,50,100,500,1000)),
 tax_rounding_mode text NOT NULL CHECK (tax_rounding_mode IN ('nearest','up','down')),
 tax_rounding_unit_paise smallint NOT NULL CHECK (tax_rounding_unit_paise IN (1,50,100,500,1000)),
 rebate_income_limit_paise bigint CHECK (rebate_income_limit_paise BETWEEN 1 AND 100000000000),
 rebate_max_paise bigint CHECK (rebate_max_paise BETWEEN 1 AND 100000000000),
 rebate_marginal_relief boolean,
 cess_rate_ppm integer NOT NULL CHECK (cess_rate_ppm BETWEEN 0 AND 1000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_income_tax_rebate CHECK (num_nulls(rebate_income_limit_paise,rebate_max_paise,rebate_marginal_relief) IN (0,3)),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

CREATE TABLE workforce_income_tax_age_bands (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, min_age smallint NOT NULL CHECK (min_age BETWEEN 0 AND 120),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,min_age),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_income_tax_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT
);

CREATE TABLE workforce_income_tax_slabs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, min_age smallint NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 16),
 from_paise bigint NOT NULL CHECK (from_paise BETWEEN 0 AND 100000000000),
 to_paise bigint CHECK (to_paise > from_paise AND to_paise <= 100000000000),
 rate_ppm integer NOT NULL CHECK (rate_ppm BETWEEN 0 AND 1000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,min_age,position),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,min_age) REFERENCES workforce_income_tax_age_bands(tenant_id,hospital_id,revision_id,min_age) ON DELETE RESTRICT
);

CREATE TABLE workforce_income_tax_surcharges (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 8),
 above_paise bigint NOT NULL CHECK (above_paise BETWEEN 1 AND 100000000000),
 rate_ppm integer NOT NULL CHECK (rate_ppm BETWEEN 0 AND 1000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_income_tax_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION workforce_statutory_parameters() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n int;
BEGIN
 SELECT (SELECT count(*) FROM workforce_statutory_pf_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_esi_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_pt_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_lwf_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_income_tax_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)) INTO n;
 IF n <> NEW.covered::int THEN
  RAISE EXCEPTION 'a covered statutory revision needs its parameters and an uncovered one none' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_parameters';
 END IF;
 RETURN NULL;
END $$;

-- The year matches the effective date, a band starts at age 0, each band's slabs cover every income once and surcharge thresholds rise.
CREATE FUNCTION workforce_income_tax_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE broken boolean;
BEGIN
 WITH bands AS (
  SELECT b.min_age FROM workforce_income_tax_age_bands b WHERE (b.tenant_id,b.hospital_id,b.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
 ), slabs AS (
  SELECT b.min_age, s.from_paise, s.to_paise, lag(s.to_paise) OVER w AS prev, row_number() OVER w AS rn, count(s.position) OVER (PARTITION BY b.min_age) AS n
  FROM bands b LEFT JOIN workforce_income_tax_slabs s ON (s.tenant_id,s.hospital_id,s.revision_id,s.min_age)=(NEW.tenant_id,NEW.hospital_id,NEW.id,b.min_age)
  WINDOW w AS (PARTITION BY b.min_age ORDER BY s.from_paise)
 ), surcharges AS (
  SELECT s.above_paise, lag(s.above_paise) OVER (ORDER BY s.position) AS prev FROM workforce_income_tax_surcharges s WHERE (s.tenant_id,s.hospital_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
 )
 SELECT NOT EXISTS (SELECT 1 FROM workforce_income_tax_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) AND p.tax_year=workforce_tax_year(NEW.effective_on))
  OR NOT EXISTS (SELECT 1 FROM bands WHERE min_age=0)
  OR EXISTS (SELECT 1 FROM slabs a WHERE (a.rn=1 AND a.from_paise<>0) OR (a.rn>1 AND a.from_paise IS DISTINCT FROM a.prev) OR ((a.rn=a.n) <> (a.to_paise IS NULL)))
  OR EXISTS (SELECT 1 FROM surcharges s WHERE s.above_paise <= s.prev) INTO broken;
 IF broken THEN
  RAISE EXCEPTION 'income-tax rules need a matching tax year, an age 0 band, complete slabs and rising surcharge thresholds' USING ERRCODE='23514', CONSTRAINT='workforce_income_tax_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_income_tax_complete AFTER INSERT ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW WHEN (NEW.kind IN ('income_tax_new','income_tax_old') AND NEW.covered) EXECUTE FUNCTION workforce_income_tax_complete();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_income_tax_params','workforce_income_tax_age_bands','workforce_income_tax_slabs','workforce_income_tax_surcharges'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_parameter_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_statutory_parameter_creation()',tbl);
 END LOOP;
END $$;
