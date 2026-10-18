SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Only benefits handlers set app.workforce_benefits, so directory, enrollment and payroll scopes never see these rows.
CREATE FUNCTION workforce_benefits_allowed(row_tenant uuid, row_hospital uuid, writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (COALESCE(current_setting('app.workforce_benefits',true)='write',false)
   OR (NOT writing AND COALESCE(current_setting('app.workforce_benefits',true)='read',false)))
  AND workforce_staff_allowed(row_tenant,row_hospital,writing)
$$;

CREATE TABLE workforce_benefit_plans (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 name text NOT NULL, status text NOT NULL CHECK (status IN ('active','retired')),
 effective_from date NOT NULL, effective_until date,
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 CONSTRAINT workforce_benefit_plan_code UNIQUE(tenant_id,hospital_id,code),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_benefit_plan_name ON workforce_benefit_plans(tenant_id,hospital_id,lower(name));
CREATE INDEX workforce_benefit_plans_page ON workforce_benefit_plans(tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE workforce_benefit_plan_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, plan_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0), code text NOT NULL,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 kind text NOT NULL CHECK (kind IN ('health','life','accident','other')),
 description text NOT NULL CHECK (length(description) <= 500),
 covers_dependents boolean NOT NULL,
 covered_relationships uuid[] NOT NULL CHECK (cardinality(covered_relationships) <= 20 AND array_position(covered_relationships, NULL) IS NULL),
 effective_from date NOT NULL CHECK (effective_from BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 effective_until date CHECK (effective_until BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 status text NOT NULL CHECK (status IN ('active','retired')),
 changed_by_subject text NOT NULL CHECK (length(changed_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_benefit_plan_period CHECK (effective_until IS NULL OR effective_until >= effective_from),
 CONSTRAINT workforce_benefit_plan_retired_until CHECK (status <> 'retired' OR effective_until IS NOT NULL),
 CONSTRAINT workforce_benefit_plan_relationships CHECK (covers_dependents = (cardinality(covered_relationships) > 0)),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,plan_id,version),
 UNIQUE(tenant_id,hospital_id,plan_id,version,id),
 UNIQUE(tenant_id,hospital_id,plan_id,id),
 FOREIGN KEY(tenant_id,hospital_id,plan_id) REFERENCES workforce_benefit_plans(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_benefit_plan_history ON workforce_benefit_plan_revisions(tenant_id,hospital_id,plan_id,created_at DESC,id DESC);
ALTER TABLE workforce_benefit_plans ADD CONSTRAINT workforce_benefit_plan_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_benefit_plan_revisions(tenant_id,hospital_id,plan_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_staff_dependents (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL, id uuid NOT NULL,
 full_name text NOT NULL, relationship_master_id uuid NOT NULL, birth_date date NOT NULL,
 status text NOT NULL CHECK (status IN ('active','removed')),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_staff_dependent_unique ON workforce_staff_dependents(tenant_id,hospital_id,staff_id,lower(full_name),birth_date) WHERE status = 'active';
CREATE INDEX workforce_staff_dependents_page ON workforce_staff_dependents(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE TABLE workforce_staff_dependent_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL, dependent_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 full_name text NOT NULL CHECK (length(btrim(full_name)) BETWEEN 1 AND 150),
 relationship_revision_id uuid NOT NULL, relationship_kind text GENERATED ALWAYS AS ('relationship'::text) STORED,
 relationship_master_id uuid NOT NULL,
 birth_date date NOT NULL CHECK (birth_date BETWEEN DATE '1900-01-01' AND DATE '2200-12-31'),
 phone text NOT NULL CHECK (phone = '' OR phone ~ '^\+[1-9][0-9]{6,14}$'),
 status text NOT NULL CHECK (status IN ('active','removed')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 changed_by_subject text NOT NULL CHECK (length(changed_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,dependent_id,version),
 UNIQUE(tenant_id,hospital_id,dependent_id,version,id),
 UNIQUE(tenant_id,hospital_id,dependent_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,dependent_id) REFERENCES workforce_staff_dependents(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,relationship_revision_id,relationship_kind) REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_dependent_history ON workforce_staff_dependent_revisions(tenant_id,hospital_id,dependent_id,created_at DESC,id DESC);
ALTER TABLE workforce_staff_dependents ADD CONSTRAINT workforce_staff_dependent_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_staff_dependent_revisions(tenant_id,hospital_id,dependent_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_benefit_enrollment_proposals (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, enrollment_id uuid NOT NULL, staff_id uuid NOT NULL,
 plan_id uuid NOT NULL, plan_revision_id uuid NOT NULL, dependent_id uuid, dependent_revision_id uuid,
 base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 coverage_from date NOT NULL CHECK (coverage_from BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 coverage_until date CHECK (coverage_until BETWEEN DATE '2000-01-01' AND DATE '2200-12-31'),
 eligibility_basis text NOT NULL CHECK (length(btrim(eligibility_basis)) BETWEEN 1 AND 500),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_benefit_coverage_period CHECK (coverage_until IS NULL OR coverage_until >= coverage_from),
 CHECK ((dependent_id IS NULL) = (dependent_revision_id IS NULL)),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_benefit_proposal_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_benefit_proposal_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_benefit_proposal_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 CONSTRAINT workforce_benefit_pending_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, plan_id WITH =, (COALESCE(dependent_id,staff_id)) WITH =,
  daterange(coverage_from,coverage_until,'[]') WITH &&) WHERE (status = 'pending'),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,enrollment_id,approved_version),
 UNIQUE(tenant_id,hospital_id,enrollment_id,approved_version,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,plan_id,plan_revision_id) REFERENCES workforce_benefit_plan_revisions(tenant_id,hospital_id,plan_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id,dependent_id) REFERENCES workforce_staff_dependents(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,dependent_id,dependent_revision_id) REFERENCES workforce_staff_dependent_revisions(tenant_id,hospital_id,dependent_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_benefit_proposal_one_pending ON workforce_benefit_enrollment_proposals(tenant_id,hospital_id,enrollment_id) WHERE status = 'pending';
CREATE INDEX workforce_benefit_proposal_history ON workforce_benefit_enrollment_proposals(tenant_id,hospital_id,enrollment_id,created_at DESC,id DESC);
CREATE INDEX workforce_benefit_proposal_staff ON workforce_benefit_enrollment_proposals(tenant_id,hospital_id,staff_id) WHERE status = 'pending';

-- Approved coverage; the plan revision it was first approved under never changes.
CREATE TABLE workforce_benefit_enrollments (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 plan_id uuid NOT NULL, original_plan_revision_id uuid NOT NULL, dependent_id uuid,
 coverage_from date NOT NULL, coverage_until date CHECK (coverage_until IS NULL OR coverage_until >= coverage_from),
 version bigint NOT NULL CHECK (version > 0), proposal_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 CONSTRAINT workforce_benefit_coverage_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, plan_id WITH =, (COALESCE(dependent_id,staff_id)) WITH =,
  daterange(coverage_from,coverage_until,'[]') WITH &&),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id,dependent_id) REFERENCES workforce_staff_dependents(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,plan_id,original_plan_revision_id) REFERENCES workforce_benefit_plan_revisions(tenant_id,hospital_id,plan_id,id) ON DELETE RESTRICT,
 CONSTRAINT workforce_benefit_enrollment_proposal FOREIGN KEY(tenant_id,hospital_id,id,version,proposal_id)
  REFERENCES workforce_benefit_enrollment_proposals(tenant_id,hospital_id,enrollment_id,approved_version,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX workforce_benefit_enrollments_staff ON workforce_benefit_enrollments(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE INDEX workforce_benefit_enrollments_plan ON workforce_benefit_enrollments(tenant_id,hospital_id,plan_id);
CREATE INDEX workforce_benefit_enrollments_dependent ON workforce_benefit_enrollments(tenant_id,hospital_id,dependent_id) WHERE dependent_id IS NOT NULL;

-- Current rows must equal the revision they point at; deferred so the pair is written in either order.
CREATE FUNCTION workforce_benefit_plan_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_benefit_plan_revisions r
   WHERE (r.tenant_id,r.hospital_id,r.plan_id,r.id,r.version,r.code,r.name,r.status,r.effective_from)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.revision_id,NEW.version,NEW.code,NEW.name,NEW.status,NEW.effective_from)
    AND r.effective_until IS NOT DISTINCT FROM NEW.effective_until) THEN
  RAISE EXCEPTION 'a benefit plan mirrors its current revision' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_benefit_plan_matches AFTER INSERT OR UPDATE ON workforce_benefit_plans DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_benefit_plan_matches();
CREATE FUNCTION workforce_staff_dependent_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_staff_dependent_revisions r
   WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.dependent_id,r.id,r.version,r.full_name,r.relationship_master_id,r.birth_date,r.status)
    =(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.revision_id,NEW.version,NEW.full_name,NEW.relationship_master_id,NEW.birth_date,NEW.status)) THEN
  RAISE EXCEPTION 'a dependent mirrors its current revision' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_staff_dependent_matches AFTER INSERT OR UPDATE ON workforce_staff_dependents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_staff_dependent_matches();

-- A retired plan is final, and a plan's dates may never strand coverage already approved under it.
CREATE FUNCTION workforce_benefit_plan_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.code,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a benefit plan keeps its code and advances one version at a time' USING ERRCODE='23514';
 END IF;
 IF OLD.status='retired' THEN
  RAISE EXCEPTION 'a retired benefit plan cannot change' USING ERRCODE='23514', CONSTRAINT='workforce_benefit_plan_retired';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_benefit_enrollments e WHERE (e.tenant_id,e.hospital_id,e.plan_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
   AND (e.coverage_from<NEW.effective_from OR (NEW.effective_until IS NOT NULL AND (e.coverage_until IS NULL OR e.coverage_until>NEW.effective_until)))) THEN
  RAISE EXCEPTION 'approved coverage would fall outside the plan dates' USING ERRCODE='23514', CONSTRAINT='workforce_benefit_coverage_outside_plan';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_plan_advance BEFORE UPDATE ON workforce_benefit_plans FOR EACH ROW EXECUTE FUNCTION workforce_benefit_plan_advance();

CREATE FUNCTION workforce_benefit_plan_revision_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_benefit_plans p WHERE (p.tenant_id,p.hospital_id,p.id,p.version,p.code)=(NEW.tenant_id,NEW.hospital_id,NEW.plan_id,NEW.version,NEW.code)) THEN
  RAISE EXCEPTION 'a plan revision records the plan version it becomes' USING ERRCODE='23514';
 END IF;
 IF cardinality(NEW.covered_relationships)<>(SELECT count(DISTINCT x) FROM unnest(NEW.covered_relationships) x)
   OR EXISTS(SELECT 1 FROM unnest(NEW.covered_relationships) x WHERE NOT EXISTS(SELECT 1 FROM workforce_hr_masters m WHERE (m.tenant_id,m.hospital_id,m.id,m.kind)=(NEW.tenant_id,NEW.hospital_id,x,'relationship'))) THEN
  RAISE EXCEPTION 'covered relationships are this hospital''s relationship masters' USING ERRCODE='23514', CONSTRAINT='workforce_benefit_plan_relationship_unknown';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_plan_revision_insert BEFORE INSERT ON workforce_benefit_plan_revisions FOR EACH ROW EXECUTE FUNCTION workforce_benefit_plan_revision_insert();

-- The relationship master is copied from the cited revision, never written by the caller.
CREATE FUNCTION workforce_staff_dependent_revision_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT r.master_id INTO NEW.relationship_master_id FROM workforce_hr_master_revisions r WHERE (r.tenant_id,r.hospital_id,r.id,r.kind)=(NEW.tenant_id,NEW.hospital_id,NEW.relationship_revision_id,'relationship');
 IF NOT EXISTS(SELECT 1 FROM workforce_staff_dependents d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.id,d.version)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.dependent_id,NEW.version)) THEN
  RAISE EXCEPTION 'a dependent revision records the dependent version it becomes' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_dependent_revision_insert BEFORE INSERT ON workforce_staff_dependent_revisions FOR EACH ROW EXECUTE FUNCTION workforce_staff_dependent_revision_insert();

-- New dependents need an active staff member; a dependent with coverage not yet ended keeps its relationship and cannot be removed.
CREATE FUNCTION workforce_staff_dependent_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN
   RAISE EXCEPTION 'a dependent starts at version 1' USING ERRCODE='23514';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND s.employment_status='active') THEN
   RAISE EXCEPTION 'dependents are added for active staff only' USING ERRCODE='23514', CONSTRAINT='workforce_staff_dependent_staff_inactive';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'a dependent keeps its staff member and advances one version at a time' USING ERRCODE='23514';
 END IF;
 IF (NEW.status='removed' OR NEW.relationship_master_id<>OLD.relationship_master_id) AND (
   EXISTS(SELECT 1 FROM workforce_benefit_enrollment_proposals p WHERE (p.tenant_id,p.hospital_id,p.dependent_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) AND p.status='pending')
   OR EXISTS(SELECT 1 FROM workforce_benefit_enrollments e WHERE (e.tenant_id,e.hospital_id,e.dependent_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) AND (e.coverage_until IS NULL OR e.coverage_until>=CURRENT_DATE))) THEN
  RAISE EXCEPTION 'end the dependent''s coverage before removing them or changing their relationship' USING ERRCODE='23514', CONSTRAINT='workforce_staff_dependent_covered';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_dependent_write BEFORE INSERT OR UPDATE ON workforce_staff_dependents FOR EACH ROW EXECUTE FUNCTION workforce_staff_dependent_write();

-- Returns the constraint a proposal breaks against current state, or NULL; checked on proposal and again on approval.
CREATE FUNCTION workforce_benefit_proposal_problem(p workforce_benefit_enrollment_proposals) RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE
 cur workforce_benefit_enrollments;
 plan workforce_benefit_plans;
 rev workforce_benefit_plan_revisions;
 dep workforce_staff_dependents;
 narrowing boolean := false;
BEGIN
 IF EXISTS(SELECT 1 FROM workforce_benefit_enrollment_proposals q WHERE (q.tenant_id,q.hospital_id,q.enrollment_id)=(p.tenant_id,p.hospital_id,p.enrollment_id) AND q.id<>p.id
   AND ((q.staff_id,q.plan_id) IS DISTINCT FROM (p.staff_id,p.plan_id) OR q.dependent_id IS DISTINCT FROM p.dependent_id)) THEN
  RETURN 'workforce_benefit_enrollment_identity';
 END IF;
 SELECT * INTO cur FROM workforce_benefit_enrollments e WHERE (e.tenant_id,e.hospital_id,e.id)=(p.tenant_id,p.hospital_id,p.enrollment_id);
 IF FOUND THEN
  IF (cur.staff_id,cur.plan_id) IS DISTINCT FROM (p.staff_id,p.plan_id) OR cur.dependent_id IS DISTINCT FROM p.dependent_id THEN
   RETURN 'workforce_benefit_enrollment_identity';
  END IF;
  narrowing := p.coverage_from>=cur.coverage_from AND (cur.coverage_until IS NULL OR (p.coverage_until IS NOT NULL AND p.coverage_until<=cur.coverage_until));
 END IF;
 IF p.base_version IS DISTINCT FROM COALESCE(cur.version,0) THEN
  RETURN 'workforce_benefit_enrollment_version';
 END IF;
 SELECT * INTO plan FROM workforce_benefit_plans x WHERE (x.tenant_id,x.hospital_id,x.id)=(p.tenant_id,p.hospital_id,p.plan_id);
 IF plan.revision_id IS DISTINCT FROM p.plan_revision_id THEN
  RETURN 'workforce_benefit_plan_changed';
 END IF;
 IF plan.status='retired' AND NOT narrowing THEN
  RETURN 'workforce_benefit_plan_retired';
 END IF;
 IF p.coverage_from<plan.effective_from OR (plan.effective_until IS NOT NULL AND (p.coverage_until IS NULL OR p.coverage_until>plan.effective_until)) THEN
  RETURN 'workforce_benefit_coverage_outside_plan';
 END IF;
 IF NOT narrowing AND NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(p.tenant_id,p.hospital_id,p.staff_id) AND s.employment_status='active') THEN
  RETURN 'workforce_benefit_staff_inactive';
 END IF;
 IF p.dependent_id IS NOT NULL THEN
  SELECT * INTO dep FROM workforce_staff_dependents d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.id)=(p.tenant_id,p.hospital_id,p.staff_id,p.dependent_id);
  IF dep.revision_id IS DISTINCT FROM p.dependent_revision_id THEN
   RETURN 'workforce_benefit_dependent_changed';
  END IF;
  IF dep.status<>'active' AND NOT narrowing THEN
   RETURN 'workforce_benefit_dependent_removed';
  END IF;
  SELECT * INTO rev FROM workforce_benefit_plan_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(p.tenant_id,p.hospital_id,p.plan_revision_id);
  IF NOT narrowing AND NOT (rev.covers_dependents AND dep.relationship_master_id=ANY(rev.covered_relationships)) THEN
   RETURN 'workforce_benefit_relationship_not_covered';
  END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_benefit_enrollments e WHERE (e.tenant_id,e.hospital_id,e.plan_id)=(p.tenant_id,p.hospital_id,p.plan_id)
   AND COALESCE(e.dependent_id,e.staff_id)=COALESCE(p.dependent_id,p.staff_id) AND e.id<>p.enrollment_id
   AND daterange(e.coverage_from,e.coverage_until,'[]') && daterange(p.coverage_from,p.coverage_until,'[]')) THEN
  RETURN 'workforce_benefit_coverage_overlap';
 END IF;
 RETURN NULL;
END $$;

CREATE FUNCTION workforce_benefit_proposal_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE problem text;
BEGIN
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL THEN
  RAISE EXCEPTION 'a benefit enrollment proposal starts pending' USING ERRCODE='23514';
 END IF;
 problem := workforce_benefit_proposal_problem(NEW);
 IF problem IS NOT NULL THEN
  RAISE EXCEPTION 'benefit enrollment proposal refused' USING ERRCODE='23514', CONSTRAINT=problem;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_proposal_insert BEFORE INSERT ON workforce_benefit_enrollment_proposals FOR EACH ROW EXECUTE FUNCTION workforce_benefit_proposal_insert();

CREATE FUNCTION workforce_benefit_proposal_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 allowed text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
 problem text;
BEGIN
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed) THEN
  RAISE EXCEPTION 'a benefit enrollment proposal is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status='approved' THEN
  problem := workforce_benefit_proposal_problem(NEW);
  IF problem IS NOT NULL THEN
   RAISE EXCEPTION 'benefit enrollment approval refused' USING ERRCODE='23514', CONSTRAINT=problem;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_proposal_decision BEFORE UPDATE ON workforce_benefit_enrollment_proposals FOR EACH ROW EXECUTE FUNCTION workforce_benefit_proposal_decision();

CREATE FUNCTION workforce_benefit_proposal_approved() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' AND NOT EXISTS(SELECT 1 FROM workforce_benefit_enrollments e WHERE (e.tenant_id,e.hospital_id,e.id,e.proposal_id)=(NEW.tenant_id,NEW.hospital_id,NEW.enrollment_id,NEW.id)) THEN
  RAISE EXCEPTION 'an approved benefit proposal must become the coverage in force' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_benefit_proposal_approved AFTER UPDATE ON workforce_benefit_enrollment_proposals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_benefit_proposal_approved();

-- Only approval writes coverage, copying exactly what the approved proposal said.
CREATE FUNCTION workforce_benefit_enrollment_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.workforce_benefit_approval',true),'')<>'authorized'
   OR (TG_OP='INSERT' AND (NEW.version<>1 OR NEW.original_plan_revision_id IS DISTINCT FROM (SELECT p.plan_revision_id FROM workforce_benefit_enrollment_proposals p WHERE (p.tenant_id,p.hospital_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.proposal_id))))
   OR (TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.staff_id,NEW.plan_id,NEW.original_plan_revision_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.staff_id,OLD.plan_id,OLD.original_plan_revision_id,OLD.created_at)
     OR NEW.dependent_id IS DISTINCT FROM OLD.dependent_id OR NEW.version<>OLD.version+1))
   OR NOT EXISTS(SELECT 1 FROM workforce_benefit_enrollment_proposals p WHERE (p.tenant_id,p.hospital_id,p.id,p.enrollment_id,p.staff_id,p.plan_id,p.coverage_from,p.status)
     =(NEW.tenant_id,NEW.hospital_id,NEW.proposal_id,NEW.id,NEW.staff_id,NEW.plan_id,NEW.coverage_from,'approved')
     AND p.dependent_id IS NOT DISTINCT FROM NEW.dependent_id AND p.coverage_until IS NOT DISTINCT FROM NEW.coverage_until) THEN
  RAISE EXCEPTION 'benefit coverage changes only by approving a proposal, one version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_enrollment_write BEFORE INSERT OR UPDATE ON workforce_benefit_enrollments FOR EACH ROW EXECUTE FUNCTION workforce_benefit_enrollment_write();

CREATE TRIGGER workforce_benefit_plan_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_benefit_plan_revisions FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
CREATE TRIGGER workforce_staff_dependent_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_staff_dependent_revisions FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_benefit_plans','workforce_benefit_plan_revisions','workforce_staff_dependents','workforce_staff_dependent_revisions','workforce_benefit_enrollment_proposals','workforce_benefit_enrollments'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY benefits_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_benefits_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_benefits_allowed(tenant_id,hospital_id,true))',tbl);
  EXECUTE format('CREATE TRIGGER workforce_benefits_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  EXECUTE format('CREATE TRIGGER workforce_benefits_audit AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()',tbl);
 END LOOP;
END $$;
