SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_credential_reminder_thresholds_ok(t integer[]) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT t IS NOT NULL AND array_ndims(t)=1 AND array_lower(t,1)=1 AND cardinality(t) BETWEEN 1 AND 6
  AND NOT EXISTS(SELECT 1 FROM unnest(t) WITH ORDINALITY a(v,i) WHERE v IS NULL OR v<0 OR v>365 OR (i>1 AND v>=t[i-1]))
$$;

CREATE TABLE workforce_credential_reminder_policies (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, version bigint NOT NULL CHECK (version > 0),
 thresholds integer[] NOT NULL CONSTRAINT workforce_credential_reminder_thresholds CHECK (workforce_credential_reminder_thresholds_ok(thresholds)),
 created_by_subject text NOT NULL CHECK (length(created_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,version),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION workforce_credential_reminder_policy_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.version IS DISTINCT FROM COALESCE((SELECT max(p.version) FROM workforce_credential_reminder_policies p WHERE (p.tenant_id,p.hospital_id)=(NEW.tenant_id,NEW.hospital_id)),0)+1 THEN
  RAISE EXCEPTION 'reminder policy versions advance by one' USING ERRCODE='23514', CONSTRAINT='workforce_credential_reminder_policy_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_reminder_policy_advances BEFORE INSERT ON workforce_credential_reminder_policies FOR EACH ROW EXECUTE FUNCTION workforce_credential_reminder_policy_advances();
CREATE TRIGGER workforce_credential_reminder_policy_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_credential_reminder_policies FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();

CREATE TABLE workforce_credential_reminders (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL DEFAULT gen_random_uuid(),
 staff_id uuid NOT NULL, credential_id uuid NOT NULL, validity_version bigint NOT NULL CHECK (validity_version > 0),
 policy_version bigint NOT NULL, threshold_days integer NOT NULL CHECK (threshold_days BETWEEN 0 AND 365),
 channel text NOT NULL CHECK (channel = 'hr_inbox'),
 valid_until date NOT NULL, due_on date NOT NULL CHECK (due_on = valid_until - threshold_days),
 status text NOT NULL CHECK (status IN ('open','acknowledged','superseded')),
 acknowledged_by_subject text CHECK (length(acknowledged_by_subject) BETWEEN 1 AND 200), acknowledged_at timestamptz, superseded_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 CONSTRAINT workforce_credential_reminder_once UNIQUE(tenant_id,hospital_id,staff_id,credential_id,validity_version,threshold_days,channel),
 FOREIGN KEY(tenant_id,hospital_id,policy_version) REFERENCES workforce_credential_reminder_policies(tenant_id,hospital_id,version),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,credential_id,validity_version) REFERENCES workforce_credential_reviews(tenant_id,hospital_id,staff_id,credential_id,approved_version),
 CHECK ((status = 'acknowledged') = (acknowledged_by_subject IS NOT NULL AND acknowledged_at IS NOT NULL)),
 CHECK ((status = 'superseded') = (superseded_at IS NOT NULL))
);
CREATE INDEX workforce_credential_reminder_inbox ON workforce_credential_reminders(tenant_id,hospital_id,status,due_on,id);

-- A reminder is written only for the current revision and the current policy, so a late activity cannot revive a superseded schedule.
CREATE FUNCTION workforce_credential_reminder_written() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>'open' OR NEW.acknowledged_by_subject IS NOT NULL OR NEW.superseded_at IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM workforce_credential_validity v WHERE (v.tenant_id,v.hospital_id,v.staff_id,v.credential_id,v.version,v.valid_until)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.validity_version,NEW.valid_until))
   OR NOT EXISTS(SELECT 1 FROM workforce_credential_reminder_policies p WHERE (p.tenant_id,p.hospital_id,p.version)=(NEW.tenant_id,NEW.hospital_id,NEW.policy_version)
    AND NEW.threshold_days = ANY(p.thresholds)
    AND p.version=(SELECT max(q.version) FROM workforce_credential_reminder_policies q WHERE (q.tenant_id,q.hospital_id)=(NEW.tenant_id,NEW.hospital_id))) THEN
  RAISE EXCEPTION 'a reminder opens on the current reviewed revision and reminder policy' USING ERRCODE='23514', CONSTRAINT='workforce_credential_reminder_current';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_reminder_written BEFORE INSERT ON workforce_credential_reminders FOR EACH ROW EXECUTE FUNCTION workforce_credential_reminder_written();

CREATE FUNCTION workforce_credential_reminder_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allowed text[] := ARRAY['status','acknowledged_by_subject','acknowledged_at','superseded_at','updated_at'];
BEGIN
 IF OLD.status<>'open' OR NEW.status='open' OR (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed)
   OR (current_user='helivanta_document_worker' AND NEW.status<>'superseded') THEN
  RAISE EXCEPTION 'a reminder closes once and its content is immutable' USING ERRCODE='23514', CONSTRAINT='workforce_credential_reminder_closed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_reminder_transition BEFORE UPDATE ON workforce_credential_reminders FOR EACH ROW EXECUTE FUNCTION workforce_credential_reminder_transition();
CREATE TRIGGER workforce_credential_reminder_no_delete BEFORE DELETE OR TRUNCATE ON workforce_credential_reminders FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_credential_reminder_audit AFTER INSERT OR UPDATE ON workforce_credential_reminders FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- The worker sees one hospital, and one credential, only inside a reminder transaction.
CREATE FUNCTION workforce_reminder_worker_hospital(t uuid,h uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_document_worker'
  AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
  AND NULLIF(current_setting('app.workforce_reminder_credential',true),'') IS NOT NULL
  AND NULLIF(current_setting('app.workforce_document_file',true),'') IS NULL
$$;
CREATE FUNCTION workforce_reminder_worker_credential(t uuid,h uuid,s uuid,c uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_reminder_worker_hospital(t,h)
  AND s=NULLIF(current_setting('app.workforce_reminder_staff',true),'')::uuid
  AND c=NULLIF(current_setting('app.workforce_reminder_credential',true),'')::uuid
$$;

ALTER TABLE workforce_credential_reminder_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_credential_reminder_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_credential_reminder_policies
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_hospital(tenant_id,hospital_id) ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker' THEN false ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
ALTER TABLE workforce_credential_reminders ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_credential_reminders FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_credential_reminders
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_credential(tenant_id,hospital_id,staff_id,credential_id) ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_credential(tenant_id,hospital_id,staff_id,credential_id) ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
ALTER POLICY enrollment_isolation ON workforce_credential_validity
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_credential(tenant_id,hospital_id,staff_id,credential_id) ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
ALTER POLICY hospital_isolation ON workforce_roster_settings
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_hospital(tenant_id,hospital_id) ELSE workforce_staff_allowed(tenant_id,hospital_id,false) END);

-- Column grants keep numbers, issuers and every other HR field out of the worker's reach.
GRANT SELECT(tenant_id,hospital_id,staff_id,credential_id,valid_until,version) ON workforce_credential_validity TO helivanta_document_worker;
GRANT SELECT(tenant_id,hospital_id,timezone) ON workforce_roster_settings TO helivanta_document_worker;
GRANT SELECT ON workforce_credential_reminder_policies TO helivanta_document_worker;
GRANT SELECT,INSERT(tenant_id,hospital_id,staff_id,credential_id,validity_version,policy_version,threshold_days,channel,valid_until,due_on,status),UPDATE(status,superseded_at,updated_at)
 ON workforce_credential_reminders TO helivanta_document_worker;

ALTER POLICY tenant_isolation ON audit_events
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND CASE WHEN current_user='helivanta_document_worker' THEN
   (actor_subject='service:workforce-document-worker' AND resource_type='workforce_documents'
    AND resource_id=NULLIF(current_setting('app.workforce_document_file',true),'')::uuid AND action IN('read','update'))
   OR (actor_subject='service:workforce-credential-reminders' AND resource_type='workforce_credential_reminders' AND action IN('insert','update'))
  ELSE true END);
-- The row check lives in a worker-only policy, so other roles never plan a subquery on reminders.
CREATE POLICY workforce_worker_audit_boundary ON audit_events AS RESTRICTIVE FOR INSERT TO helivanta_document_worker
 WITH CHECK(resource_type='workforce_documents'
  OR EXISTS(SELECT 1 FROM workforce_credential_reminders r WHERE r.tenant_id=audit_events.tenant_id AND r.id=audit_events.resource_id));

-- The boundary is recreated so the worker may emit exactly document changes or its own reminder changes.
DROP POLICY workforce_worker_outbox_boundary ON outbox_events;
CREATE POLICY workforce_worker_outbox_boundary ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_document_worker
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND tenant_id IS NOT NULL AND payload->>'tenant_id'=tenant_id::text AND payload->>'type'=subject
 AND payload->'data'->>'hospital_id'=current_setting('app.workforce_hospital',true)
 AND ((subject='helivanta.in.workforce.document_changed.v1'
  AND payload->'data'->>'staff_id'=current_setting('app.workforce_document_staff',true)
  AND payload->'data'->>'file_id'=current_setting('app.workforce_document_file',true)
  AND EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=outbox_events.tenant_id
  AND workforce_document_worker_allowed(d.tenant_id,d.hospital_id,d.staff_id,d.id)
  AND d.status=payload->'data'->>'status' AND d.scan_attempt::text=payload->'data'->>'scan_attempt'))
 OR (subject='helivanta.in.workforce.credential_reminder_changed.v1'
  AND payload->'data'->>'staff_id'=current_setting('app.workforce_reminder_staff',true)
  AND payload->'data'->>'credential_id'=current_setting('app.workforce_reminder_credential',true)
  AND EXISTS(SELECT 1 FROM workforce_credential_reminders r WHERE r.tenant_id=outbox_events.tenant_id
  AND workforce_reminder_worker_credential(r.tenant_id,r.hospital_id,r.staff_id,r.credential_id)
  AND r.id::text=payload->'data'->>'reminder_id' AND r.status=payload->'data'->>'status'
  AND r.validity_version::text=payload->'data'->>'version' AND r.threshold_days::text=payload->'data'->>'threshold_days'))));
