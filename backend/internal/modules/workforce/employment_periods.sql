SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_employment_period_drafts (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 revision_id uuid NOT NULL CHECK(substring(revision_id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740990),
 status text NOT NULL DEFAULT 'draft' CHECK(status='draft'),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740990),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740990),
 engagement_kind text NOT NULL CHECK(engagement_kind IN ('employee','consultant','contractor','agency_worker','locum','trainee','volunteer')),
 duration text NOT NULL CHECK(duration IN ('fixed','ongoing')),
 starts_at timestamptz NOT NULL CHECK(isfinite(starts_at)),
 ends_at timestamptz CHECK(isfinite(ends_at)),
 contracting_party text NOT NULL CHECK(contracting_party=btrim(contracting_party) AND length(contracting_party)<=200),
 sponsor_id uuid,
 reason text NOT NULL CHECK(reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 evidence_reference text NOT NULL CHECK(evidence_reference=btrim(evidence_reference) AND length(evidence_reference) BETWEEN 1 AND 300),
 proposed_by_subject text NOT NULL CHECK(length(proposed_by_subject) BETWEEN 1 AND 200),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,sponsor_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK((duration='fixed' AND ends_at IS NOT NULL AND ends_at>starts_at) OR (duration='ongoing' AND ends_at IS NULL)),
 CHECK(sponsor_id IS NULL OR sponsor_id<>staff_id),
 CHECK(engagement_kind NOT IN ('contractor','agency_worker') OR (length(contracting_party)>0 AND sponsor_id IS NOT NULL))
);
CREATE INDEX workforce_employment_period_drafts_page ON workforce_employment_period_drafts(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE INDEX workforce_employment_period_sponsor ON workforce_employment_period_drafts(tenant_id,hospital_id,sponsor_id) WHERE sponsor_id IS NOT NULL;
CREATE TABLE workforce_employment_period_history(LIKE workforce_employment_period_drafts INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE workforce_employment_period_history ADD PRIMARY KEY(tenant_id,hospital_id,id,version);
ALTER TABLE workforce_employment_period_history ADD FOREIGN KEY(tenant_id,hospital_id,id) REFERENCES workforce_employment_period_drafts(tenant_id,hospital_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX workforce_employment_period_receipt ON workforce_employment_period_history(tenant_id,hospital_id,revision_id);
CREATE INDEX workforce_employment_period_history_page ON workforce_employment_period_history(tenant_id,hospital_id,staff_id,id,updated_at DESC,revision_id DESC);
CREATE FUNCTION workforce_employment_period_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'employment draft history is retained' USING ERRCODE='23514'; END IF;
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') OR workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) IS NOT TRUE THEN
  RAISE EXCEPTION 'employment draft requires current enrollment authority' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.proposed_by_subject<>NEW.actor_subject THEN RAISE EXCEPTION 'invalid initial employment draft' USING ERRCODE='23514'; END IF;
  NEW.created_at:=clock_timestamp(); NEW.updated_at:=NEW.created_at;
 ELSE
  IF NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id OR (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.status,NEW.proposed_by_subject,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,OLD.status,OLD.proposed_by_subject,OLD.created_at) THEN RAISE EXCEPTION 'employment draft requires a new revision' USING ERRCODE='23514'; END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.staff_id AND s.version=NEW.staff_version AND s.employment_epoch=NEW.employment_epoch) THEN RAISE EXCEPTION 'employment source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_period_source'; END IF;
 IF NEW.sponsor_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.sponsor_id AND s.employment_status='active') THEN RAISE EXCEPTION 'sponsor unavailable' USING ERRCODE='23514',CONSTRAINT='workforce_employment_period_sponsor'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_period_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_period_drafts FOR EACH ROW EXECUTE FUNCTION workforce_employment_period_guard();
CREATE FUNCTION workforce_employment_period_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO workforce_employment_period_history SELECT NEW.*; RETURN NULL; END $$;
CREATE TRIGGER workforce_employment_period_snapshot AFTER INSERT OR UPDATE ON workforce_employment_period_drafts FOR EACH ROW EXECUTE FUNCTION workforce_employment_period_snapshot();
CREATE FUNCTION workforce_employment_period_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN RAISE EXCEPTION 'employment history is immutable and owner-maintained' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_period_history_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_period_history FOR EACH ROW EXECUTE FUNCTION workforce_employment_period_history_guard();
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['workforce_employment_period_drafts','workforce_employment_period_history'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY employment_period_scope ON %I USING(hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id))',t);
  EXECUTE format('CREATE POLICY employment_period_no_delete ON %I AS RESTRICTIVE FOR DELETE USING(false)',t);
  EXECUTE format('CREATE TRIGGER employment_period_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',t);
 END LOOP;
END $$;
