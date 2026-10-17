SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_income_tax_claim_groups (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 8),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 label text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 100),
 cap_paise bigint NOT NULL CHECK (cap_paise BETWEEN 1 AND 1000000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 CONSTRAINT workforce_income_tax_claim_group_code UNIQUE(tenant_id,hospital_id,revision_id,code),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_income_tax_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT
);

CREATE TABLE workforce_income_tax_claims (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, position smallint NOT NULL CHECK (position BETWEEN 1 AND 24),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 label text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 100),
 kind text NOT NULL CHECK (kind IN ('deduction','house_property_interest','hra')),
 cap_paise bigint CHECK (cap_paise BETWEEN 1 AND 1000000000),
 cap_group text,
 hra_rent_excess_rate_ppm integer CHECK (hra_rent_excess_rate_ppm BETWEEN 0 AND 1000000),
 hra_metro_rate_ppm integer CHECK (hra_metro_rate_ppm BETWEEN 0 AND 1000000),
 hra_other_rate_ppm integer CHECK (hra_other_rate_ppm BETWEEN 0 AND 1000000),
 landlord_pan_rent_above_paise bigint CHECK (landlord_pan_rent_above_paise BETWEEN 1 AND 1000000000),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_income_tax_claim_fields CHECK (CASE kind
  WHEN 'hra' THEN cap_paise IS NULL AND cap_group IS NULL AND num_nulls(hra_rent_excess_rate_ppm,hra_metro_rate_ppm,hra_other_rate_ppm,landlord_pan_rent_above_paise) = 0
  WHEN 'house_property_interest' THEN cap_paise IS NOT NULL AND cap_group IS NULL AND num_nonnulls(hra_rent_excess_rate_ppm,hra_metro_rate_ppm,hra_other_rate_ppm,landlord_pan_rent_above_paise) = 0
  ELSE num_nonnulls(hra_rent_excess_rate_ppm,hra_metro_rate_ppm,hra_other_rate_ppm,landlord_pan_rent_above_paise) = 0 END),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 CONSTRAINT workforce_income_tax_claim_code UNIQUE(tenant_id,hospital_id,revision_id,code),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_income_tax_params(tenant_id,hospital_id,revision_id) ON DELETE RESTRICT,
 CONSTRAINT workforce_income_tax_claim_group FOREIGN KEY(tenant_id,hospital_id,revision_id,cap_group) REFERENCES workforce_income_tax_claim_groups(tenant_id,hospital_id,revision_id,code) ON DELETE RESTRICT
);
-- HRA and house property interest each have one statutory formula, so a revision carries at most one of each.
CREATE UNIQUE INDEX workforce_income_tax_claim_one_kind ON workforce_income_tax_claims(tenant_id,hospital_id,revision_id,kind) WHERE kind IN ('hra','house_property_interest');

-- A shared cap no claim draws on is a mistake in the entered rule, not a harmless extra.
CREATE FUNCTION workforce_income_tax_claims() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM workforce_income_tax_claim_groups g WHERE (g.tenant_id,g.hospital_id,g.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
  AND NOT EXISTS (SELECT 1 FROM workforce_income_tax_claims c WHERE (c.tenant_id,c.hospital_id,c.revision_id,c.cap_group)=(g.tenant_id,g.hospital_id,g.revision_id,g.code))) THEN
  RAISE EXCEPTION 'every income-tax claim group caps at least one claim' USING ERRCODE='23514', CONSTRAINT='workforce_income_tax_claims';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_income_tax_claims AFTER INSERT ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW WHEN (NEW.kind IN ('income_tax_new','income_tax_old') AND NEW.covered) EXECUTE FUNCTION workforce_income_tax_claims();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_income_tax_claim_groups','workforce_income_tax_claims'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  EXECUTE format('CREATE TRIGGER workforce_statutory_parameter_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_statutory_parameter_creation()',tbl);
 END LOOP;
END $$;
