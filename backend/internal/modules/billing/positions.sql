SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Each admission account's INR position, kept current by the writes that
-- change it, so the billing desk lists accounts by what they need through an
-- index rather than by computing every account in a hospital's history.
-- A position is never typed in: every write recomputes it from the records.

CREATE TABLE billing_account_positions (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 account_created_at timestamptz NOT NULL,
 status text NOT NULL,
 discharge_ordered boolean NOT NULL,
 balance_minor bigint NOT NULL,
 deposit_held_minor bigint NOT NULL,
 refundable_minor bigint NOT NULL,
 has_final_bill boolean NOT NULL,
 refreshed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,admission_id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);
-- One index per desk view, newest first, holding only the accounts in it.
CREATE INDEX billing_positions_all ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC);
CREATE INDEX billing_positions_collect ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC) WHERE balance_minor > 0;
CREATE INDEX billing_positions_refund ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC) WHERE refundable_minor > 0;
CREATE INDEX billing_positions_ordered ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC) WHERE status='open' AND discharge_ordered;
CREATE INDEX billing_positions_ward ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC) WHERE status='open' AND NOT discharge_ordered;
CREATE INDEX billing_positions_released ON billing_account_positions (tenant_id,hospital_id,account_created_at DESC,admission_id DESC) WHERE status='closed';

-- Billing's consumers (pharmacy charges, bed days, releases) write entries
-- that change a position; recomputing it there needs the receipts,
-- applications and bills of the admission, so they may read those too. Only
-- they and grant holders; nothing else changes.
ALTER POLICY tenant_isolation ON billing_cash_receipts
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
ALTER POLICY tenant_isolation ON billing_deposit_applications
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
ALTER POLICY tenant_isolation ON billing_interim_bills
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));

ALTER TABLE billing_account_positions ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_account_positions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_account_positions
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
CREATE TRIGGER billing_account_positions_kept BEFORE DELETE OR TRUNCATE ON billing_account_positions FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- Whatever a writer supplies, the position is what the records say. It runs
-- after the row lock is taken, with a fresh snapshot per statement, so the
-- last of two concurrent refreshes counts both writes.
CREATE FUNCTION billing_position_compute() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE acct record; final boolean;
BEGIN
 SELECT hospital_id,status,created_at INTO acct FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'a position is of a known account' USING ERRCODE='23514'; END IF;
 final := EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.kind='final');
 NEW.hospital_id := acct.hospital_id;
 NEW.account_created_at := acct.created_at;
 NEW.status := acct.status;
 NEW.discharge_ordered := billing_discharge_ordered(NEW.tenant_id,NEW.admission_id);
 NEW.balance_minor := billing_owed(NEW.tenant_id,NEW.admission_id,'INR') - billing_received(NEW.tenant_id,NEW.admission_id,'INR','payment')
  - billing_applied(NEW.tenant_id,NEW.admission_id,'INR');
 NEW.deposit_held_minor := billing_held(NEW.tenant_id,NEW.admission_id,'INR');
 NEW.refundable_minor := CASE WHEN final THEN GREATEST(billing_refundable(NEW.tenant_id,NEW.admission_id,'INR'),0) ELSE 0 END;
 NEW.has_final_bill := final;
 NEW.refreshed_at := clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER billing_position_compute BEFORE INSERT OR UPDATE ON billing_account_positions FOR EACH ROW EXECUTE FUNCTION billing_position_compute();

CREATE FUNCTION billing_position_refresh(row_tenant uuid,row_admission uuid) RETURNS void LANGUAGE sql AS $$
 INSERT INTO billing_account_positions (tenant_id,hospital_id,admission_id,account_created_at,status,discharge_ordered,balance_minor,deposit_held_minor,refundable_minor,has_final_bill)
 SELECT tenant_id,hospital_id,admission_id,created_at,status,false,0,0,0,false FROM billing_admission_accounts WHERE tenant_id=row_tenant AND admission_id=row_admission
 ON CONFLICT (tenant_id,admission_id) DO UPDATE SET refreshed_at=clock_timestamp()
$$;

-- Every write that changes a position refreshes it.
CREATE FUNCTION billing_position_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM billing_position_refresh(NEW.tenant_id,NEW.admission_id);
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_admission_accounts','billing_account_entries','billing_cash_receipts','billing_deposit_applications','billing_interim_bills','billing_discharge_orders'] LOOP
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION billing_position_changed()', t||'_position', t);
 END LOOP;
END $$;

-- The billing worker fills in accounts that existed before this migration:
-- it reads only these keys across tenants, then refreshes under each tenant.
GRANT SELECT (tenant_id, admission_id) ON billing_admission_accounts TO helivanta_system;
GRANT SELECT (tenant_id, admission_id) ON billing_account_positions TO helivanta_system;
