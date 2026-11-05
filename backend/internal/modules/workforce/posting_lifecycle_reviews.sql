SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_posting_lifecycle_reviews (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 revision_id uuid NOT NULL,initial_revision_id uuid NOT NULL,
 reviewer_subject text NOT NULL CHECK(length(reviewer_subject) BETWEEN 1 AND 200),
 reviewer_staff_id uuid NOT NULL,reviewer_link_id uuid NOT NULL,reviewer_generation_id uuid NOT NULL,
 review_assignment_id uuid NOT NULL,review_assignment_version bigint NOT NULL CHECK(review_assignment_version BETWEEN 1 AND 9007199254740990),
 scope_epoch bigint NOT NULL CHECK(scope_epoch BETWEEN 1 AND 9007199254740990),
 membership_epoch bigint NOT NULL CHECK(membership_epoch BETWEEN 1 AND 9007199254740990),
 support_permission text NOT NULL CHECK(support_permission IN ('workforce.posting.read','workforce.posting.management.review')),
 support_assignment_id uuid NOT NULL,support_assignment_version bigint NOT NULL CHECK(support_assignment_version BETWEEN 1 AND 9007199254740990),
 source_count smallint NOT NULL CHECK(source_count BETWEEN 1 AND 32),
 checked_at timestamptz NOT NULL,effective_until timestamptz NOT NULL CHECK(effective_until>checked_at),
 creation_transaction bigint NOT NULL DEFAULT txid_current(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,revision_id),UNIQUE(tenant_id,hospital_id,staff_id,proposal_id),
 UNIQUE(tenant_id,hospital_id,staff_id,proposal_id,revision_id),
 CHECK(checked_at>'0001-01-01T00:00:00Z' AND effective_until<'10000-01-01T00:00:00Z'),
 CHECK((source_count=1 AND support_permission='workforce.posting.management.review' AND review_assignment_id<>support_assignment_id)
 OR (source_count>1 AND support_permission='workforce.posting.read')),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_supervision_intents(tenant_id,hospital_id,staff_id,proposal_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,initial_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_link_id) REFERENCES workforce_login_links(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,reviewer_generation_id) REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_posting_lifecycle_sources (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,revision_id uuid NOT NULL,
 hop smallint NOT NULL CHECK(hop BETWEEN 0 AND 31),source_staff_id uuid NOT NULL,source_proposal_id uuid NOT NULL,source_revision_id uuid NOT NULL,
 cutoff_id uuid,cutoff_version bigint NOT NULL CHECK(cutoff_version BETWEEN 0 AND 9007199254740990),cutoff_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,revision_id,hop),UNIQUE(tenant_id,revision_id,source_staff_id),UNIQUE(tenant_id,revision_id,source_proposal_id),
 CHECK((cutoff_version=0 AND cutoff_id IS NULL AND cutoff_at IS NULL) OR (cutoff_version>0 AND cutoff_id IS NOT NULL AND cutoff_at IS NOT NULL AND cutoff_at>'0001-01-01T00:00:00Z' AND cutoff_at<'10000-01-01T00:00:00Z')),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id,revision_id) REFERENCES workforce_posting_lifecycle_reviews(tenant_id,hospital_id,staff_id,proposal_id,revision_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,source_staff_id,source_proposal_id) REFERENCES workforce_posting_proposals(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,source_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,cutoff_id) REFERENCES workforce_posting_cutoffs(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON workforce_posting_lifecycle_reviews(tenant_id,initial_revision_id);
CREATE INDEX ON workforce_posting_lifecycle_reviews(tenant_id,hospital_id,reviewer_staff_id);
CREATE INDEX ON workforce_posting_lifecycle_reviews(tenant_id,hospital_id,reviewer_link_id);
CREATE INDEX ON workforce_posting_lifecycle_reviews(tenant_id,hospital_id,reviewer_generation_id);
CREATE INDEX ON workforce_posting_lifecycle_sources(tenant_id,hospital_id,source_staff_id,source_proposal_id);
CREATE INDEX ON workforce_posting_lifecycle_sources(tenant_id,source_revision_id);
CREATE INDEX ON workforce_posting_lifecycle_sources(tenant_id,cutoff_id);

ALTER TABLE workforce_posting_lifecycle_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_lifecycle_reviews FORCE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_lifecycle_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_lifecycle_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_lifecycle_reviews
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
CREATE POLICY tenant_isolation ON workforce_posting_lifecycle_sources
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));
CREATE TRIGGER workforce_posting_lifecycle_reviews_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_lifecycle_reviews FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_posting_lifecycle_sources_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_lifecycle_sources FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_posting_lifecycle_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.workforce_posting_lifecycle_approval',true) IS DISTINCT FROM 'verified'
 OR NOT workforce_posting_approval_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.initial_revision_id)
 OR NEW.creation_transaction IS DISTINCT FROM txid_current()
 OR NEW.checked_at>clock_timestamp() OR NEW.effective_until<=clock_timestamp()
 OR (NEW.reviewer_subject,NEW.reviewer_staff_id,NEW.reviewer_link_id,NEW.reviewer_generation_id,NEW.review_assignment_id,NEW.review_assignment_version,NEW.scope_epoch,NEW.membership_epoch) IS DISTINCT FROM
 (current_setting('app.workforce_subject',true),NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_link',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_generation',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_assignment_version',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint)
 OR (NEW.support_permission,NEW.support_assignment_id,NEW.support_assignment_version,NEW.scope_epoch,NEW.membership_epoch) IS DISTINCT FROM
 (current_setting('app.workforce_posting_lifecycle_support_permission',true),NULLIF(current_setting('app.workforce_posting_lifecycle_support_assignment',true),'')::uuid,NULLIF(current_setting('app.workforce_posting_lifecycle_support_version',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_lifecycle_support_scope_epoch',true),'')::bigint,NULLIF(current_setting('app.workforce_posting_lifecycle_support_membership_epoch',true),'')::bigint)
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version,r.status,r.actor_subject,r.creation_transaction)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,2,'approved',NEW.reviewer_subject,txid_current())) THEN
 RAISE EXCEPTION 'lifecycle review requires its current independent approval' USING ERRCODE='23514',CONSTRAINT='workforce_posting_lifecycle_guard';
 END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_lifecycle_review_guard BEFORE INSERT ON workforce_posting_lifecycle_reviews FOR EACH ROW EXECUTE FUNCTION workforce_posting_lifecycle_review_guard();

CREATE FUNCTION workforce_posting_lifecycle_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review record;cutoff record;previous_staff text;
BEGIN
 SELECT * INTO review FROM workforce_posting_lifecycle_reviews r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.revision_id,r.creation_transaction)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,txid_current());
 IF review.revision_id IS NULL OR NEW.hop>=review.source_count
 OR current_setting('app.workforce_posting_lifecycle_approval',true) IS DISTINCT FROM 'verified'
 OR NOT workforce_posting_proposal_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR (NEW.hop=0 AND (NEW.source_staff_id,NEW.source_proposal_id,NEW.source_revision_id) IS DISTINCT FROM (NEW.staff_id,NEW.proposal_id,review.initial_revision_id))
 OR (NEW.hop>0 AND NOT EXISTS(SELECT 1 FROM workforce_posting_chain_sources c WHERE
 (c.tenant_id,c.hospital_id,c.staff_id,c.proposal_id,c.revision_id,c.hop,c.ancestor_staff_id,c.ancestor_proposal_id,c.ancestor_revision_id)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.revision_id,NEW.hop,NEW.source_staff_id,NEW.source_proposal_id,NEW.source_revision_id))) THEN
 RAISE EXCEPTION 'lifecycle evidence requires its exact retained chain' USING ERRCODE='23514',CONSTRAINT='workforce_posting_lifecycle_guard';
 END IF;
 previous_staff:=current_setting('app.workforce_posting_staff',true);
 PERFORM set_config('app.workforce_posting_staff',NEW.source_staff_id::text,true);
 SELECT c.id,c.version,c.expected_revision_id,c.cutoff_at,c.recorded_at INTO cutoff FROM workforce_posting_cutoffs c WHERE
 (c.tenant_id,c.hospital_id,c.staff_id,c.proposal_id)=(NEW.tenant_id,NEW.hospital_id,NEW.source_staff_id,NEW.source_proposal_id)
 ORDER BY c.version DESC LIMIT 1;
 PERFORM set_config('app.workforce_posting_staff',previous_staff,true);
 IF (cutoff.id IS NULL AND (NEW.cutoff_version<>0 OR NEW.cutoff_id IS NOT NULL OR NEW.cutoff_at IS NOT NULL))
 OR (cutoff.id IS NOT NULL AND ((NEW.cutoff_id,NEW.cutoff_version,NEW.cutoff_at,NEW.source_revision_id) IS DISTINCT FROM
 (cutoff.id,cutoff.version,cutoff.cutoff_at,cutoff.expected_revision_id) OR cutoff.recorded_at>review.checked_at)) THEN
 RAISE EXCEPTION 'lifecycle cutoff observation changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_lifecycle_guard';
 END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_lifecycle_source_guard BEFORE INSERT ON workforce_posting_lifecycle_sources FOR EACH ROW EXECUTE FUNCTION workforce_posting_lifecycle_source_guard();

CREATE FUNCTION workforce_posting_lifecycle_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE child record;total integer;first_hop integer;last_hop integer;ancestors integer;deadline timestamptz;
BEGIN
 SELECT * INTO STRICT child FROM workforce_posting_proposal_revisions r WHERE (r.tenant_id,r.id)=(NEW.tenant_id,NEW.initial_revision_id);
 SELECT ancestor_count INTO ancestors FROM workforce_posting_chain_reviews r WHERE (r.tenant_id,r.revision_id)=(NEW.tenant_id,NEW.revision_id);
 SELECT count(*),min(s.hop),max(s.hop),min(CASE WHEN s.hop=0 THEN
 LEAST(child.eligibility_until,COALESCE(child.ends_at,child.eligibility_until),COALESCE(s.cutoff_at,child.eligibility_until)) ELSE
 LEAST(c.eligibility_until,COALESCE(c.ends_at,c.eligibility_until),COALESCE(s.cutoff_at,c.eligibility_until)) END)
 INTO total,first_hop,last_hop,deadline FROM workforce_posting_lifecycle_sources s
 LEFT JOIN workforce_posting_chain_sources c ON (c.tenant_id,c.revision_id,c.hop)=(s.tenant_id,s.revision_id,s.hop)
 WHERE (s.tenant_id,s.revision_id)=(NEW.tenant_id,NEW.revision_id);
 IF total<>NEW.source_count OR first_hop IS DISTINCT FROM 0 OR last_hop IS DISTINCT FROM NEW.source_count-1
 OR NEW.source_count<>COALESCE(ancestors,0)+1 OR deadline IS DISTINCT FROM NEW.effective_until
 OR NEW.effective_until<=clock_timestamp() OR child.starts_at>=NEW.effective_until
 OR (NEW.source_count=1 AND NOT EXISTS(SELECT 1 FROM workforce_posting_management_reviews r WHERE (r.tenant_id,r.revision_id)=(NEW.tenant_id,NEW.revision_id))) THEN
 RAISE EXCEPTION 'lifecycle approval requires complete exact observations and deadline' USING ERRCODE='23514',CONSTRAINT='workforce_posting_lifecycle_integrity';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_lifecycle_integrity AFTER INSERT ON workforce_posting_lifecycle_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_lifecycle_integrity();

CREATE FUNCTION workforce_posting_lifecycle_approval_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE required boolean;source record;previous_staff text;
BEGIN
 IF NEW.status<>'approved' THEN RETURN NULL;END IF;
 required:=COALESCE(current_setting('app.workforce_posting_lifecycle_approval',true)='verified',false);
 previous_staff:=current_setting('app.workforce_posting_staff',true);
 FOR source IN SELECT c.ancestor_staff_id,c.ancestor_proposal_id FROM workforce_posting_chain_sources c WHERE (c.tenant_id,c.revision_id)=(NEW.tenant_id,NEW.id) LOOP
  PERFORM set_config('app.workforce_posting_staff',source.ancestor_staff_id::text,true);
  IF EXISTS(SELECT 1 FROM workforce_posting_cutoffs c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.proposal_id)=(NEW.tenant_id,NEW.hospital_id,source.ancestor_staff_id,source.ancestor_proposal_id)) THEN required:=true;END IF;
  PERFORM set_config('app.workforce_posting_staff',previous_staff,true);
 END LOOP;
 PERFORM set_config('app.workforce_posting_staff',previous_staff,true);
 IF required AND NOT EXISTS(SELECT 1 FROM workforce_posting_lifecycle_reviews r WHERE
 (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.revision_id,r.creation_transaction)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.id,txid_current())) THEN
 RAISE EXCEPTION 'approval requires retained lifecycle review' USING ERRCODE='23514',CONSTRAINT='workforce_posting_lifecycle_integrity';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_lifecycle_approval_integrity AFTER INSERT ON workforce_posting_proposal_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_lifecycle_approval_integrity();
