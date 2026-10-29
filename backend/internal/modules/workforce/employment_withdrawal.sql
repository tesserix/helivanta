SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE workforce_employment_period_drafts DROP CONSTRAINT workforce_employment_period_drafts_status_check,
 ADD CONSTRAINT workforce_employment_period_drafts_status_check CHECK(status IN ('draft','withdrawn')) NOT VALID;
ALTER TABLE workforce_employment_period_drafts VALIDATE CONSTRAINT workforce_employment_period_drafts_status_check;
ALTER TABLE workforce_employment_period_history DROP CONSTRAINT workforce_employment_period_drafts_status_check,
 ADD CONSTRAINT workforce_employment_period_history_status_check CHECK(status IN ('draft','withdrawn')) NOT VALID;
ALTER TABLE workforce_employment_period_history VALIDATE CONSTRAINT workforce_employment_period_history_status_check;
CREATE OR REPLACE FUNCTION workforce_employment_period_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'employment draft history is retained' USING ERRCODE='23514'; END IF;
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') OR workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) IS NOT TRUE THEN
  RAISE EXCEPTION 'employment draft requires current enrollment authority' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'draft' OR NEW.version<>1 OR NEW.proposed_by_subject<>NEW.actor_subject THEN RAISE EXCEPTION 'invalid initial employment draft' USING ERRCODE='23514'; END IF;
  NEW.created_at:=clock_timestamp(); NEW.updated_at:=NEW.created_at;
 ELSE
  IF OLD.status<>'draft' THEN RAISE EXCEPTION 'withdrawn employment draft is terminal' USING ERRCODE='23514'; END IF;
  IF NEW.status='withdrawn' THEN
   IF current_setting('app.workforce_employment_withdrawal',true) IS DISTINCT FROM 'authorized'
    OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id
    OR (to_jsonb(NEW)-ARRAY['status','reason','revision_id','version','actor_subject','input_hash','updated_at'])
      IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','reason','revision_id','version','actor_subject','input_hash','updated_at']) THEN
    RAISE EXCEPTION 'withdrawal preserves employment terms and requires a new command' USING ERRCODE='23514';
   END IF;
   NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
   RETURN NEW;
  END IF;
  IF NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id OR (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.status,NEW.proposed_by_subject,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,OLD.status,OLD.proposed_by_subject,OLD.created_at) THEN RAISE EXCEPTION 'employment draft requires a new revision' USING ERRCODE='23514'; END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.staff_id AND s.version=NEW.staff_version AND s.employment_epoch=NEW.employment_epoch) THEN RAISE EXCEPTION 'employment source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_period_source'; END IF;
 IF NEW.sponsor_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.sponsor_id AND s.employment_status='active') THEN RAISE EXCEPTION 'sponsor unavailable' USING ERRCODE='23514',CONSTRAINT='workforce_employment_period_sponsor'; END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION workforce_employment_evidence_draft_open() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_employment_period_drafts p
  WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.version)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.period_id,NEW.period_version)
  AND p.status='draft') THEN
  RAISE EXCEPTION 'new evidence requires an open employment draft' USING ERRCODE='23514',CONSTRAINT='workforce_employment_evidence_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_evidence_draft_open BEFORE INSERT ON workforce_employment_evidence_bindings
 FOR EACH ROW EXECUTE FUNCTION workforce_employment_evidence_draft_open();
