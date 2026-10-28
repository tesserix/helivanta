SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_reconciliations ADD CONSTRAINT practitioner_reconciliation_owner_unique UNIQUE(tenant_id,practitioner_id,registration_id,id);
CREATE TABLE practitioner_reconciliation_withdrawals (
 tenant_id uuid NOT NULL, practitioner_id uuid NOT NULL, registration_id uuid NOT NULL, reconciliation_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 reason_reference uuid NOT NULL CHECK(substring(reason_reference::text,15,1)='7' AND substring(reason_reference::text,20,1) IN ('8','9','a','b')),
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[0-9a-f]{64}$'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,reconciliation_id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id,reconciliation_id) REFERENCES practitioner_registration_reconciliations(tenant_id,practitioner_id,registration_id,id)
);
ALTER TABLE practitioner_reconciliation_withdrawals ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_reconciliation_withdrawals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_reconciliation_withdrawals
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_reconciliation_withdraw',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_reconciliation_withdrawal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'reconciliation withdrawal is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_reconciliation_withdraw',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'withdrawal authority required' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.registration_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration unavailable' USING ERRCODE='P0002'; END IF;
 PERFORM 1 FROM practitioner_registration_reconciliations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.reconciliation_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'reconciliation unavailable' USING ERRCODE='P0002'; END IF;
 NEW.created_at:=practitioner_registry_now();RETURN NEW;
END $$;
CREATE TRIGGER practitioner_reconciliation_withdrawal_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_reconciliation_withdrawals FOR EACH ROW EXECUTE FUNCTION practitioner_reconciliation_withdrawal_guard();
CREATE TRIGGER practitioner_reconciliation_withdrawal_audit AFTER INSERT ON practitioner_reconciliation_withdrawals FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE OR REPLACE FUNCTION practitioner_registration_unresolved(tenant uuid, registration uuid, active_review uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM practitioner_credential_reviews c WHERE c.tenant_id=tenant AND c.credential_kind='registration' AND c.credential_id=registration AND c.finding<>'active' AND NOT EXISTS(SELECT 1 FROM practitioner_registration_reconciliations x WHERE x.tenant_id=c.tenant_id AND x.registration_id=c.credential_id AND x.active_review_id=active_review AND x.adverse_review_ids ? c.id::text AND NOT EXISTS(SELECT 1 FROM practitioner_reconciliation_withdrawals w WHERE w.tenant_id=x.tenant_id AND w.reconciliation_id=x.id)))
$$;
