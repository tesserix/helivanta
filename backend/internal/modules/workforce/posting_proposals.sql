SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_posting_proposals (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 revision_id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740990),
 draft_id uuid NOT NULL,draft_revision_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','withdrawn')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_posting_proposal_live_draft ON workforce_posting_proposals(tenant_id,hospital_id,staff_id,draft_id,draft_revision_id) WHERE status IN ('pending','approved');
CREATE INDEX ON workforce_posting_proposals(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE TABLE workforce_posting_proposal_revisions (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 command_id uuid NOT NULL CHECK(substring(command_id::text,15,1)='7' AND substring(command_id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740989),
 version bigint NOT NULL CHECK(version=expected_version+1),
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','withdrawn')),
 draft_id uuid NOT NULL,draft_revision_id uuid NOT NULL,draft_version bigint NOT NULL CHECK(draft_version BETWEEN 1 AND 9007199254740990),
 generation_id uuid NOT NULL,
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740990),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 2 AND 9007199254740990),
 department_id uuid NOT NULL,department_revision_id uuid NOT NULL,
 starts_at timestamptz NOT NULL CHECK(starts_at>'0001-01-01T00:00:00Z' AND starts_at<'10000-01-01T00:00:00Z'),
 ends_at timestamptz CHECK(ends_at>starts_at AND ends_at<'10000-01-01T00:00:00Z'),
 eligibility_until timestamptz NOT NULL CHECK(eligibility_until>starts_at AND eligibility_until<'10000-01-01T00:00:00Z'),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 sources_examined boolean NOT NULL CHECK(sources_examined),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,command_id),
 UNIQUE(tenant_id,hospital_id,staff_id,proposal_id,version),
 UNIQUE(tenant_id,hospital_id,staff_id,proposal_id,id,version,draft_id,draft_revision_id,status),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_proposals(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,hospital_id,staff_id,draft_id,draft_revision_id,draft_version) REFERENCES workforce_posting_draft_revisions(tenant_id,hospital_id,staff_id,posting_id,id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,generation_id) REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON workforce_posting_proposal_revisions(tenant_id,hospital_id,staff_id,proposal_id,created_at DESC,id DESC);
ALTER TABLE workforce_posting_proposals ADD CONSTRAINT workforce_posting_proposal_current_revision
 FOREIGN KEY(tenant_id,hospital_id,staff_id,id,revision_id,version,draft_id,draft_revision_id,status)
 REFERENCES workforce_posting_proposal_revisions(tenant_id,hospital_id,staff_id,proposal_id,id,version,draft_id,draft_revision_id,status) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION workforce_posting_proposal_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_scoped_posting_context(t,h,s,writing)
 AND current_setting('app.workforce_posting_review_mode',true) IN ('read','submit','approve','reject','withdraw')
 AND ((NOT writing AND current_setting('app.workforce_posting_permission',true) IN ('workforce.posting.proposal.read','workforce.posting.submit','workforce.posting.review'))
 OR (writing AND ((current_setting('app.workforce_posting_review_mode',true) IN ('submit','withdraw')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.submit')
 OR (current_setting('app.workforce_posting_review_mode',true) IN ('approve','reject')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.review')))),false)
$$;
ALTER TABLE workforce_posting_proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_proposals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_proposals
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
ALTER TABLE workforce_posting_proposal_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_proposal_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_proposal_revisions
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));

CREATE FUNCTION workforce_posting_association_context(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND workforce_posting_proposal_context(t,h,NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid,true)
 AND current_setting('app.workforce_posting_review_mode',true) IN ('approve','reject')
 AND s IN (NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid)
 AND NULLIF(current_setting('app.workforce_posting_proposal',true),'')::uuid IS NOT NULL,false)
$$;
DO $migration$ DECLARE prior record;BEGIN
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
 FROM pg_policy p WHERE p.polrelid='public.workforce_login_links'::regclass AND p.polpermissive;
 EXECUTE format('CREATE POLICY posting_decision_link_delete_guard ON workforce_login_links AS RESTRICTIVE FOR DELETE USING (%s)',prior.read_expression);
 EXECUTE format('ALTER POLICY %I ON workforce_login_links USING ((%s) OR workforce_posting_association_context(tenant_id,hospital_id,staff_id))',prior.polname,prior.read_expression);
END $migration$;

CREATE FUNCTION workforce_posting_proposal_decider(t uuid,h uuid,s uuid,p uuid,decision text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 WITH original AS (
  SELECT r.actor_subject,r.draft_id FROM workforce_posting_proposal_revisions r
  WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.version)=(t,h,s,p,1)
 ), contributors AS (
  SELECT actor_subject AS subject FROM original
  UNION SELECT r.actor_subject FROM workforce_posting_draft_revisions r JOIN original o ON r.posting_id=o.draft_id
  WHERE (r.tenant_id,r.hospital_id,r.staff_id)=(t,h,s)
 )
 SELECT COALESCE(workforce_posting_proposal_context(t,h,s,true)
 AND p=NULLIF(current_setting('app.workforce_posting_proposal',true),'')::uuid
 AND decision=current_setting('app.workforce_posting_review_mode',true)
 AND EXISTS(SELECT 1 FROM original)
 AND CASE WHEN decision='withdraw' THEN EXISTS(SELECT 1 FROM original WHERE actor_subject=current_setting('app.workforce_subject',true))
 ELSE decision IN ('approve','reject')
 AND NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid<>s
 AND current_setting('app.workforce_generation',true)='read'
 AND NULLIF(current_setting('app.workforce_generation_staff',true),'')::uuid=s
 AND NOT EXISTS(SELECT 1 FROM contributors WHERE subject=current_setting('app.workforce_subject',true))
 AND NOT EXISTS(SELECT 1 FROM workforce_employment_generations g WHERE (g.tenant_id,g.hospital_id,g.staff_id)=(t,h,s) AND g.declared_subject=current_setting('app.workforce_subject',true))
 AND NOT EXISTS(SELECT 1 FROM workforce_login_links l WHERE (l.tenant_id,l.hospital_id)=(t,h)
 AND ((l.staff_id=s AND l.subject=current_setting('app.workforce_subject',true))
 OR (l.staff_id=NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid AND l.subject IN (SELECT subject FROM contributors))))
 END,false)
$$;

CREATE FUNCTION workforce_posting_proposal_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
   OR NEW.status IS DISTINCT FROM (CASE current_setting('app.workforce_posting_review_mode',true) WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' ELSE '' END)
   OR NOT workforce_posting_proposal_decider(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,current_setting('app.workforce_posting_review_mode',true)) THEN
   RAISE EXCEPTION 'invalid posting proposal decision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
  END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_proposal_head_guard BEFORE INSERT OR UPDATE ON workforce_posting_proposals FOR EACH ROW EXECUTE FUNCTION workforce_posting_proposal_head_guard();

CREATE FUNCTION workforce_posting_proposal_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
   OR NEW.status IS DISTINCT FROM (CASE current_setting('app.workforce_posting_review_mode',true) WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' ELSE '' END)
   OR NOT workforce_posting_proposal_decider(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,current_setting('app.workforce_posting_review_mode',true)) THEN
   RAISE EXCEPTION 'invalid posting proposal decision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
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
CREATE TRIGGER workforce_posting_proposal_revision_guard BEFORE INSERT ON workforce_posting_proposal_revisions FOR EACH ROW EXECUTE FUNCTION workforce_posting_proposal_revision_guard();

CREATE FUNCTION workforce_posting_proposal_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proposal uuid;
BEGIN
 IF TG_TABLE_NAME='workforce_posting_proposals' THEN proposal:=NEW.id; ELSE proposal:=NEW.proposal_id; END IF;
 IF EXISTS(SELECT 1 FROM workforce_posting_proposals p JOIN workforce_posting_proposal_revisions r ON(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id)=(p.tenant_id,p.hospital_id,p.staff_id,p.id) WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.staff_id=NEW.staff_id AND p.id=proposal AND r.version>p.version) THEN
  RAISE EXCEPTION 'posting proposal head must retain its latest revision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_proposal_guard';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_proposal_head_integrity AFTER INSERT OR UPDATE ON workforce_posting_proposals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_proposal_integrity();
CREATE CONSTRAINT TRIGGER workforce_posting_proposal_revision_integrity AFTER INSERT ON workforce_posting_proposal_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_proposal_integrity();
CREATE TRIGGER workforce_posting_proposal_preserve BEFORE DELETE OR TRUNCATE ON workforce_posting_proposals FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_posting_proposal_revision_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_proposal_revisions FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
