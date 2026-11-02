SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_posting_drafts (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 revision_id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740990),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_posting_draft_revisions (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,posting_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 command_id uuid NOT NULL CHECK(substring(command_id::text,15,1)='7' AND substring(command_id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740989),
 version bigint NOT NULL CHECK(version=expected_version+1),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740990),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740990),
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7' AND substring(department_id::text,20,1) IN ('8','9','a','b')),
 department_revision_id uuid NOT NULL CHECK(substring(department_revision_id::text,15,1)='7' AND substring(department_revision_id::text,20,1) IN ('8','9','a','b')),
 starts_at timestamptz NOT NULL CHECK(starts_at>'0001-01-01T00:00:00Z' AND starts_at<'10000-01-01T00:00:00Z'),
 ends_at timestamptz CHECK(ends_at>starts_at AND ends_at<'10000-01-01T00:00:00Z'),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,command_id),
 UNIQUE(tenant_id,hospital_id,staff_id,posting_id,version),
 UNIQUE(tenant_id,hospital_id,staff_id,posting_id,id,version),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,posting_id) REFERENCES workforce_posting_drafts(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE workforce_posting_drafts ADD CONSTRAINT workforce_posting_draft_current_revision
 FOREIGN KEY(tenant_id,hospital_id,staff_id,id,revision_id,version)
 REFERENCES workforce_posting_draft_revisions(tenant_id,hospital_id,staff_id,posting_id,id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX ON workforce_posting_drafts(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE INDEX ON workforce_posting_draft_revisions(tenant_id,hospital_id,staff_id,posting_id,created_at DESC,id DESC);

CREATE FUNCTION workforce_posting_draft_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND s=NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid
 AND current_setting('app.workforce_posting',true) IN ('read','write')
 AND (NOT writing OR current_setting('app.workforce_posting',true)='write')
 AND workforce_staff_allowed(t,h,writing),false)
$$;
ALTER TABLE workforce_posting_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_drafts
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_draft_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_draft_context(tenant_id,hospital_id,staff_id,true));
ALTER TABLE workforce_posting_draft_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_draft_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_draft_revisions
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_draft_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_draft_context(tenant_id,hospital_id,staff_id,true));

CREATE FUNCTION workforce_posting_draft_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_posting_draft_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true) THEN
  RAISE EXCEPTION 'posting preparation requires current owner context' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN RAISE EXCEPTION 'posting draft must start at version one' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.created_at) IS DISTINCT FROM
   (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,OLD.created_at)
   OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id THEN
   RAISE EXCEPTION 'posting draft identity or version changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_draft_guard';
  END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_draft_write_guard BEFORE INSERT OR UPDATE ON workforce_posting_drafts
 FOR EACH ROW EXECUTE FUNCTION workforce_posting_draft_write_guard();

CREATE FUNCTION workforce_posting_draft_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_version bigint;source_epoch bigint;
BEGIN
 IF NOT workforce_posting_draft_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'')
 OR NEW.department_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 OR NEW.department_revision_id IS DISTINCT FROM NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid THEN
  RAISE EXCEPTION 'posting revision requires its exact current source' USING ERRCODE='42501';
 END IF;
 SELECT version,employment_epoch INTO source_version,source_epoch FROM workforce_staff
 WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.staff_id FOR SHARE;
 IF source_version IS DISTINCT FROM NEW.staff_version OR source_epoch IS DISTINCT FROM NEW.employment_epoch THEN
  RAISE EXCEPTION 'posting Staff source changed' USING ERRCODE='23514',CONSTRAINT='workforce_posting_draft_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_draft_revision_guard BEFORE INSERT ON workforce_posting_draft_revisions
 FOR EACH ROW EXECUTE FUNCTION workforce_posting_draft_revision_guard();

CREATE FUNCTION workforce_posting_draft_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;head_version bigint;head_revision uuid;latest_version bigint;latest_revision uuid;
BEGIN
 IF TG_TABLE_NAME='workforce_posting_drafts' THEN target:=NEW.id; ELSE target:=NEW.posting_id; END IF;
 SELECT version,revision_id INTO head_version,head_revision FROM workforce_posting_drafts
 WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND staff_id=NEW.staff_id AND id=target;
 SELECT version,id INTO latest_version,latest_revision FROM workforce_posting_draft_revisions
 WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND staff_id=NEW.staff_id AND posting_id=target ORDER BY version DESC LIMIT 1;
 IF head_version IS DISTINCT FROM latest_version OR head_revision IS DISTINCT FROM latest_revision OR head_version IS NULL THEN
  RAISE EXCEPTION 'posting draft must retain its latest immutable revision' USING ERRCODE='23514',CONSTRAINT='workforce_posting_draft_guard';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_draft_integrity AFTER INSERT OR UPDATE ON workforce_posting_drafts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_draft_integrity();
CREATE CONSTRAINT TRIGGER workforce_posting_draft_revision_integrity AFTER INSERT ON workforce_posting_draft_revisions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_draft_integrity();
CREATE TRIGGER workforce_posting_draft_preserve BEFORE DELETE OR TRUNCATE ON workforce_posting_drafts
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_posting_draft_revision_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_draft_revisions
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
