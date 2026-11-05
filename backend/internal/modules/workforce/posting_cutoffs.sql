SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_posting_cutoff_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_scoped_posting_context(t,h,s,writing)
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.cutoff'
 AND current_setting('app.workforce_posting_cutoff',true) IN ('read','write')
 AND (NOT writing OR current_setting('app.workforce_posting_cutoff',true)='write'),false)
$$;

CREATE TABLE workforce_posting_cutoffs (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_revision_id uuid NOT NULL,
 expected_cutoff_version bigint NOT NULL CHECK(expected_cutoff_version BETWEEN 0 AND 9007199254740989),
 version bigint NOT NULL CHECK(version=expected_cutoff_version+1),
 mode text NOT NULL CHECK(mode IN ('immediate','scheduled')),
 requested_cutoff_at timestamptz,
 cutoff_at timestamptz NOT NULL CHECK(cutoff_at>'0001-01-01T00:00:00Z' AND cutoff_at<'10000-01-01T00:00:00Z'),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 sources_examined boolean NOT NULL CHECK(sources_examined),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 actor_staff_id uuid NOT NULL,actor_link_id uuid NOT NULL,actor_generation_id uuid NOT NULL,
 assignment_id uuid NOT NULL,assignment_version bigint NOT NULL CHECK(assignment_version>0),
 scope_epoch bigint NOT NULL CHECK(scope_epoch>0),membership_epoch bigint NOT NULL CHECK(membership_epoch>0),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,proposal_id,version),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_proposals(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,expected_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,actor_staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,actor_link_id) REFERENCES workforce_login_links(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,actor_generation_id) REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK(CASE WHEN mode='immediate' THEN requested_cutoff_at IS NULL AND cutoff_at=recorded_at
 ELSE requested_cutoff_at IS NOT NULL AND requested_cutoff_at=cutoff_at AND requested_cutoff_at>recorded_at END)
);
CREATE INDEX ON workforce_posting_cutoffs(tenant_id,expected_revision_id);
CREATE INDEX ON workforce_posting_cutoffs(tenant_id,hospital_id,actor_staff_id);
CREATE INDEX ON workforce_posting_cutoffs(tenant_id,hospital_id,actor_link_id);
CREATE INDEX ON workforce_posting_cutoffs(tenant_id,hospital_id,actor_generation_id);
CREATE INDEX workforce_posting_cutoff_history ON workforce_posting_cutoffs(tenant_id,hospital_id,staff_id,proposal_id,recorded_at DESC,id DESC);

ALTER TABLE workforce_posting_cutoffs ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_cutoffs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_cutoffs
 USING(hms_tenant_visible(tenant_id) AND
 (workforce_posting_cutoff_context(tenant_id,hospital_id,staff_id,false)
 OR workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false)
 OR workforce_posting_supervisor_choices_context(tenant_id,hospital_id)))
 WITH CHECK(workforce_posting_cutoff_context(tenant_id,hospital_id,staff_id,true));
CREATE TRIGGER workforce_posting_cutoffs_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_cutoffs FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

DO $migration$
DECLARE item text; prior record;
BEGIN
 FOREACH item IN ARRAY ARRAY['workforce_posting_proposals','workforce_posting_proposal_revisions'] LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
  FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item))::regclass AND p.polpermissive;
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR workforce_posting_cutoff_context(tenant_id,hospital_id,staff_id,false))',prior.polname,item,prior.read_expression);
  EXECUTE format('CREATE POLICY cutoff_read_no_update ON %I AS RESTRICTIVE FOR UPDATE USING (NOT workforce_posting_cutoff_context(tenant_id,hospital_id,staff_id,false))',item);
  EXECUTE format('CREATE POLICY cutoff_read_no_delete ON %I AS RESTRICTIVE FOR DELETE USING (NOT workforce_posting_cutoff_context(tenant_id,hospital_id,staff_id,false))',item);
 END LOOP;
END $migration$;

CREATE FUNCTION workforce_posting_cutoff_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous_version bigint; previous_cutoff timestamptz; observed timestamptz;
BEGIN
 IF NOT workforce_posting_cutoff_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR NEW.proposal_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_proposal',true),'')::uuid
 OR NEW.expected_revision_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_expected_revision',true),'')::uuid
 OR (NEW.actor_subject,NEW.actor_staff_id,NEW.actor_link_id,NEW.actor_generation_id,NEW.assignment_id,NEW.assignment_version,NEW.scope_epoch,NEW.membership_epoch) IS DISTINCT FROM
 (current_setting('app.workforce_subject',true),NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_link',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_generation',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment_version',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint) THEN
  RAISE EXCEPTION 'posting cutoff requires its current scoped owner actor' USING ERRCODE='42501';
 END IF;
 PERFORM workforce_lock_supervision(NEW.tenant_id,NEW.hospital_id);
 IF NOT EXISTS(SELECT 1 FROM workforce_posting_proposals p JOIN workforce_posting_proposal_revisions r ON
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version,r.status)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.status)
 WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.status)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.expected_revision_id,'approved')) THEN
  RAISE EXCEPTION 'posting approval changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_cutoff_guard';
 END IF;
 SELECT c.version,c.cutoff_at INTO previous_version,previous_cutoff FROM workforce_posting_cutoffs c
 WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.proposal_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id)
 ORDER BY c.version DESC LIMIT 1;
 observed:=clock_timestamp();
 NEW.recorded_at:=observed;
 IF NEW.mode='immediate' AND NEW.requested_cutoff_at IS NULL THEN
  NEW.cutoff_at:=observed;
 ELSIF NEW.mode='scheduled' AND NEW.requested_cutoff_at>observed THEN
  NEW.cutoff_at:=NEW.requested_cutoff_at;
 ELSE
  RAISE EXCEPTION 'invalid posting cutoff instant' USING ERRCODE='23514',CONSTRAINT='workforce_posting_cutoff_guard';
 END IF;
 IF NEW.expected_cutoff_version<>COALESCE(previous_version,0)
 OR (previous_cutoff IS NOT NULL AND NEW.cutoff_at>=previous_cutoff) THEN
  RAISE EXCEPTION 'posting cutoff cannot extend or replace stale intent' USING ERRCODE='23514',CONSTRAINT='workforce_posting_cutoff_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_cutoff_guard BEFORE INSERT ON workforce_posting_cutoffs FOR EACH ROW EXECUTE FUNCTION workforce_posting_cutoff_guard();
