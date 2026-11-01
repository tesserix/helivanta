SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_employment_generation_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(row_tenant)
  AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
  AND row_staff=NULLIF(current_setting('app.workforce_generation_staff',true),'')::uuid
  AND current_setting('app.workforce_generation',true) IN ('read','activate','guard','cohort','identity_epoch')
  AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL,false)
$$;

CREATE TABLE workforce_identity_epochs (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 epoch bigint NOT NULL CHECK(epoch BETWEEN 1 AND 9007199254740990),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
ALTER TABLE workforce_staff NO FORCE ROW LEVEL SECURITY;
INSERT INTO workforce_identity_epochs(tenant_id,hospital_id,staff_id,epoch)
 SELECT tenant_id,hospital_id,id,1 FROM workforce_staff;
ALTER TABLE workforce_staff FORCE ROW LEVEL SECURITY;
ALTER TABLE workforce_identity_epochs ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_identity_epochs FORCE ROW LEVEL SECURITY;
CREATE POLICY identity_epoch_isolation ON workforce_identity_epochs
 USING(hms_tenant_visible(tenant_id) AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id)
  AND current_setting('app.workforce_generation',true)='identity_epoch');

CREATE FUNCTION workforce_identity_epoch_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()<2 OR current_setting('app.workforce_generation',true) IS DISTINCT FROM 'identity_epoch' THEN
  RAISE EXCEPTION 'identity epochs are maintained by the owner' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.epoch<>1 THEN RAISE EXCEPTION 'invalid initial identity epoch' USING ERRCODE='23514'; END IF;
  NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.created_at)
   OR NEW.epoch<>OLD.epoch+1 THEN RAISE EXCEPTION 'invalid identity epoch transition' USING ERRCODE='23514'; END IF;
  NEW.updated_at:=clock_timestamp();
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_identity_epoch_write BEFORE INSERT OR UPDATE ON workforce_identity_epochs
 FOR EACH ROW EXECUTE FUNCTION workforce_identity_epoch_write();
CREATE TRIGGER workforce_identity_epoch_preserve BEFORE DELETE OR TRUNCATE ON workforce_identity_epochs
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_capture_identity_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; changed boolean; prior_mode text:=current_setting('app.workforce_generation',true);
 prior_staff text:=current_setting('app.workforce_generation_staff',true); next_epoch bigint;
 row_tenant uuid:=COALESCE(NEW.tenant_id,OLD.tenant_id); row_hospital uuid:=COALESCE(NEW.hospital_id,OLD.hospital_id);
BEGIN
 IF TG_TABLE_NAME='workforce_staff' THEN
  target:=NEW.id;
  changed:=TG_OP='INSERT' OR (NEW.employee_code,NEW.display_name) IS DISTINCT FROM (OLD.employee_code,OLD.display_name);
 ELSE
  target:=COALESCE(NEW.staff_id,OLD.staff_id);
  IF TG_OP='INSERT' THEN
   changed:=(NEW.given_name,NEW.family_name,NEW.date_of_birth) IS DISTINCT FROM ('','',NULL::date);
  ELSIF TG_OP='DELETE' THEN
   changed:=(OLD.given_name,OLD.family_name,OLD.date_of_birth) IS DISTINCT FROM ('','',NULL::date);
  ELSE
   changed:=(NEW.given_name,NEW.family_name,NEW.date_of_birth) IS DISTINCT FROM (OLD.given_name,OLD.family_name,OLD.date_of_birth);
  END IF;
 END IF;
 IF NOT changed THEN RETURN NULL; END IF;
 PERFORM set_config('app.workforce_generation','identity_epoch',true),set_config('app.workforce_generation_staff',target::text,true);
 IF TG_TABLE_NAME='workforce_staff' AND TG_OP='INSERT' THEN
  INSERT INTO workforce_identity_epochs(tenant_id,hospital_id,staff_id,epoch) VALUES(NEW.tenant_id,NEW.hospital_id,target,1);
 ELSE
  UPDATE workforce_identity_epochs SET epoch=epoch+1
   WHERE (tenant_id,hospital_id,staff_id)=(row_tenant,row_hospital,target) RETURNING epoch INTO next_epoch;
  IF NOT FOUND THEN RAISE EXCEPTION 'identity basis is unavailable' USING ERRCODE='23514'; END IF;
  INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
   VALUES(row_tenant,row_hospital,NULLIF(current_setting('app.workforce_subject',true),''),'workforce.identity_basis.advanced',target,jsonb_build_object('identity_epoch',next_epoch));
 END IF;
 PERFORM set_config('app.workforce_generation',COALESCE(prior_mode,''),true),set_config('app.workforce_generation_staff',COALESCE(prior_staff,''),true);
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_capture_staff_identity_epoch AFTER INSERT OR UPDATE ON workforce_staff
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_identity_epoch();
CREATE TRIGGER workforce_capture_profile_identity_epoch AFTER INSERT OR UPDATE OR DELETE ON workforce_enrollment_profile
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_identity_epoch();

ALTER TABLE workforce_employment_reviews ADD COLUMN identity_epoch bigint CHECK(identity_epoch BETWEEN 1 AND 9007199254740990);
ALTER TABLE workforce_employment_review_history ADD COLUMN identity_epoch bigint CHECK(identity_epoch BETWEEN 1 AND 9007199254740990);
ALTER TABLE workforce_person_reviews ADD COLUMN identity_epoch bigint CHECK(identity_epoch BETWEEN 1 AND 9007199254740990);
ALTER TABLE workforce_person_review_history ADD COLUMN identity_epoch bigint CHECK(identity_epoch BETWEEN 1 AND 9007199254740990);
CREATE FUNCTION workforce_capture_employment_review_basis() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior_mode text:=current_setting('app.workforce_generation',true);
 prior_staff text:=current_setting('app.workforce_generation_staff',true);
BEGIN
 IF NEW.identity_epoch IS NOT NULL THEN RAISE EXCEPTION 'employment identity basis is maintained by the owner' USING ERRCODE='23514'; END IF;
 PERFORM set_config('app.workforce_generation','cohort',true),set_config('app.workforce_generation_staff',NEW.staff_id::text,true);
 SELECT epoch INTO STRICT NEW.identity_epoch FROM workforce_identity_epochs
  WHERE (tenant_id,hospital_id,staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) FOR SHARE;
 PERFORM set_config('app.workforce_generation',COALESCE(prior_mode,''),true),set_config('app.workforce_generation_staff',COALESCE(prior_staff,''),true);
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_capture_employment_review_basis BEFORE INSERT ON workforce_employment_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_employment_review_basis();
CREATE TRIGGER workforce_capture_person_review_basis BEFORE INSERT ON workforce_person_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_employment_review_basis();
CREATE FUNCTION workforce_preserve_review_identity_basis() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.identity_epoch IS DISTINCT FROM OLD.identity_epoch THEN
  RAISE EXCEPTION 'review identity basis is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_preserve_person_identity_basis BEFORE UPDATE ON workforce_person_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_preserve_review_identity_basis();
CREATE TRIGGER workforce_preserve_employment_identity_basis BEFORE UPDATE ON workforce_employment_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_preserve_review_identity_basis();

CREATE TABLE workforce_employment_review_cohorts (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
ALTER TABLE workforce_employment_reviews NO FORCE ROW LEVEL SECURITY;
INSERT INTO workforce_employment_review_cohorts(tenant_id,hospital_id,staff_id,created_at)
 SELECT tenant_id,hospital_id,staff_id,min(created_at) FROM workforce_employment_reviews
 GROUP BY tenant_id,hospital_id,staff_id;
ALTER TABLE workforce_employment_reviews FORCE ROW LEVEL SECURITY;

CREATE TABLE workforce_employment_generations (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 revision_id uuid NOT NULL CHECK(substring(revision_id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version=1),
 review_id uuid NOT NULL,review_revision_id uuid NOT NULL,review_version bigint NOT NULL,
 review_staff_version bigint NOT NULL CHECK(review_staff_version BETWEEN 1 AND 9007199254740989),
 review_employment_epoch bigint NOT NULL CHECK(review_employment_epoch BETWEEN 1 AND 9007199254740989),
 staff_version bigint NOT NULL CHECK(staff_version=review_staff_version+1),
 employment_epoch bigint NOT NULL CHECK(employment_epoch=review_employment_epoch+1),
 identity_hash text NOT NULL CHECK(identity_hash ~ '^[a-f0-9]{64}$'),
 identity_epoch bigint NOT NULL CHECK(identity_epoch BETWEEN 1 AND 9007199254740990),
 starts_at timestamptz NOT NULL,ends_at timestamptz,eligibility_until timestamptz NOT NULL DEFAULT 'infinity',
 access_intent text NOT NULL CHECK(access_intent IN ('digital','non_digital')),
 declared_subject text NOT NULL CHECK(length(declared_subject)<=200
  AND ((access_intent='digital' AND length(declared_subject)>0)
   OR (access_intent='non_digital' AND declared_subject=''))),
 activated_by_subject text NOT NULL CHECK(length(activated_by_subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 activated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,revision_id),
 UNIQUE(tenant_id,hospital_id,review_id,review_version),
 UNIQUE(tenant_id,hospital_id,staff_id,employment_epoch),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,review_id,review_version) REFERENCES workforce_employment_review_history(tenant_id,hospital_id,id,version) ON DELETE RESTRICT,
 CHECK(ends_at IS NULL OR ends_at>starts_at)
);

CREATE FUNCTION workforce_employment_generation_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE reviewed record; terms record;
BEGIN
 IF current_setting('app.workforce_generation',true) IS DISTINCT FROM 'activate'
  OR NEW.activated_by_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') THEN
  RAISE EXCEPTION 'employment activation requires current attributable authority' USING ERRCODE='42501';
 END IF;
 SELECT r.staff_version,r.employment_epoch,r.identity_hash,r.identity_epoch,r.access_intent,r.declared_subject,
 r.period_id,r.period_revision_id,r.period_version,r.person_review_id,r.person_review_revision_id,r.person_review_version INTO reviewed FROM workforce_employment_reviews r
  WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id,r.revision_id,r.version,r.status)=
   (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.review_id,NEW.review_revision_id,NEW.review_version,'approved');
 IF NOT FOUND OR (reviewed.staff_version,reviewed.employment_epoch,reviewed.identity_hash,reviewed.identity_epoch,reviewed.access_intent,reviewed.declared_subject)
  IS DISTINCT FROM (NEW.review_staff_version,NEW.review_employment_epoch,NEW.identity_hash,NEW.identity_epoch,NEW.access_intent,NEW.declared_subject)
  OR NOT EXISTS(SELECT 1 FROM workforce_identity_epochs e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.identity_epoch)) THEN
  RAISE EXCEPTION 'approved employment source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_generation_source';
 END IF;
 SELECT p.starts_at,p.ends_at INTO STRICT terms FROM workforce_employment_period_history p
  WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)=
   (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,reviewed.period_id,reviewed.period_revision_id,reviewed.period_version);
 IF (NEW.starts_at,NEW.ends_at) IS DISTINCT FROM (terms.starts_at,terms.ends_at)
  OR NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE
    (s.tenant_id,s.hospital_id,s.id,s.version,s.employment_epoch,s.employment_status)=
    (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.staff_version,NEW.employment_epoch,'active')) THEN
  RAISE EXCEPTION 'activation must consume the exact Staff and terms revision' USING ERRCODE='23514',CONSTRAINT='workforce_employment_generation_source';
 END IF;
 NEW.activated_at:=clock_timestamp();NEW.created_at:=NEW.activated_at;NEW.updated_at:=NEW.activated_at;
 SELECT LEAST(p.valid_until,COALESCE(NEW.ends_at,'infinity'::timestamptz)) INTO NEW.eligibility_until
  FROM workforce_person_reviews p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.status,p.identity_epoch)=
   (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,reviewed.person_review_id,reviewed.person_review_revision_id,reviewed.person_review_version,'approved',NEW.identity_epoch);
 IF NEW.eligibility_until IS NULL OR NEW.activated_at<NEW.starts_at OR NEW.activated_at>=NEW.eligibility_until THEN
  RAISE EXCEPTION 'employment term is not effective' USING ERRCODE='23514',CONSTRAINT='workforce_employment_generation_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_generation_write BEFORE INSERT ON workforce_employment_generations
 FOR EACH ROW EXECUTE FUNCTION workforce_employment_generation_write();
CREATE TRIGGER workforce_employment_generation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_employment_generations
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_employment_review_cohort_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_employment_review_cohorts
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

ALTER TABLE workforce_employment_generations ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_employment_generations FORCE ROW LEVEL SECURITY;
CREATE POLICY generation_isolation ON workforce_employment_generations
 USING(hms_tenant_visible(tenant_id) AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id)
  AND current_setting('app.workforce_generation',true)='activate');
ALTER TABLE workforce_employment_review_cohorts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_employment_review_cohorts FORCE ROW LEVEL SECURITY;
CREATE POLICY cohort_isolation ON workforce_employment_review_cohorts
 USING(hms_tenant_visible(tenant_id) AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_employment_generation_context(tenant_id,hospital_id,staff_id)
  AND current_setting('app.workforce_generation',true)='cohort');

CREATE FUNCTION workforce_capture_employment_review_cohort() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior_mode text:=current_setting('app.workforce_generation',true);
 prior_staff text:=current_setting('app.workforce_generation_staff',true);
BEGIN
 PERFORM set_config('app.workforce_generation','cohort',true),set_config('app.workforce_generation_staff',NEW.staff_id::text,true);
 INSERT INTO workforce_employment_review_cohorts(tenant_id,hospital_id,staff_id)
  VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) ON CONFLICT DO NOTHING;
 PERFORM set_config('app.workforce_generation',COALESCE(prior_mode,''),true),set_config('app.workforce_generation_staff',COALESCE(prior_staff,''),true);
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_capture_employment_review_cohort AFTER INSERT ON workforce_employment_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_employment_review_cohort();

CREATE FUNCTION workforce_reviewed_activation_required() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior_mode text:=current_setting('app.workforce_generation',true);
 prior_staff text:=current_setting('app.workforce_generation_staff',true); tracked boolean;
BEGIN
 IF NEW.employment_status<>'active' OR NEW.employment_status IS NOT DISTINCT FROM OLD.employment_status THEN RETURN NULL; END IF;
 PERFORM set_config('app.workforce_generation','guard',true),set_config('app.workforce_generation_staff',NEW.id::text,true);
 SELECT EXISTS(SELECT 1 FROM workforce_employment_review_cohorts c
  WHERE (c.tenant_id,c.hospital_id,c.staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)) INTO tracked;
 IF tracked AND NOT EXISTS(SELECT 1 FROM workforce_employment_generations g
  WHERE (g.tenant_id,g.hospital_id,g.staff_id,g.staff_version,g.employment_epoch)=
   (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.version,NEW.employment_epoch)
   AND g.starts_at<=clock_timestamp() AND (g.ends_at IS NULL OR g.ends_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'reviewed employment requires a new matching activation receipt' USING ERRCODE='23514',CONSTRAINT='workforce_reviewed_activation_required';
 END IF;
 PERFORM set_config('app.workforce_generation',COALESCE(prior_mode,''),true),set_config('app.workforce_generation_staff',COALESCE(prior_staff,''),true);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_reviewed_activation_required AFTER UPDATE ON workforce_staff
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_reviewed_activation_required();

ALTER TABLE workforce_login_links ADD COLUMN employment_generation_id uuid;
ALTER TABLE workforce_login_link_history ADD COLUMN employment_generation_id uuid;
ALTER TABLE workforce_login_links ADD FOREIGN KEY(tenant_id,hospital_id,employment_generation_id)
 REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT;
ALTER TABLE workforce_login_link_history ADD FOREIGN KEY(tenant_id,hospital_id,employment_generation_id)
 REFERENCES workforce_employment_generations(tenant_id,hospital_id,id) ON DELETE RESTRICT;
CREATE INDEX ON workforce_login_links(tenant_id,hospital_id,employment_generation_id);
CREATE INDEX ON workforce_login_link_history(tenant_id,hospital_id,employment_generation_id);
CREATE FUNCTION workforce_bind_login_generation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE basis record; current_epoch bigint; tracked boolean;
 prior_mode text:=current_setting('app.workforce_generation',true);
 prior_staff text:=current_setting('app.workforce_generation_staff',true);
BEGIN
 IF TG_OP='UPDATE' AND NEW.employment_generation_id IS DISTINCT FROM OLD.employment_generation_id THEN
  RAISE EXCEPTION 'login generation binding is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND NEW.status<>'active' THEN RETURN NEW; END IF;
 PERFORM set_config('app.workforce_generation','guard',true),set_config('app.workforce_generation_staff',NEW.staff_id::text,true);
 SELECT s.employment_epoch INTO STRICT current_epoch FROM workforce_staff s
  WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id);
 SELECT g.id,g.access_intent,g.declared_subject,g.starts_at,g.eligibility_until,g.identity_epoch INTO basis
  FROM workforce_employment_generations g
  WHERE (g.tenant_id,g.hospital_id,g.staff_id,g.employment_epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,current_epoch);
 IF FOUND THEN
  IF basis.access_intent<>'digital' OR basis.declared_subject<>NEW.subject OR basis.starts_at>clock_timestamp()
   OR basis.eligibility_until<=clock_timestamp()
   OR NOT EXISTS(SELECT 1 FROM workforce_identity_epochs e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,basis.identity_epoch))
   OR (TG_OP='UPDATE' AND NEW.employment_generation_id IS DISTINCT FROM basis.id) THEN
   RAISE EXCEPTION 'login requires its exact current digital employment generation' USING ERRCODE='23514',CONSTRAINT='workforce_login_generation';
  END IF;
  NEW.employment_generation_id:=basis.id;
 ELSE
  SELECT EXISTS(SELECT 1 FROM workforce_employment_review_cohorts c
   WHERE (c.tenant_id,c.hospital_id,c.staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id)) INTO tracked;
  IF tracked OR NEW.employment_generation_id IS NOT NULL THEN
   RAISE EXCEPTION 'reviewed employment cannot use an unbound login' USING ERRCODE='23514',CONSTRAINT='workforce_login_generation';
  END IF;
 END IF;
 PERFORM set_config('app.workforce_generation',COALESCE(prior_mode,''),true),set_config('app.workforce_generation_staff',COALESCE(prior_staff,''),true);
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_bind_login_generation BEFORE INSERT OR UPDATE ON workforce_login_links
 FOR EACH ROW EXECUTE FUNCTION workforce_bind_login_generation();
CREATE FUNCTION workforce_capture_login_generation_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT employment_generation_id INTO NEW.employment_generation_id FROM workforce_login_links
  WHERE (tenant_id,hospital_id,id,version)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.version);
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_capture_login_generation_history BEFORE INSERT ON workforce_login_link_history
 FOR EACH ROW EXECUTE FUNCTION workforce_capture_login_generation_history();
