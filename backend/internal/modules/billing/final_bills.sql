SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Final bills (#197 slice 4). A bill is now interim or final, numbered in one
-- series per admission. A final bill is issued once a doctor has ordered
-- discharge, or after release; it is reissued, never edited, when something
-- arrives after it (a midnight before the patient leaves, a late charge).

-- Billing's record of MediCore's discharge orders, from its events. Ordered
-- and withdrawn may arrive in either order, so each is set once from either.
CREATE TABLE billing_discharge_orders (
 tenant_id uuid NOT NULL,
 order_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (order_id) STORED,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 disposition text CHECK (disposition IN ('home','transfer_out')),
 ordered_at timestamptz,
 withdrawn_at timestamptz,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,order_id),
 CHECK ((ordered_at IS NULL) = (disposition IS NULL)),
 CHECK (ordered_at IS NOT NULL OR withdrawn_at IS NOT NULL)
);
CREATE INDEX billing_discharge_orders_admission ON billing_discharge_orders (tenant_id,admission_id);
ALTER TABLE billing_discharge_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_discharge_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_discharge_orders
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_consumer());
CREATE TRIGGER billing_discharge_orders_audit AFTER INSERT OR UPDATE ON billing_discharge_orders FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_discharge_orders_kept BEFORE DELETE OR TRUNCATE ON billing_discharge_orders FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();
CREATE FUNCTION billing_discharge_order_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.order_id,NEW.hospital_id,NEW.admission_id,NEW.recorded_at) IS DISTINCT FROM (OLD.tenant_id,OLD.order_id,OLD.hospital_id,OLD.admission_id,OLD.recorded_at)
  OR (OLD.ordered_at IS NOT NULL AND (NEW.ordered_at,NEW.disposition) IS DISTINCT FROM (OLD.ordered_at,OLD.disposition))
  OR (OLD.withdrawn_at IS NOT NULL AND NEW.withdrawn_at IS DISTINCT FROM OLD.withdrawn_at) THEN
  RAISE EXCEPTION 'a discharge order is recorded once and withdrawn once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_discharge_order_guard BEFORE UPDATE ON billing_discharge_orders FOR EACH ROW EXECUTE FUNCTION billing_discharge_order_guard();

CREATE OR REPLACE FUNCTION billing_consumer() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_setting('app.actor_subject',true) IN ('system:billing-bed-occupancy-started','system:billing-admission-released',
  'system:billing-pharmacy-admission-charge','system:billing-pharmacy-admission-reversal',
  'system:billing-bed-occupancy-ended','system:billing-bed-census',
  'system:billing-discharge-ordered','system:billing-discharge-withdrawn')
$$;

-- An active order: recorded as ordered and not withdrawn.
CREATE FUNCTION billing_discharge_ordered(row_tenant uuid,row_admission uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM billing_discharge_orders o WHERE o.tenant_id=row_tenant AND o.admission_id=row_admission
  AND o.ordered_at IS NOT NULL AND o.withdrawn_at IS NULL)
$$;

-- Every bed day the stays imply is already recorded: setup applied by the
-- census, at least one stay, every stay's start known, no open stay past a
-- midnight the census has not yet counted (the 15-minute grace included), and
-- after release every stay ended, so the minimum day is settled. Anything
-- else is a day that may still be added, which a final bill must not miss.
CREATE FUNCTION billing_bed_days_settled(row_tenant uuid,row_hospital uuid,row_admission uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM billing_bed_charging c WHERE c.tenant_id=row_tenant AND c.hospital_id=row_hospital AND c.applied_at IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM billing_bed_rates r WHERE r.tenant_id=row_tenant AND r.hospital_id=row_hospital AND r.applied_at IS NULL)
 AND EXISTS (SELECT 1 FROM billing_bed_stays s WHERE s.tenant_id=row_tenant AND s.admission_id=row_admission)
 AND NOT EXISTS (SELECT 1 FROM billing_bed_stays s JOIN billing_admission_accounts a ON a.tenant_id=s.tenant_id AND a.admission_id=s.admission_id
  WHERE s.tenant_id=row_tenant AND s.admission_id=row_admission
  AND (s.started_at IS NULL
   OR (s.ended_at IS NULL AND (a.status='closed' OR s.next_census_at IS NULL OR s.next_census_at - interval '15 minutes' <= clock_timestamp()))))
$$;

ALTER TABLE billing_interim_bills ADD COLUMN kind text NOT NULL DEFAULT 'interim' CHECK (kind IN ('interim','final'));

-- When each kind may be issued, held here as well as by the handler:
-- interim on an open account without a discharge order; final once discharge
-- is ordered or the account is closed, with bed charging set up and every bed
-- day priced, so a final bill never leaves out a charge it could not price.
CREATE OR REPLACE FUNCTION billing_interim_bill_next() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE acct record;
BEGIN
 SELECT status INTO acct FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND admission_id=NEW.admission_id FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a bill is issued on a known account' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='interim' AND (acct.status<>'open' OR billing_discharge_ordered(NEW.tenant_id,NEW.admission_id)) THEN
  RAISE EXCEPTION 'an interim bill is issued only on an open account before discharge is ordered' USING ERRCODE='23514', CONSTRAINT='billing_interim_bill_open';
 END IF;
 IF NEW.kind='final' AND acct.status='open' AND NOT billing_discharge_ordered(NEW.tenant_id,NEW.admission_id) THEN
  RAISE EXCEPTION 'a final bill is issued once discharge is ordered or the patient has left' USING ERRCODE='23514', CONSTRAINT='billing_final_bill_discharge';
 END IF;
 IF NEW.kind='final' AND (NEW.unpriced_bed_days<>0
  OR NOT EXISTS (SELECT 1 FROM billing_bed_charging c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id)
  OR EXISTS (SELECT 1 FROM billing_bed_days d WHERE d.tenant_id=NEW.tenant_id AND d.admission_id=NEW.admission_id AND d.voided_at IS NULL AND d.priced_at IS NULL)) THEN
  RAISE EXCEPTION 'a final bill needs bed charging set up and every bed day priced' USING ERRCODE='23514', CONSTRAINT='billing_final_bill_priced';
 END IF;
 IF NEW.kind='final' AND NOT billing_bed_days_settled(NEW.tenant_id,NEW.hospital_id,NEW.admission_id) THEN
  RAISE EXCEPTION 'a final bill waits until every bed day is counted' USING ERRCODE='23514', CONSTRAINT='billing_final_bill_settled';
 END IF;
 IF NEW.issued_at IS NOT NULL OR NEW.issuing_xact IS NOT NULL OR NEW.number <> (SELECT COALESCE(max(number),0)+1 FROM billing_interim_bills WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id) THEN
  RAISE EXCEPTION 'bills are numbered in order as they are issued' USING ERRCODE='23514';
 END IF;
 NEW.issued_at := clock_timestamp();
 NEW.issuing_xact := pg_current_xact_id();
 RETURN NEW;
END $$;
