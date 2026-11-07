SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE iam_operating_duties(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(id<> '00000000-0000-0000-0000-000000000000'),
 employer_kind text NOT NULL CHECK(employer_kind IN('pharmacy','supplier')),employer_id uuid NOT NULL,location_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(subject)BETWEEN 1 AND 200),template_key text NOT NULL,
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),
 prepared_by text NOT NULL CHECK(length(prepared_by)BETWEEN 1 AND 200),membership_epoch bigint NOT NULL CHECK(membership_epoch>0),
 status text NOT NULL CHECK(status IN('pending_review','approved_pending_activation','active','rejected','withdrawn','revoked')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),authority_epoch bigint NOT NULL CHECK(authority_epoch BETWEEN 1 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(length(actor_subject)BETWEEN 1 AND 200),reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 CHECK(iam_operating_template_permissions(template_key,employer_kind)IS NOT NULL),
 CHECK(input#>>'{scope,employer_kind}'IS NOT DISTINCT FROM employer_kind AND input#>>'{scope,employer_id}'IS NOT DISTINCT FROM employer_id::text AND input#>>'{scope,location_id}'IS NOT DISTINCT FROM location_id::text AND input->>'subject'IS NOT DISTINCT FROM subject AND input->>'template_key'IS NOT DISTINCT FROM template_key)
);
CREATE INDEX iam_operating_duties_scope ON iam_operating_duties(tenant_id,employer_kind,employer_id,location_id,subject,created_at DESC,id DESC);
CREATE INDEX iam_operating_duties_staff_page ON iam_operating_duties(tenant_id,employer_kind,employer_id,(input#>>'{identity,staff_id}'),created_at DESC,id DESC);
CREATE UNIQUE INDEX iam_operating_duties_one_live_template ON iam_operating_duties(tenant_id,employer_kind,employer_id,location_id,subject,template_key)WHERE status IN('pending_review','approved_pending_activation','active');
ALTER TABLE iam_operating_duties ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_operating_duties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_operating_duties USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized' AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER iam_operating_duty_no_removal BEFORE DELETE OR TRUNCATE ON iam_operating_duties FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();

CREATE TABLE iam_operating_duty_history(
 tenant_id uuid NOT NULL,duty_id uuid NOT NULL,version bigint NOT NULL,status text NOT NULL,authority_epoch bigint NOT NULL,
 actor_subject text NOT NULL,reason text NOT NULL,recorded_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,duty_id,version),FOREIGN KEY(tenant_id,duty_id)REFERENCES iam_operating_duties(tenant_id,id)ON DELETE RESTRICT
);
ALTER TABLE iam_operating_duty_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_operating_duty_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_operating_duty_history USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized' AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER iam_operating_history_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_operating_duty_history FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();

CREATE FUNCTION iam_operating_duty_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command text:=current_setting('app.iam_operating_command',true);
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')THEN RAISE EXCEPTION 'operating duty requires current actor' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN
  IF command<>'prepare' OR NEW.status<>'pending_review' OR NEW.version<>1 OR NEW.authority_epoch<>1 OR NEW.prepared_by<>NEW.actor_subject OR NEW.reason IS DISTINCT FROM NEW.input->>'reason' THEN RAISE EXCEPTION 'operating duty requires attributable preparation' USING ERRCODE='23514';END IF;
  NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;
 ELSE
  IF (to_jsonb(NEW)-ARRAY['status','version','authority_epoch','actor_subject','reason','updated_at'])IS DISTINCT FROM(to_jsonb(OLD)-ARRAY['status','version','authority_epoch','actor_subject','reason','updated_at'])OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'operating duty input is immutable' USING ERRCODE='23514';END IF;
  IF NEW.status='revoked' AND OLD.status IN('pending_review','approved_pending_activation','active') AND command='revoke' THEN
   IF NEW.authority_epoch<>OLD.authority_epoch+1 THEN RAISE EXCEPTION 'operating revocation must advance authority epoch' USING ERRCODE='23514';END IF;
  ELSIF OLD.status='pending_review' AND NEW.status IN('approved_pending_activation','rejected')AND command='review' THEN
   IF NEW.actor_subject IN(OLD.prepared_by,OLD.subject)OR NEW.authority_epoch<>OLD.authority_epoch THEN RAISE EXCEPTION 'operating review must be independent' USING ERRCODE='23514';END IF;
  ELSIF OLD.status='pending_review' AND NEW.status='withdrawn'AND command='withdraw'AND NEW.actor_subject=OLD.prepared_by THEN
   IF NEW.authority_epoch<>OLD.authority_epoch THEN RAISE EXCEPTION 'withdrawal cannot change epoch' USING ERRCODE='23514';END IF;
  ELSIF OLD.status='approved_pending_activation'AND NEW.status='active'AND command='project' THEN
   IF NEW.authority_epoch<>OLD.authority_epoch THEN RAISE EXCEPTION 'projection cannot change epoch' USING ERRCODE='23514';END IF;
  ELSE RAISE EXCEPTION 'invalid operating duty transition' USING ERRCODE='23514';END IF;
  NEW.updated_at:=clock_timestamp();
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_duty_guard BEFORE INSERT OR UPDATE ON iam_operating_duties FOR EACH ROW EXECUTE FUNCTION iam_operating_duty_guard();
CREATE FUNCTION iam_operating_duty_record()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO iam_operating_duty_history(tenant_id,duty_id,version,status,authority_epoch,actor_subject,reason,recorded_at)VALUES(NEW.tenant_id,NEW.id,NEW.version,NEW.status,NEW.authority_epoch,NEW.actor_subject,NEW.reason,NEW.updated_at);
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)VALUES(NEW.tenant_id,NEW.actor_subject,'iam.operating.changed','iam_operating_duties',NEW.id,ARRAY['status','version','authority_epoch']);
 RETURN NULL;
END $$;
CREATE TRIGGER iam_operating_duty_record AFTER INSERT OR UPDATE ON iam_operating_duties FOR EACH ROW EXECUTE FUNCTION iam_operating_duty_record();

CREATE FUNCTION iam_operating_history_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_operating_duties d WHERE(d.tenant_id,d.id,d.version,d.status,d.authority_epoch,d.actor_subject,d.reason,d.updated_at)=(NEW.tenant_id,NEW.duty_id,NEW.version,NEW.status,NEW.authority_epoch,NEW.actor_subject,NEW.reason,NEW.recorded_at))THEN RAISE EXCEPTION 'operating history must match its owner record' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_history_guard BEFORE INSERT ON iam_operating_duty_history FOR EACH ROW EXECUTE FUNCTION iam_operating_history_guard();

CREATE TABLE iam_operating_snapshots(
 tenant_id uuid NOT NULL,duty_id uuid NOT NULL,version bigint NOT NULL,authority_epoch bigint NOT NULL,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,duty_id,version),FOREIGN KEY(tenant_id,duty_id)REFERENCES iam_operating_duties(tenant_id,id)ON DELETE RESTRICT
);
ALTER TABLE iam_operating_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_operating_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_operating_snapshots USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized');
CREATE TRIGGER iam_operating_snapshot_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_operating_snapshots FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE FUNCTION iam_operating_snapshot_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d iam_operating_duties;
BEGIN
 SELECT * INTO d FROM iam_operating_duties WHERE tenant_id=NEW.tenant_id AND id=NEW.duty_id FOR SHARE;
 IF NOT FOUND OR d.status<>'approved_pending_activation'OR d.version<>NEW.version OR d.authority_epoch<>NEW.authority_epoch OR current_setting('app.iam_operating_command',true)<>'review' OR d.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NEW.document->>'TenantID'IS DISTINCT FROM d.tenant_id::text OR NEW.document->>'ID'IS DISTINCT FROM d.id::text OR NEW.document->>'Version'IS DISTINCT FROM d.version::text OR NEW.document->>'ScopeEpoch'IS DISTINCT FROM d.authority_epoch::text
 OR NEW.document->>'EmployerKind'IS DISTINCT FROM d.employer_kind OR NEW.document->>'EmployerID'IS DISTINCT FROM d.employer_id::text OR NEW.document->>'LocationID'IS DISTINCT FROM d.location_id::text OR NEW.document->>'Subject'IS DISTINCT FROM d.subject OR NEW.document->>'MembershipEpoch'IS DISTINCT FROM d.membership_epoch::text
 OR NEW.document->>'SourceVersion'IS DISTINCT FROM d.input#>>'{scope,expected_employer_version}' OR NEW.document->>'LocationVersion'IS DISTINCT FROM d.input#>>'{scope,expected_location_version}'
 OR NEW.document#>>'{Identity,StaffID}'IS DISTINCT FROM d.input#>>'{identity,staff_id}'OR NEW.document#>>'{Identity,StaffVersion}'IS DISTINCT FROM d.input#>>'{identity,staff_version}'OR NEW.document#>>'{Identity,LinkID}'IS DISTINCT FROM d.input#>>'{identity,link_id}'OR NEW.document#>>'{Identity,LinkVersion}'IS DISTINCT FROM d.input#>>'{identity,link_version}'
 OR NEW.document->>'ValidFrom'IS DISTINCT FROM d.input->>'valid_from'OR NEW.document->>'ExpiresAt'IS DISTINCT FROM d.input->>'expires_at'OR NEW.document->'Permissions'IS DISTINCT FROM iam_operating_template_permissions(d.template_key,d.employer_kind)
 THEN RAISE EXCEPTION 'operating snapshot must match exact independent approval' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_snapshot_guard BEFORE INSERT ON iam_operating_snapshots FOR EACH ROW EXECUTE FUNCTION iam_operating_snapshot_guard();

CREATE TABLE iam_operating_commands(
 tenant_id uuid NOT NULL,id uuid NOT NULL,duty_id uuid NOT NULL,actor_subject text NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 9007199254740990),input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,duty_id,expected_version),FOREIGN KEY(tenant_id,duty_id)REFERENCES iam_operating_duties(tenant_id,id)ON DELETE RESTRICT,
 CHECK(input->>'expected_version'IS NOT DISTINCT FROM expected_version::text AND receipt->>'id'IS NOT DISTINCT FROM id::text AND receipt->>'duty_id'IS NOT DISTINCT FROM duty_id::text AND receipt->>'actor_subject'IS NOT DISTINCT FROM actor_subject AND receipt->'input'IS NOT DISTINCT FROM input)
);
ALTER TABLE iam_operating_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_operating_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_operating_commands USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized'AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER iam_operating_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_operating_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE FUNCTION iam_operating_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d iam_operating_duties;
BEGIN
 SELECT * INTO d FROM iam_operating_duties WHERE tenant_id=NEW.tenant_id AND id=NEW.duty_id FOR SHARE;
 IF NOT FOUND OR d.version<>NEW.expected_version+1 OR d.actor_subject<>NEW.actor_subject OR NEW.receipt#>>'{result,id}'IS DISTINCT FROM d.id::text OR NEW.receipt#>>'{result,version}'IS DISTINCT FROM d.version::text OR NEW.receipt#>>'{result,status}'IS DISTINCT FROM d.status OR NEW.receipt#>'{result,input}'IS DISTINCT FROM d.input OR NEW.receipt#>>'{result,prepared_by}'IS DISTINCT FROM d.prepared_by OR (NEW.receipt#>>'{result,created_at}')::timestamptz IS DISTINCT FROM d.created_at OR(NEW.receipt#>>'{result,updated_at}')::timestamptz IS DISTINCT FROM d.updated_at OR(NEW.receipt->>'created_at')::timestamptz IS DISTINCT FROM NEW.created_at OR NEW.created_at<>d.updated_at OR NEW.input->>'reason'IS DISTINCT FROM d.reason
 THEN RAISE EXCEPTION 'operating command receipt must match its owner transition' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_command_guard BEFORE INSERT ON iam_operating_commands FOR EACH ROW EXECUTE FUNCTION iam_operating_command_guard();

CREATE TABLE iam_operating_projection_acks(
 tenant_id uuid NOT NULL,duty_id uuid NOT NULL,version bigint NOT NULL,authority_epoch bigint NOT NULL,acknowledged_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,duty_id,version),FOREIGN KEY(tenant_id,duty_id,version)REFERENCES iam_operating_snapshots(tenant_id,duty_id,version)ON DELETE RESTRICT
);
ALTER TABLE iam_operating_projection_acks ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_operating_projection_acks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_operating_projection_acks USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_operating_access',true)='authorized'AND current_setting('app.iam_operating_command',true)='project');
CREATE TRIGGER iam_operating_ack_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_operating_projection_acks FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE FUNCTION iam_operating_ack_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_operating_duties d JOIN iam_operating_snapshots s ON(s.tenant_id,s.duty_id,s.version,s.authority_epoch)=(d.tenant_id,d.id,d.version,d.authority_epoch)WHERE(d.tenant_id,d.id,d.version,d.authority_epoch)=(NEW.tenant_id,NEW.duty_id,NEW.version,NEW.authority_epoch)AND d.status='approved_pending_activation')THEN RAISE EXCEPTION 'operating acknowledgement must match pending source revision' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_ack_guard BEFORE INSERT ON iam_operating_projection_acks FOR EACH ROW EXECUTE FUNCTION iam_operating_ack_guard();
CREATE FUNCTION iam_operating_activation_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='active'AND NOT EXISTS(SELECT 1 FROM iam_operating_projection_acks WHERE(tenant_id,duty_id,version,authority_epoch)=(OLD.tenant_id,OLD.id,OLD.version,OLD.authority_epoch))THEN RAISE EXCEPTION 'operating activation requires exact acknowledged revision' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_activation_guard BEFORE UPDATE ON iam_operating_duties FOR EACH ROW EXECUTE FUNCTION iam_operating_activation_guard();
