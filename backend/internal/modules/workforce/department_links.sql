SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE workforce_department_links (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, master_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 master_revision_id uuid NOT NULL,
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7' AND substring(department_id::text,20,1) IN ('8','9','a','b')),
 department_revision_id uuid NOT NULL CHECK(substring(department_revision_id::text,15,1)='7' AND substring(department_revision_id::text,20,1) IN ('8','9','a','b')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 expected_version bigint NOT NULL CHECK(expected_version=version-1),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 status text NOT NULL CHECK(status IN ('pending','matched','rejected')),
 observed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,hospital_id,master_id,version),
 CHECK((status='pending')=(observed_at IS NULL)),
 FOREIGN KEY(tenant_id,hospital_id,master_id) REFERENCES workforce_hr_masters(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,master_revision_id) REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_department_link_history ON workforce_department_links(tenant_id,hospital_id,master_id,created_at DESC,id DESC);
CREATE INDEX workforce_department_link_source ON workforce_department_links(tenant_id,hospital_id,master_revision_id);
CREATE UNIQUE INDEX workforce_department_link_pending ON workforce_department_links(tenant_id,hospital_id,master_id) WHERE status='pending';
ALTER TABLE workforce_department_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_department_links FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_department_links
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND
  (workforce_staff_allowed(tenant_id,hospital_id,false) OR current_setting('app.workforce_department_observation',true)='authorized'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND
  ((workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting('app.workforce_department_link',true)='authorized')
   OR current_setting('app.workforce_department_observation',true)='authorized'));
CREATE TRIGGER workforce_department_link_retained BEFORE DELETE OR TRUNCATE ON workforce_department_links
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE FUNCTION workforce_department_link_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior_version bigint; prior_pending boolean;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':department-link:'||NEW.hospital_id::text||':'||NEW.master_id::text,0));
 IF TG_OP='INSERT' THEN
  IF current_setting('app.workforce_department_link',true) IS DISTINCT FROM 'authorized'
   OR NOT EXISTS(SELECT 1 FROM workforce_hospital_access WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND subject=current_setting('app.workforce_subject',true) AND level='manager')
   OR NEW.actor_subject IS DISTINCT FROM current_setting('app.workforce_subject',true)
   OR NEW.status<>'pending' OR NEW.observed_at IS NOT NULL THEN
   RAISE EXCEPTION 'department mapping preparation is unavailable' USING ERRCODE='23514',CONSTRAINT='workforce_department_link_guard';
  END IF;
  PERFORM 1 FROM workforce_hr_masters m JOIN workforce_hr_master_revisions r
   ON (r.tenant_id,r.hospital_id,r.master_id,r.id)=(m.tenant_id,m.hospital_id,m.id,m.revision_id)
   WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.id=NEW.master_id
   AND m.kind='department' AND m.active AND r.active AND r.id=NEW.master_revision_id FOR SHARE OF m;
  IF NOT FOUND THEN
   RAISE EXCEPTION 'department source revision is unavailable' USING ERRCODE='23514',CONSTRAINT='workforce_department_link_guard';
  END IF;
  SELECT COALESCE(max(version),0),COALESCE(bool_or(status='pending'),false) INTO prior_version,prior_pending FROM workforce_department_links
   WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND master_id=NEW.master_id;
  IF prior_pending OR NEW.expected_version<>prior_version THEN
   RAISE EXCEPTION 'department mapping version conflicts' USING ERRCODE='23514',CONSTRAINT='workforce_department_link_guard';
  END IF;
 ELSE
  IF current_setting('app.workforce_department_observation',true) IS DISTINCT FROM 'authorized'
   OR (NEW.tenant_id,NEW.hospital_id,NEW.master_id,NEW.id,NEW.master_revision_id,NEW.department_id,NEW.department_revision_id,NEW.version,NEW.expected_version,NEW.actor_subject,NEW.reason,NEW.created_at)
    IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.master_id,OLD.id,OLD.master_revision_id,OLD.department_id,OLD.department_revision_id,OLD.version,OLD.expected_version,OLD.actor_subject,OLD.reason,OLD.created_at)
   OR OLD.status<>'pending' OR NEW.status='pending' OR NEW.observed_at IS NULL THEN
   RAISE EXCEPTION 'department observation or identity conflicts' USING ERRCODE='23514',CONSTRAINT='workforce_department_link_guard';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_department_link_guard BEFORE INSERT OR UPDATE ON workforce_department_links
 FOR EACH ROW EXECUTE FUNCTION workforce_department_link_guard();
