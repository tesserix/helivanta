SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_statutory_rules (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('pf','esi')),
 version bigint NOT NULL CHECK (version >= 0), revision_id uuid, effective_on date,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((version = 0) = (revision_id IS NULL) AND (revision_id IS NULL) = (effective_on IS NULL)),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,kind), UNIQUE(tenant_id,hospital_id,id,kind),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_statutory_rules_page ON workforce_statutory_rules(tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE workforce_statutory_rule_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, rule_id uuid NOT NULL, kind text NOT NULL,
 sequence integer NOT NULL CHECK (sequence > 0), base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80), covered boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 citation text NOT NULL CHECK (length(btrim(citation)) BETWEEN 1 AND 500),
 reference text NOT NULL CHECK (length(reference) <= 300),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_statutory_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_statutory_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_statutory_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,id,kind),
 UNIQUE(tenant_id,hospital_id,rule_id,sequence), UNIQUE(tenant_id,hospital_id,rule_id,approved_version),
 UNIQUE(tenant_id,hospital_id,rule_id,approved_version,id,effective_on),
 FOREIGN KEY(tenant_id,hospital_id,rule_id,kind) REFERENCES workforce_statutory_rules(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_statutory_one_pending ON workforce_statutory_rule_revisions(tenant_id,hospital_id,rule_id) WHERE status = 'pending';
CREATE INDEX workforce_statutory_rule_history ON workforce_statutory_rule_revisions(tenant_id,hospital_id,rule_id,created_at DESC,id DESC);
ALTER TABLE workforce_statutory_rules ADD CONSTRAINT workforce_statutory_rule_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id,effective_on) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,rule_id,approved_version,id,effective_on) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_statutory_pf_params (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, kind text GENERATED ALWAYS AS ('pf') STORED,
 wage_basis text NOT NULL CHECK (wage_basis IN ('statutory_wages','gross')),
 contribution_basis text NOT NULL CHECK (contribution_basis IN ('capped','actual')),
 wage_ceiling_paise bigint NOT NULL CHECK (wage_ceiling_paise BETWEEN 1 AND 1000000000),
 employee_rate_ppm integer NOT NULL CHECK (employee_rate_ppm BETWEEN 0 AND 1000000),
 employer_rate_ppm integer NOT NULL CHECK (employer_rate_ppm BETWEEN 0 AND 1000000),
 eps_rate_ppm integer NOT NULL CHECK (eps_rate_ppm BETWEEN 0 AND 1000000),
 eps_wage_ceiling_paise bigint NOT NULL CHECK (eps_wage_ceiling_paise BETWEEN 0 AND 1000000000),
 edli_rate_ppm integer NOT NULL CHECK (edli_rate_ppm BETWEEN 0 AND 1000000),
 edli_wage_ceiling_paise bigint NOT NULL CHECK (edli_wage_ceiling_paise BETWEEN 0 AND 1000000000),
 admin_rate_ppm integer NOT NULL CHECK (admin_rate_ppm BETWEEN 0 AND 1000000),
 admin_minimum_paise bigint NOT NULL CHECK (admin_minimum_paise BETWEEN 0 AND 1000000000),
 rounding_mode text NOT NULL CHECK (rounding_mode IN ('nearest','up','down')),
 rounding_unit_paise smallint NOT NULL CHECK (rounding_unit_paise IN (1,50,100,500,1000)),
 created_at timestamptz NOT NULL DEFAULT now(),
 -- EPS is carved out of the employer share, so it can never exceed it.
 CHECK (eps_rate_ppm <= employer_rate_ppm AND eps_wage_ceiling_paise <= wage_ceiling_paise),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

CREATE TABLE workforce_statutory_esi_params (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL, kind text GENERATED ALWAYS AS ('esi') STORED,
 wage_basis text NOT NULL CHECK (wage_basis IN ('statutory_wages','gross')),
 wage_ceiling_paise bigint NOT NULL CHECK (wage_ceiling_paise BETWEEN 1 AND 1000000000),
 disability_wage_ceiling_paise bigint NOT NULL CHECK (disability_wage_ceiling_paise BETWEEN 1 AND 1000000000),
 employee_rate_ppm integer NOT NULL CHECK (employee_rate_ppm BETWEEN 0 AND 1000000),
 employer_rate_ppm integer NOT NULL CHECK (employer_rate_ppm BETWEEN 0 AND 1000000),
 employee_exempt_daily_paise bigint CHECK (employee_exempt_daily_paise BETWEEN 1 AND 10000000),
 period_start_mask smallint NOT NULL CHECK (period_start_mask BETWEEN 1 AND 4095),
 rounding_mode text NOT NULL CHECK (rounding_mode IN ('nearest','up','down')),
 rounding_unit_paise smallint NOT NULL CHECK (rounding_unit_paise IN (1,50,100,500,1000)),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (disability_wage_ceiling_paise >= wage_ceiling_paise),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_statutory_rule_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

-- Approved revisions advance in effective date, so the highest approved version on or before the day is in force.
CREATE FUNCTION workforce_statutory_rule_on(t uuid, h uuid, rule_kind text, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT r.id FROM workforce_statutory_rule_revisions r WHERE r.tenant_id=t AND r.hospital_id=h AND r.kind=rule_kind AND r.status='approved' AND r.effective_on<=day
 ORDER BY r.approved_version DESC LIMIT 1
$$;

-- A proposal starts pending on the rule's current version and never dates itself before the revision in force.
CREATE FUNCTION workforce_statutory_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rule workforce_statutory_rules;
BEGIN
 SELECT * INTO rule FROM workforce_statutory_rules s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.rule_id);
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL OR NEW.base_version IS DISTINCT FROM rule.version THEN
  RAISE EXCEPTION 'a statutory proposal starts pending on the current version' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on < rule.effective_on THEN
  RAISE EXCEPTION 'a statutory revision cannot take effect before the one in force' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_effective';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_statutory_proposal BEFORE INSERT ON workforce_statutory_rule_revisions FOR EACH ROW EXECUTE FUNCTION workforce_statutory_proposal();

-- Deferred so parameters inserted after their revision are counted at commit.
CREATE FUNCTION workforce_statutory_parameters() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n int;
BEGIN
 SELECT (SELECT count(*) FROM workforce_statutory_pf_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id))
  + (SELECT count(*) FROM workforce_statutory_esi_params p WHERE (p.tenant_id,p.hospital_id,p.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)) INTO n;
 IF n <> NEW.covered::int THEN
  RAISE EXCEPTION 'a covered statutory revision needs its parameters and an uncovered one none' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_parameters';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_statutory_parameters AFTER INSERT ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_statutory_parameters();

-- The only change a proposal ever takes is its single decision; its content stays as proposed.
CREATE FUNCTION workforce_statutory_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE decision text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
BEGIN
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - decision) IS DISTINCT FROM (to_jsonb(OLD) - decision) THEN
  RAISE EXCEPTION 'a statutory proposal is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_statutory_decision BEFORE UPDATE ON workforce_statutory_rule_revisions FOR EACH ROW EXECUTE FUNCTION workforce_statutory_decision();

CREATE FUNCTION workforce_statutory_approved() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' AND NOT EXISTS(SELECT 1 FROM workforce_statutory_rules s WHERE (s.tenant_id,s.hospital_id,s.id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.rule_id,NEW.id)) THEN
  RAISE EXCEPTION 'an approved statutory revision must become the rule in force' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_statutory_approved AFTER UPDATE ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_statutory_approved();

CREATE FUNCTION workforce_statutory_rule_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.kind,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.kind,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a statutory rule keeps its identity and advances one approved version at a time' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on < OLD.effective_on THEN
  RAISE EXCEPTION 'a statutory revision cannot take effect before the one in force' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_effective';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_statutory_rule_advance BEFORE UPDATE ON workforce_statutory_rules FOR EACH ROW EXECUTE FUNCTION workforce_statutory_rule_advance();

CREATE FUNCTION workforce_statutory_parameter_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_statutory_rule_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id) AND r.created_tx=txid_current()) THEN
  RAISE EXCEPTION 'statutory parameters must be inserted in their revision creating transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_statutory_rules','workforce_statutory_rule_revisions','workforce_statutory_pf_params','workforce_statutory_esi_params'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  IF tbl IN ('workforce_statutory_rules','workforce_statutory_rule_revisions') THEN
   EXECUTE format('CREATE TRIGGER workforce_statutory_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_statutory_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
   EXECUTE format('CREATE TRIGGER workforce_statutory_parameter_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_statutory_parameter_creation()',tbl);
  END IF;
 END LOOP;
END $$;
