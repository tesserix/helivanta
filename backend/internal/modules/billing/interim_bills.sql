SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Interim bills (#197 slice 3): the account frozen as issued, numbered within
-- the admission. A statement, not a tax invoice. A bill lists entry ids, not a
-- time cut: an entry whose transaction was still open when the bill was issued
-- is on the next bill, never skipped between two.

-- A line names an entry of the bill's own admission.
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entries_admission_entry UNIQUE (tenant_id,admission_id,id);

CREATE TABLE billing_interim_bills (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 number integer NOT NULL CHECK (number >= 1),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 issued_by text NOT NULL CHECK (length(btrim(issued_by)) BETWEEN 1 AND 200),
 issued_at timestamptz NOT NULL DEFAULT now(),
 -- Live bed days without a rate when issued: they have no amount, and the
 -- bill says so rather than implying zero.
 unpriced_bed_days integer NOT NULL CHECK (unpriced_bed_days >= 0),
 PRIMARY KEY (tenant_id,admission_id,number),
 UNIQUE (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,admission_id,number),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);
CREATE INDEX billing_interim_bills_page ON billing_interim_bills (tenant_id,admission_id,issued_at DESC,id DESC);

CREATE TABLE billing_interim_bill_lines (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 number integer NOT NULL,
 id uuid NOT NULL,
 -- Not on the previous bill.
 new boolean NOT NULL,
 PRIMARY KEY (tenant_id,admission_id,number,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id,number) REFERENCES billing_interim_bills (tenant_id,hospital_id,admission_id,number),
 FOREIGN KEY (tenant_id,admission_id,id) REFERENCES billing_account_entries (tenant_id,admission_id,id)
);

DO $$
DECLARE t text;
BEGIN
 FOREACH t IN ARRAY ARRAY['billing_interim_bills','billing_interim_bill_lines'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
END $$;
CREATE POLICY tenant_isolation ON billing_interim_bills
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND issued_by=NULLIF(current_setting('app.actor_subject',true),'') AND billing_hospital_allowed(tenant_id,hospital_id));
CREATE POLICY tenant_isolation ON billing_interim_bill_lines
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id));
-- The bill is audited; its lines are fixed with it and name audited entries.
CREATE TRIGGER billing_interim_bills_audit AFTER INSERT ON billing_interim_bills FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- The next number, on an open account, as issued now. The account row is
-- share-locked so a release cannot close it under a bill being issued.
CREATE FUNCTION billing_interim_bill_next() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND admission_id=NEW.admission_id AND status='open' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'an interim bill is issued only on an open account' USING ERRCODE='23514', CONSTRAINT='billing_interim_bill_open';
 END IF;
 IF NEW.issued_at IS DISTINCT FROM now() OR NEW.number <> (SELECT COALESCE(max(number),0)+1 FROM billing_interim_bills WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id) THEN
  RAISE EXCEPTION 'interim bills are numbered in order as they are issued' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_interim_bill_next BEFORE INSERT ON billing_interim_bills FOR EACH ROW EXECUTE FUNCTION billing_interim_bill_next();

-- A bill's lines are written only by the transaction that issued it, so an
-- issued bill never gains a line.
CREATE FUNCTION billing_interim_bill_line_issued() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM billing_interim_bills WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND admission_id=NEW.admission_id AND number=NEW.number
  AND issued_at=now() AND issued_by=NULLIF(current_setting('app.actor_subject',true),'');
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a bill''s lines are written only as it is issued' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_interim_bill_line_issued BEFORE INSERT ON billing_interim_bill_lines FOR EACH ROW EXECUTE FUNCTION billing_interim_bill_line_issued();
