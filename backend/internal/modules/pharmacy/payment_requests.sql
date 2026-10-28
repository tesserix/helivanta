-- Payment requests (#1386 C-4a): what the pharmacy asked payments to
-- collect, and the outcome payments reported. The money state is the
-- payments module's; this row is the pharmacy's record of asking and of
-- what it was told, so the counter never reads a payments table.
CREATE TABLE pharmacy_payment_requests (
 id uuid NOT NULL CHECK (substring(id::text,15,1) = '7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('counter_sale')),
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000000),
 currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 status text NOT NULL DEFAULT 'requested'
  CHECK (status IN ('requested','awaiting_payment','cancelling','paid','failed','expired','cancelled')),
 intent_id uuid,
 qr_image_url text CHECK (qr_image_url IS NULL OR qr_image_url ~ '^https://'),
 expires_at timestamptz,
 failure_reason text CHECK (failure_reason IN ('provider_not_configured','provider_unavailable','provider_rejected')),
 provider_payment_id text,
 rrn text,
 paid_at timestamptz,
 late boolean NOT NULL DEFAULT false,
 actor_subject text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,id),
 CHECK ((status = 'paid') = (provider_payment_id IS NOT NULL AND paid_at IS NOT NULL)),
 CHECK ((status = 'failed') = (failure_reason IS NOT NULL)),
 CHECK (NOT late OR status = 'paid')
);
ALTER TABLE pharmacy_payment_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_payment_requests FORCE ROW LEVEL SECURITY;
-- Staff reach a request through their outlet grant. An outcome consumer
-- holds no grant; it names the one request its event is about in
-- app.pharmacy_payment_outcome, and only that row is visible and writable.
CREATE POLICY tenant_isolation ON pharmacy_payment_requests
 USING (hms_tenant_visible(tenant_id) AND (pharmacy_outlet_allowed(tenant_id,outlet_id,false)
  OR id = NULLIF(current_setting('app.pharmacy_payment_outcome',true),'')::uuid))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true)
  OR (tenant_id = current_setting('app.tenant_id',true)::uuid AND id = NULLIF(current_setting('app.pharmacy_payment_outcome',true),'')::uuid));
CREATE TRIGGER pharmacy_payment_request_audit AFTER INSERT OR UPDATE ON pharmacy_payment_requests
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION pharmacy_payment_requests_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'payment requests are never deleted' USING ERRCODE='23514';
END $$;
CREATE TRIGGER pharmacy_payment_request_kept BEFORE DELETE OR TRUNCATE ON pharmacy_payment_requests
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_payment_requests_kept();
