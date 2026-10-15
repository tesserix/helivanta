SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE pharmacy_payables (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,
 payee_reference text NOT NULL CHECK (length(btrim(payee_reference)) BETWEEN 1 AND 100 AND payee_reference=upper(btrim(payee_reference))),
 payee_name text NOT NULL CHECK (length(btrim(payee_name)) BETWEEN 1 AND 200),
 document_reference text NOT NULL CHECK (length(btrim(document_reference)) BETWEEN 1 AND 100 AND document_reference=upper(btrim(document_reference))),
 issued_on date NOT NULL,due_on date NOT NULL CHECK (due_on>=issued_on),currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 line_count integer NOT NULL CHECK (line_count BETWEEN 1 AND 100),total_minor bigint NOT NULL CHECK (total_minor BETWEEN 1 AND 1000000000000),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),UNIQUE (tenant_id,outlet_id,payee_reference,document_reference),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE pharmacy_payables ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_payables FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_payables USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false)) WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_payable_page ON pharmacy_payables (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_payable_due ON pharmacy_payables (tenant_id,outlet_id,currency,due_on,id);
CREATE TRIGGER pharmacy_payable_audit AFTER INSERT ON pharmacy_payables FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_payable_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_payables FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TABLE pharmacy_payable_lines (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,payable_id uuid NOT NULL,
 position integer NOT NULL CHECK (position BETWEEN 1 AND 100),receipt_id uuid,
 category text NOT NULL CHECK (category IN ('inventory','utilities','rent','salary','transport','maintenance','professional','other')),
 description text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 500),net_minor bigint NOT NULL CHECK (net_minor BETWEEN 0 AND 1000000000000),
 tax_minor bigint NOT NULL CHECK (tax_minor BETWEEN 0 AND 1000000000000),recoverable_tax_minor bigint NOT NULL CHECK (recoverable_tax_minor BETWEEN 0 AND tax_minor),
 landed_minor bigint NOT NULL CHECK (landed_minor BETWEEN 0 AND 1000000000000),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),UNIQUE (tenant_id,outlet_id,payable_id,position),UNIQUE (tenant_id,outlet_id,receipt_id),
 FOREIGN KEY (tenant_id,outlet_id,payable_id) REFERENCES pharmacy_payables (tenant_id,outlet_id,id) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,outlet_id,receipt_id) REFERENCES pharmacy_stock_movements (tenant_id,outlet_id,id) ON DELETE RESTRICT,
 CHECK ((category='inventory')=(receipt_id IS NOT NULL)),CHECK (net_minor+tax_minor+landed_minor>0)
);
ALTER TABLE pharmacy_payable_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_payable_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_payable_lines USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false)) WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_payable_line_audit AFTER INSERT ON pharmacy_payable_lines FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_payable_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_payable_lines FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE FUNCTION pharmacy_payable_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE doc uuid; actual_total numeric; actual_count integer; expected_total bigint; expected_count integer;
BEGIN
 IF TG_TABLE_NAME='pharmacy_payables' THEN doc := NEW.id; ELSE doc := NEW.payable_id; END IF;
 SELECT total_minor,line_count INTO expected_total,expected_count FROM pharmacy_payables WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=doc;
 SELECT COALESCE(sum(net_minor+tax_minor+landed_minor),0),count(*) INTO actual_total,actual_count FROM pharmacy_payable_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payable_id=doc;
 IF actual_total<>expected_total OR actual_count<>expected_count THEN RAISE EXCEPTION 'payable line totals do not match' USING ERRCODE='23514',CONSTRAINT='pharmacy_payable_totals'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_payable_complete AFTER INSERT ON pharmacy_payables DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_payable_complete();
CREATE CONSTRAINT TRIGGER pharmacy_payable_lines_complete AFTER INSERT ON pharmacy_payable_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_payable_complete();
CREATE FUNCTION pharmacy_payable_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.receipt_id IS NOT NULL AND NOT EXISTS (
 SELECT 1 FROM pharmacy_stock_movements m JOIN pharmacy_stock_batches b ON b.tenant_id=m.tenant_id AND b.outlet_id=m.outlet_id AND b.id=m.batch_id
 JOIN pharmacy_catalogue_revisions r ON r.tenant_id=b.tenant_id AND r.outlet_id=b.outlet_id AND r.product_id=b.product_id AND r.version=b.product_version
 JOIN pharmacy_payables p ON p.tenant_id=m.tenant_id AND p.outlet_id=m.outlet_id AND p.id=NEW.payable_id
 WHERE m.tenant_id=NEW.tenant_id AND m.outlet_id=NEW.outlet_id AND m.id=NEW.receipt_id AND m.kind='receipt' AND r.snapshot->>'currency'=p.currency
 ) THEN RAISE EXCEPTION 'invalid acquisition receipt' USING ERRCODE='23514',CONSTRAINT='pharmacy_payable_receipt'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_payable_receipt_guard BEFORE INSERT ON pharmacy_payable_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_payable_receipt_guard();

CREATE TABLE pharmacy_settlements (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,payable_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('payment','credit','payment_reversal')),amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000000),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),paid_by text NOT NULL CHECK (length(btrim(paid_by)) BETWEEN 1 AND 200),
 method text NOT NULL CHECK (method IN ('cash','bank','card','upi','other')),
 source_reference text NOT NULL CHECK (length(btrim(source_reference)) BETWEEN 1 AND 100 AND source_reference=upper(btrim(source_reference))),
 occurred_on date NOT NULL,reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),reversal_of uuid,
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),UNIQUE (tenant_id,outlet_id,source_reference),UNIQUE (tenant_id,outlet_id,reversal_of),
 FOREIGN KEY (tenant_id,outlet_id,payable_id) REFERENCES pharmacy_payables (tenant_id,outlet_id,id) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,outlet_id,reversal_of) REFERENCES pharmacy_settlements (tenant_id,outlet_id,id) ON DELETE RESTRICT,
 CHECK ((kind='payment_reversal')=(reversal_of IS NOT NULL))
);
ALTER TABLE pharmacy_settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_settlements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_settlements USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false)) WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_settlement_page ON pharmacy_settlements (tenant_id,outlet_id,payable_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_settlement_audit AFTER INSERT ON pharmacy_settlements FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_settlement_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_settlements FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_settlement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE doc pharmacy_payables; original pharmacy_settlements; settled numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':payable:'||NEW.payable_id::text,0));
 SELECT * INTO doc FROM pharmacy_payables WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.payable_id;
 IF doc.id IS NULL OR doc.currency<>NEW.currency OR NEW.occurred_on<doc.issued_on THEN RAISE EXCEPTION 'invalid settlement document, currency or date' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_document'; END IF;
 IF NEW.kind='payment_reversal' THEN
  SELECT * INTO original FROM pharmacy_settlements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.reversal_of;
  IF original.id IS NULL OR original.kind<>'payment' OR original.payable_id<>NEW.payable_id OR original.amount_minor<>NEW.amount_minor OR NEW.occurred_on<original.occurred_on THEN
   RAISE EXCEPTION 'reversal must match original payment' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_reversal';
  END IF;
 END IF;
 SELECT COALESCE(sum(CASE WHEN kind='payment_reversal' THEN -amount_minor ELSE amount_minor END),0) INTO settled FROM pharmacy_settlements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payable_id=NEW.payable_id;
 settled := settled + CASE WHEN NEW.kind='payment_reversal' THEN -NEW.amount_minor ELSE NEW.amount_minor END;
 IF settled<0 OR settled>doc.total_minor THEN RAISE EXCEPTION 'settlement exceeds outstanding amount' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_balance'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_settlement_guard BEFORE INSERT ON pharmacy_settlements FOR EACH ROW EXECUTE FUNCTION pharmacy_settlement_guard();

CREATE VIEW pharmacy_payable_balances WITH (security_invoker=true) AS
 SELECT p.id,p.tenant_id,p.outlet_id,p.payee_reference,p.payee_name,p.document_reference,p.issued_on,p.due_on,p.currency,p.line_count,p.total_minor,p.actor_subject,p.created_at,
 COALESCE(s.paid,0)::bigint AS paid_minor,COALESCE(s.credit,0)::bigint AS credit_minor,(p.total_minor-COALESCE(s.paid,0)-COALESCE(s.credit,0))::bigint AS outstanding_minor
 FROM pharmacy_payables p LEFT JOIN LATERAL (
 SELECT sum(CASE WHEN kind='payment' THEN amount_minor WHEN kind='payment_reversal' THEN -amount_minor ELSE 0 END) AS paid,
 sum(CASE WHEN kind='credit' THEN amount_minor ELSE 0 END) AS credit FROM pharmacy_settlements WHERE tenant_id=p.tenant_id AND outlet_id=p.outlet_id AND payable_id=p.id
 ) s ON true;

CREATE VIEW pharmacy_inventory_audit WITH (security_invoker=true) AS
 SELECT b.id,b.tenant_id,b.outlet_id,b.product_id,p.sku,p.brand_name,p.unit_of_issue,p.currency,b.lot_number,b.expires_on,b.created_at,
 COALESCE(s.on_hand,0)::bigint AS on_hand_units,COALESCE(s.received,0)::bigint AS received_units,COALESCE(s.uncosted,0)::bigint AS uncosted_receipt_units,
 COALESCE(s.cost,0)::text AS acquisition_cost_minor,
 CASE WHEN COALESCE(s.receipt_count,0)>0 AND s.uncosted=0 AND s.corrections=0 AND s.credits=0 THEN s.cost::text ELSE NULL END AS valuation_minor,
 CASE WHEN COALESCE(s.receipt_count,0)=0 THEN 'no_receipts' WHEN s.uncosted>0 THEN 'cost_missing' WHEN s.corrections>0 OR s.credits>0 THEN 'valuation_review' ELSE 'complete' END AS valuation_status
 FROM pharmacy_stock_batches b JOIN pharmacy_catalogue_products p ON p.tenant_id=b.tenant_id AND p.outlet_id=b.outlet_id AND p.id=b.product_id
 LEFT JOIN LATERAL (
 SELECT sum(m.quantity_units) AS on_hand,
 COALESCE(sum(m.quantity_units) FILTER (WHERE m.kind='receipt'),0) AS received,
 COALESCE(sum(m.quantity_units) FILTER (WHERE m.kind='receipt' AND l.id IS NULL),0) AS uncosted,
 COALESCE(sum(l.net_minor+l.tax_minor-l.recoverable_tax_minor+l.landed_minor),0) AS cost,
 count(*) FILTER (WHERE m.kind='receipt') AS receipt_count,count(*) FILTER (WHERE m.kind='correction') AS corrections,
 count(*) FILTER (WHERE EXISTS (SELECT 1 FROM pharmacy_settlements e WHERE e.tenant_id=l.tenant_id AND e.outlet_id=l.outlet_id AND e.payable_id=l.payable_id AND e.kind='credit')) AS credits
 FROM pharmacy_stock_movements m LEFT JOIN pharmacy_payable_lines l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.receipt_id=m.id
 WHERE m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id
 ) s ON true;
