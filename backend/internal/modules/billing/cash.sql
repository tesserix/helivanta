SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Cash counters (#198 slice 1): every rupee taken has a counter, a shift, a
-- cashier and a numbered receipt, and a shift closes blind against what the
-- system expected. Append-only: a receipt is voided, never edited.

CREATE TABLE billing_counters (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{0,19}$'),
 name text NOT NULL CHECK (name=btrim(name) AND length(name) BETWEEN 1 AND 100),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 UNIQUE (tenant_id,hospital_id,code)
);
CREATE INDEX billing_counters_page ON billing_counters (tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE billing_cash_shifts (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 counter_id uuid NOT NULL,
 cashier text NOT NULL CHECK (length(btrim(cashier)) BETWEEN 1 AND 200),
 currency text NOT NULL CHECK (currency='INR'),
 opening_float_minor bigint NOT NULL CHECK (opening_float_minor BETWEEN 0 AND 1000000000),
 opened_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','pending_authorisation','closed')),
 expected_minor bigint,
 declared_minor bigint CHECK (declared_minor BETWEEN 0 AND 100000000000),
 variance_minor bigint GENERATED ALWAYS AS (declared_minor - expected_minor) STORED,
 closed_at timestamptz,
 authorised_by text,
 authorisation_reason text CHECK (authorisation_reason IS NULL OR (authorisation_reason=btrim(authorisation_reason) AND length(authorisation_reason) BETWEEN 1 AND 500)),
 authorised_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,hospital_id,counter_id) REFERENCES billing_counters (tenant_id,hospital_id,id),
 CONSTRAINT billing_cash_shift_close CHECK (
  (status='open' AND expected_minor IS NULL AND declared_minor IS NULL AND closed_at IS NULL AND authorised_by IS NULL)
  OR (status='pending_authorisation' AND expected_minor IS NOT NULL AND declared_minor IS NOT NULL AND closed_at IS NOT NULL
      AND declared_minor<>expected_minor AND authorised_by IS NULL)
  OR (status='closed' AND expected_minor IS NOT NULL AND declared_minor IS NOT NULL AND closed_at IS NOT NULL
      AND (declared_minor=expected_minor) = (authorised_by IS NULL))),
 CHECK ((authorised_by IS NULL) = (authorisation_reason IS NULL) AND (authorised_by IS NULL) = (authorised_at IS NULL))
);
-- A counter and a cashier each have one shift not yet closed.
CREATE UNIQUE INDEX billing_cash_shift_counter_open ON billing_cash_shifts (tenant_id,counter_id) WHERE status<>'closed';
CREATE UNIQUE INDEX billing_cash_shift_cashier_open ON billing_cash_shifts (tenant_id,hospital_id,cashier) WHERE status<>'closed';
CREATE INDEX billing_cash_shifts_page ON billing_cash_shifts (tenant_id,hospital_id,opened_at DESC,id DESC);

CREATE TABLE billing_cash_receipts (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 shift_id uuid NOT NULL,
 counter_id uuid NOT NULL,
 number integer NOT NULL CHECK (number >= 1),
 cashier text NOT NULL,
 admission_id uuid NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 void_id uuid CHECK (substring(void_id::text,15,1)='7'),
 void_reason text CHECK (void_reason IS NULL OR (void_reason=btrim(void_reason) AND length(void_reason) BETWEEN 1 AND 500)),
 voided_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,counter_id,number),
 CONSTRAINT billing_cash_receipt_void_id UNIQUE (tenant_id,void_id),
 FOREIGN KEY (tenant_id,hospital_id,shift_id) REFERENCES billing_cash_shifts (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id),
 CHECK (num_nulls(void_id,void_reason,voided_at) IN (0,3))
);
CREATE INDEX billing_cash_receipts_admission ON billing_cash_receipts (tenant_id,admission_id,received_at DESC,id DESC);
CREATE INDEX billing_cash_receipts_shift ON billing_cash_receipts (tenant_id,shift_id);

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_counters','billing_cash_shifts','billing_cash_receipts'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
END $$;
CREATE TRIGGER billing_counters_fixed BEFORE UPDATE ON billing_counters FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

CREATE FUNCTION billing_actor() RETURNS text LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.actor_subject',true),'') $$;

-- A counter is set up by the acting subject.
CREATE FUNCTION billing_counter_created() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.created_by IS DISTINCT FROM billing_actor() THEN RAISE EXCEPTION 'a counter is set up by the acting subject' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_counter_created BEFORE INSERT ON billing_counters FOR EACH ROW EXECUTE FUNCTION billing_counter_created();

-- Expected cash: the float plus the shift's receipts that stand.
CREATE FUNCTION billing_shift_expected(row_tenant uuid,row_shift uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT s.opening_float_minor + COALESCE((SELECT sum(r.amount_minor) FROM billing_cash_receipts r
  WHERE r.tenant_id=s.tenant_id AND r.shift_id=s.id AND r.voided_at IS NULL),0)
 FROM billing_cash_shifts s WHERE s.tenant_id=row_tenant AND s.id=row_shift
$$;

-- A shift opens for the acting cashier, and changes only by closing (blind,
-- against the computed expected cash) and, for a variance, by authorisation
-- from someone else.
CREATE FUNCTION billing_shift_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'open' OR NEW.cashier IS DISTINCT FROM billing_actor() THEN
   RAISE EXCEPTION 'a shift opens for the acting cashier' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.counter_id,NEW.cashier,NEW.currency,NEW.opening_float_minor,NEW.opened_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.counter_id,OLD.cashier,OLD.currency,OLD.opening_float_minor,OLD.opened_at) THEN
  RAISE EXCEPTION 'a shift keeps its counter, cashier and float' USING ERRCODE='23514';
 END IF;
 IF OLD.status='open' AND NEW.status<>'open' THEN
  IF billing_actor() IS DISTINCT FROM OLD.cashier OR NEW.authorised_by IS NOT NULL
   OR NEW.expected_minor IS DISTINCT FROM billing_shift_expected(OLD.tenant_id,OLD.id) THEN
   RAISE EXCEPTION 'a shift is closed by its cashier against the cash it took' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='pending_authorisation' AND NEW.status='closed' THEN
  IF (NEW.expected_minor,NEW.declared_minor,NEW.closed_at) IS DISTINCT FROM (OLD.expected_minor,OLD.declared_minor,OLD.closed_at)
   OR NEW.authorised_by IS DISTINCT FROM billing_actor() OR NEW.authorised_by=OLD.cashier THEN
   RAISE EXCEPTION 'a variance is authorised by someone other than the cashier' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_own_variance';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a shift closes once' USING ERRCODE='23514';
END $$;
CREATE TRIGGER billing_shift_guard BEFORE INSERT OR UPDATE ON billing_cash_shifts FOR EACH ROW EXECUTE FUNCTION billing_shift_guard();

-- A receipt is taken in its cashier's own open shift, numbered next on the
-- counter, and never for more than the account's balance due in its currency.
-- The shift row is share-locked so a close waits for it (the foreign key's
-- own key-share lock does too); the admission lock
-- is the one every charge and bill takes, so two receipts are one after the
-- other against the same balance.
CREATE FUNCTION billing_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record; due bigint;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT counter_id,cashier,currency,status INTO s FROM billing_cash_shifts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.shift_id FOR SHARE;
  IF NOT FOUND OR s.status<>'open' OR s.cashier IS DISTINCT FROM billing_actor() OR NEW.cashier IS DISTINCT FROM s.cashier
   OR NEW.counter_id<>s.counter_id OR NEW.currency<>s.currency OR NEW.void_id IS NOT NULL THEN
   RAISE EXCEPTION 'a receipt is taken in its cashier''s open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
  END IF;
  PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
  IF NEW.number <> (SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE tenant_id=NEW.tenant_id AND counter_id=NEW.counter_id) THEN
   RAISE EXCEPTION 'receipts are numbered in order on their counter' USING ERRCODE='23514';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
  SELECT COALESCE((SELECT sum(CASE kind WHEN 'charge' THEN amount_minor ELSE -amount_minor END) FROM billing_account_entries
    WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id AND currency=NEW.currency),0)
   - COALESCE((SELECT sum(amount_minor) FROM billing_cash_receipts
    WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id AND currency=NEW.currency AND voided_at IS NULL),0) INTO due;
  IF NEW.amount_minor > due THEN
   RAISE EXCEPTION 'a receipt is never more than the balance due' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_balance';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.number,NEW.cashier,NEW.admission_id,NEW.amount_minor,NEW.currency,NEW.received_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.number,OLD.cashier,OLD.admission_id,OLD.amount_minor,OLD.currency,OLD.received_at)
  OR OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL OR billing_actor() IS DISTINCT FROM OLD.cashier THEN
  RAISE EXCEPTION 'a receipt is voided once, by its cashier, and otherwise fixed' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM billing_cash_shifts WHERE tenant_id=OLD.tenant_id AND id=OLD.shift_id AND status='open' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a receipt is voided only while its shift is open' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_receipt_guard BEFORE INSERT OR UPDATE ON billing_cash_receipts FOR EACH ROW EXECUTE FUNCTION billing_receipt_guard();
