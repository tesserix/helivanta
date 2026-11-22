SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

-- An immutable, versioned Tally ledger map per outlet (ADR-0076 §2); one successor per map.
CREATE TABLE pharmacy_tally_ledger_maps(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,replaces uuid CHECK(replaces<>id),
 entry_count integer NOT NULL CHECK(entry_count BETWEEN 1 AND 1000),intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 CONSTRAINT pharmacy_tally_ledger_map_successor UNIQUE NULLS NOT DISTINCT(tenant_id,outlet_id,replaces),
 FOREIGN KEY(tenant_id,outlet_id,replaces)REFERENCES pharmacy_tally_ledger_maps(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_tally_ledger_maps ENABLE ROW LEVEL SECURITY;ALTER TABLE pharmacy_tally_ledger_maps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_tally_ledger_maps
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_tally_ledger_map_audit AFTER INSERT ON pharmacy_tally_ledger_maps FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_tally_ledger_map_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_tally_ledger_maps FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_tally_ledger_map_entries(
 id uuid NOT NULL DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,map_id uuid NOT NULL,
 head text NOT NULL CONSTRAINT pharmacy_tally_ledger_map_head CHECK(head~'^(voucher_type:(sale|credit_note|supplier_payment|supplier_payment_reversal)|tender:(cash|upi|upi_verified|card|admission_account)|sales_(goods|service):(taxable:(0|[1-9][0-9]{0,5}|1000000)|(nil_rated|exempt):0)|[cs]gst:(0|[1-9][0-9]{0,5}|1000000)|payment_method:(cash|bank|card|upi|other)|supplier:.+)$'
  AND(head!~'^supplier:'OR(length(head)BETWEEN 10 AND 109 AND substr(head,10)=upper(btrim(substr(head,10)))))AND head!~'[\x01-\x1f\x7f-\x9f￾￿]'),
 ledger_name text NOT NULL CONSTRAINT pharmacy_tally_ledger_map_name CHECK(length(ledger_name)BETWEEN 1 AND 100 AND ledger_name=btrim(ledger_name)AND ledger_name!~'[\x01-\x1f\x7f-\x9f￾￿]'),
 PRIMARY KEY(tenant_id,outlet_id,map_id,head),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,map_id)REFERENCES pharmacy_tally_ledger_maps(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_tally_ledger_map_entries ENABLE ROW LEVEL SECURITY;ALTER TABLE pharmacy_tally_ledger_map_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_tally_ledger_map_entries
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)
 AND EXISTS(SELECT 1 FROM pharmacy_tally_ledger_maps m WHERE m.tenant_id=pharmacy_tally_ledger_map_entries.tenant_id AND m.outlet_id=pharmacy_tally_ledger_map_entries.outlet_id AND m.id=pharmacy_tally_ledger_map_entries.map_id AND m.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),'')));
CREATE TRIGGER pharmacy_tally_ledger_map_entry_audit AFTER INSERT ON pharmacy_tally_ledger_map_entries FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_tally_ledger_map_entry_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_tally_ledger_map_entries FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- A version holds exactly its counted entries and keeps every head the version it replaces named.
-- A version holds exactly its counted entries and every head it replaces.
CREATE FUNCTION pharmacy_tally_ledger_map_complete(row_tenant uuid,row_outlet uuid,target uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM pharmacy_tally_ledger_maps m WHERE m.tenant_id=row_tenant AND m.outlet_id=row_outlet AND m.id=target
  AND m.entry_count=(SELECT count(*)FROM pharmacy_tally_ledger_map_entries e WHERE e.tenant_id=m.tenant_id AND e.outlet_id=m.outlet_id AND e.map_id=m.id)
  AND NOT EXISTS(SELECT 1 FROM pharmacy_tally_ledger_map_entries o WHERE o.tenant_id=m.tenant_id AND o.outlet_id=m.outlet_id AND o.map_id=m.replaces
   AND NOT EXISTS(SELECT 1 FROM pharmacy_tally_ledger_map_entries n WHERE n.tenant_id=o.tenant_id AND n.outlet_id=o.outlet_id AND n.map_id=m.id AND n.head=o.head)))
$$;
CREATE FUNCTION pharmacy_tally_ledger_map_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT pharmacy_tally_ledger_map_complete(NEW.tenant_id,NEW.outlet_id,NEW.id)THEN
  RAISE EXCEPTION 'ledger map requires its counted entries and every head it replaces' USING ERRCODE='23514',CONSTRAINT='pharmacy_tally_ledger_map_entries';
 END IF;
 RETURN NEW;
END $$;
-- Checked once per statement, not per row: a version's entries arrive in one statement.
CREATE FUNCTION pharmacy_tally_ledger_map_entries_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE version record;
BEGIN
 -- A loop, so the check runs per version rather than pushed below DISTINCT onto every row.
 FOR version IN SELECT DISTINCT tenant_id,outlet_id,map_id FROM added LOOP
  IF NOT pharmacy_tally_ledger_map_complete(version.tenant_id,version.outlet_id,version.map_id)THEN
   RAISE EXCEPTION 'ledger map requires its counted entries and every head it replaces' USING ERRCODE='23514',CONSTRAINT='pharmacy_tally_ledger_map_entries';
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_tally_ledger_map_guard AFTER INSERT ON pharmacy_tally_ledger_maps DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_tally_ledger_map_guard();
CREATE TRIGGER pharmacy_tally_ledger_map_entry_guard AFTER INSERT ON pharmacy_tally_ledger_map_entries REFERENCING NEW TABLE AS added FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_tally_ledger_map_entries_guard();

CREATE INDEX pharmacy_settlement_day ON pharmacy_settlements(tenant_id,outlet_id,occurred_on);

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_tally_ledger_maps','pharmacy_tally_ledger_map_entries'] LOOP
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
 END LOOP;
END $purpose$;

-- Extend the checked purpose inventory only after validating its existing fingerprint.
DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='pharmacy_operating_choice_context';
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION pharmacy_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
