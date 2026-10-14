SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_roster_configs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('area','group','weekly_template')),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 description text NOT NULL CHECK (length(description) <= 500), active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,id,kind,code), UNIQUE(tenant_id,hospital_id,kind,code),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_roster_config_name_unique ON workforce_roster_configs(tenant_id,hospital_id,kind,lower(name));
CREATE INDEX workforce_roster_configs_page ON workforce_roster_configs(tenant_id,hospital_id,kind,created_at DESC,id DESC);

CREATE TABLE workforce_roster_config_revisions (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,id uuid NOT NULL,config_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('area','group','weekly_template')),code text NOT NULL,name text NOT NULL,description text NOT NULL,active boolean NOT NULL,
 effective_on date NOT NULL,reviewed_by_subject text NOT NULL,
 version bigint NOT NULL CHECK (version > 0),created_at timestamptz NOT NULL DEFAULT now(),created_tx bigint NOT NULL DEFAULT txid_current(),
 PRIMARY KEY(tenant_id,hospital_id,id),UNIQUE(tenant_id,hospital_id,id,kind),
 UNIQUE(tenant_id,hospital_id,config_id,version,id),UNIQUE(tenant_id,hospital_id,config_id,version),
 FOREIGN KEY(tenant_id,hospital_id,config_id,kind,code) REFERENCES workforce_roster_configs(tenant_id,hospital_id,id,kind,code) ON DELETE RESTRICT
);
CREATE INDEX workforce_roster_config_history ON workforce_roster_config_revisions(tenant_id,hospital_id,config_id,created_at DESC,id DESC);
ALTER TABLE workforce_roster_configs ADD CONSTRAINT workforce_roster_config_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,config_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_roster_config_days (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,revision_id uuid NOT NULL,
 kind text GENERATED ALWAYS AS ('weekly_template') STORED,
 weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),weekly_off boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,weekday),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE TABLE workforce_roster_config_members (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,revision_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('area','group','weekly_template')),
 position smallint NOT NULL CHECK(position BETWEEN 0 AND 99),weekday smallint,
 shift_revision_id uuid NOT NULL,area_revision_id uuid,area_kind text GENERATED ALWAYS AS ('area') STORED,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position),
 UNIQUE NULLS NOT DISTINCT(tenant_id,hospital_id,revision_id,weekday,shift_revision_id),
 CHECK ((kind='weekly_template' AND weekday IS NOT NULL AND weekday BETWEEN 0 AND 6 AND area_revision_id IS NOT NULL AND position BETWEEN weekday*2 AND weekday*2+1)
  OR (kind IN ('area','group') AND weekday IS NULL AND area_revision_id IS NULL)),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,revision_id,weekday) REFERENCES workforce_roster_config_days(tenant_id,hospital_id,revision_id,weekday) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,shift_revision_id) REFERENCES workforce_shift_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,area_revision_id,area_kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_roster_member_shift ON workforce_roster_config_members(tenant_id,hospital_id,shift_revision_id);
CREATE INDEX workforce_roster_member_area ON workforce_roster_config_members(tenant_id,hospital_id,area_revision_id) WHERE area_revision_id IS NOT NULL;

CREATE FUNCTION workforce_roster_child_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_roster_config_revisions r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.revision_id AND r.created_tx=txid_current()) THEN
  RAISE EXCEPTION 'roster children must be inserted in their revision creating transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION workforce_roster_aggregate_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('area','group') THEN
  IF NOT EXISTS(SELECT 1 FROM workforce_roster_config_members m WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id) THEN
   RAISE EXCEPTION 'roster configuration requires shift membership' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (SELECT count(*) FROM workforce_roster_config_days d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.revision_id=NEW.id)<>7
   OR NOT EXISTS(SELECT 1 FROM workforce_roster_config_members m WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id)
   OR EXISTS(SELECT 1 FROM workforce_roster_config_days d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.revision_id=NEW.id
      AND d.weekly_off = EXISTS(SELECT 1 FROM workforce_roster_config_members m WHERE m.tenant_id=d.tenant_id AND m.hospital_id=d.hospital_id AND m.revision_id=d.revision_id AND m.weekday=d.weekday))
   OR EXISTS(SELECT 1 FROM workforce_roster_config_members m JOIN workforce_roster_config_revisions a ON (a.tenant_id,a.hospital_id,a.id)=(m.tenant_id,m.hospital_id,m.area_revision_id)
     WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id AND a.effective_on>NEW.effective_on)
   OR EXISTS(SELECT 1 FROM workforce_roster_config_members m WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id
      AND NOT EXISTS(SELECT 1 FROM workforce_roster_config_members a WHERE a.tenant_id=m.tenant_id AND a.hospital_id=m.hospital_id AND a.revision_id=m.area_revision_id AND a.shift_revision_id=m.shift_revision_id)) THEN
   RAISE EXCEPTION 'weekly template has invalid days or area membership' USING ERRCODE='23514';
  END IF;

  IF EXISTS(
   WITH spans AS (
    SELECT m.position,m.weekday*1440+substring(r.start_time,1,2)::int*60+substring(r.start_time,4,2)::int AS start_min,
      m.weekday*1440+substring(r.start_time,1,2)::int*60+substring(r.start_time,4,2)::int+r.duration_minutes AS end_min
    FROM workforce_roster_config_members m JOIN workforce_shift_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(m.tenant_id,m.hospital_id,m.shift_revision_id)
    WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id
   )
   SELECT 1 FROM spans a JOIN spans b ON a.position<b.position CROSS JOIN (VALUES(-10080),(0),(10080)) delta(n)
   WHERE a.start_min<b.end_min+delta.n AND b.start_min+delta.n<a.end_min
  ) OR EXISTS(
   SELECT 1 FROM workforce_roster_config_members m JOIN workforce_shift_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(m.tenant_id,m.hospital_id,m.shift_revision_id)
   JOIN workforce_roster_config_days d ON (d.tenant_id,d.hospital_id,d.revision_id)=(m.tenant_id,m.hospital_id,m.revision_id)
   CROSS JOIN (VALUES(-10080),(0),(10080)) delta(n)
   WHERE m.tenant_id=NEW.tenant_id AND m.hospital_id=NEW.hospital_id AND m.revision_id=NEW.id AND d.weekly_off
    AND m.weekday*1440+substring(r.start_time,1,2)::int*60+substring(r.start_time,4,2)::int<(d.weekday+1)*1440+delta.n
    AND d.weekday*1440+delta.n<m.weekday*1440+substring(r.start_time,1,2)::int*60+substring(r.start_time,4,2)::int+r.duration_minutes
  ) THEN
   RAISE EXCEPTION 'weekly template has overlapping shifts or weekly-off work' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_roster_aggregate_valid AFTER INSERT ON workforce_roster_config_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_roster_aggregate_valid();

CREATE FUNCTION workforce_roster_stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.kind,NEW.code) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.kind,OLD.code) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'roster identity is immutable and versions must advance' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_stable_identity BEFORE UPDATE ON workforce_roster_configs FOR EACH ROW EXECUTE FUNCTION workforce_roster_stable_identity();
CREATE TRIGGER workforce_roster_no_delete BEFORE DELETE OR TRUNCATE ON workforce_roster_configs FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_roster_configs','workforce_roster_config_revisions','workforce_roster_config_days','workforce_roster_config_members'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_roster'',true)=''authorized'')',tbl);
  IF tbl<>'workforce_roster_configs' THEN
   EXECUTE format('CREATE TRIGGER workforce_roster_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
  IF tbl IN ('workforce_roster_config_days','workforce_roster_config_members') THEN
   EXECUTE format('CREATE TRIGGER workforce_roster_child_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_roster_child_creation()',tbl);
  END IF;
 END LOOP;
END $$;
