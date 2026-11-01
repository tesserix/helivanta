SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Cash pickups and counting by note (#198 slice 3): cash taken from an open
-- drawer to the safe by someone other than its cashier, and a close that
-- counts notes and coins rather than typing a total.

-- What a count may hold, per currency. INR: RBI legal tender (Indian Currency
-- FAQ; ₹2000 withdrawn from circulation but still legal tender), checked
-- 2026-11-02. Billing cannot read the workspace country policy, so the list
-- lives here, once, and the shell reads it from the API.
CREATE FUNCTION billing_cash_denominations(row_currency text) RETURNS TABLE (key text, kind text, value_minor bigint)
 LANGUAGE sql IMMUTABLE AS $$
 SELECT k.kind||':'||k.value_minor, k.kind, k.value_minor
  FROM (VALUES (1,'note',200000::bigint),(2,'note',50000),(3,'note',20000),(4,'note',10000),(5,'note',5000),(6,'note',2000),(7,'note',1000),
   (8,'note',500),(9,'note',200),(10,'note',100),(11,'coin',2000),(12,'coin',1000),(13,'coin',500),(14,'coin',200),(15,'coin',100),(16,'coin',50))
   AS k(ord,kind,value_minor)
  WHERE row_currency='INR' ORDER BY k.ord
$$;

-- A count's value, or null when it is not a JSON object of known keys to
-- whole counts 0 to 1,000,000: a count the system cannot read is refused,
-- never read as zero.
CREATE FUNCTION billing_cash_count_total(row_currency text,counts jsonb) RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE e record; d bigint; n numeric; total bigint := 0;
BEGIN
 IF counts IS NULL OR jsonb_typeof(counts)<>'object' THEN RETURN NULL; END IF;
 FOR e IN SELECT * FROM jsonb_each(counts) LOOP
  SELECT value_minor INTO d FROM billing_cash_denominations(row_currency) WHERE key=e.key;
  IF NOT FOUND OR jsonb_typeof(e.value)<>'number' THEN RETURN NULL; END IF;
  n := (e.value #>> '{}')::numeric;
  IF n<>trunc(n) OR n<0 OR n>1000000 THEN RETURN NULL; END IF;
  total := total + d*n::bigint;
 END LOOP;
 RETURN total;
END $$;

-- The close keeps its count; a shift closed before this slice has none.
ALTER TABLE billing_cash_shifts ADD COLUMN declared_counts jsonb;
ALTER TABLE billing_cash_shifts ADD CONSTRAINT billing_cash_shift_count
 CHECK (declared_counts IS NULL OR declared_minor=billing_cash_count_total(currency,declared_counts));

-- A pickup takes a counted sum out of an open drawer; a correction puts back
-- what a pickup overstated. Both are fixed once written.
CREATE TABLE billing_cash_pickups (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 shift_id uuid NOT NULL,
 counter_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('pickup','correction')),
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 100000000000),
 currency text NOT NULL CHECK (currency='INR'),
 counts jsonb,
 corrects_id uuid,
 reason text CHECK (reason IS NULL OR (reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500)),
 recorded_by text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,shift_id,id),
 FOREIGN KEY (tenant_id,hospital_id,shift_id) REFERENCES billing_cash_shifts (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,shift_id,corrects_id) REFERENCES billing_cash_pickups (tenant_id,shift_id,id),
 CONSTRAINT billing_cash_pickup_form CHECK (
  (kind='pickup' AND corrects_id IS NULL AND reason IS NULL AND counts IS NOT NULL AND amount_minor=billing_cash_count_total(currency,counts))
  OR (kind='correction' AND corrects_id IS NOT NULL AND reason IS NOT NULL AND counts IS NULL))
);
CREATE INDEX billing_cash_pickups_shift ON billing_cash_pickups (tenant_id,shift_id,recorded_at,id);

ALTER TABLE billing_cash_pickups ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_cash_pickups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_cash_pickups
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id));
CREATE TRIGGER billing_cash_pickups_audit AFTER INSERT ON billing_cash_pickups FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_cash_pickups_kept BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_cash_pickups FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- Recorded by the acting subject, never the shift's cashier, while the shift
-- is open. The shift share lock orders it with the close; the counter lock
-- with every receipt, refund and void at the counter, so the drawer bound is
-- read as it stands.
CREATE FUNCTION billing_cash_pickup_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record; left_minor bigint;
BEGIN
 SELECT counter_id,cashier,currency,status INTO s FROM billing_cash_shifts
  WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.shift_id FOR SHARE;
 IF NOT FOUND OR s.status<>'open' OR NEW.counter_id<>s.counter_id OR NEW.currency<>s.currency THEN
  RAISE EXCEPTION 'cash is picked up from an open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_pickup_shift';
 END IF;
 IF NEW.recorded_by IS DISTINCT FROM billing_actor() OR NEW.recorded_by=s.cashier THEN
  RAISE EXCEPTION 'a pickup is recorded by whoever takes the cash, never its cashier' USING ERRCODE='23514', CONSTRAINT='billing_cash_pickup_own';
 END IF;
 PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
 IF NEW.kind='pickup' THEN
  -- Never below an empty drawer: refunds and voids are bounded by the
  -- expected cash and would otherwise be checked against a negative figure.
  IF NEW.amount_minor > billing_shift_expected(NEW.tenant_id,NEW.shift_id) THEN
   RAISE EXCEPTION 'a pickup is never more than the drawer holds' USING ERRCODE='23514', CONSTRAINT='billing_cash_pickup_drawer';
  END IF;
  RETURN NEW;
 END IF;
 SELECT p.amount_minor - COALESCE((SELECT sum(c.amount_minor) FROM billing_cash_pickups c WHERE c.tenant_id=p.tenant_id AND c.corrects_id=p.id),0)
  INTO left_minor FROM billing_cash_pickups p WHERE p.tenant_id=NEW.tenant_id AND p.shift_id=NEW.shift_id AND p.id=NEW.corrects_id AND p.kind='pickup';
 IF NOT FOUND OR NEW.amount_minor > left_minor THEN
  RAISE EXCEPTION 'a correction puts back no more than its pickup still counts' USING ERRCODE='23514', CONSTRAINT='billing_cash_pickup_over';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_cash_pickups_guard BEFORE INSERT ON billing_cash_pickups FOR EACH ROW EXECUTE FUNCTION billing_cash_pickup_guard();

-- What went to the safe and stayed there: pickups less their corrections.
CREATE FUNCTION billing_shift_picked_up(row_tenant uuid,row_shift uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(CASE kind WHEN 'pickup' THEN amount_minor ELSE -amount_minor END),0)::bigint
  FROM billing_cash_pickups WHERE tenant_id=row_tenant AND shift_id=row_shift
$$;

-- Expected cash, as in 0011, less what went to the safe.
CREATE OR REPLACE FUNCTION billing_shift_expected(row_tenant uuid,row_shift uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT s.opening_float_minor + COALESCE((SELECT sum(CASE r.purpose WHEN 'refund' THEN -r.amount_minor ELSE r.amount_minor END) FROM billing_cash_receipts r
  WHERE r.tenant_id=s.tenant_id AND r.shift_id=s.id AND r.voided_at IS NULL AND r.mode='cash'),0)
  - billing_shift_picked_up(s.tenant_id,s.id)
 FROM billing_cash_shifts s WHERE s.tenant_id=row_tenant AND s.id=row_shift
$$;

-- As in 0011, and: a close carries its count, and the count stays fixed.
CREATE OR REPLACE FUNCTION billing_shift_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'open' OR NEW.cashier IS DISTINCT FROM billing_actor() OR NEW.declared_counts IS NOT NULL THEN
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
  IF NEW.declared_counts IS NULL THEN
   RAISE EXCEPTION 'a shift is closed by counting its notes and coins' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_count';
  END IF;
  IF EXISTS (SELECT 1 FROM billing_upi_requests u WHERE u.tenant_id=OLD.tenant_id AND u.shift_id=OLD.id AND billing_upi_pending(u.status,u.created_at)) THEN
   RAISE EXCEPTION 'a shift does not close while a UPI payment waits' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_upi_waiting';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='pending_authorisation' AND NEW.status='closed' THEN
  IF (NEW.expected_minor,NEW.declared_minor,NEW.closed_at,NEW.declared_counts) IS DISTINCT FROM (OLD.expected_minor,OLD.declared_minor,OLD.closed_at,OLD.declared_counts)
   OR NEW.authorised_by IS DISTINCT FROM billing_actor() OR NEW.authorised_by=OLD.cashier THEN
   RAISE EXCEPTION 'a variance is authorised by someone other than the cashier' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_own_variance';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a shift closes once' USING ERRCODE='23514';
END $$;
