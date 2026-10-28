SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE emergency_bases (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (get_byte(uuid_send(id),6)/16=7),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 150),
 timezone text NOT NULL CHECK (length(timezone) BETWEEN 1 AND 100),
 policy_reference text NOT NULL CHECK (policy_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 contact_retention_days int NOT NULL CHECK(contact_retention_days BETWEEN 1 AND 365),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
ALTER TABLE emergency_bases ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_bases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_bases
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.emergency_admin',true)='true');
CREATE INDEX ON emergency_bases(tenant_id,created_at DESC,id DESC);

CREATE TABLE emergency_base_access (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 level text NOT NULL CHECK(level IN('reader','dispatcher','supervisor')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,subject),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE emergency_base_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_base_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_base_access
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND (current_setting('app.emergency_admin',true)='true' OR subject=NULLIF(current_setting('app.emergency_subject',true),'')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.emergency_admin',true)='true');

CREATE FUNCTION emergency_allowed(t uuid,b uuid,ability text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND b=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND EXISTS(SELECT 1 FROM emergency_base_access a WHERE a.tenant_id=t AND a.base_id=b
 AND a.subject=NULLIF(current_setting('app.emergency_subject',true),'')
 AND (ability='read' OR (ability='intake' AND a.level IN('dispatcher','supervisor')) OR (ability='close' AND a.level='supervisor')))
$$;

CREATE TABLE emergency_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK(length(actor_subject)>0),
 action text NOT NULL,
 resource_id uuid NOT NULL,
 target_subject text,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_audit(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_audit
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (current_setting('app.emergency_admin',true)='true' OR emergency_allowed(tenant_id,base_id,'read')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject=NULLIF(current_setting('app.emergency_subject',true),''));
CREATE FUNCTION emergency_append_only() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'emergency history is append only' USING ERRCODE='23514'; END $$;
CREATE TRIGGER emergency_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_audit FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE TRIGGER emergency_base_no_removal BEFORE DELETE OR TRUNCATE ON emergency_bases FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE TRIGGER emergency_access_no_truncate BEFORE TRUNCATE ON emergency_base_access FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE TABLE emergency_cases (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 case_number text NOT NULL,
 channel text NOT NULL CHECK(channel IN('phone','walk_in','inter_facility')),
 requested_service text NOT NULL CHECK(requested_service IN('unknown','bls','als','neonatal','patient_transport','mortuary','home_visit')),
 processing_basis text NOT NULL CHECK(processing_basis IN('medical_emergency','consent')),
 policy_reference text NOT NULL,
 status text NOT NULL DEFAULT 'received' CHECK(status IN('received','closed')),
 priority text NOT NULL DEFAULT 'untriaged' CHECK(priority='untriaged'),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 request_fingerprint text NOT NULL CHECK(request_fingerprint LIKE 'vault:v1:%'),
 contact_ciphertext text CHECK(contact_ciphertext LIKE 'vault:v%'),
 incident_ciphertext text NOT NULL CHECK(incident_ciphertext LIKE 'vault:v%'),
 contact_retain_until timestamptz NOT NULL,
 contact_purged_at timestamptz,
 closure_reason text CHECK(closure_reason IN('duplicate_report','caller_withdrew','no_response_required','entered_in_error')),
 closure_ciphertext text CHECK(closure_ciphertext LIKE 'vault:v%'),
 closure_fingerprint text CHECK(closure_fingerprint LIKE 'vault:v1:%'),
 closed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,case_number),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT,
 CHECK(contact_retain_until>created_at),
 CHECK((contact_ciphertext IS NULL)=(contact_purged_at IS NOT NULL)),
 CHECK((status='received' AND closed_at IS NULL AND closure_reason IS NULL AND closure_ciphertext IS NULL AND closure_fingerprint IS NULL AND version=1)
 OR (status='closed' AND closed_at IS NOT NULL AND closure_reason IS NOT NULL AND closure_ciphertext IS NOT NULL AND closure_fingerprint IS NOT NULL AND version=2)),
 CHECK(processing_basis<>'medical_emergency' OR requested_service NOT IN('patient_transport','mortuary'))
);
CREATE INDEX ON emergency_cases(tenant_id,base_id,created_at DESC,id DESC);
CREATE INDEX ON emergency_cases(contact_retain_until,tenant_id,base_id,id) WHERE contact_ciphertext IS NOT NULL;
ALTER TABLE emergency_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_cases
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake'));

CREATE TABLE emergency_case_timeline (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 case_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 version bigint NOT NULL,
 state text NOT NULL CHECK(state IN('received','closed')),
 reason text,
 actor_subject text NOT NULL CHECK(length(actor_subject)>0),
 occurred_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,case_id,version),
 FOREIGN KEY(tenant_id,base_id,case_id) REFERENCES emergency_cases(tenant_id,base_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_case_timeline(tenant_id,base_id,case_id,created_at DESC,id DESC);
CREATE FUNCTION emergency_timeline_matches_case() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM emergency_cases c WHERE c.tenant_id=NEW.tenant_id AND c.base_id=NEW.base_id AND c.id=NEW.case_id AND c.version=NEW.version AND c.status=NEW.state AND c.closure_reason IS NOT DISTINCT FROM NEW.reason) THEN
 RAISE EXCEPTION 'timeline must match the current case transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_timeline_case_guard BEFORE INSERT ON emergency_case_timeline FOR EACH ROW EXECUTE FUNCTION emergency_timeline_matches_case();
ALTER TABLE emergency_case_timeline ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_case_timeline FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_case_timeline
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake') AND actor_subject=NULLIF(current_setting('app.emergency_subject',true),''));
CREATE TRIGGER emergency_timeline_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_case_timeline FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE TRIGGER emergency_case_no_removal BEFORE DELETE OR TRUNCATE ON emergency_cases FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE FUNCTION emergency_case_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.base_id,NEW.id,NEW.case_number,NEW.channel,NEW.requested_service,NEW.processing_basis,NEW.policy_reference,NEW.priority,NEW.request_fingerprint,NEW.incident_ciphertext,NEW.contact_retain_until,NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.id,OLD.case_number,OLD.channel,OLD.requested_service,OLD.processing_basis,OLD.policy_reference,OLD.priority,OLD.request_fingerprint,OLD.incident_ciphertext,OLD.contact_retain_until,OLD.created_at) THEN
 RAISE EXCEPTION 'case intake identity is immutable' USING ERRCODE='23514'; END IF;
 IF current_user='helivanta_system' AND OLD.contact_ciphertext IS NOT NULL AND NEW.contact_ciphertext IS NULL AND OLD.contact_retain_until<=now()
 AND NEW.contact_purged_at>=OLD.contact_retain_until AND NEW.contact_purged_at<=now()
 AND (NEW.version,NEW.status,NEW.closed_at,NEW.closure_reason,NEW.closure_ciphertext,NEW.closure_fingerprint) IS NOT DISTINCT FROM (OLD.version,OLD.status,OLD.closed_at,OLD.closure_reason,OLD.closure_ciphertext,OLD.closure_fingerprint) THEN RETURN NEW; END IF;
 IF (NEW.contact_ciphertext,NEW.contact_purged_at) IS DISTINCT FROM (OLD.contact_ciphertext,OLD.contact_purged_at) THEN
 RAISE EXCEPTION 'caller contact changes require expiry' USING ERRCODE='23514'; END IF;
 IF NOT (OLD.status='received' AND NEW.status='closed' AND NEW.version=OLD.version+1 AND emergency_allowed(NEW.tenant_id,NEW.base_id,'close')) THEN
 RAISE EXCEPTION 'case transition is not authorised' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_case_guard BEFORE UPDATE ON emergency_cases FOR EACH ROW EXECUTE FUNCTION emergency_case_change();
CREATE FUNCTION emergency_case_timeline_required() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM emergency_case_timeline t WHERE t.tenant_id=NEW.tenant_id AND t.base_id=NEW.base_id AND t.case_id=NEW.id AND t.version=NEW.version AND t.state=NEW.status AND t.reason IS NOT DISTINCT FROM NEW.closure_reason) THEN
 RAISE EXCEPTION 'case change requires its immutable timeline' USING ERRCODE='23514'; END IF; RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER emergency_case_timeline_required AFTER INSERT OR UPDATE ON emergency_cases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION emergency_case_timeline_required();

GRANT SELECT ON emergency_cases,emergency_case_timeline TO helivanta_system;
GRANT UPDATE(contact_ciphertext,contact_purged_at,updated_at) ON emergency_cases TO helivanta_system;
GRANT INSERT ON emergency_audit TO helivanta_system;
GRANT SELECT(id) ON emergency_audit TO helivanta_system;
GRANT USAGE ON SEQUENCE emergency_audit_id_seq TO helivanta_system;
