SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION pharmacy_staff_binding_context(t uuid,o uuid,h uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND h=NULLIF(current_setting('app.pharmacy_staff_binding_hospital',true),'')::uuid
 AND NULLIF(current_setting('app.pharmacy_staff_binding_assignment',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.pharmacy_staff_binding_actor',true),'') IS NOT NULL
 AND current_setting('app.pharmacy_staff_binding_permission',true) IN
 ('pharmacy.staff_binding.read','pharmacy.staff_binding.prepare','pharmacy.staff_binding.review')
 AND current_setting('app.pharmacy_staff_binding_mode',true) IN ('read','write')
 AND (NOT writing OR (current_setting('app.pharmacy_staff_binding_mode',true)='write'
 AND current_setting('app.pharmacy_staff_binding_permission',true) IN ('pharmacy.staff_binding.prepare','pharmacy.staff_binding.review')))
 AND pharmacy_outlet_allowed(t,o,writing),false)
$$;

CREATE TABLE pharmacy_staff_department_bindings (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'),
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7'),
 department_revision_id uuid NOT NULL CHECK(substring(department_revision_id::text,15,1)='7'),
 replaces_binding_id uuid CHECK(substring(replaces_binding_id::text,15,1)='7'),
 replaces_binding_version bigint CHECK(replaces_binding_version BETWEEN 1 AND 9007199254740990),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500 AND reason !~ '[[:cntrl:]]'),
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','withdrawn','ended')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740990),
 proposed_by_subject text NOT NULL CHECK(length(proposed_by_subject) BETWEEN 1 AND 200),
 proposer_staff_id uuid NOT NULL CHECK(proposer_staff_id<>'00000000-0000-0000-0000-000000000000'),
 created_at timestamptz NOT NULL,
 decided_by_subject text,
 reviewer_staff_id uuid,
 decision_reason text CHECK(length(btrim(decision_reason)) BETWEEN 1 AND 500 AND decision_reason !~ '[[:cntrl:]]'),
 decided_at timestamptz,
 ended_at timestamptz,
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,outlet_id,hospital_id,id),
 FOREIGN KEY(tenant_id,outlet_id) REFERENCES pharmacy_outlets(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,hospital_id,replaces_binding_id) REFERENCES pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,id),
 CHECK((replaces_binding_id IS NULL)=(replaces_binding_version IS NULL)),
 CHECK(replaces_binding_id IS NULL OR replaces_binding_id<>id),
 CHECK((status='pending' AND version=1 AND decided_by_subject IS NULL AND reviewer_staff_id IS NULL AND decision_reason IS NULL AND decided_at IS NULL AND ended_at IS NULL)
 OR(status IN ('approved','rejected','withdrawn','ended') AND version>=2 AND decided_by_subject IS NOT NULL AND reviewer_staff_id IS NOT NULL AND decision_reason IS NOT NULL AND decided_at>=created_at
 AND ((status='ended' AND ended_at>=decided_at) OR(status<>'ended' AND ended_at IS NULL))))
);
CREATE UNIQUE INDEX pharmacy_staff_binding_current ON pharmacy_staff_department_bindings(tenant_id,outlet_id) WHERE status='approved';
CREATE INDEX pharmacy_staff_binding_page ON pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_staff_department_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_staff_department_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings
 USING(hms_tenant_visible(tenant_id) AND pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,false))
 WITH CHECK(pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,true));

CREATE FUNCTION pharmacy_staff_binding_state_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text:=current_setting('app.pharmacy_subject',true);
 actor_staff uuid:=NULLIF(current_setting('app.pharmacy_staff_binding_actor',true),'')::uuid;
 permission text:=current_setting('app.pharmacy_staff_binding_permission',true);
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'department binding history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF permission<>'pharmacy.staff_binding.prepare' OR NEW.status<>'pending' OR NEW.version<>1 OR NEW.proposed_by_subject IS DISTINCT FROM actor OR NEW.proposer_staff_id IS DISTINCT FROM actor_staff THEN
   RAISE EXCEPTION 'department binding preparation requires current scoped authority' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.tenant_id,NEW.outlet_id,NEW.hospital_id,NEW.id,NEW.department_id,NEW.department_revision_id,NEW.replaces_binding_id,NEW.replaces_binding_version,NEW.reason,NEW.proposed_by_subject,NEW.proposer_staff_id,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.tenant_id,OLD.outlet_id,OLD.hospital_id,OLD.id,OLD.department_id,OLD.department_revision_id,OLD.replaces_binding_id,OLD.replaces_binding_version,OLD.reason,OLD.proposed_by_subject,OLD.proposer_staff_id,OLD.created_at)
 OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'department binding intent is immutable and versioned' USING ERRCODE='23514';
 END IF;
 IF OLD.status='pending' AND NEW.status IN ('approved','rejected','withdrawn') THEN
  IF NEW.decided_by_subject IS DISTINCT FROM actor OR NEW.reviewer_staff_id IS DISTINCT FROM actor_staff THEN
   RAISE EXCEPTION 'department binding decision actor is required' USING ERRCODE='23514';
  END IF;
  IF NEW.status='withdrawn' THEN
   IF permission<>'pharmacy.staff_binding.prepare' OR actor IS DISTINCT FROM OLD.proposed_by_subject OR actor_staff IS DISTINCT FROM OLD.proposer_staff_id THEN
    RAISE EXCEPTION 'only the current proposer can withdraw a pending binding' USING ERRCODE='23514';
   END IF;
  ELSIF permission<>'pharmacy.staff_binding.review' OR actor=OLD.proposed_by_subject OR actor_staff=OLD.proposer_staff_id THEN
   RAISE EXCEPTION 'department binding review must be independent' USING ERRCODE='23514',CONSTRAINT='pharmacy_staff_binding_independence';
  END IF;
 ELSIF OLD.status='approved' AND NEW.status='ended' AND permission='pharmacy.staff_binding.review' THEN
  IF ROW(NEW.decided_by_subject,NEW.reviewer_staff_id,NEW.decision_reason,NEW.decided_at) IS DISTINCT FROM ROW(OLD.decided_by_subject,OLD.reviewer_staff_id,OLD.decision_reason,OLD.decided_at) THEN
   RAISE EXCEPTION 'ending retains original binding approval' USING ERRCODE='23514';
  END IF;
 ELSE
  RAISE EXCEPTION 'invalid department binding transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_staff_binding_state_guard BEFORE INSERT OR UPDATE OR DELETE ON pharmacy_staff_department_bindings
 FOR EACH ROW EXECUTE FUNCTION pharmacy_staff_binding_state_guard();
CREATE TRIGGER pharmacy_staff_binding_no_truncate BEFORE TRUNCATE ON pharmacy_staff_department_bindings
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_staff_binding_commands (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 binding_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 operation text NOT NULL CHECK(operation IN ('propose','approve','reject','withdraw','end')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740989),
 input_hash text NOT NULL CHECK(input_hash~'^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 actor_staff_id uuid NOT NULL CHECK(actor_staff_id<>'00000000-0000-0000-0000-000000000000'),
 actor_source jsonb NOT NULL CHECK(jsonb_typeof(actor_source)='object' AND octet_length(actor_source::text)<=2048),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500 AND reason !~ '[[:cntrl:]]'),
 recorded_at timestamptz NOT NULL,
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=32768),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,hospital_id,binding_id) REFERENCES pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,id),
 CHECK(actor_subject IS NOT DISTINCT FROM receipt->>'actor_subject')
);
CREATE INDEX pharmacy_staff_binding_commands_page ON pharmacy_staff_binding_commands(tenant_id,outlet_id,hospital_id,binding_id,recorded_at DESC,id DESC);
ALTER TABLE pharmacy_staff_binding_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_staff_binding_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY pharmacy_staff_binding_command_scope ON pharmacy_staff_binding_commands
 USING(hms_tenant_visible(tenant_id) AND pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,false))
 WITH CHECK(pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,true));
CREATE TRIGGER pharmacy_staff_binding_commands_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_staff_binding_commands
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_staff_binding_audit (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 binding_id uuid,
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 action text NOT NULL CHECK(action IN ('read','propose','approve','reject','withdraw','end')),
 command_id uuid,
 recorded_at timestamptz NOT NULL,
 changes jsonb NOT NULL CHECK(jsonb_typeof(changes)='object' AND octet_length(changes::text)<=4096),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id) REFERENCES pharmacy_outlets(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,hospital_id,binding_id) REFERENCES pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,id)
);
CREATE INDEX pharmacy_staff_binding_audit_page ON pharmacy_staff_binding_audit(tenant_id,outlet_id,hospital_id,recorded_at DESC,id DESC);
ALTER TABLE pharmacy_staff_binding_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_staff_binding_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY pharmacy_staff_binding_audit_scope ON pharmacy_staff_binding_audit
 USING(hms_tenant_visible(tenant_id) AND pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,false))
 WITH CHECK(pharmacy_staff_binding_context(tenant_id,outlet_id,hospital_id,false));
CREATE TRIGGER pharmacy_staff_binding_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_staff_binding_audit
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
