SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_login_links (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 200 AND subject !~ '[[:space:]*#:]'),
 status text NOT NULL CHECK (status IN ('pending','active','rejected','withdrawn','revoked')),
 version bigint NOT NULL CHECK (version>0),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 proposal_reason text NOT NULL CHECK (length(btrim(proposal_reason)) BETWEEN 1 AND 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_login_links_one_staff ON workforce_login_links(tenant_id,hospital_id,staff_id) WHERE status IN ('pending','active');
CREATE UNIQUE INDEX workforce_login_links_one_subject ON workforce_login_links(tenant_id,hospital_id,subject) WHERE status IN ('pending','active');
CREATE INDEX workforce_login_links_list ON workforce_login_links(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE TABLE workforce_login_link_history (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 version bigint NOT NULL, status text NOT NULL, actor_subject text NOT NULL, reason text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,id,version),
 FOREIGN KEY(tenant_id,hospital_id,id) REFERENCES workforce_login_links(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_login_link_history_page ON workforce_login_link_history(tenant_id,hospital_id,id,created_at DESC);

CREATE FUNCTION workforce_login_link_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command text := current_setting('app.workforce_login_link_command',true);
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') THEN
  RAISE EXCEPTION 'login link actor must be authenticated' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF command IS DISTINCT FROM 'manage' OR NEW.status<>'pending' OR NEW.version<>1
    OR NEW.proposed_by_subject<>NEW.actor_subject OR NEW.reason<>NEW.proposal_reason THEN
   RAISE EXCEPTION 'login link starts as an attributable proposal' USING ERRCODE='23514';
  END IF;
  NEW.created_at:=clock_timestamp(); NEW.updated_at:=NEW.created_at;
 ELSE
  IF OLD.status NOT IN ('pending','active') OR NEW.version<>OLD.version+1
    OR (to_jsonb(NEW)-ARRAY['status','version','actor_subject','reason','updated_at']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['status','version','actor_subject','reason','updated_at']) THEN
   RAISE EXCEPTION 'login link proposal is immutable and versions advance once' USING ERRCODE='23514';
  END IF;
  IF NEW.status IN ('active','rejected') THEN
   IF OLD.status<>'pending' OR command IS DISTINCT FROM 'review'
     OR NEW.actor_subject IN (NEW.proposed_by_subject,NEW.subject) THEN
    RAISE EXCEPTION 'login link needs an independent reviewer' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='withdrawn' THEN
   IF OLD.status<>'pending' OR command IS DISTINCT FROM 'manage' OR NEW.actor_subject<>NEW.proposed_by_subject THEN
    RAISE EXCEPTION 'only the proposer withdraws a pending link' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='revoked' THEN
   IF command IS DISTINCT FROM 'manage' AND command IS DISTINCT FROM 'inactivate' THEN
    RAISE EXCEPTION 'link revocation requires authority' USING ERRCODE='23514';
   END IF;
   IF command='inactivate' AND (NEW.reason<>'Staff made inactive' OR NOT EXISTS (
      SELECT 1 FROM workforce_staff WHERE (tenant_id,hospital_id,id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND employment_status='inactive')) THEN
    RAISE EXCEPTION 'automatic revocation requires inactive staff' USING ERRCODE='23514';
   END IF;
  ELSE
   RAISE EXCEPTION 'invalid login link transition' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 IF NEW.status IN ('pending','active') AND NOT EXISTS (
  SELECT 1 FROM workforce_staff WHERE (tenant_id,hospital_id,id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND employment_status='active') THEN
  RAISE EXCEPTION 'login link requires active staff' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_login_link_guard BEFORE INSERT OR UPDATE ON workforce_login_links FOR EACH ROW EXECUTE FUNCTION workforce_login_link_guard();

CREATE FUNCTION workforce_login_link_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO workforce_login_link_history(tenant_id,hospital_id,id,version,status,actor_subject,reason,created_at)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.version,NEW.status,NEW.actor_subject,NEW.reason,NEW.updated_at);
 INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.actor_subject,'workforce.login_link.changed',NEW.id,
  jsonb_build_object('staff_id',NEW.staff_id,'version',NEW.version,'status',NEW.status));
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_login_link_record AFTER INSERT OR UPDATE ON workforce_login_links FOR EACH ROW EXECUTE FUNCTION workforce_login_link_record();

CREATE FUNCTION workforce_login_link_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_login_links l WHERE
   (l.tenant_id,l.hospital_id,l.id,l.version,l.status,l.actor_subject,l.reason,l.updated_at)=
   (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.version,NEW.status,NEW.actor_subject,NEW.reason,NEW.created_at)) THEN
  RAISE EXCEPTION 'link history must match the committed version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_login_link_history_guard BEFORE INSERT ON workforce_login_link_history FOR EACH ROW EXECUTE FUNCTION workforce_login_link_history_guard();
CREATE TRIGGER workforce_login_link_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_login_link_history FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_login_links_no_delete BEFORE DELETE OR TRUNCATE ON workforce_login_links FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

DO $$DECLARE tbl text; BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_login_links','workforce_login_link_history'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY login_link_isolation ON %I USING(hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_login_link_access'',true)=''authorized'') WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_login_link_access'',true)=''authorized'')',tbl);
 END LOOP;
END $$;

CREATE FUNCTION workforce_inactive_login_links_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous text := current_setting('app.workforce_login_link_access',true); blocked boolean;
BEGIN
 IF NEW.employment_status='inactive' THEN
  PERFORM set_config('app.workforce_login_link_access','authorized',true);
  SELECT EXISTS(SELECT 1 FROM workforce_login_links WHERE (tenant_id,hospital_id,staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) AND status IN ('pending','active')) INTO blocked;
  PERFORM set_config('app.workforce_login_link_access',COALESCE(previous,''),true);
  IF blocked THEN RAISE EXCEPTION 'inactive staff cannot retain a current login link' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_inactive_login_links_guard AFTER UPDATE ON workforce_staff DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_inactive_login_links_guard();
