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
  IF current_setting('app.iam_governance_command',true) IS DISTINCT FROM 'grant'
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
