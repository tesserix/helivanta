SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_partner_groups (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 membership_policy_version bigint NOT NULL DEFAULT 1 CHECK(membership_policy_version=1),
 hospital_id uuid NOT NULL CHECK (hospital_id<>'00000000-0000-0000-0000-000000000000'),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 120),
 purpose text NOT NULL CHECK(purpose IN ('procurement','receiving','finance')),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 500),
 valid_from timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>valid_from AND expires_at<=valid_from+interval '366 days'),
 status text NOT NULL DEFAULT 'pending_scope' CHECK(status='pending_scope'),
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),
 proposed_by text NOT NULL CHECK(char_length(proposed_by) BETWEEN 1 AND 200),
 proposer_membership_epoch bigint NOT NULL CHECK(proposer_membership_epoch>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id)
);
ALTER TABLE iam_partner_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_partner_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_partner_groups
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true) IN ('read','write','observe'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true)='write');
CREATE INDEX ON iam_partner_groups(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE FUNCTION iam_partner_group_preserve() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'partner group proposals are immutable' USING ERRCODE='23514'; END $$;
CREATE TRIGGER iam_partner_group_immutable BEFORE UPDATE OR DELETE ON iam_partner_groups FOR EACH ROW EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_partner_group_no_truncate BEFORE TRUNCATE ON iam_partner_groups FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();

CREATE TABLE iam_partner_group_scope_observations (
 tenant_id uuid NOT NULL, group_id uuid NOT NULL, hospital_id uuid NOT NULL,
 observed_exists boolean NOT NULL, source_event_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,group_id),
 FOREIGN KEY(tenant_id,group_id) REFERENCES iam_partner_groups(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_partner_group_scope_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_partner_group_scope_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_partner_group_scope_observations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true) IN('read','write','observe'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true)='observe');
CREATE TRIGGER iam_partner_group_scope_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_partner_group_scope_observations FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TABLE iam_partner_group_commands (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN('8','9','a','b')),
 group_id uuid NOT NULL, hospital_id uuid NOT NULL,
 operation text NOT NULL CHECK(operation IN('approve','reject','withdraw','suspend')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 9007199254740990),
 result_version bigint NOT NULL CHECK(result_version=expected_version+1),
 result_status text NOT NULL CHECK(result_status IN('approved','rejected','withdrawn','suspended')),
 actor_subject text NOT NULL CHECK(char_length(actor_subject) BETWEEN 1 AND 200),
 actor_membership_epoch bigint NOT NULL CHECK(actor_membership_epoch>0),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 500),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'), receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,group_id,expected_version),
 FOREIGN KEY(tenant_id,group_id) REFERENCES iam_partner_groups(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_partner_group_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_partner_group_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_partner_group_commands
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true) IN('read','write','observe'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_groups',true)='write');
CREATE INDEX ON iam_partner_group_commands(tenant_id,group_id,result_version DESC);
CREATE TRIGGER iam_partner_group_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_partner_group_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE FUNCTION iam_partner_group_command_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p iam_partner_groups%ROWTYPE; current_status text; current_version bigint; eligible boolean;
BEGIN
 SELECT * INTO p FROM iam_partner_groups WHERE tenant_id=NEW.tenant_id AND id=NEW.group_id FOR UPDATE;
 IF NOT FOUND OR p.hospital_id<>NEW.hospital_id OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'partner group command scope changed' USING ERRCODE='23514',CONSTRAINT='iam_partner_group_command_guard';
 END IF;
 SELECT result_status,result_version INTO current_status,current_version FROM iam_partner_group_commands WHERE tenant_id=NEW.tenant_id AND group_id=NEW.group_id ORDER BY result_version DESC LIMIT 1;
 IF NOT FOUND THEN
  SELECT observed_exists INTO eligible FROM iam_partner_group_scope_observations WHERE tenant_id=NEW.tenant_id AND group_id=NEW.group_id AND hospital_id=NEW.hospital_id;
  current_status:=CASE WHEN NOT FOUND THEN 'pending_scope' WHEN eligible THEN 'pending_review' ELSE 'scope_rejected' END;
  current_version:=CASE WHEN current_status='pending_scope' THEN 1 ELSE 2 END;
 END IF;
 IF NEW.expected_version<>current_version
  OR (NEW.operation IN('approve','reject') AND (current_status<>'pending_review' OR NEW.actor_subject=p.proposed_by OR p.expires_at<=clock_timestamp()))
  OR (NEW.operation='withdraw' AND (current_status NOT IN('pending_scope','pending_review') OR NEW.actor_subject<>p.proposed_by))
  OR (NEW.operation='suspend' AND current_status<>'approved')
  OR NEW.result_status IS DISTINCT FROM (CASE NEW.operation WHEN 'approve' THEN 'approved' WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' WHEN 'suspend' THEN 'suspended' END)
  OR NEW.receipt->>'id' IS DISTINCT FROM NEW.id::text
  OR NEW.receipt->>'group_id' IS DISTINCT FROM NEW.group_id::text
  OR NEW.receipt#>>'{group,id}' IS DISTINCT FROM NEW.group_id::text
  OR NEW.receipt#>>'{group,hospital_id}' IS DISTINCT FROM NEW.hospital_id::text
  OR NEW.receipt#>>'{group,membership_policy_version}' IS DISTINCT FROM p.membership_policy_version::text
  OR NEW.receipt#>>'{group,status}' IS DISTINCT FROM NEW.result_status
  OR NEW.receipt#>>'{group,version}' IS DISTINCT FROM NEW.result_version::text
  OR NEW.receipt#>>'{group,external_access_enabled}' IS DISTINCT FROM 'false' THEN
  RAISE EXCEPTION 'partner group decision requires a current independent transition' USING ERRCODE='23514',CONSTRAINT='iam_partner_group_command_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_partner_group_command_guard BEFORE INSERT ON iam_partner_group_commands FOR EACH ROW EXECUTE FUNCTION iam_partner_group_command_guard();
