SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'hospital stocktake requires unchanged pharmacy selection authority';
 END IF;
END $$;

CREATE FUNCTION pharmacy_stocktake_hospital_frame(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_stock_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_stock_access',true)='authorized'
 AND NULLIF(current_setting('app.pharmacy_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid IS NOT NULL
 AND((operation='read'AND current_setting('app.pharmacy_stock_permission',true)IN('pharmacy.stocktake.read','pharmacy.stocktake.count','pharmacy.stocktake.review'))
 OR(operation='count'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stocktake.count')
 OR(operation='review'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stocktake.review')),false)
$$;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_staff_department_bindings'::regclass AND polname='pharmacy_staff_binding_scope';
 EXECUTE format($policy$ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING((%s)OR(pharmacy_stocktake_hospital_frame(tenant_id,outlet_id,'read')AND hospital_id=NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid AND department_id=NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid))$policy$,prior);
END $$;
CREATE FUNCTION pharmacy_stocktake_hospital_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT pharmacy_stocktake_hospital_frame(t,o,operation)AND EXISTS(
 SELECT 1 FROM pharmacy_staff_department_bindings b
 JOIN pharmacy_outlets x ON(x.tenant_id,x.id)=(b.tenant_id,b.outlet_id)
 JOIN pharmacy_organizations p ON(p.tenant_id,p.id)=(x.tenant_id,x.pharmacy_id)
 WHERE b.tenant_id=t AND b.outlet_id=o AND b.status='approved'
 AND b.hospital_id=NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid
 AND b.department_id=NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid
 AND x.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid
 AND p.ownership_type='hospital_owned')
$$;
CREATE FUNCTION pharmacy_stocktake_independent_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(COALESCE(current_setting('app.pharmacy_stocktake_hospital',true),'')='' AND COALESCE(current_setting('app.pharmacy_stocktake_department',true),'')='' AND hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_stock_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_stock_access',true)='authorized'
 AND NULLIF(current_setting('app.pharmacy_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND((operation='read'AND current_setting('app.pharmacy_stock_permission',true)IN('pharmacy.stocktake.read','pharmacy.stocktake.count','pharmacy.stocktake.review'))
 OR(operation='count'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stocktake.count')
 OR(operation='review'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stocktake.review'))
 AND EXISTS(SELECT 1 FROM pharmacy_outlets x WHERE x.tenant_id=t AND x.id=o
 AND x.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid),false)
$$;

CREATE OR REPLACE FUNCTION pharmacy_stocktake_operating_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN COALESCE(current_setting('app.pharmacy_stocktake_hospital',true),'')<>''
 THEN pharmacy_stocktake_hospital_scope(t,o,operation)ELSE pharmacy_stocktake_independent_scope(t,o,operation)END
$$;
CREATE TABLE pharmacy_stocktake_hospital_evidence(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 pharmacy_id uuid NOT NULL,stocktake_id uuid,
 kind text NOT NULL CHECK(kind IN('start','list_read','record_read','register_read','lines_read','count','count_read','submit','cancel','approve','reject','decision_read')),
 command_id uuid,
 permission text NOT NULL CHECK(permission IN('pharmacy.stocktake.read','pharmacy.stocktake.count','pharmacy.stocktake.review')),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'),
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7'),
 binding_id uuid NOT NULL CHECK(substring(binding_id::text,15,1)='7'),
 binding_version bigint NOT NULL CHECK(binding_version BETWEEN 2 AND 9007199254740991),
 department_revision_id uuid NOT NULL CHECK(substring(department_revision_id::text,15,1)='7'),
 generation_id uuid NOT NULL CHECK(substring(generation_id::text,15,1)='7'),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740991),
 authority_basis jsonb NOT NULL CHECK(jsonb_typeof(authority_basis)='object'AND octet_length(authority_basis::text)<=4096),
 staff_id uuid NOT NULL CHECK(staff_id<>'00000000-0000-0000-0000-000000000000'),
 link_id uuid NOT NULL CHECK(link_id<>'00000000-0000-0000-0000-000000000000'),
 link_version bigint NOT NULL CHECK(link_version BETWEEN 2 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 CHECK((kind='list_read')=(stocktake_id IS NULL)),
 FOREIGN KEY(tenant_id,outlet_id,hospital_id,binding_id)REFERENCES pharmacy_staff_department_bindings(tenant_id,outlet_id,hospital_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id,command_id)REFERENCES pharmacy_stocktake_commands(tenant_id,outlet_id,stocktake_id,id)DEFERRABLE INITIALLY DEFERRED,
 CHECK((kind IN('count','count_read','submit','cancel','approve','reject','decision_read'))=(command_id IS NOT NULL)),
 CHECK(kind NOT IN('count','submit','cancel','approve','reject')OR id=command_id),
 CHECK(kind NOT IN('count','submit','cancel')OR permission='pharmacy.stocktake.count'),
 CHECK(kind NOT IN('approve','reject')OR permission='pharmacy.stocktake.review'),
 CHECK(kind NOT IN('list_read','record_read','register_read','lines_read','count_read','decision_read')OR permission='pharmacy.stocktake.read'),
 CHECK(kind<>'start'OR(id=stocktake_id AND permission='pharmacy.stocktake.count'))
);
CREATE INDEX ON pharmacy_stocktake_hospital_evidence(tenant_id,outlet_id,stocktake_id,staff_id,kind);
CREATE INDEX ON pharmacy_stocktake_hospital_evidence(tenant_id,outlet_id,stocktake_id,command_id,kind);
ALTER TABLE pharmacy_stocktake_hospital_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stocktake_hospital_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stocktake_hospital_evidence
 USING(hms_tenant_visible(tenant_id)AND pharmacy_stocktake_hospital_scope(tenant_id,outlet_id,'read')AND hospital_id=NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid AND department_id=NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND pharmacy_stocktake_hospital_scope(tenant_id,outlet_id,CASE WHEN kind IN('start','count','submit','cancel')THEN 'count'WHEN kind IN('approve','reject')THEN 'review'ELSE 'read'END)
 AND pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid
 AND permission=current_setting('app.pharmacy_stock_permission',true)
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
 AND hospital_id=NULLIF(current_setting('app.pharmacy_stocktake_hospital',true),'')::uuid
 AND department_id=NULLIF(current_setting('app.pharmacy_stocktake_department',true),'')::uuid
 AND authority_basis=NULLIF(current_setting('app.pharmacy_stocktake_basis',true),'')::jsonb
 AND staff_id=(authority_basis->'actor'->>'StaffID')::uuid
 AND link_id=(authority_basis->'actor'->>'LinkID')::uuid
 AND link_version=(authority_basis->'actor'->>'LinkVersion')::bigint
 AND generation_id=(authority_basis->'actor'->>'GenerationID')::uuid
 AND employment_epoch=(authority_basis->'actor'->>'EmploymentEpoch')::bigint
 AND binding_id=(authority_basis->>'binding_id')::uuid
 AND binding_version=(authority_basis->>'binding_version')::bigint
 AND department_revision_id=(authority_basis->>'department_revision_id')::uuid
 AND EXISTS(SELECT 1 FROM pharmacy_staff_department_bindings b WHERE b.tenant_id=pharmacy_stocktake_hospital_evidence.tenant_id AND b.id=pharmacy_stocktake_hospital_evidence.binding_id AND b.outlet_id=pharmacy_stocktake_hospital_evidence.outlet_id AND b.status='approved'AND b.version=pharmacy_stocktake_hospital_evidence.binding_version AND b.department_revision_id=pharmacy_stocktake_hospital_evidence.department_revision_id));
CREATE POLICY operating_choice_purpose ON pharmacy_stocktake_hospital_evidence AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY operating_choice_delete ON pharmacy_stocktake_hospital_evidence AS RESTRICTIVE FOR DELETE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE TRIGGER pharmacy_stocktake_hospital_evidence_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_hospital_evidence FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE POLICY hospital_stocktake_separation ON pharmacy_stocktake_operating_evidence AS RESTRICTIVE
 USING(COALESCE(current_setting('app.pharmacy_stocktake_hospital',true),'')='')
 WITH CHECK(COALESCE(current_setting('app.pharmacy_stocktake_hospital',true),'')='');
DO $$DECLARE proof text;BEGIN
 FOREACH proof IN ARRAY ARRAY['operation','source']LOOP
 EXECUTE format($constraint$ALTER TABLE pharmacy_stocktake_hospital_evidence ADD CONSTRAINT %I CHECK(COALESCE(
 jsonb_typeof(authority_basis->%L)='object'
 AND(authority_basis->%L->>'AssignmentID')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND authority_basis->%L->>'ScopeKind'='department'
 AND substring(authority_basis->%L->>'DepartmentID',15,1)='7'
 AND(authority_basis->%L->>'Version')::bigint BETWEEN 1 AND 9007199254740991
 AND(authority_basis->%L->>'ScopeEpoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(authority_basis->%L->>'MembershipEpoch')::bigint BETWEEN 1 AND 9007199254740991,false))$constraint$,
 'pharmacy_stocktake_hospital_'||proof,proof,proof,proof,proof,proof,proof,proof);
 END LOOP;
END $$;
CREATE FUNCTION pharmacy_stocktake_hospital_evidence_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='start'AND EXISTS(SELECT 1 FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id)THEN
 RAISE EXCEPTION 'historical stocktake authority cannot be invented' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF NEW.kind IN('count','submit','cancel','approve','reject')AND EXISTS(SELECT 1 FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.command_id)THEN
 RAISE EXCEPTION 'historical count authority cannot be invented' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_hospital_evidence_guard BEFORE INSERT ON pharmacy_stocktake_hospital_evidence FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_hospital_evidence_guard();

ALTER TABLE pharmacy_stocktakes DROP CONSTRAINT pharmacy_stocktakes_authority_kind_check;
ALTER TABLE pharmacy_stocktakes ADD CONSTRAINT pharmacy_stocktakes_authority_kind_check CHECK(authority_kind IN('legacy','operating','hospital'))NOT VALID;
ALTER TABLE pharmacy_stocktakes VALIDATE CONSTRAINT pharmacy_stocktakes_authority_kind_check;
CREATE OR REPLACE FUNCTION pharmacy_stocktake_operating_start_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pharmacy_stocktake_independent_scope(NEW.tenant_id,NEW.outlet_id,'count')AND NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.id
 AND id=NEW.id AND kind='start'AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed stocktake needs source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF pharmacy_stocktake_independent_scope(NEW.tenant_id,NEW.outlet_id,'count')THEN
 NEW.authority_kind:='operating';
 ELSIF pharmacy_stocktake_hospital_scope(NEW.tenant_id,NEW.outlet_id,'count')THEN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.id AND id=NEW.id AND kind='start'AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'hospital count needs original source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 NEW.authority_kind:='hospital';
 ELSIF NEW.authority_kind<>'legacy'THEN
 RAISE EXCEPTION 'reviewed stocktake needs held authority' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION pharmacy_stocktake_operating_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE model text;reviewed boolean;operation text;
BEGIN
 SELECT authority_kind INTO STRICT model FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id;
 reviewed:=starts_with(COALESCE(current_setting('app.pharmacy_stock_permission',true),''),'pharmacy.stocktake.');
 IF model='operating'OR(reviewed AND model<>'hospital')THEN
 operation:=CASE WHEN NEW.kind IN('approve','reject')THEN 'review'ELSE 'count'END;
 IF model<>'operating'OR NOT pharmacy_stocktake_independent_scope(NEW.tenant_id,NEW.outlet_id,operation)OR NEW.kind NOT IN('count','submit','cancel','approve','reject')OR NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id
 AND id=NEW.id AND command_id=NEW.id AND kind=NEW.kind AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed command needs original source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF operation='review'THEN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id AND id=NEW.stocktake_id AND kind='start')
 OR EXISTS(SELECT 1 FROM pharmacy_stocktake_commands c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.stocktake_id=NEW.stocktake_id AND c.kind IN('count','submit')
 AND NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_operating_evidence e WHERE e.tenant_id=c.tenant_id AND e.outlet_id=c.outlet_id AND e.stocktake_id=c.stocktake_id AND e.command_id=c.id AND e.kind=c.kind AND e.actor_subject=c.actor_subject))THEN
 RAISE EXCEPTION 'reviewed participant history is incomplete' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF EXISTS(SELECT 1 FROM pharmacy_stocktake_operating_evidence p JOIN pharmacy_stocktake_operating_evidence r ON(r.tenant_id,r.outlet_id,r.stocktake_id)=(p.tenant_id,p.outlet_id,p.stocktake_id)
 WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.stocktake_id=NEW.stocktake_id AND r.id=NEW.id AND r.kind=NEW.kind
 AND p.kind IN('start','count','submit')AND p.staff_id=r.staff_id)THEN
 RAISE EXCEPTION 'reviewer Staff participated in the count' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_independent';
 END IF;
 END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION pharmacy_stocktake_hospital_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE model text;operation text;
BEGIN
 SELECT authority_kind INTO STRICT model FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id;
 IF model='hospital'THEN
 operation:=CASE WHEN NEW.kind IN('approve','reject')THEN 'review'ELSE 'count'END;
 IF NOT pharmacy_stocktake_hospital_scope(NEW.tenant_id,NEW.outlet_id,operation)OR NEW.kind NOT IN('count','submit','cancel','approve','reject')OR NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_hospital_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id
 AND id=NEW.id AND command_id=NEW.id AND kind=NEW.kind AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed command needs original source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF operation='review'THEN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id AND id=NEW.stocktake_id AND kind='start')
 OR EXISTS(SELECT 1 FROM pharmacy_stocktake_commands c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.stocktake_id=NEW.stocktake_id AND c.kind IN('count','submit')
 AND NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence e WHERE e.tenant_id=c.tenant_id AND e.outlet_id=c.outlet_id AND e.stocktake_id=c.stocktake_id AND e.command_id=c.id AND e.kind=c.kind AND e.actor_subject=c.actor_subject))THEN
 RAISE EXCEPTION 'reviewed participant history is incomplete' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF EXISTS(SELECT 1 FROM pharmacy_stocktake_hospital_evidence p JOIN pharmacy_stocktake_hospital_evidence r ON(r.tenant_id,r.outlet_id,r.stocktake_id)=(p.tenant_id,p.outlet_id,p.stocktake_id)
 WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.stocktake_id=NEW.stocktake_id AND r.id=NEW.id AND r.kind=NEW.kind
 AND p.kind IN('start','count','submit')AND p.staff_id=r.staff_id)THEN
 RAISE EXCEPTION 'reviewer Staff participated in the count' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_independent';
 END IF;
 END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_hospital_command_guard BEFORE INSERT ON pharmacy_stocktake_commands FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_hospital_command_guard();
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

-- Capture only owner metadata. No customer rows or private Workforce facts enter this seal.
CREATE FUNCTION pharmacy_stocktake_hospital_support_state()RETURNS jsonb LANGUAGE sql STABLE AS $$
 WITH targets AS(
  SELECT oid,relname,relkind,relpersistence,relrowsecurity,relforcerowsecurity FROM pg_class
  WHERE relnamespace=current_schema()::regnamespace
  AND(starts_with(relname,'pharmacy_stocktake')OR relname IN('pharmacy_staff_department_bindings','pharmacy_stock_movements'))
  AND relkind IN('r','p')
 )SELECT jsonb_build_object(
 'tables',(SELECT jsonb_agg(jsonb_build_array(relname,relkind,relpersistence,relrowsecurity,relforcerowsecurity)ORDER BY relname)FROM targets),
 'policies',(SELECT jsonb_agg(jsonb_build_array(t.relname,p.polname,p.polcmd,p.polpermissive,p.polroles,pg_get_expr(p.polqual,p.polrelid),pg_get_expr(p.polwithcheck,p.polrelid))ORDER BY t.relname,p.polname)FROM pg_policy p JOIN targets t ON t.oid=p.polrelid),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(t.relname,c.conname,c.contype,c.convalidated,c.condeferrable,c.condeferred,pg_get_constraintdef(c.oid))ORDER BY t.relname,c.conname)FROM pg_constraint c JOIN targets t ON t.oid=c.conrelid),
 'triggers',(SELECT jsonb_agg(jsonb_build_array(t.relname,g.tgname,g.tgenabled,pg_get_triggerdef(g.oid))ORDER BY t.relname,g.tgname)FROM pg_trigger g JOIN targets t ON t.oid=g.tgrelid WHERE NOT g.tgisinternal),
 'functions',(SELECT jsonb_agg(jsonb_build_array(p.proname,pg_get_function_identity_arguments(p.oid),md5(pg_get_functiondef(p.oid)))ORDER BY p.proname,pg_get_function_identity_arguments(p.oid))FROM pg_proc p
 WHERE p.pronamespace=current_schema()::regnamespace AND p.prokind='f'
 AND(starts_with(p.proname,'pharmacy_stocktake_')OR starts_with(p.proname,'pharmacy_staff_binding_')OR starts_with(p.proname,'pharmacy_operating_choice_')OR p.proname IN('hms_forbid_mutation','pharmacy_catalogue_immutable'))
 AND p.proname<>'pharmacy_stocktake_hospital_integrity'))
$$;
DO $hospital_integrity$
DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',
 format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
