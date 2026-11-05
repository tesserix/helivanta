SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_posting_chain_reviews (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 revision_id uuid NOT NULL,initial_revision_id uuid NOT NULL,
 reviewer_subject text NOT NULL CHECK(length(reviewer_subject) BETWEEN 1 AND 200),
 ancestor_count smallint NOT NULL CHECK(ancestor_count BETWEEN 1 AND 31),
 eligibility_until timestamptz NOT NULL,
 creation_transaction bigint NOT NULL DEFAULT txid_current(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,revision_id),UNIQUE(tenant_id,hospital_id,staff_id,proposal_id),
 UNIQUE(tenant_id,hospital_id,staff_id,proposal_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_supervision_intents(tenant_id,hospital_id,staff_id,proposal_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,initial_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT
);

CREATE TABLE workforce_posting_chain_sources (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,revision_id uuid NOT NULL,
 hop smallint NOT NULL CHECK(hop BETWEEN 1 AND 31),
 ancestor_staff_id uuid NOT NULL CHECK(ancestor_staff_id<>staff_id),
 ancestor_proposal_id uuid NOT NULL CHECK(ancestor_proposal_id<>proposal_id),ancestor_revision_id uuid NOT NULL,
 generation_id uuid NOT NULL,staff_version bigint NOT NULL CHECK(staff_version>0),employment_epoch bigint NOT NULL CHECK(employment_epoch>=2),
 kind text NOT NULL CHECK(kind IN ('hospital_management','department_supervisor')),
 department_id uuid NOT NULL,department_revision_id uuid NOT NULL,
 starts_at timestamptz NOT NULL,ends_at timestamptz CHECK(ends_at>starts_at),eligibility_until timestamptz NOT NULL CHECK(eligibility_until>starts_at),
 supervisor_staff_id uuid,supervisor_proposal_id uuid,supervisor_revision_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,revision_id,hop),
 UNIQUE(tenant_id,revision_id,ancestor_staff_id),UNIQUE(tenant_id,revision_id,ancestor_proposal_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id,revision_id) REFERENCES workforce_posting_chain_reviews(tenant_id,hospital_id,staff_id,proposal_id,revision_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,ancestor_staff_id,ancestor_proposal_id) REFERENCES workforce_posting_proposals(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,ancestor_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,generation_id) REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK(CASE WHEN kind='hospital_management' THEN supervisor_staff_id IS NULL AND supervisor_proposal_id IS NULL AND supervisor_revision_id IS NULL
 ELSE supervisor_staff_id IS NOT NULL AND supervisor_proposal_id IS NOT NULL AND supervisor_revision_id IS NOT NULL END)
);

CREATE INDEX workforce_posting_chain_reviews_initial ON workforce_posting_chain_reviews(tenant_id,initial_revision_id);
CREATE INDEX workforce_posting_chain_sources_ancestor ON workforce_posting_chain_sources(tenant_id,hospital_id,ancestor_staff_id,ancestor_proposal_id,ancestor_revision_id);
CREATE INDEX workforce_posting_chain_sources_revision ON workforce_posting_chain_sources(tenant_id,ancestor_revision_id);
CREATE INDEX workforce_posting_chain_sources_generation ON workforce_posting_chain_sources(tenant_id,hospital_id,generation_id);

ALTER TABLE workforce_posting_chain_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_chain_reviews FORCE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_chain_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_chain_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_chain_reviews
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
CREATE POLICY tenant_isolation ON workforce_posting_chain_sources
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
CREATE TRIGGER workforce_posting_chain_reviews_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_chain_reviews FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_posting_chain_sources_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_chain_sources FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_posting_chain_approval_context(t uuid,h uuid,s uuid,p uuid,initial uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_posting_proposal_decider(t,h,s,p,'approve')
 AND current_setting('app.workforce_posting_chain_approval',true)='verified'
 AND NULLIF(current_setting('app.workforce_posting_chain_until',true),'')::timestamptz>clock_timestamp()
 AND EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
 JOIN workforce_posting_supervision_intents i ON (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)
 JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(r.tenant_id,r.hospital_id,r.staff_id)
 WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(t,h,s,p,initial,1)
 AND r.supervision_requested AND i.kind IN ('department_supervisor','department_member') AND staff.employment_status='active'
 AND (staff.version,staff.employment_epoch)=(r.staff_version,r.employment_epoch)
 AND r.generation_id=NULLIF(current_setting('app.workforce_posting_target_generation',true),'')::uuid
 AND r.eligibility_until=NULLIF(current_setting('app.workforce_posting_target_until',true),'')::timestamptz
 AND r.department_id=NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 AND r.department_revision_id=NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid
 AND NULLIF(current_setting('app.workforce_posting_chain_until',true),'')::timestamptz<=r.eligibility_until),false)
$$;

CREATE FUNCTION workforce_posting_approval_context(t uuid,h uuid,s uuid,p uuid,initial uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_posting_root_approval_context(t,h,s,p,initial) OR workforce_posting_chain_approval_context(t,h,s,p,initial)
$$;

CREATE FUNCTION workforce_posting_chain_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_posting_chain_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.initial_revision_id)
 OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.workforce_subject',true)
 OR NEW.creation_transaction IS DISTINCT FROM txid_current()
 OR NEW.eligibility_until IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_chain_until',true),'')::timestamptz
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version,r.status,r.actor_subject,r.creation_transaction)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,2,'approved',NEW.reviewer_subject,txid_current())) THEN
 RAISE EXCEPTION 'supervision review requires its current independent approval' USING ERRCODE='23514',CONSTRAINT='workforce_posting_chain_guard';
 END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_chain_review_guard BEFORE INSERT ON workforce_posting_chain_reviews FOR EACH ROW EXECUTE FUNCTION workforce_posting_chain_review_guard();

CREATE FUNCTION workforce_posting_chain_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous_staff text; ancestor record; intent record; has_evidence boolean;
BEGIN
 IF current_setting('app.workforce_posting_chain_approval',true) IS DISTINCT FROM 'verified'
 OR NOT workforce_posting_proposal_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_chain_reviews r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.revision_id,r.creation_transaction)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,txid_current()) AND NEW.hop<=r.ancestor_count) THEN
 RAISE EXCEPTION 'chain evidence requires its current child approval' USING ERRCODE='23514',CONSTRAINT='workforce_posting_chain_guard';
 END IF;
 previous_staff:=current_setting('app.workforce_posting_staff',true);
 PERFORM set_config('app.workforce_posting_staff',NEW.ancestor_staff_id::text,true);
 SELECT r.*,s.version AS current_staff_version,s.employment_epoch AS current_epoch,s.employment_status INTO ancestor
 FROM workforce_posting_proposals p JOIN workforce_posting_proposal_revisions r
 ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id)
 JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(p.tenant_id,p.hospital_id,p.staff_id)
 WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.status)=
 (NEW.tenant_id,NEW.hospital_id,NEW.ancestor_staff_id,NEW.ancestor_proposal_id,NEW.ancestor_revision_id,'approved') FOR SHARE OF s;
 SELECT * INTO intent FROM workforce_posting_supervision_intents i WHERE
 (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id)=(NEW.tenant_id,NEW.hospital_id,NEW.ancestor_staff_id,NEW.ancestor_proposal_id);
 IF NEW.kind='hospital_management' THEN
 SELECT EXISTS(SELECT 1 FROM workforce_posting_management_reviews m WHERE (m.tenant_id,m.hospital_id,m.staff_id,m.proposal_id,m.revision_id)=
 (NEW.tenant_id,NEW.hospital_id,NEW.ancestor_staff_id,NEW.ancestor_proposal_id,NEW.ancestor_revision_id)) INTO has_evidence;
 ELSE
 SELECT EXISTS(SELECT 1 FROM workforce_posting_chain_reviews m WHERE (m.tenant_id,m.hospital_id,m.staff_id,m.proposal_id,m.revision_id)=
 (NEW.tenant_id,NEW.hospital_id,NEW.ancestor_staff_id,NEW.ancestor_proposal_id,NEW.ancestor_revision_id)) INTO has_evidence;
 END IF;
 PERFORM set_config('app.workforce_posting_staff',previous_staff,true);
 IF ancestor.id IS NULL OR ancestor.employment_status IS DISTINCT FROM 'active' OR NOT has_evidence
 OR (ancestor.generation_id,ancestor.staff_version,ancestor.employment_epoch,ancestor.department_id,ancestor.department_revision_id,ancestor.starts_at,ancestor.ends_at,ancestor.eligibility_until) IS DISTINCT FROM
 (NEW.generation_id,NEW.staff_version,NEW.employment_epoch,NEW.department_id,NEW.department_revision_id,NEW.starts_at,NEW.ends_at,NEW.eligibility_until)
 OR (ancestor.current_staff_version,ancestor.current_epoch) IS DISTINCT FROM (NEW.staff_version,NEW.employment_epoch)
 OR intent.kind IS DISTINCT FROM NEW.kind
 OR (intent.supervisor_staff_id,intent.supervisor_proposal_id,intent.supervisor_revision_id) IS DISTINCT FROM (NEW.supervisor_staff_id,NEW.supervisor_proposal_id,NEW.supervisor_revision_id)
 OR NEW.eligibility_until<=clock_timestamp() THEN
 RAISE EXCEPTION 'supervisor source changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_chain_guard';
 END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_chain_source_guard BEFORE INSERT ON workforce_posting_chain_sources FOR EACH ROW EXECUTE FUNCTION workforce_posting_chain_source_guard();

CREATE FUNCTION workforce_posting_chain_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total integer;first_hop integer;last_hop integer;cutoff timestamptz;child record;intent record;first_source record;
BEGIN
 SELECT r.* INTO STRICT child FROM workforce_posting_proposal_revisions r WHERE (r.tenant_id,r.id)=(NEW.tenant_id,NEW.revision_id);
 SELECT * INTO STRICT intent FROM workforce_posting_supervision_intents i WHERE (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id);
 SELECT count(*),min(hop),max(hop),min(eligibility_until) INTO total,first_hop,last_hop,cutoff FROM workforce_posting_chain_sources s WHERE (s.tenant_id,s.revision_id)=(NEW.tenant_id,NEW.revision_id);
 SELECT * INTO first_source FROM workforce_posting_chain_sources s WHERE (s.tenant_id,s.revision_id,s.hop)=(NEW.tenant_id,NEW.revision_id,1);
 IF total<>NEW.ancestor_count OR first_hop IS DISTINCT FROM 1 OR last_hop IS DISTINCT FROM NEW.ancestor_count
 OR intent.kind NOT IN ('department_supervisor','department_member')
 OR (intent.supervisor_staff_id,intent.supervisor_proposal_id,intent.supervisor_revision_id) IS DISTINCT FROM
 (first_source.ancestor_staff_id,first_source.ancestor_proposal_id,first_source.ancestor_revision_id)
 OR NEW.eligibility_until IS DISTINCT FROM LEAST(child.eligibility_until,cutoff) OR NEW.eligibility_until<=clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_chain_sources s WHERE (s.tenant_id,s.revision_id,s.hop,s.kind)=(NEW.tenant_id,NEW.revision_id,NEW.ancestor_count,'hospital_management'))
 OR EXISTS(SELECT 1 FROM workforce_posting_chain_sources s LEFT JOIN workforce_posting_chain_sources next ON (next.tenant_id,next.revision_id,next.hop)=(s.tenant_id,s.revision_id,s.hop+1)
 WHERE (s.tenant_id,s.revision_id)=(NEW.tenant_id,NEW.revision_id) AND
 ((s.hop<NEW.ancestor_count AND (s.kind<>'department_supervisor' OR (s.supervisor_staff_id,s.supervisor_proposal_id,s.supervisor_revision_id) IS DISTINCT FROM (next.ancestor_staff_id,next.ancestor_proposal_id,next.ancestor_revision_id)))
 OR (s.hop=1 AND (child.starts_at<s.starts_at OR (s.ends_at IS NOT NULL AND (child.ends_at IS NULL OR child.ends_at>s.ends_at))))
 OR (next.hop IS NOT NULL AND (s.starts_at<next.starts_at OR (next.ends_at IS NOT NULL AND (s.ends_at IS NULL OR s.ends_at>next.ends_at)))))) THEN
 RAISE EXCEPTION 'approved child requires a complete exact supervisor chain' USING ERRCODE='23514',CONSTRAINT='workforce_posting_chain_integrity';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_chain_integrity AFTER INSERT ON workforce_posting_chain_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_chain_integrity();

CREATE OR REPLACE FUNCTION workforce_posting_root_approval_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' AND NOT EXISTS(SELECT 1 FROM workforce_posting_management_reviews m
 WHERE (m.tenant_id,m.hospital_id,m.staff_id,m.proposal_id,m.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.id))
 AND NOT EXISTS(SELECT 1 FROM workforce_posting_chain_reviews m
 WHERE (m.tenant_id,m.hospital_id,m.staff_id,m.proposal_id,m.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.id)) THEN
 RAISE EXCEPTION 'approved posting requires retained supervision evidence' USING ERRCODE='23514',CONSTRAINT='workforce_posting_management_integrity';
 END IF;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION workforce_posting_proposal_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_posting_proposal_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true) THEN
  RAISE EXCEPTION 'posting proposal requires current scoped owner context' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.status<>'pending' OR current_setting('app.workforce_posting_review_mode',true)<>'submit' THEN
   RAISE EXCEPTION 'invalid initial posting proposal' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
  END IF;
  NEW.created_at:=clock_timestamp();
 ELSE
  IF OLD.status<>'pending' OR NEW.version<>OLD.version+1
   OR (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.draft_id,NEW.draft_revision_id,NEW.created_at) IS DISTINCT FROM
      (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,OLD.draft_id,OLD.draft_revision_id,OLD.created_at)
   OR OLD.revision_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_expected_revision',true),'')::uuid
   OR NEW.status IS DISTINCT FROM (CASE current_setting('app.workforce_posting_review_mode',true) WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' WHEN 'approve' THEN 'approved' ELSE '' END)
   OR NOT workforce_posting_proposal_decider(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,current_setting('app.workforce_posting_review_mode',true)) THEN
   RAISE EXCEPTION 'invalid posting proposal decision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
  END IF;
 END IF;
 IF NEW.status='approved' AND NOT workforce_posting_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,OLD.revision_id) THEN
  RAISE EXCEPTION 'management approval source changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;


CREATE OR REPLACE FUNCTION workforce_posting_proposal_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source record;draft record;generation record;prior record;
BEGIN
 IF NOT workforce_posting_proposal_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') THEN
  RAISE EXCEPTION 'posting proposal requires current scoped owner context' USING ERRCODE='42501';
 END IF;
 IF NEW.version>1 THEN
  SELECT id,status,draft_id,draft_revision_id,draft_version,generation_id,staff_version,employment_epoch,department_id,department_revision_id,starts_at,ends_at,eligibility_until INTO prior FROM workforce_posting_proposal_revisions WHERE
   (tenant_id,hospital_id,staff_id,proposal_id,id,version)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NULLIF(current_setting('app.workforce_posting_expected_revision',true),'')::uuid,NEW.expected_version);
  IF prior.id IS NULL OR prior.status<>'pending'
   OR (NEW.draft_id,NEW.draft_revision_id,NEW.draft_version,NEW.generation_id,NEW.staff_version,NEW.employment_epoch,NEW.department_id,NEW.department_revision_id,NEW.starts_at,NEW.ends_at,NEW.eligibility_until) IS DISTINCT FROM
      (prior.draft_id,prior.draft_revision_id,prior.draft_version,prior.generation_id,prior.staff_version,prior.employment_epoch,prior.department_id,prior.department_revision_id,prior.starts_at,prior.ends_at,prior.eligibility_until)
   OR NEW.status IS DISTINCT FROM (CASE current_setting('app.workforce_posting_review_mode',true) WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' WHEN 'approve' THEN 'approved' ELSE '' END)
   OR NOT workforce_posting_proposal_decider(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,current_setting('app.workforce_posting_review_mode',true)) THEN
   RAISE EXCEPTION 'invalid posting proposal decision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
  END IF;
  IF NEW.status='approved' AND NOT workforce_posting_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,prior.id) THEN
   RAISE EXCEPTION 'management approval source changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.status<>'pending' OR current_setting('app.workforce_posting_review_mode',true)<>'submit'
 OR NEW.generation_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_target_generation',true),'')::uuid
 OR NEW.eligibility_until IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_target_until',true),'')::timestamptz
 OR NEW.department_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 OR NEW.department_revision_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid THEN
  RAISE EXCEPTION 'posting proposal requires its exact reviewed sources' USING ERRCODE='42501';
 END IF;
 SELECT version,employment_epoch,employment_status INTO source FROM workforce_staff WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.staff_id FOR SHARE;
 SELECT r.*,d.revision_id AS current_revision INTO draft FROM workforce_posting_draft_revisions r JOIN workforce_posting_drafts d
 ON(d.tenant_id,d.hospital_id,d.staff_id,d.id)=(r.tenant_id,r.hospital_id,r.staff_id,r.posting_id)
 WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.posting_id=NEW.draft_id AND r.id=NEW.draft_revision_id AND r.version=NEW.draft_version;
 SELECT staff_id,employment_epoch,starts_at,ends_at INTO generation FROM workforce_employment_generations WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.generation_id;
 IF source.version IS DISTINCT FROM NEW.staff_version OR source.employment_epoch IS DISTINCT FROM NEW.employment_epoch OR source.employment_status IS DISTINCT FROM 'active'
 OR generation.staff_id IS DISTINCT FROM NEW.staff_id OR generation.employment_epoch IS DISTINCT FROM NEW.employment_epoch
 OR generation.starts_at IS NULL OR NEW.starts_at<generation.starts_at OR (generation.ends_at IS NOT NULL AND (NEW.ends_at IS NULL OR NEW.ends_at>generation.ends_at))
 OR draft.id IS DISTINCT FROM NEW.draft_revision_id OR draft.current_revision IS DISTINCT FROM NEW.draft_revision_id
 OR (draft.staff_version,draft.employment_epoch,draft.department_id,draft.department_revision_id,draft.starts_at,draft.ends_at) IS DISTINCT FROM
 (NEW.staff_version,NEW.employment_epoch,NEW.department_id,NEW.department_revision_id,NEW.starts_at,NEW.ends_at)
 OR NEW.eligibility_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'posting proposal source changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
 END IF;
 RETURN NEW;
END $$;
