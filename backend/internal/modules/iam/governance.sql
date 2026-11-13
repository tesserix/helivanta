SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION iam_governance_owner(t uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.iam_governance_access',true)='owner'
 AND EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=t AND subject=NULLIF(current_setting('app.actor_subject',true),'') AND role_key='tenant_admin')
$$;
CREATE FUNCTION iam_governance_owner_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t uuid; role text;
BEGIN
 IF TG_OP='DELETE' THEN t:=OLD.tenant_id;role:=OLD.role_key;
 ELSE t:=NEW.tenant_id;role:=NEW.role_key;END IF;
 IF role='tenant_admin' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended('iam.governance_owners:'||t::text,0));
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD;END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_governance_owner_change BEFORE INSERT OR DELETE ON iam_members
 FOR EACH ROW EXECUTE FUNCTION iam_governance_owner_change();

CREATE TABLE iam_governance_grants (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 membership_epoch bigint NOT NULL CHECK(membership_epoch BETWEEN 1 AND 9007199254740991),
 template_key text NOT NULL CHECK(length(template_key) BETWEEN 1 AND 120),
 scope_kind text NOT NULL CHECK(scope_kind IN('tenant','hospital','pharmacy','supplier')),
 scope_id uuid,
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 status text NOT NULL CHECK(status IN('pending_review','provisional_active','active','rejected','revoked')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 issued_by text NOT NULL CHECK(length(issued_by) BETWEEN 1 AND 200 AND issued_by<>subject),
 owner_count bigint NOT NULL CHECK(owner_count BETWEEN 1 AND 1000),
 authorization_mode text NOT NULL CHECK(authorization_mode IN('independent_review','single_owner_provisional')),
 valid_from timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>valid_from),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT,
 CHECK((scope_kind='tenant')=(scope_id IS NULL)),
 CHECK(input->>'subject' IS NOT DISTINCT FROM subject
 AND input->>'expected_membership_epoch' IS NOT DISTINCT FROM membership_epoch::text
 AND input->>'template_key' IS NOT DISTINCT FROM template_key
 AND input->>'scope_kind' IS NOT DISTINCT FROM scope_kind
 AND input->>'scope_id' IS NOT DISTINCT FROM scope_id::text
 AND receipt->>'id' IS NOT DISTINCT FROM id::text
 AND receipt->>'tenant_id' IS NOT DISTINCT FROM tenant_id::text
 AND receipt->'input' IS NOT DISTINCT FROM input
 AND receipt->>'issued_by' IS NOT DISTINCT FROM issued_by
 AND receipt->>'owner_count' IS NOT DISTINCT FROM owner_count::text
 AND receipt->>'authorization_mode' IS NOT DISTINCT FROM authorization_mode)
);
ALTER TABLE iam_governance_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_governance_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_governance_grants
 USING(hms_tenant_visible(tenant_id) AND iam_governance_owner(tenant_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND iam_governance_owner(tenant_id));
CREATE INDEX iam_governance_grants_history ON iam_governance_grants(tenant_id,created_at DESC,id DESC);
CREATE INDEX iam_governance_grants_subject ON iam_governance_grants(tenant_id,subject,scope_kind,scope_id,expires_at);
CREATE FUNCTION iam_governance_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT iam_governance_owner(NEW.tenant_id) OR current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'grant'
   OR NEW.issued_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
   OR NEW.version<>1 OR NEW.status NOT IN('pending_review','provisional_active')
   OR NEW.receipt->>'status' IS DISTINCT FROM NEW.status
   OR NEW.receipt->>'version' IS DISTINCT FROM '1'
   OR NOT EXISTS(SELECT 1 FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject AND epoch=NEW.membership_epoch AND member_count>0)
   OR EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject AND role_key='tenant_admin')
   OR NEW.owner_count<>(SELECT count(*) FROM iam_members WHERE tenant_id=NEW.tenant_id AND role_key='tenant_admin')
   OR (NEW.status='provisional_active') IS DISTINCT FROM (NEW.owner_count=1 AND NEW.authorization_mode='single_owner_provisional')
   OR (NEW.owner_count>1 AND NEW.authorization_mode<>'independent_review') THEN
   RAISE EXCEPTION 'governance creation requires current attributed owner admission' USING ERRCODE='23514';
  END IF;
 ELSE
  RAISE EXCEPTION 'governance state requires a separately retained decision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_governance_grant_guard BEFORE INSERT OR UPDATE ON iam_governance_grants
 FOR EACH ROW EXECUTE FUNCTION iam_governance_grant_guard();
CREATE TRIGGER iam_governance_grants_preserve BEFORE DELETE OR TRUNCATE ON iam_governance_grants
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();

CREATE TABLE iam_governance_owner_notices (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 grant_id uuid NOT NULL,
 grant_version bigint NOT NULL CHECK(grant_version>0),
 recipient_subject text NOT NULL CHECK(length(recipient_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK(version=1),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 seen_at timestamptz,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,grant_id,grant_version,recipient_subject),
 FOREIGN KEY(tenant_id,grant_id) REFERENCES iam_governance_grants(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_governance_owner_notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_governance_owner_notices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_governance_owner_notices
 USING(hms_tenant_visible(tenant_id) AND iam_governance_owner(tenant_id) AND recipient_subject=NULLIF(current_setting('app.actor_subject',true),''))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND iam_governance_owner(tenant_id));
CREATE INDEX iam_governance_notices_inbox ON iam_governance_owner_notices(tenant_id,recipient_subject,created_at DESC,id DESC);
CREATE FUNCTION iam_governance_notice_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'grant'
 OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.recipient_subject AND role_key='tenant_admin')
 OR NOT EXISTS(SELECT 1 FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND id=NEW.grant_id AND version=NEW.grant_version)
 OR NEW.seen_at IS NOT NULL THEN
  RAISE EXCEPTION 'governance owner notice requires an attributed grant transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_governance_notice_guard BEFORE INSERT ON iam_governance_owner_notices
 FOR EACH ROW EXECUTE FUNCTION iam_governance_notice_guard();
CREATE TRIGGER iam_governance_notices_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_governance_owner_notices
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();

CREATE FUNCTION iam_governance_routing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.role_key='governance_delegate' AND (
 current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'grant'
 OR NOT EXISTS(SELECT 1 FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject
 AND status IN('active','provisional_active') AND expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'governance routing membership requires current source intent' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_governance_routing_guard BEFORE INSERT ON iam_members
 FOR EACH ROW EXECUTE FUNCTION iam_governance_routing_guard();

CREATE FUNCTION iam_governance_auditor_context(t uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.iam_governance_access',true)='audit'
 AND EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=t AND subject=NULLIF(current_setting('app.actor_subject',true),'') AND role_key='governance_delegate')
$$;
CREATE FUNCTION iam_governance_read(t uuid,g uuid,s text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (
 iam_governance_owner(t) OR iam_governance_auditor_context(t) OR (
 EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=t AND subject=NULLIF(current_setting('app.actor_subject',true),'') AND role_key='governance_delegate')
 AND ((current_setting('app.iam_governance_access',true)='source' AND s=NULLIF(current_setting('app.actor_subject',true),''))
 OR(current_setting('app.iam_governance_access',true)='lock' AND g::text=ANY(string_to_array(COALESCE(current_setting('app.iam_governance_lock_ids',true),''),',')))))
 )
$$;
ALTER POLICY tenant_isolation ON iam_governance_grants
 USING(hms_tenant_visible(tenant_id) AND iam_governance_read(tenant_id,id,subject))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (iam_governance_owner(tenant_id) OR iam_governance_auditor_context(tenant_id)));

ALTER POLICY tenant_isolation ON iam_governance_owner_notices
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (iam_governance_owner(tenant_id) OR iam_governance_auditor_context(tenant_id)));

CREATE TABLE iam_governance_decisions (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 grant_id uuid NOT NULL,
 grant_subject text NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 9007199254740990),
 result_version bigint NOT NULL CHECK(result_version=expected_version+1),
 decision text NOT NULL CHECK(decision IN('approve','reject','revoke')),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200 AND actor_subject<>grant_subject),
 actor_membership_epoch bigint NOT NULL CHECK(actor_membership_epoch>0),
 authority_kind text NOT NULL CHECK(authority_kind IN('owner','auditor')),
 authority_grant_id uuid,
 authority_grant_version bigint,
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,grant_id,expected_version),
 FOREIGN KEY(tenant_id,grant_id) REFERENCES iam_governance_grants(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,authority_grant_id) REFERENCES iam_governance_grants(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,actor_subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT,
 CHECK((authority_kind='owner')=(authority_grant_id IS NULL)),
 CHECK((authority_grant_id IS NULL)=(authority_grant_version IS NULL)),
 CHECK(receipt->>'id' IS NOT DISTINCT FROM id::text AND receipt->>'tenant_id' IS NOT DISTINCT FROM tenant_id::text
 AND receipt->>'grant_id' IS NOT DISTINCT FROM grant_id::text AND receipt->>'actor_subject' IS NOT DISTINCT FROM actor_subject
 AND receipt->>'result_version' IS NOT DISTINCT FROM result_version::text
 AND receipt#>>'{input,expected_version}' IS NOT DISTINCT FROM expected_version::text
 AND receipt#>>'{input,decision}' IS NOT DISTINCT FROM decision)
);
ALTER TABLE iam_governance_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_governance_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_governance_decisions
 USING(hms_tenant_visible(tenant_id) AND iam_governance_read(tenant_id,grant_id,grant_subject))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (iam_governance_owner(tenant_id) OR iam_governance_auditor_context(tenant_id)));
CREATE INDEX iam_governance_decisions_history ON iam_governance_decisions(tenant_id,grant_id,created_at DESC,id DESC);
CREATE TRIGGER iam_governance_decisions_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_governance_decisions
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE FUNCTION iam_governance_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent iam_governance_grants; source iam_governance_grants;
BEGIN
 SELECT * INTO parent FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND id=NEW.grant_id;
 IF NOT FOUND OR current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'decision'
 OR NEW.id::text IS DISTINCT FROM NULLIF(current_setting('app.iam_governance_decision_id',true),'')
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NEW.expected_version<>parent.version OR NEW.grant_subject<>parent.subject
 OR NOT EXISTS(SELECT 1 FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND epoch=NEW.actor_membership_epoch AND member_count>0)
 OR (NEW.decision IN('approve','reject') AND (NEW.actor_subject=parent.issued_by OR parent.status NOT IN('pending_review','provisional_active')))
 OR (NEW.decision='revoke' AND parent.status NOT IN('pending_review','provisional_active','active')) THEN
  RAISE EXCEPTION 'governance decision requires current independent source and version' USING ERRCODE='23514';
 END IF;
 IF NEW.authority_kind='owner' THEN
  IF NOT iam_governance_owner(NEW.tenant_id) THEN RAISE EXCEPTION 'current owner required' USING ERRCODE='42501';END IF;
 ELSE
  SELECT * INTO source FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND id=NEW.authority_grant_id;
  IF NOT FOUND OR source.subject<>NEW.actor_subject OR source.version<>NEW.authority_grant_version OR source.membership_epoch<>NEW.actor_membership_epoch
   OR source.template_key<>'iam.governance_auditor.v1' OR source.scope_kind<>'tenant' OR source.scope_id IS NOT NULL
   OR source.status NOT IN('active','provisional_active') OR source.valid_from>clock_timestamp() OR source.expires_at<=clock_timestamp()
   OR (source.status='provisional_active' AND (1<>(SELECT count(*) FROM iam_members WHERE tenant_id=NEW.tenant_id AND role_key='tenant_admin') OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND role_key='tenant_admin' AND subject=source.issued_by))) THEN
   RAISE EXCEPTION 'current independent auditor source required' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_governance_decision_guard BEFORE INSERT ON iam_governance_decisions
 FOR EACH ROW EXECUTE FUNCTION iam_governance_decision_guard();

CREATE FUNCTION iam_governance_transition_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command iam_governance_decisions;
BEGIN
 IF ROW(NEW.tenant_id,NEW.id,NEW.subject,NEW.membership_epoch,NEW.template_key,NEW.scope_kind,NEW.scope_id,NEW.input,NEW.input_hash,NEW.issued_by,NEW.owner_count,NEW.authorization_mode,NEW.valid_from,NEW.expires_at,NEW.receipt,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.tenant_id,OLD.id,OLD.subject,OLD.membership_epoch,OLD.template_key,OLD.scope_kind,OLD.scope_id,OLD.input,OLD.input_hash,OLD.issued_by,OLD.owner_count,OLD.authorization_mode,OLD.valid_from,OLD.expires_at,OLD.receipt,OLD.created_at) THEN
  RAISE EXCEPTION 'governance creation evidence is immutable' USING ERRCODE='23514';
 END IF;
 SELECT * INTO command FROM iam_governance_decisions WHERE tenant_id=OLD.tenant_id AND grant_id=OLD.id
 AND id=NULLIF(current_setting('app.iam_governance_decision_id',true),'')::uuid AND expected_version=OLD.version;
 IF NOT FOUND OR current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'decision'
 OR command.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NEW.version<>command.result_version OR NEW.updated_at<>command.created_at OR NEW.updated_at<=OLD.updated_at
 OR NEW.status IS DISTINCT FROM CASE command.decision WHEN 'approve' THEN 'active' WHEN 'reject' THEN 'rejected' ELSE 'revoked' END THEN
  RAISE EXCEPTION 'governance transition requires its retained decision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER iam_governance_grant_guard ON iam_governance_grants;
CREATE TRIGGER iam_governance_grant_guard BEFORE INSERT ON iam_governance_grants
 FOR EACH ROW EXECUTE FUNCTION iam_governance_grant_guard();
CREATE TRIGGER iam_governance_transition_guard BEFORE UPDATE ON iam_governance_grants
 FOR EACH ROW EXECUTE FUNCTION iam_governance_transition_guard();
CREATE OR REPLACE FUNCTION iam_governance_notice_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.iam_governance_command',true),'') NOT IN('grant','decision')
 OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.recipient_subject AND role_key='tenant_admin')
 OR NOT EXISTS(SELECT 1 FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND id=NEW.grant_id AND version=NEW.grant_version)
 OR NEW.seen_at IS NOT NULL THEN
  RAISE EXCEPTION 'governance owner notice requires an attributed transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION iam_governance_routing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.role_key='governance_delegate' AND (
 COALESCE(current_setting('app.iam_governance_command',true),'') NOT IN('grant','decision')
 OR NOT EXISTS(SELECT 1 FROM iam_governance_grants WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject
 AND status IN('active','provisional_active') AND expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'governance routing membership requires current source intent' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
