SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$DECLARE body text;intact boolean;BEGIN
 IF pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true OR pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'stocktake recovery requires unchanged source authority';
 END IF;
 -- The sealed choice body is trusted by the source seal. Check its structural
 -- expression without the runtime prohibition on the DDL owner's TRUNCATE grant.
 SELECT prosrc INTO body FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_choice_integrity()');
 IF left(body,65)IS DISTINCT FROM left('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=',65)
 OR length(body)-length(replace(body,'pharmacy_operating_choice_support()',''))<>length('pharmacy_operating_choice_support()') THEN
  RAISE EXCEPTION 'stocktake recovery choice structure cannot be verified';
 END IF;
 EXECUTE replace(body,'pharmacy_operating_choice_support()','pharmacy_operating_choice_integrity()')INTO intact;
 IF intact IS DISTINCT FROM true THEN RAISE EXCEPTION 'stocktake recovery choice structure changed';END IF;
END $$;

CREATE FUNCTION pharmacy_stocktake_source_recovery_integrity()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT i.indrelid='pharmacy_staff_department_bindings'::regclass AND i.indisunique AND i.indisvalid AND i.indisready AND i.indimmediate AND i.indnkeyatts=2 AND i.indnatts=2
 AND pg_get_expr(i.indpred,i.indrelid)='(status = ''approved''::text)'
 AND(SELECT array_agg(a.attname ORDER BY k.ordinality)FROM unnest(i.indkey::smallint[])WITH ORDINALITY k(attnum,ordinality)JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum)=ARRAY['tenant_id','outlet_id']::name[]
 FROM pg_index i WHERE i.indexrelid=to_regclass('pharmacy_staff_binding_current')),false)
$$;
DO $$BEGIN IF pharmacy_stocktake_source_recovery_integrity()IS DISTINCT FROM true THEN RAISE EXCEPTION 'stocktake recovery requires a unique current binding per outlet';END IF;END $$;

ALTER TABLE pharmacy_stocktakes ADD COLUMN started_xact xid8;

CREATE TABLE pharmacy_stocktake_source_invalidations(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,stocktake_id uuid NOT NULL,replacement_stocktake_id uuid NOT NULL,
 original_binding_id uuid NOT NULL,replacement_binding_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK(length(actor_subject)BETWEEN 1 AND 200),posted_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,stocktake_id),
 CHECK(stocktake_id<>replacement_stocktake_id AND original_binding_id<>replacement_binding_id),
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,outlet_id,replacement_stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,original_binding_id)REFERENCES pharmacy_staff_department_bindings(tenant_id,id),
 FOREIGN KEY(tenant_id,replacement_binding_id)REFERENCES pharmacy_staff_department_bindings(tenant_id,id)
);
ALTER TABLE pharmacy_stocktake_source_invalidations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stocktake_source_invalidations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stocktake_source_invalidations
 USING(hms_tenant_visible(tenant_id)AND pharmacy_stocktake_hospital_scope(tenant_id,outlet_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_stocktake_hospital_scope(tenant_id,outlet_id,'count')AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE POLICY operating_choice_purpose ON pharmacy_stocktake_source_invalidations AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY operating_choice_delete ON pharmacy_stocktake_source_invalidations AS RESTRICTIVE FOR DELETE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY hospital_stocktake_choice_purpose ON pharmacy_stocktake_source_invalidations AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='');

-- Original start references support internal source comparison. Public receipt
-- recovery still checks its original hospital/department and verified Staff.
ALTER POLICY tenant_isolation ON pharmacy_stocktake_hospital_evidence
 USING(hms_tenant_visible(tenant_id)AND pharmacy_stocktake_hospital_scope(tenant_id,outlet_id,'read')AND(kind='start'OR(hospital_id=NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid AND department_id=NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid)));

CREATE FUNCTION pharmacy_stocktake_source_invalidation_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'source invalidation is append only' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_source_history';END IF;
 IF NEW.posted_xact<>pg_current_xact_id()OR pharmacy_stocktake_source_recovery_integrity()IS DISTINCT FROM true OR NOT EXISTS(
  SELECT 1 FROM pharmacy_stocktake_hospital_evidence original JOIN pharmacy_stocktake_hospital_evidence replacement
   ON replacement.tenant_id=original.tenant_id AND replacement.outlet_id=original.outlet_id
  JOIN pharmacy_stocktakes s ON s.tenant_id=original.tenant_id AND s.outlet_id=original.outlet_id AND s.id=original.stocktake_id
  WHERE original.tenant_id=NEW.tenant_id AND original.outlet_id=NEW.outlet_id AND original.stocktake_id=NEW.stocktake_id AND original.kind='start' AND original.id=NEW.stocktake_id
   AND original.binding_id=NEW.original_binding_id AND replacement.stocktake_id=NEW.replacement_stocktake_id AND replacement.kind='start' AND replacement.id=NEW.replacement_stocktake_id
   AND replacement.binding_id=NEW.replacement_binding_id AND replacement.actor_subject=NEW.actor_subject
   AND replacement.permission='pharmacy.stocktake.count' AND replacement.authority_basis=NULLIF(current_setting('app.pharmacy_stocktake_basis',true),'')::jsonb
   AND s.authority_kind='hospital'AND s.sealed
   AND COALESCE((SELECT c.state FROM pharmacy_stocktake_commands c WHERE c.tenant_id=s.tenant_id AND c.outlet_id=s.outlet_id AND c.stocktake_id=s.id ORDER BY c.version DESC LIMIT 1),'counting')IN('counting','submitted')
 )THEN RAISE EXCEPTION 'source invalidation has no current replacement proof' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_source_history';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_source_invalidation_guard BEFORE INSERT OR UPDATE OR DELETE ON pharmacy_stocktake_source_invalidations FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_source_invalidation_guard();
CREATE FUNCTION pharmacy_stocktake_source_write_audit()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,NEW.actor_subject,'pharmacy.stocktake.source_invalidated','pharmacy_stocktakes',NEW.stocktake_id,ARRAY['source_invalidated','replacement_stocktake_id']::text[]);
 RETURN NULL;
END $$;
CREATE TRIGGER pharmacy_stocktake_source_write_audit AFTER INSERT ON pharmacy_stocktake_source_invalidations FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_source_write_audit();

CREATE TRIGGER pharmacy_stocktake_source_history_immutable BEFORE TRUNCATE ON pharmacy_stocktake_source_invalidations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE FUNCTION pharmacy_stocktake_source_commit_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stocktakes replacement WHERE replacement.tenant_id=NEW.tenant_id AND replacement.outlet_id=NEW.outlet_id AND replacement.id=NEW.replacement_stocktake_id
  AND replacement.authority_kind='hospital'AND replacement.sealed AND replacement.actor_subject=NEW.actor_subject AND replacement.started_xact=NEW.posted_xact)THEN
  RAISE EXCEPTION 'source recovery requires a new sealed count in the same command' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_source_commit';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_stocktake_source_commit_guard AFTER INSERT ON pharmacy_stocktake_source_invalidations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_source_commit_guard();


CREATE FUNCTION pharmacy_stocktake_source_invalidated(t uuid,o uuid,s uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT take.authority_kind='hospital'AND take.sealed
  AND COALESCE((SELECT c.state FROM pharmacy_stocktake_commands c WHERE c.tenant_id=t AND c.outlet_id=o AND c.stocktake_id=s ORDER BY c.version DESC LIMIT 1),'counting')IN('counting','submitted')
  AND EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence original WHERE original.tenant_id=t AND original.outlet_id=o AND original.stocktake_id=s AND original.kind='start' AND original.id=s
   AND NOT EXISTS(SELECT 1 FROM pharmacy_staff_department_bindings b WHERE b.tenant_id=t AND b.outlet_id=o AND b.id=original.binding_id AND b.status='approved'AND b.version=original.binding_version AND b.department_revision_id=original.department_revision_id AND b.hospital_id=original.hospital_id AND b.department_id=original.department_id))
 FROM pharmacy_stocktakes take WHERE take.tenant_id=t AND take.outlet_id=o AND take.id=s),false)
$$;

CREATE FUNCTION pharmacy_stocktake_source_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF(SELECT authority_kind FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id)='hospital'AND(
  EXISTS(SELECT 1 FROM pharmacy_stocktake_source_invalidations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id)
  OR NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence original JOIN pharmacy_stocktake_hospital_evidence command
   ON command.tenant_id=original.tenant_id AND command.outlet_id=original.outlet_id AND command.stocktake_id=original.stocktake_id
   AND command.hospital_id=original.hospital_id AND command.department_id=original.department_id AND command.binding_id=original.binding_id AND command.binding_version=original.binding_version AND command.department_revision_id=original.department_revision_id
   WHERE original.tenant_id=NEW.tenant_id AND original.outlet_id=NEW.outlet_id AND original.stocktake_id=NEW.stocktake_id AND original.kind='start'AND original.id=NEW.stocktake_id
    AND command.kind=NEW.kind AND command.command_id=NEW.id AND command.actor_subject=NEW.actor_subject)
 )THEN RAISE EXCEPTION 'stocktake command source snapshot changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_source_snapshot';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_source_command_guard BEFORE INSERT ON pharmacy_stocktake_commands FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_source_command_guard();

CREATE OR REPLACE FUNCTION pharmacy_stocktake_start_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.started_xact:=pg_current_xact_id();
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') OR NEW.sealed THEN
  RAISE EXCEPTION 'stocktake actor or seal changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_identity';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':stocktake-start',0));
 IF EXISTS(SELECT 1 FROM pharmacy_stocktakes s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id
  AND NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_source_invalidations i WHERE i.tenant_id=s.tenant_id AND i.outlet_id=s.outlet_id AND i.stocktake_id=s.id)
  AND COALESCE((SELECT c.state FROM pharmacy_stocktake_commands c WHERE c.tenant_id=s.tenant_id AND c.outlet_id=s.outlet_id AND c.stocktake_id=s.id ORDER BY c.version DESC LIMIT 1),'counting')IN('counting','submitted'))THEN
  RAISE EXCEPTION 'an outlet count is already open' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_open';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.quarantine_location_id AND stock_status='quarantine')THEN
  RAISE EXCEPTION 'stocktake needs quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_quarantine';
 END IF;
 SELECT COALESCE(MAX(number),0)+1 INTO NEW.number FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id;
 RETURN NEW;
END $$;

-- Reviewed correction rows already have complete statement-level proof of the
-- immutable line, approved destination, units, actor and source reference.
-- Keep per-row location checks for all other stock operations.
DROP TRIGGER pharmacy_stock_movement_location_guard ON pharmacy_stock_movements;
CREATE TRIGGER pharmacy_stock_movement_location_guard BEFORE INSERT ON pharmacy_stock_movements
 FOR EACH ROW WHEN(NEW.kind IS DISTINCT FROM 'correction'OR COALESCE(current_setting('app.pharmacy_stock_permission',true),'')IS DISTINCT FROM 'pharmacy.stocktake.review')
 EXECUTE FUNCTION pharmacy_stock_movement_location_guard();

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
DO $choices_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_choice_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=%L::jsonb,false)',reviewed::text));
END $choices_integrity$;
DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
