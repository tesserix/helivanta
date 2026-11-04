SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_posting_management_reviews (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 revision_id uuid NOT NULL,initial_revision_id uuid NOT NULL,
 hospital_management_reviewed boolean NOT NULL CHECK(hospital_management_reviewed),
 reviewer_subject text NOT NULL CHECK(length(reviewer_subject) BETWEEN 1 AND 200),
 reviewer_staff_id uuid NOT NULL,reviewer_link_id uuid NOT NULL,reviewer_generation_id uuid NOT NULL,
 review_assignment_id uuid NOT NULL,review_assignment_version bigint NOT NULL CHECK(review_assignment_version BETWEEN 1 AND 9007199254740990),
 review_scope_epoch bigint NOT NULL CHECK(review_scope_epoch BETWEEN 1 AND 9007199254740990),
 review_membership_epoch bigint NOT NULL CHECK(review_membership_epoch BETWEEN 1 AND 9007199254740990),
 management_assignment_id uuid NOT NULL,management_assignment_version bigint NOT NULL CHECK(management_assignment_version BETWEEN 1 AND 9007199254740990),
 management_scope_epoch bigint NOT NULL CHECK(management_scope_epoch=review_scope_epoch),
 management_membership_epoch bigint NOT NULL CHECK(management_membership_epoch=review_membership_epoch),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,revision_id),UNIQUE(tenant_id,hospital_id,staff_id,proposal_id),
 CHECK(review_assignment_id<>management_assignment_id),
 CHECK(substring(review_assignment_id::text,15,1)='7' AND substring(review_assignment_id::text,20,1) IN ('8','9','a','b')),
 CHECK(substring(management_assignment_id::text,15,1)='7' AND substring(management_assignment_id::text,20,1) IN ('8','9','a','b')),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_supervision_intents(tenant_id,hospital_id,staff_id,proposal_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,initial_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_link_id) REFERENCES workforce_login_links(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_generation_id) REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
ALTER TABLE workforce_posting_management_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_management_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_management_reviews
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
CREATE TRIGGER workforce_posting_management_review_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_management_reviews FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_posting_root_approval_context(t uuid,h uuid,s uuid,p uuid,initial uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_posting_proposal_decider(t,h,s,p,'approve')
 AND current_setting('app.workforce_posting_root_approval',true)='verified'
 AND NULLIF(current_setting('app.workforce_posting_management_assignment',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_management_assignment',true),'')::uuid<>NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid
 AND NULLIF(current_setting('app.workforce_posting_management_assignment_version',true),'')::bigint>0
 AND NULLIF(current_setting('app.workforce_posting_management_scope_epoch',true),'')::bigint=NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint
 AND NULLIF(current_setting('app.workforce_posting_management_membership_epoch',true),'')::bigint=NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint
 AND EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
 JOIN workforce_posting_supervision_intents i ON
 (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)
 JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(r.tenant_id,r.hospital_id,r.staff_id)
 WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(t,h,s,p,initial,1)
 AND r.supervision_requested AND i.kind='hospital_management' AND staff.employment_status='active'
 AND (staff.version,staff.employment_epoch)=(r.staff_version,r.employment_epoch)
 AND r.generation_id=NULLIF(current_setting('app.workforce_posting_target_generation',true),'')::uuid
 AND r.eligibility_until=NULLIF(current_setting('app.workforce_posting_target_until',true),'')::timestamptz
 AND r.department_id=NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 AND r.department_revision_id=NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid
 AND r.eligibility_until>clock_timestamp()),false)
$$;

CREATE FUNCTION workforce_posting_management_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_posting_root_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.initial_revision_id)
 OR (NEW.reviewer_subject,NEW.reviewer_staff_id,NEW.reviewer_link_id,NEW.reviewer_generation_id,NEW.review_assignment_id,NEW.review_assignment_version,NEW.review_scope_epoch,NEW.review_membership_epoch,NEW.management_assignment_id,NEW.management_assignment_version,NEW.management_scope_epoch,NEW.management_membership_epoch) IS DISTINCT FROM
 (current_setting('app.workforce_subject',true),NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_link',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_generation',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment_version',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_management_assignment',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_management_assignment_version',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_management_scope_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_management_membership_epoch',true),'')::bigint)
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version,r.status,r.actor_subject)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,2,'approved',NEW.reviewer_subject)
 AND r.creation_transaction=txid_current()) THEN
  RAISE EXCEPTION 'management review requires its current independent approval' USING ERRCODE='23514',CONSTRAINT='workforce_posting_management_guard';
 END IF;
 NEW.created_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_management_review_guard BEFORE INSERT ON workforce_posting_management_reviews FOR EACH ROW EXECUTE FUNCTION workforce_posting_management_review_guard();

CREATE FUNCTION workforce_posting_root_approval_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' AND NOT EXISTS(SELECT 1 FROM workforce_posting_management_reviews m
 WHERE (m.tenant_id,m.hospital_id,m.staff_id,m.proposal_id,m.revision_id)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.id)) THEN
  RAISE EXCEPTION 'approved root requires retained management evidence' USING ERRCODE='23514',CONSTRAINT='workforce_posting_management_integrity';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_root_approval_integrity AFTER INSERT ON workforce_posting_proposal_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_root_approval_integrity();

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
 IF NEW.status='approved' AND NOT workforce_posting_root_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,OLD.revision_id) THEN
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
  IF NEW.status='approved' AND NOT workforce_posting_root_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,prior.id) THEN
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
