SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_shifts (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
  shift_type text NOT NULL CHECK (shift_type IN ('day','evening','night','rotating','on_call')),
  code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  active boolean NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  revision_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),

 start_time text NOT NULL CHECK (start_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 end_time text NOT NULL CHECK (end_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 duration_minutes integer GENERATED ALWAYS AS (((substring(end_time,1,2)::int*60+substring(end_time,4,2)::int)-(substring(start_time,1,2)::int*60+substring(start_time,4,2)::int)+1440)%1440) STORED,
 break_minutes integer NOT NULL CHECK (break_minutes >= 0),
 grace_minutes integer NOT NULL CHECK (grace_minutes BETWEEN 0 AND 240),
 full_day_ot_minutes integer NOT NULL CHECK (full_day_ot_minutes BETWEEN 0 AND 1440),
 half_day_ot_minutes integer NOT NULL CHECK (half_day_ot_minutes BETWEEN 0 AND full_day_ot_minutes),
 locum boolean NOT NULL,
 CHECK (duration_minutes > 0 AND break_minutes < duration_minutes),
  PRIMARY KEY (tenant_id,hospital_id,id),
  UNIQUE (tenant_id,hospital_id,code),
  UNIQUE (tenant_id,hospital_id,id,code),
  FOREIGN KEY (tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_shift_name_unique ON workforce_shifts (tenant_id,hospital_id,lower(name));
CREATE INDEX workforce_shifts_page ON workforce_shifts (tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE workforce_shift_revisions (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
  shift_id uuid NOT NULL, version bigint NOT NULL CHECK (version > 0),
  shift_type text NOT NULL, code text NOT NULL, name text NOT NULL, description text NOT NULL, active boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),

 start_time text NOT NULL CHECK (start_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 end_time text NOT NULL CHECK (end_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 duration_minutes integer GENERATED ALWAYS AS (((substring(end_time,1,2)::int*60+substring(end_time,4,2)::int)-(substring(start_time,1,2)::int*60+substring(start_time,4,2)::int)+1440)%1440) STORED,
 break_minutes integer NOT NULL CHECK (break_minutes >= 0),
 grace_minutes integer NOT NULL CHECK (grace_minutes BETWEEN 0 AND 240),
 full_day_ot_minutes integer NOT NULL CHECK (full_day_ot_minutes BETWEEN 0 AND 1440),
 half_day_ot_minutes integer NOT NULL CHECK (half_day_ot_minutes BETWEEN 0 AND full_day_ot_minutes),
 locum boolean NOT NULL,
 CHECK (duration_minutes > 0 AND break_minutes < duration_minutes),
  PRIMARY KEY (tenant_id,hospital_id,id),
  UNIQUE (tenant_id,hospital_id,shift_id,version,id),
  UNIQUE (tenant_id,hospital_id,shift_id,version),
  FOREIGN KEY (tenant_id,hospital_id,shift_id,code) REFERENCES workforce_shifts(tenant_id,hospital_id,id,code) ON DELETE RESTRICT
);
CREATE INDEX workforce_shift_history_page ON workforce_shift_revisions (tenant_id,hospital_id,shift_id,created_at DESC,id DESC);
ALTER TABLE workforce_shifts ADD CONSTRAINT workforce_shift_current_revision
  FOREIGN KEY (tenant_id,hospital_id,id,version,revision_id)
  REFERENCES workforce_shift_revisions(tenant_id,hospital_id,shift_id,version,id)
  DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE workforce_shifts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_shifts FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_shifts
  USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND workforce_staff_allowed(tenant_id,hospital_id,true)
    AND current_setting('app.workforce_shift',true) = 'authorized');
ALTER TABLE workforce_shift_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_shift_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_shift_revisions
  USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND workforce_staff_allowed(tenant_id,hospital_id,true)
    AND current_setting('app.workforce_shift',true) = 'authorized');
CREATE TRIGGER workforce_shift_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_shift_revisions
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
CREATE TRIGGER workforce_shift_no_delete BEFORE DELETE OR TRUNCATE ON workforce_shifts
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE FUNCTION workforce_shift_stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.code) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.code)
    OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'shift identity is immutable and versions must advance' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER workforce_shift_stable_identity BEFORE UPDATE ON workforce_shifts
  FOR EACH ROW EXECUTE FUNCTION workforce_shift_stable_identity();
