SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$DECLARE choice_definition text;choice_metadata_ok boolean;BEGIN
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true OR pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true OR pharmacy_stocktake_source_recovery_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'existing pharmacy operating/hospital authority metadata changed' USING ERRCODE='23514';
 END IF;
 -- The hospital seal above authenticates this definition. Its runtime support
 -- probe forbids TRUNCATE for app roles, so it cannot run as the schema owner.
 SELECT prosrc INTO choice_definition FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_choice_integrity()');
 IF choice_definition IS NULL OR position('pharmacy_operating_choice_support()'IN choice_definition)=0 THEN
  RAISE EXCEPTION 'unexpected pharmacy choice integrity definition' USING ERRCODE='23514';
 END IF;
 EXECUTE replace(choice_definition,'pharmacy_operating_choice_support()','pharmacy_operating_choice_integrity()') INTO choice_metadata_ok;
 IF choice_metadata_ok IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'existing pharmacy choice authority metadata changed' USING ERRCODE='23514';
 END IF;
END $$;

-- The ward requester frame: one hospital department at one outlet, never combined with pharmacist access.
CREATE FUNCTION pharmacy_ward_indent_frame(t uuid,o uuid,h uuid,d uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_indent_outlet',true),'')::uuid
 AND h=NULLIF(current_setting('app.pharmacy_indent_hospital',true),'')::uuid
 AND d=NULLIF(current_setting('app.pharmacy_indent_department',true),'')::uuid
 AND NULLIF(current_setting('app.pharmacy_indent_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_indent_subject',true)=current_setting('app.actor_subject',true)
 AND COALESCE(current_setting('app.pharmacy_outlet',true),'')=''
 AND COALESCE(current_setting('app.pharmacy_subject',true),'')=''
 AND((operation='read'AND current_setting('app.pharmacy_indent_permission',true)IN('pharmacy.indent.read','pharmacy.indent.raise'))
 OR(operation='raise'AND current_setting('app.pharmacy_indent_permission',true)='pharmacy.indent.raise')),false)
$$;
CREATE FUNCTION pharmacy_ward_indent_affiliated(t uuid,o uuid,h uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM pharmacy_outlets x JOIN pharmacy_hospital_affiliations a ON a.tenant_id=x.tenant_id AND a.pharmacy_id=x.pharmacy_id
 WHERE x.tenant_id=t AND x.id=o AND a.hospital_id=h
 AND(SELECT v.kind FROM pharmacy_affiliation_verifications v WHERE v.tenant_id=a.tenant_id AND v.affiliation_id=a.id ORDER BY v.sequence DESC LIMIT 1)='accepted')
$$;
CREATE FUNCTION pharmacy_ward_indent_catalogue_scope()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(pharmacy_ward_indent_frame(s.t,s.o,s.h,s.d,'read')AND pharmacy_ward_indent_affiliated(s.t,s.o,s.h),false)
 FROM(SELECT NULLIF(current_setting('app.tenant_id',true),'')::uuid t,NULLIF(current_setting('app.pharmacy_indent_outlet',true),'')::uuid o,
 NULLIF(current_setting('app.pharmacy_indent_hospital',true),'')::uuid h,NULLIF(current_setting('app.pharmacy_indent_department',true),'')::uuid d)s
$$;

CREATE TABLE pharmacy_ward_indents(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'),
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7'),
 kind text NOT NULL CHECK(kind='patient'),
 occupancy_id uuid NOT NULL,admission_id uuid NOT NULL,patient_id uuid NOT NULL,
 ward_code text NOT NULL CHECK(length(ward_code)BETWEEN 1 AND 40),room_code text NOT NULL CHECK(length(room_code)BETWEEN 1 AND 40),bed_code text NOT NULL CHECK(length(bed_code)BETWEEN 1 AND 40),
 hospital_name text NOT NULL CHECK(length(btrim(hospital_name))BETWEEN 1 AND 200 AND hospital_name!~'[[:cntrl:]]'),
 department_name text NOT NULL CHECK(length(btrim(department_name))BETWEEN 1 AND 200 AND department_name!~'[[:cntrl:]]'),
 line_count integer NOT NULL CHECK(line_count BETWEEN 1 AND 50),
 status text NOT NULL CHECK(status IN('open','closed','cancelled')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 2),
 close_reason text CHECK(close_reason IS NULL OR(length(close_reason)BETWEEN 5 AND 200 AND close_reason=btrim(close_reason)AND close_reason!~'[[:cntrl:]]')),
 closed_at timestamptz,closed_by text CHECK(closed_by IS NULL OR length(btrim(closed_by))BETWEEN 1 AND 200),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[0-9a-f]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 staff_id uuid NOT NULL CHECK(staff_id<>'00000000-0000-0000-0000-000000000000'),
 link_id uuid NOT NULL CHECK(link_id<>'00000000-0000-0000-0000-000000000000'),
 link_version bigint NOT NULL CHECK(link_version BETWEEN 2 AND 9007199254740991),
 generation_id uuid NOT NULL CHECK(substring(generation_id::text,15,1)='7'),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740991),
 authority_basis jsonb NOT NULL CHECK(jsonb_typeof(authority_basis)='object'AND octet_length(authority_basis::text)<=4096),
 raised_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
 raised_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 UNIQUE(tenant_id,outlet_id,id,hospital_id,department_id),
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,occupancy_id)REFERENCES pharmacy_bed_occupancies(tenant_id,occupancy_id)ON DELETE RESTRICT,
 CHECK((status='open')=(version=1)),
 CHECK((status='open')=(closed_at IS NULL)AND(status='open')=(closed_by IS NULL)),
 CHECK(status='closed'OR close_reason IS NULL)
);
CREATE INDEX pharmacy_ward_indent_queue ON pharmacy_ward_indents(tenant_id,outlet_id,raised_at,id)WHERE status='open';
CREATE INDEX pharmacy_ward_indent_department ON pharmacy_ward_indents(tenant_id,outlet_id,hospital_id,department_id,raised_at DESC,id DESC);
DO $proof$DECLARE proof text;BEGIN
 FOREACH proof IN ARRAY ARRAY['operation','source']LOOP
  EXECUTE format($constraint$ALTER TABLE pharmacy_ward_indents ADD CONSTRAINT %I CHECK(COALESCE(
   jsonb_typeof(authority_basis->%L)='object'
   AND(authority_basis->%L->>'AssignmentID')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
   AND authority_basis->%L->>'ScopeKind'='department'
   AND(authority_basis->%L->>'DepartmentID')::uuid=department_id
   AND(authority_basis->%L->>'Version')::bigint BETWEEN 1 AND 9007199254740991
   AND(authority_basis->%L->>'ScopeEpoch')::bigint BETWEEN 1 AND 9007199254740991
   AND(authority_basis->%L->>'MembershipEpoch')::bigint BETWEEN 1 AND 9007199254740991,false))$constraint$,
   'pharmacy_ward_indent_'||proof,proof,proof,proof,proof,proof,proof,proof);
 END LOOP;
END $proof$;
ALTER TABLE pharmacy_ward_indents ADD CONSTRAINT pharmacy_ward_indent_actor CHECK(COALESCE(
 staff_id=(authority_basis->'actor'->>'StaffID')::uuid AND link_id=(authority_basis->'actor'->>'LinkID')::uuid
 AND link_version=(authority_basis->'actor'->>'LinkVersion')::bigint AND generation_id=(authority_basis->'actor'->>'GenerationID')::uuid
 AND employment_epoch=(authority_basis->'actor'->>'EmploymentEpoch')::bigint,false));
ALTER TABLE pharmacy_ward_indents ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_ward_indents FORCE ROW LEVEL SECURITY;
CREATE POLICY ward_indent_read ON pharmacy_ward_indents FOR SELECT
 USING(hms_tenant_visible(tenant_id)AND(pharmacy_outlet_allowed(tenant_id,outlet_id,false)OR pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'read')));
CREATE POLICY ward_indent_raise ON pharmacy_ward_indents FOR INSERT
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'raise')
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
 AND authority_basis=NULLIF(current_setting('app.pharmacy_indent_basis',true),'')::jsonb);
CREATE POLICY ward_indent_settle ON pharmacy_ward_indents FOR UPDATE
 USING(hms_tenant_visible(tenant_id)AND(pharmacy_outlet_allowed(tenant_id,outlet_id,true)OR pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'raise')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND(
 (pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND status='closed'AND closed_by=NULLIF(current_setting('app.pharmacy_subject',true),''))
 OR(pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'raise')AND status='cancelled'AND closed_by=NULLIF(current_setting('app.actor_subject',true),''))));

-- A raise re-resolves the open stay and the verified affiliation; nothing else may be inserted.
CREATE FUNCTION pharmacy_ward_indent_raise_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>'open'OR NEW.version<>1 OR NEW.raised_xact<>pg_current_xact_id()THEN
  RAISE EXCEPTION 'a ward indent is raised open' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_raise';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pharmacy_bed_occupancies o WHERE o.tenant_id=NEW.tenant_id AND o.occupancy_id=NEW.occupancy_id AND o.hospital_id=NEW.hospital_id
  AND o.admission_id=NEW.admission_id AND o.patient_id=NEW.patient_id AND o.ward_code=NEW.ward_code AND o.room_code=NEW.room_code AND o.bed_code=NEW.bed_code
  AND o.started_at IS NOT NULL AND o.ended_at IS NULL)THEN
  RAISE EXCEPTION 'the stay is not open' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_stay_open';
 END IF;
 IF NOT pharmacy_ward_indent_affiliated(NEW.tenant_id,NEW.outlet_id,NEW.hospital_id)THEN
  RAISE EXCEPTION 'the hospital has not verified this pharmacy' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_affiliated';
 END IF;
 NEW.raised_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_ward_indent_raise_guard BEFORE INSERT ON pharmacy_ward_indents FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_raise_guard();
CREATE TRIGGER pharmacy_ward_indent_audit AFTER INSERT OR UPDATE ON pharmacy_ward_indents FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_ward_indent_no_delete BEFORE DELETE OR TRUNCATE ON pharmacy_ward_indents FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_ward_indent_lines(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,indent_id uuid NOT NULL,line_number integer NOT NULL CHECK(line_number BETWEEN 1 AND 50),
 hospital_id uuid NOT NULL,department_id uuid NOT NULL,
 product_id uuid NOT NULL,requested_units bigint NOT NULL CHECK(requested_units BETWEEN 1 AND 1000000),
 PRIMARY KEY(tenant_id,outlet_id,indent_id,line_number),
 UNIQUE(tenant_id,outlet_id,indent_id,product_id),
 FOREIGN KEY(tenant_id,outlet_id,indent_id,hospital_id,department_id)REFERENCES pharmacy_ward_indents(tenant_id,outlet_id,id,hospital_id,department_id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,product_id)REFERENCES pharmacy_catalogue_products(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_ward_indent_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_ward_indent_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY ward_indent_line_read ON pharmacy_ward_indent_lines FOR SELECT
 USING(hms_tenant_visible(tenant_id)AND(pharmacy_outlet_allowed(tenant_id,outlet_id,false)OR pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'read')));
CREATE POLICY ward_indent_line_raise ON pharmacy_ward_indent_lines FOR INSERT
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'raise'));
CREATE FUNCTION pharmacy_ward_indent_line_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE indent pharmacy_ward_indents;BEGIN
 SELECT * INTO indent FROM pharmacy_ward_indents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.indent_id;
 IF NOT FOUND OR indent.status<>'open'OR indent.raised_xact<>pg_current_xact_id()OR NEW.line_number>indent.line_count THEN
  RAISE EXCEPTION 'ward indent lines are fixed when it is raised' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_lines_fixed';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pharmacy_catalogue_products p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id AND p.id=NEW.product_id AND p.status='active')THEN
  RAISE EXCEPTION 'only an active product can be indented' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_product_active';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_ward_indent_line_guard BEFORE INSERT ON pharmacy_ward_indent_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_line_guard();
CREATE TRIGGER pharmacy_ward_indent_line_audit AFTER INSERT ON pharmacy_ward_indent_lines FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_ward_indent_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_ward_indent_lines FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
-- The raise sets this IMMEDIATE inside its frame; left deferred it reads no lines and refuses.
CREATE FUNCTION pharmacy_ward_indent_complete()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF(SELECT count(*)FROM pharmacy_ward_indent_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.indent_id=NEW.id)<>NEW.line_count THEN
  RAISE EXCEPTION 'a ward indent carries every line it declares' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_ward_indent_complete AFTER INSERT ON pharmacy_ward_indents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_complete();

-- One row per sale line an issue posted; the issue is identified by its sale.
CREATE TABLE pharmacy_ward_indent_issues(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,sale_id uuid NOT NULL,sale_line integer NOT NULL,
 indent_id uuid NOT NULL,line_number integer NOT NULL,hospital_id uuid NOT NULL,department_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 1000000),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,sale_id,sale_line),
 FOREIGN KEY(tenant_id,outlet_id,sale_id,sale_line)REFERENCES pharmacy_sale_lines(tenant_id,outlet_id,sale_id,line_number)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,indent_id,line_number)REFERENCES pharmacy_ward_indent_lines(tenant_id,outlet_id,indent_id,line_number)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,indent_id,hospital_id,department_id)REFERENCES pharmacy_ward_indents(tenant_id,outlet_id,id,hospital_id,department_id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_ward_indent_issue_line ON pharmacy_ward_indent_issues(tenant_id,outlet_id,indent_id,line_number);
ALTER TABLE pharmacy_ward_indent_issues ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_ward_indent_issues FORCE ROW LEVEL SECURITY;
CREATE POLICY ward_indent_issue_read ON pharmacy_ward_indent_issues FOR SELECT
 USING(hms_tenant_visible(tenant_id)AND(pharmacy_outlet_allowed(tenant_id,outlet_id,false)OR pharmacy_ward_indent_frame(tenant_id,outlet_id,hospital_id,department_id,'read')));
CREATE POLICY ward_indent_issue_post ON pharmacy_ward_indent_issues FOR INSERT
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)
 AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
-- Locks the indent, so concurrent issues serialise before the cumulative limit is read.
CREATE FUNCTION pharmacy_ward_indent_issue_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE indent pharmacy_ward_indents;sale pharmacy_sales;sold pharmacy_sale_lines;requested bigint;product uuid;issued bigint;BEGIN
 SELECT * INTO indent FROM pharmacy_ward_indents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.indent_id FOR UPDATE;
 IF NOT FOUND OR indent.status<>'open'THEN
  RAISE EXCEPTION 'only an open ward indent can be issued' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_open';
 END IF;
 SELECT * INTO sale FROM pharmacy_sales WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.sale_id;
 IF NOT FOUND OR sale.posted_xact<>pg_current_xact_id()OR sale.actor_subject<>NEW.actor_subject
  OR sale.admission_occupancy_id IS DISTINCT FROM indent.occupancy_id OR sale.admission_id IS DISTINCT FROM indent.admission_id OR sale.patient_id IS DISTINCT FROM indent.patient_id
  OR NOT EXISTS(SELECT 1 FROM pharmacy_sale_tenders t WHERE t.tenant_id=sale.tenant_id AND t.outlet_id=sale.outlet_id AND t.sale_id=sale.id AND t.kind='admission_account')
  OR EXISTS(SELECT 1 FROM pharmacy_ward_indent_issues i WHERE i.tenant_id=NEW.tenant_id AND i.outlet_id=NEW.outlet_id AND i.sale_id=NEW.sale_id AND i.indent_id<>NEW.indent_id)THEN
  RAISE EXCEPTION 'an issue is a sale posted now to the indent''s admission' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_issue_sale';
 END IF;
 SELECT * INTO sold FROM pharmacy_sale_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.sale_id AND line_number=NEW.sale_line;
 SELECT l.requested_units,l.product_id INTO requested,product FROM pharmacy_ward_indent_lines l
 WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.indent_id=NEW.indent_id AND l.line_number=NEW.line_number;
 IF sold.product_id IS DISTINCT FROM product OR sold.quantity_units IS DISTINCT FROM NEW.quantity_units THEN
  RAISE EXCEPTION 'an issue line is the indented product' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_issue_line';
 END IF;
 SELECT COALESCE(sum(i.quantity_units),0)INTO issued FROM pharmacy_ward_indent_issues i
 WHERE i.tenant_id=NEW.tenant_id AND i.outlet_id=NEW.outlet_id AND i.indent_id=NEW.indent_id AND i.line_number=NEW.line_number;
 IF issued+NEW.quantity_units>requested THEN
  RAISE EXCEPTION 'more than the indent requested' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_over_issue';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_ward_indent_issue_guard BEFORE INSERT ON pharmacy_ward_indent_issues FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_issue_guard();
CREATE TRIGGER pharmacy_ward_indent_issue_audit AFTER INSERT ON pharmacy_ward_indent_issues FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_ward_indent_issue_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_ward_indent_issues FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE FUNCTION pharmacy_ward_indent_issue_complete()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF(SELECT count(*)FROM pharmacy_sale_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.sale_id=NEW.sale_id)
  <>(SELECT count(*)FROM pharmacy_ward_indent_issues i WHERE i.tenant_id=NEW.tenant_id AND i.outlet_id=NEW.outlet_id AND i.sale_id=NEW.sale_id)
  OR EXISTS(SELECT 1 FROM pharmacy_sale_charge_lines c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.sale_id=NEW.sale_id)THEN
  RAISE EXCEPTION 'every line of an issue sale is an indent line' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_issue_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_ward_indent_issue_complete AFTER INSERT ON pharmacy_ward_indent_issues DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_issue_complete();

-- Closed means nothing outstanding or a short-supply reason; cancel only before any issue.
CREATE FUNCTION pharmacy_ward_indent_transition()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fixed text[]:=ARRAY['status','version','close_reason','closed_at','closed_by'];outstanding boolean;BEGIN
 IF OLD.status<>'open'OR NEW.status NOT IN('closed','cancelled')OR NEW.version<>OLD.version+1
  OR NEW.closed_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
  OR(to_jsonb(NEW)-fixed)<>(to_jsonb(OLD)-fixed)THEN
  RAISE EXCEPTION 'a ward indent closes or cancels once' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_transition';
 END IF;
 IF NEW.status='cancelled'AND EXISTS(SELECT 1 FROM pharmacy_ward_indent_issues i WHERE i.tenant_id=OLD.tenant_id AND i.outlet_id=OLD.outlet_id AND i.indent_id=OLD.id)THEN
  RAISE EXCEPTION 'an issued ward indent cannot be cancelled' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_issued';
 END IF;
 IF NEW.status='closed'THEN
  SELECT EXISTS(SELECT 1 FROM pharmacy_ward_indent_lines l WHERE l.tenant_id=OLD.tenant_id AND l.outlet_id=OLD.outlet_id AND l.indent_id=OLD.id
   AND l.requested_units>(SELECT COALESCE(sum(i.quantity_units),0)FROM pharmacy_ward_indent_issues i
   WHERE i.tenant_id=l.tenant_id AND i.outlet_id=l.outlet_id AND i.indent_id=l.indent_id AND i.line_number=l.line_number))INTO outstanding;
  IF outstanding=(NEW.close_reason IS NULL)THEN
   RAISE EXCEPTION 'a ward indent closes short only with a reason and only while units are outstanding' USING ERRCODE='23514',CONSTRAINT='pharmacy_ward_indent_outstanding';
  END IF;
 END IF;
 NEW.closed_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_ward_indent_transition BEFORE UPDATE ON pharmacy_ward_indents FOR EACH ROW EXECUTE FUNCTION pharmacy_ward_indent_transition();

-- Read-only purpose paths for the requester frame: the target outlet's catalogue and returns on its own issues.
CREATE POLICY ward_indent_products ON pharmacy_catalogue_products FOR SELECT
 USING(current_user<>'helivanta_drug_approver'AND hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND outlet_id=NULLIF(current_setting('app.pharmacy_indent_outlet',true),'')::uuid AND(SELECT pharmacy_ward_indent_catalogue_scope()));
CREATE POLICY ward_indent_returns ON pharmacy_credit_note_lines FOR SELECT
 USING(hms_tenant_visible(tenant_id)AND(SELECT COALESCE(current_setting('app.pharmacy_indent_subject',true),'')<>'')
 AND EXISTS(SELECT 1 FROM pharmacy_ward_indent_issues i WHERE i.tenant_id=pharmacy_credit_note_lines.tenant_id AND i.outlet_id=pharmacy_credit_note_lines.outlet_id
 AND i.sale_id=pharmacy_credit_note_lines.sale_id AND i.sale_line=pharmacy_credit_note_lines.sale_line));

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_ward_indents','pharmacy_ward_indent_lines','pharmacy_ward_indent_issues'] LOOP
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY hospital_stocktake_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY hospital_stocktake_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')$policy$,target);
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

-- Seals were checked before DDL; the hospital seal regains the 0070 recovery check 0074 dropped.
DO $choice_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_choice_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=%L::jsonb,false)',reviewed::text));
END $choice_integrity$;
DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
