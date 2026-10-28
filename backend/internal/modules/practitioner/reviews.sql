SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_registration_reviews (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 registration_id uuid NOT NULL,
 revision_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 appointment_id uuid NOT NULL,
 appointment_version bigint NOT NULL CHECK(appointment_version=2),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7' AND substring(review_reference::text,20,1) IN ('8','9','a','b')),
 source_reference uuid NOT NULL CHECK(substring(source_reference::text,15,1)='7' AND substring(source_reference::text,20,1) IN ('8','9','a','b')),
 source_checked_at timestamptz NOT NULL CHECK(source_checked_at BETWEEN '0001-01-01T00:00:00Z' AND '9999-12-31T23:59:59.999999Z'),
 finding text NOT NULL CHECK(finding IN ('active','suspended','expired','not_found','unavailable')),
 evidence_ids jsonb NOT NULL CHECK(jsonb_typeof(evidence_ids)='array' AND jsonb_array_length(evidence_ids) BETWEEN 1 AND 10),
 evidence_snapshot jsonb NOT NULL CHECK(jsonb_typeof(evidence_snapshot)='array' AND jsonb_array_length(evidence_snapshot)=jsonb_array_length(evidence_ids)),
 reviewer text NOT NULL CHECK(octet_length(reviewer) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id) REFERENCES practitioner_registrations(tenant_id,practitioner_id,id),
 FOREIGN KEY(tenant_id,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,id),
 FOREIGN KEY(tenant_id,appointment_id,appointment_version) REFERENCES practitioner_steward_events(tenant_id,appointment_id,version)
);
CREATE INDEX practitioner_review_page ON practitioner_registration_reviews(tenant_id,practitioner_id,registration_id,revision_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_reviews
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_review',true)='true' AND reviewer=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head bigint; registration_author text; revision_author text; identity_author text; appointment practitioner_steward_appointments; evidence practitioner_registration_evidence; file text; snapshots jsonb:='[]'::jsonb; previous_file text; at_time timestamptz:=practitioner_registry_now();
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'registration review is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_review',true) IS DISTINCT FROM 'true' OR NEW.reviewer IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'registration review authority required' USING ERRCODE='42501'; END IF;
 SELECT version,created_by INTO head,registration_author FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.registration_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration review parent unavailable' USING ERRCODE='P0002'; END IF;
 SELECT created_by INTO revision_author FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id AND version=head;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration review revision is stale' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 SELECT created_by INTO identity_author FROM practitioner_records WHERE tenant_id=NEW.tenant_id AND id=NEW.practitioner_id;
 SELECT * INTO appointment FROM practitioner_steward_appointments WHERE tenant_id=NEW.tenant_id AND id=NEW.appointment_id AND subject=NEW.reviewer FOR UPDATE;
 at_time:=practitioner_registry_now();
 IF NOT FOUND OR appointment.status<>'active' OR appointment.version<>NEW.appointment_version OR appointment.valid_until<=at_time OR NEW.source_checked_at>at_time OR NEW.reviewer IN (identity_author,registration_author,revision_author) THEN RAISE EXCEPTION 'current independent reviewer appointment required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 IF jsonb_typeof(NEW.evidence_ids)<>'array' OR jsonb_array_length(NEW.evidence_ids) NOT BETWEEN 1 AND 10 THEN RAISE EXCEPTION 'bounded review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 FOR file IN SELECT jsonb_array_elements_text(NEW.evidence_ids) ORDER BY 1 LOOP
  IF file=previous_file OR file::uuid::text<>file THEN RAISE EXCEPTION 'unique canonical review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
  SELECT * INTO evidence FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=file::uuid FOR UPDATE;
  IF NOT FOUND OR evidence.status<>'available' OR evidence.created_by=NEW.reviewer OR evidence.released_generation IS NULL OR evidence.scanned_at IS NULL THEN RAISE EXCEPTION 'released independent review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
  snapshots:=snapshots||jsonb_build_array(jsonb_build_object('id',evidence.id,'sha256',evidence.sha256,'size',evidence.size,'source_generation',evidence.source_generation,'released_generation',evidence.released_generation,'scan_attempt',evidence.scan_attempt,'scanned_at',evidence.scanned_at));
  previous_file:=file;
 END LOOP;
 at_time:=practitioner_registry_now();
 IF appointment.valid_until<=at_time THEN RAISE EXCEPTION 'reviewer appointment elapsed before review' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 NEW.evidence_ids:=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements(NEW.evidence_ids));
 NEW.evidence_snapshot:=snapshots; NEW.created_at:=at_time;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_review_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_reviews FOR EACH ROW EXECUTE FUNCTION practitioner_review_guard();
CREATE TRIGGER practitioner_review_audit AFTER INSERT ON practitioner_registration_reviews FOR EACH ROW EXECUTE FUNCTION audit_record_change();
