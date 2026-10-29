SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Who may read a hospital's admission accounts (#197): an explicit billing
-- grant, separate from clinical and pharmacy access.
CREATE TABLE billing_hospital_access (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,hospital_id,subject)
);
ALTER TABLE billing_hospital_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_hospital_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_hospital_access
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER billing_hospital_access_audit AFTER INSERT OR UPDATE OR DELETE ON billing_hospital_access FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- One account per admission (#197 slice 1). Opened by whichever event names
-- the admission first; closed only by its release. Charges after release are
-- still recorded: late postings happen, and a closed stay still owes them.
CREATE TABLE billing_admission_accounts (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (admission_id) STORED,
 patient_id uuid NOT NULL,
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
 opened_at timestamptz,
 closed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,admission_id),
 UNIQUE (tenant_id,hospital_id,admission_id),
 CHECK ((status='closed') = (closed_at IS NOT NULL))
);
CREATE INDEX billing_admission_accounts_page ON billing_admission_accounts (tenant_id,hospital_id,created_at DESC,admission_id DESC);

-- What the admission owes, append-only. A replayed event never posts twice:
-- each entry is keyed on its source and kind.
CREATE TABLE billing_account_entries (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 admission_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('charge','reversal')),
 source_module text NOT NULL CHECK (source_module IN ('pharmacy')),
 source_reference uuid NOT NULL,
 source_document text NOT NULL CHECK (length(btrim(source_document)) BETWEEN 1 AND 32),
 charge_reference uuid NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 occurred_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,source_module,source_reference,kind),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id),
 -- A charge is its own reference; a reversal names the charge it reverses.
 CHECK ((kind='charge') = (charge_reference=source_reference))
);
CREATE INDEX billing_account_entries_page ON billing_account_entries (tenant_id,admission_id,recorded_at DESC,id DESC);

CREATE FUNCTION billing_hospital_allowed(row_tenant uuid,row_hospital uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND EXISTS (SELECT 1 FROM billing_hospital_access a WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital
  AND a.subject=NULLIF(current_setting('app.billing_subject',true),''))
$$;
-- The billing consumers write; a reader needs the hospital grant.
CREATE FUNCTION billing_consumer() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_setting('app.actor_subject',true) IN ('system:billing-bed-occupancy-started','system:billing-admission-released',
  'system:billing-pharmacy-admission-charge','system:billing-pharmacy-admission-reversal')
$$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_admission_accounts','billing_account_entries'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id))) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_consumer())', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
 END LOOP;
END $$;

-- Entries are never changed or removed; an account's identity is fixed and
-- it closes once.
CREATE FUNCTION billing_history_retained() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'billing history is retained' USING ERRCODE='23514';
END $$;
CREATE TRIGGER billing_account_entries_fixed BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_account_entries FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();
CREATE TRIGGER billing_admission_accounts_kept BEFORE DELETE OR TRUNCATE ON billing_admission_accounts FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();
CREATE FUNCTION billing_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.admission_id,NEW.patient_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.admission_id,OLD.patient_id,OLD.created_at)
  OR (OLD.opened_at IS NOT NULL AND NEW.opened_at IS DISTINCT FROM OLD.opened_at)
  OR (OLD.status='closed' AND (NEW.status,NEW.closed_at) IS DISTINCT FROM (OLD.status,OLD.closed_at)) THEN
  RAISE EXCEPTION 'an account keeps its admission and patient, opens once and closes once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_account_guard BEFORE UPDATE ON billing_admission_accounts FOR EACH ROW EXECUTE FUNCTION billing_account_guard();

-- A reversal is for a recorded charge of the same admission, in its currency,
-- and the reversals of a charge never exceed it.
CREATE FUNCTION billing_reversal_bounded() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE charge record; reversed bigint;
BEGIN
 IF NEW.kind<>'reversal' THEN RETURN NEW; END IF;
 -- Reversals of one charge are taken one at a time: two concurrent ones
 -- would each sum only committed reversals and together exceed the charge.
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-charge:'||NEW.charge_reference::text, 0));
 SELECT admission_id,amount_minor,currency INTO charge FROM billing_account_entries
  WHERE tenant_id=NEW.tenant_id AND source_module=NEW.source_module AND source_reference=NEW.charge_reference AND kind='charge' FOR SHARE;
 IF NOT FOUND OR charge.admission_id<>NEW.admission_id OR charge.currency<>NEW.currency THEN
  RAISE EXCEPTION 'a reversal names a recorded charge of the same admission and currency' USING ERRCODE='23514', CONSTRAINT='billing_reversal_bounded';
 END IF;
 SELECT COALESCE(sum(amount_minor),0) INTO reversed FROM billing_account_entries
  WHERE tenant_id=NEW.tenant_id AND kind='reversal' AND source_module=NEW.source_module AND charge_reference=NEW.charge_reference;
 IF reversed + NEW.amount_minor > charge.amount_minor THEN
  RAISE EXCEPTION 'reversals cannot exceed the charge' USING ERRCODE='23514', CONSTRAINT='billing_reversal_bounded';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_reversal_bounded BEFORE INSERT ON billing_account_entries FOR EACH ROW EXECUTE FUNCTION billing_reversal_bounded();

-- A reversal that arrives before its charge is parked, not refused: the bus
-- would dead-letter it after five quick failures and the patient's balance
-- would overstate what they owe. It is applied when the charge is recorded.
CREATE TABLE billing_parked_reversals (
 tenant_id uuid NOT NULL,
 source_reference uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (source_reference) STORED,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 charge_reference uuid NOT NULL,
 source_document text NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 occurred_at timestamptz NOT NULL,
 parked_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 applied_at timestamptz,
 PRIMARY KEY (tenant_id,source_reference),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);
CREATE INDEX billing_parked_reversals_waiting ON billing_parked_reversals (tenant_id,charge_reference) WHERE applied_at IS NULL;
ALTER TABLE billing_parked_reversals ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_parked_reversals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_parked_reversals
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_consumer());
CREATE TRIGGER billing_parked_reversals_audit AFTER INSERT OR UPDATE ON billing_parked_reversals FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_parked_reversals_kept BEFORE DELETE OR TRUNCATE ON billing_parked_reversals FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();
-- What was parked never changes; it is applied once.
CREATE FUNCTION billing_parked_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.source_reference,NEW.hospital_id,NEW.admission_id,NEW.charge_reference,NEW.source_document,NEW.amount_minor,NEW.currency,NEW.occurred_at,NEW.parked_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.source_reference,OLD.hospital_id,OLD.admission_id,OLD.charge_reference,OLD.source_document,OLD.amount_minor,OLD.currency,OLD.occurred_at,OLD.parked_at)
  OR OLD.applied_at IS NOT NULL OR NEW.applied_at IS NULL THEN
  RAISE EXCEPTION 'a parked reversal is applied once and otherwise fixed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_parked_guard BEFORE UPDATE ON billing_parked_reversals FOR EACH ROW EXECUTE FUNCTION billing_parked_guard();
