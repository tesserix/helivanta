SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE privacy_officers (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 512),
 display_name text NOT NULL CHECK(octet_length(display_name) BETWEEN 1 AND 200),
 contact_email text NOT NULL CHECK(octet_length(contact_email) BETWEEN 3 AND 254),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7'),
 valid_until timestamptz NOT NULL,
 ended_reason text,
 ended_reference uuid,
 status text NOT NULL DEFAULT 'nominated' CHECK(status IN ('nominated','active','revoked')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), CHECK(valid_until>created_at),
 CHECK((status='revoked' AND ended_reason IN ('revoked','superseded') AND ended_reference IS NOT NULL AND substring(ended_reference::text,15,1)='7') OR (status<>'revoked' AND ended_reason IS NULL AND ended_reference IS NULL))
);
CREATE UNIQUE INDEX privacy_current_officer ON privacy_officers(tenant_id) WHERE status='active';
ALTER TABLE privacy_officers ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_officers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_officers USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (current_setting('app.privacy_manage',true)='true' OR NULLIF(current_setting('app.privacy_accept',true),'') IS NOT NULL));
CREATE FUNCTION privacy_officer_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'officer history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'nominated' OR NEW.version<>1 OR current_setting('app.privacy_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'invalid nomination' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.subject,NEW.display_name,NEW.contact_email,NEW.review_reference,NEW.valid_until,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.subject,OLD.display_name,OLD.contact_email,OLD.review_reference,OLD.valid_until,OLD.created_at) OR NEW.version<>OLD.version+1 OR OLD.status='revoked' OR NOT ((OLD.status='nominated' AND NEW.status IN ('active','revoked')) OR (OLD.status='active' AND NEW.status='revoked')) THEN RAISE EXCEPTION 'invalid officer transition' USING ERRCODE='23514'; END IF;
  IF NEW.status='revoked' AND current_setting('app.privacy_manage',true) IS DISTINCT FROM 'true' THEN
   IF NEW.ended_reason<>'superseded' OR NEW.ended_reference::text IS DISTINCT FROM current_setting('app.privacy_accept',true) OR NOT EXISTS(SELECT 1 FROM privacy_officers WHERE tenant_id=NEW.tenant_id AND id=NEW.ended_reference AND subject=current_setting('app.actor_subject',true) AND status='nominated' AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'officer management authority required' USING ERRCODE='23514'; END IF;
  END IF;
  IF NEW.status='active' AND (NEW.subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NEW.valid_until<=clock_timestamp()) THEN RAISE EXCEPTION 'invalid acceptance' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER privacy_officer_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_officers FOR EACH ROW EXECUTE FUNCTION privacy_officer_guard();
CREATE TRIGGER privacy_officer_audit AFTER INSERT OR UPDATE ON privacy_officers FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE privacy_requests (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 512),
 officer_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('access','correction','erasure','grievance')),
 description text NOT NULL CHECK(octet_length(description) BETWEEN 1 AND 4096 AND length(btrim(description))>0),
 status text NOT NULL DEFAULT 'received' CHECK(status IN ('received','under_review','awaiting_principal','responded','withdrawn')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,officer_id) REFERENCES privacy_officers(tenant_id,id)
);
CREATE INDEX privacy_request_subject_page ON privacy_requests(tenant_id,subject,created_at DESC,id DESC);
CREATE INDEX privacy_request_queue ON privacy_requests(tenant_id,created_at DESC,id DESC);
CREATE INDEX privacy_request_officer ON privacy_requests(tenant_id,officer_id);
ALTER TABLE privacy_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_requests USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR current_setting('app.privacy_officer',true)='true')) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR current_setting('app.privacy_officer',true)='true'));
CREATE FUNCTION privacy_request_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command_action text;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'request history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'received' OR NEW.version<>1 OR NEW.subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN RAISE EXCEPTION 'invalid request' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.subject,NEW.officer_id,NEW.kind,NEW.description,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.subject,OLD.officer_id,OLD.kind,OLD.description,OLD.created_at) OR NEW.version<>OLD.version+1 OR OLD.status IN ('responded','withdrawn') THEN RAISE EXCEPTION 'invalid request transition' USING ERRCODE='23514'; END IF;
  SELECT action INTO command_action FROM privacy_commands WHERE tenant_id=NEW.tenant_id AND request_id=NEW.id AND expected_version=OLD.version AND resulting_version=NEW.version AND actor_subject=current_setting('app.actor_subject',true);
  IF NOT FOUND OR NOT (
   (command_action='withdraw' AND NEW.status='withdrawn') OR
   (command_action='reply' AND OLD.status='awaiting_principal' AND NEW.status='under_review') OR
   (command_action='start_review' AND OLD.status='received' AND NEW.status='under_review') OR
   (command_action='request_information' AND OLD.status='under_review' AND NEW.status='awaiting_principal') OR
   (command_action='respond' AND OLD.status='under_review' AND NEW.status='responded')
  ) THEN RAISE EXCEPTION 'case transition requires a matching command' USING ERRCODE='23514'; END IF;

 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER privacy_request_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_requests FOR EACH ROW EXECUTE FUNCTION privacy_request_guard();
CREATE TRIGGER privacy_request_audit AFTER INSERT OR UPDATE ON privacy_requests FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE privacy_commands (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 request_id uuid NOT NULL,
 actor_subject text NOT NULL,
 action text NOT NULL CHECK(action IN ('start_review','request_information','reply','respond','withdraw')),
 message text NOT NULL CHECK(octet_length(message) BETWEEN 1 AND 4096 AND length(btrim(message))>0),
 expected_version bigint NOT NULL CHECK(expected_version>0),
 resulting_version bigint NOT NULL CHECK(resulting_version=expected_version+1),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,request_id,resulting_version),
 FOREIGN KEY(tenant_id,request_id) REFERENCES privacy_requests(tenant_id,id)
);
CREATE INDEX privacy_commands_page ON privacy_commands(tenant_id,request_id,created_at DESC,id DESC);
ALTER TABLE privacy_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_commands USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND EXISTS(SELECT 1 FROM privacy_requests r WHERE r.tenant_id=privacy_commands.tenant_id AND r.id=privacy_commands.request_id)) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'') AND EXISTS(SELECT 1 FROM privacy_requests r WHERE r.tenant_id=privacy_commands.tenant_id AND r.id=privacy_commands.request_id));
CREATE FUNCTION privacy_command_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_subject text; target_version bigint; target_status text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'case history is immutable' USING ERRCODE='23514'; END IF;
 SELECT subject,version,status INTO target_subject,target_version,target_status FROM privacy_requests WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id FOR UPDATE;
 IF NOT FOUND OR target_version<>NEW.expected_version OR target_status IN ('responded','withdrawn') OR NEW.actor_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN RAISE EXCEPTION 'invalid case command' USING ERRCODE='23514'; END IF;
 IF NEW.action IN ('withdraw','reply') THEN
  IF NEW.actor_subject<>target_subject THEN RAISE EXCEPTION 'principal authority required' USING ERRCODE='23514'; END IF;
 ELSE
  IF NEW.actor_subject=target_subject OR current_setting('app.privacy_officer',true) IS DISTINCT FROM 'true' OR NOT EXISTS(SELECT 1 FROM privacy_officers WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND status='active' AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'current officer authority required' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER privacy_command_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_commands FOR EACH ROW EXECUTE FUNCTION privacy_command_guard();
CREATE TRIGGER privacy_command_audit AFTER INSERT ON privacy_commands FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION privacy_command_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM privacy_requests WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id AND version>=NEW.resulting_version) THEN RAISE EXCEPTION 'case command must commit its transition' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER privacy_command_committed AFTER INSERT ON privacy_commands DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION privacy_command_commit_guard();
