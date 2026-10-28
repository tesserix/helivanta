-- Payments core (#1386 C-4a): the tenant's Razorpay account, payment
-- intents and what the provider reported. Money state lives only here;
-- requesters learn it from outcome events and never read these tables.

CREATE FUNCTION payments_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'payment records are append only' USING ERRCODE='23514';
END $$;

-- Secrets are OpenBao Transit ciphertexts bound to tenant and account;
-- the plaintext never reaches the database.
CREATE TABLE payment_accounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL UNIQUE,
 provider text NOT NULL CHECK (provider = 'razorpay'),
 mode text NOT NULL CHECK (mode IN ('test','live')),
 key_id text NOT NULL CHECK (key_id ~ '^rzp_(test|live)_[A-Za-z0-9]{1,40}$'),
 key_secret_sealed text NOT NULL CHECK (key_secret_sealed LIKE 'vault:v%'),
 webhook_secret_sealed text NOT NULL CHECK (webhook_secret_sealed LIKE 'vault:v%'),
 updated_by text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((mode = 'test') = (key_id LIKE 'rzp_test_%'))
);
ALTER TABLE payment_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_accounts
 USING (hms_tenant_visible(tenant_id))
 WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER payment_account_audit AFTER INSERT OR UPDATE ON payment_accounts
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- One intent per requester's request. Status moves only through guarded
-- updates in code; every move is a row in payment_intent_transitions.
CREATE TABLE payment_intents (
 id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL,
 requester text NOT NULL CHECK (requester IN ('pharmacy')),
 request_id uuid NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000000),
 currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 status text NOT NULL CHECK (status IN ('awaiting_payment','paid','failed','expired','cancelled')),
 account_id uuid,
 provider_ref text,
 qr_image_url text CHECK (qr_image_url IS NULL OR qr_image_url ~ '^https://'),
 failure_reason text CHECK (failure_reason IN ('provider_not_configured','provider_unavailable','provider_rejected')),
 expires_at timestamptz NOT NULL,
 next_check_at timestamptz,
 checks_done integer NOT NULL DEFAULT 0,
 late_checks_left integer NOT NULL DEFAULT 2 CHECK (late_checks_left BETWEEN 0 AND 2),
 paid_payment_id text,
 late boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, requester, request_id),
 CHECK ((status = 'failed') = (failure_reason IS NOT NULL)),
 CHECK (status = 'failed' OR (provider_ref IS NOT NULL AND qr_image_url IS NOT NULL AND account_id IS NOT NULL)),
 CHECK ((status = 'paid') = (paid_payment_id IS NOT NULL)),
 CHECK (NOT late OR status = 'paid')
);
CREATE UNIQUE INDEX payment_intents_provider_ref ON payment_intents (tenant_id, provider_ref) WHERE provider_ref IS NOT NULL;
CREATE INDEX payment_intents_due ON payment_intents (next_check_at) WHERE next_check_at IS NOT NULL;
ALTER TABLE payment_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_intents
 USING (hms_tenant_visible(tenant_id))
 WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER payment_intent_audit AFTER INSERT OR UPDATE ON payment_intents
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION payment_intents_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'payment intents are never deleted' USING ERRCODE='23514';
END $$;
CREATE TRIGGER payment_intent_kept BEFORE DELETE OR TRUNCATE ON payment_intents
 FOR EACH STATEMENT EXECUTE FUNCTION payment_intents_kept();

CREATE TABLE payment_intent_transitions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 intent_id uuid NOT NULL REFERENCES payment_intents (id),
 from_status text,
 to_status text NOT NULL,
 cause text NOT NULL CHECK (cause IN ('request','provider','webhook','poll','cancel')),
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ON payment_intent_transitions (tenant_id, intent_id, at);
ALTER TABLE payment_intent_transitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_intent_transitions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_intent_transitions
 USING (hms_tenant_visible(tenant_id))
 WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER payment_intent_transition_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payment_intent_transitions
 FOR EACH STATEMENT EXECUTE FUNCTION payments_immutable();

-- Every captured payment the provider API returned for an intent's QR,
-- once. outcome says what it did: paid the intent, arrived after it was
-- paid (excess), or did not match its amount or currency (mismatch).
-- No payer VPA, name or contact is kept.
CREATE TABLE payment_provider_payments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 intent_id uuid NOT NULL REFERENCES payment_intents (id),
 provider_payment_id text NOT NULL CHECK (provider_payment_id ~ '^pay_[A-Za-z0-9]{1,40}$'),
 amount_minor bigint NOT NULL,
 currency text NOT NULL,
 rrn text NOT NULL DEFAULT '' CHECK (length(rrn) <= 40),
 outcome text NOT NULL CHECK (outcome IN ('paid','excess','mismatch')),
 provider_created_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, provider_payment_id)
);
CREATE UNIQUE INDEX payment_provider_payments_one_paid ON payment_provider_payments (tenant_id, intent_id) WHERE outcome = 'paid';
ALTER TABLE payment_provider_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_provider_payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_provider_payments
 USING (hms_tenant_visible(tenant_id))
 WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER payment_provider_payment_audit AFTER INSERT ON payment_provider_payments
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER payment_provider_payment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payment_provider_payments
 FOR EACH STATEMENT EXECUTE FUNCTION payments_immutable();

-- Verified webhook deliveries, once per provider event id.
CREATE TABLE payment_webhook_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 account_id uuid NOT NULL REFERENCES payment_accounts (id),
 event_id text NOT NULL CHECK (length(event_id) BETWEEN 1 AND 100),
 event text NOT NULL CHECK (length(event) BETWEEN 1 AND 100),
 received_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, account_id, event_id)
);
ALTER TABLE payment_webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payment_webhook_deliveries
 USING (hms_tenant_visible(tenant_id))
 WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER payment_webhook_delivery_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payment_webhook_deliveries
 FOR EACH STATEMENT EXECUTE FUNCTION payments_immutable();

-- The poller finds due intents across tenants through the system pool;
-- it reads only ids and next_check_at, then works under WithTenant.
GRANT SELECT (tenant_id, id, next_check_at) ON payment_intents TO helivanta_system;
