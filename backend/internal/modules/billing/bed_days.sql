SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Bed-day charges (#197 slice 2). A day is charged for each midnight, in the
-- hospital's timezone, a patient spends in a bed, at that bed's class; a stay
-- crossing no midnight is charged one day. Days derive from stays, so they are
-- recomputed, never typed in.

-- The hospital's timezone, set once: changing it would move midnights under
-- days already charged. applied_at is when the census picked it up.
CREATE TABLE billing_bed_charging (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 timezone text NOT NULL CHECK (length(timezone) BETWEEN 1 AND 64),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 applied_at timestamptz,
 PRIMARY KEY (tenant_id,hospital_id)
);

-- A daily rate per bed class from a date, before tax (GST is #193). Fixed once
-- set; a correction is a rate from a later date. applied_at as above.
CREATE TABLE billing_bed_rates (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 bed_class text NOT NULL CHECK (bed_class ~ '^[a-z_]{1,32}$'),
 effective_from date NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 daily_rate_minor bigint NOT NULL CHECK (daily_rate_minor BETWEEN 1 AND 1000000000),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 applied_at timestamptz,
 PRIMARY KEY (tenant_id,hospital_id,bed_class,effective_from),
 FOREIGN KEY (tenant_id,hospital_id) REFERENCES billing_bed_charging (tenant_id,hospital_id)
);
CREATE INDEX billing_bed_rates_page ON billing_bed_rates (tenant_id,hospital_id,created_at DESC,id DESC);

-- Billing's record of each stay, from MediCore's start and end events in
-- either order. next_census_at is when the census next looks at it.
CREATE TABLE billing_bed_stays (
 tenant_id uuid NOT NULL,
 occupancy_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (occupancy_id) STORED,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 bed_id uuid NOT NULL,
 bed_class text CHECK (bed_class ~ '^[a-z_]{1,32}$'),
 started_at timestamptz,
 ended_at timestamptz,
 next_census_at timestamptz,
 census_failures integer NOT NULL DEFAULT 0 CHECK (census_failures >= 0),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,occupancy_id),
 CHECK ((started_at IS NULL) = (bed_class IS NULL)),
 CHECK (started_at IS NULL OR ended_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX billing_bed_stays_admission ON billing_bed_stays (tenant_id,admission_id);
CREATE INDEX billing_bed_stays_due ON billing_bed_stays (next_census_at) WHERE next_census_at IS NOT NULL;

-- One live day per admission and date. Priced once, when a rate covers it;
-- voided once, when the stays no longer imply it. Never edited otherwise.
CREATE TABLE billing_bed_days (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 census_date date NOT NULL,
 census_at timestamptz NOT NULL,
 basis text NOT NULL CHECK (basis IN ('midnight','minimum')),
 occupancy_id uuid NOT NULL,
 bed_class text NOT NULL CHECK (bed_class ~ '^[a-z_]{1,32}$'),
 daily_rate_minor bigint CHECK (daily_rate_minor >= 1),
 currency text CHECK (currency ~ '^[A-Z]{3}$'),
 rate_effective_from date,
 priced_at timestamptz,
 voided_at timestamptz,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id),
 CHECK (num_nulls(daily_rate_minor,currency,rate_effective_from,priced_at) IN (0,4)),
 CHECK (rate_effective_from IS NULL OR rate_effective_from <= census_date)
);
CREATE UNIQUE INDEX billing_bed_days_live ON billing_bed_days (tenant_id,admission_id,census_date) WHERE voided_at IS NULL;
CREATE INDEX billing_bed_days_unpriced ON billing_bed_days (tenant_id,hospital_id,bed_class,census_date) WHERE priced_at IS NULL AND voided_at IS NULL;

ALTER TABLE billing_account_entries DROP CONSTRAINT billing_account_entries_source_module_check;
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entries_source_module_check CHECK (source_module IN ('pharmacy','bed'));

CREATE OR REPLACE FUNCTION billing_consumer() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_setting('app.actor_subject',true) IN ('system:billing-bed-occupancy-started','system:billing-admission-released',
  'system:billing-pharmacy-admission-charge','system:billing-pharmacy-admission-reversal',
  'system:billing-bed-occupancy-ended','system:billing-bed-census')
$$;

-- An account opens at its earliest stay: starts may arrive in any order, so
-- opened_at may move earlier, never later. Otherwise as in 0001.
CREATE OR REPLACE FUNCTION billing_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.admission_id,NEW.patient_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.admission_id,OLD.patient_id,OLD.created_at)
  OR (OLD.opened_at IS NOT NULL AND (NEW.opened_at IS NULL OR NEW.opened_at > OLD.opened_at))
  OR (OLD.status='closed' AND (NEW.status,NEW.closed_at) IS DISTINCT FROM (OLD.status,OLD.closed_at)) THEN
  RAISE EXCEPTION 'an account keeps its admission and patient, opens at its earliest stay and closes once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Stays and days are written by billing's consumers and census only; the
-- setup tables by an administrator holding the grant, as themselves.
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_bed_stays','billing_bed_days'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id))) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_consumer())', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
 FOREACH t IN ARRAY ARRAY['billing_bed_charging','billing_bed_rates'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id))) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND (billing_consumer() OR (created_by=NULLIF(current_setting(''app.actor_subject'',true),'''') AND billing_hospital_allowed(tenant_id,hospital_id))))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
END $$;

-- A timezone the database knows, never the server's own.
CREATE FUNCTION billing_bed_charging_zone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.timezone='Local' OR NOT EXISTS (SELECT 1 FROM pg_timezone_names WHERE name=NEW.timezone) THEN
  RAISE EXCEPTION 'unknown timezone' USING ERRCODE='23514', CONSTRAINT='billing_bed_charging_zone';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_charging_zone BEFORE INSERT ON billing_bed_charging FOR EACH ROW EXECUTE FUNCTION billing_bed_charging_zone();

-- Setup arrives unapplied; only the census applies it.
CREATE FUNCTION billing_bed_setup_unapplied() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.applied_at IS NOT NULL THEN
  RAISE EXCEPTION 'bed charging setup is applied by the census' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_charging_unapplied BEFORE INSERT ON billing_bed_charging FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_unapplied();
CREATE TRIGGER billing_bed_rates_unapplied BEFORE INSERT ON billing_bed_rates FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_unapplied();

-- Setup rows are fixed; only the census marks them applied, once.
CREATE FUNCTION billing_bed_setup_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (to_jsonb(NEW)-'applied_at') IS DISTINCT FROM (to_jsonb(OLD)-'applied_at')
  OR OLD.applied_at IS NOT NULL OR NEW.applied_at IS NULL OR NOT billing_consumer() THEN
  RAISE EXCEPTION 'bed charging setup is fixed once set' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_charging_guard BEFORE UPDATE ON billing_bed_charging FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_guard();
CREATE TRIGGER billing_bed_rates_guard BEFORE UPDATE ON billing_bed_rates FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_guard();

-- A stay keeps its identity; its start and its end are each recorded once.
CREATE FUNCTION billing_bed_stay_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.occupancy_id,NEW.hospital_id,NEW.admission_id,NEW.bed_id,NEW.recorded_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.occupancy_id,OLD.hospital_id,OLD.admission_id,OLD.bed_id,OLD.recorded_at)
  OR (OLD.started_at IS NOT NULL AND (NEW.started_at,NEW.bed_class) IS DISTINCT FROM (OLD.started_at,OLD.bed_class))
  OR (OLD.ended_at IS NOT NULL AND NEW.ended_at IS DISTINCT FROM OLD.ended_at) THEN
  RAISE EXCEPTION 'a stay starts once and ends once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_stay_guard BEFORE UPDATE ON billing_bed_stays FOR EACH ROW EXECUTE FUNCTION billing_bed_stay_guard();

-- A day is priced once and voided once; nothing else about it changes.
CREATE FUNCTION billing_bed_day_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (to_jsonb(NEW)-ARRAY['daily_rate_minor','currency','rate_effective_from','priced_at','voided_at'])
   IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['daily_rate_minor','currency','rate_effective_from','priced_at','voided_at'])
  OR OLD.voided_at IS NOT NULL
  OR (OLD.priced_at IS NOT NULL AND (NEW.daily_rate_minor,NEW.currency,NEW.rate_effective_from,NEW.priced_at) IS DISTINCT FROM (OLD.daily_rate_minor,OLD.currency,OLD.rate_effective_from,OLD.priced_at))
  OR (NEW.priced_at IS NOT NULL AND OLD.priced_at IS NULL AND NEW.voided_at IS NOT NULL) THEN
  RAISE EXCEPTION 'a bed day is priced once and voided once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_day_guard BEFORE UPDATE ON billing_bed_days FOR EACH ROW EXECUTE FUNCTION billing_bed_day_guard();

-- The census finds due work across tenants by these columns alone and does
-- the work under each tenant's own row security.
GRANT SELECT (tenant_id, occupancy_id, next_census_at) ON billing_bed_stays TO helivanta_system;
GRANT SELECT (tenant_id, hospital_id, applied_at, created_at) ON billing_bed_charging TO helivanta_system;
GRANT SELECT (tenant_id, hospital_id, bed_class, effective_from, applied_at, created_at) ON billing_bed_rates TO helivanta_system;
