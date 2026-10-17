CREATE TABLE supply_delivery_tasks (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),source_kind text NOT NULL CHECK(source_kind='manual_logistics'),source_reference text NOT NULL CHECK(length(btrim(source_reference)) BETWEEN 1 AND 100),
 destination_business_name text NOT NULL CHECK(length(btrim(destination_business_name)) BETWEEN 1 AND 200),destination_line1 text NOT NULL CHECK(length(btrim(destination_line1)) BETWEEN 1 AND 200),destination_line2 text NOT NULL CHECK(length(destination_line2)<=200),destination_city text NOT NULL CHECK(length(btrim(destination_city)) BETWEEN 1 AND 100),destination_region text NOT NULL CHECK(length(btrim(destination_region)) BETWEEN 1 AND 100),destination_postal_code text NOT NULL CHECK(length(btrim(destination_postal_code)) BETWEEN 1 AND 20),country_code text NOT NULL CHECK(country_code IN ('IN','PK')),
 business_contact_name text NOT NULL CHECK(length(business_contact_name)<=200),business_contact_phone text NOT NULL CHECK(business_contact_phone ~ '^(|[+][1-9][0-9]{7,14})$'),window_start timestamptz NOT NULL,window_end timestamptz NOT NULL CHECK(window_end>window_start),package_count integer NOT NULL CHECK(package_count BETWEEN 1 AND 100000),handling_notes text NOT NULL CHECK(length(handling_notes)<=500),
 driver_subject text NOT NULL CHECK(driver_subject='' OR driver_subject ~ '^[A-Za-z0-9._:@|+\-]{1,200}$'),assignment_expires_at timestamptz,assignment_generation bigint NOT NULL CHECK(assignment_generation BETWEEN 0 AND 9007199254740991),status text NOT NULL CHECK(status IN ('draft','assigned','in_transit','delivered','exception','cancelled')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,id),UNIQUE(tenant_id,party_id,premise_id,code),UNIQUE(tenant_id,party_id,premise_id,source_kind,source_reference),
 FOREIGN KEY(tenant_id,party_id,premise_id,country_code) REFERENCES supply_premises(tenant_id,party_id,id,country_code) ON DELETE RESTRICT,
 CHECK((status IN ('draft','cancelled') AND driver_subject='' AND assignment_expires_at IS NULL) OR (status IN ('assigned','in_transit','delivered','exception') AND driver_subject<>'' AND assignment_expires_at IS NOT NULL AND assignment_generation>0))
);
CREATE INDEX supply_delivery_tasks_page ON supply_delivery_tasks(tenant_id,party_id,premise_id,created_at DESC,id DESC);
CREATE INDEX supply_delivery_tasks_driver ON supply_delivery_tasks(tenant_id,party_id,premise_id,driver_subject,created_at DESC,id DESC);
CREATE FUNCTION supply_delivery_assigned(row_tenant uuid,row_party uuid,row_premise uuid,driver text,expires timestamptz) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT driver=NULLIF(current_setting('app.supply_subject',true),'') AND expires>clock_timestamp() AND supply_operating_owned(row_tenant,row_party,row_premise,ARRAY['driver'])
$$;
CREATE FUNCTION supply_delivery_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE controlling boolean;changed_assignment boolean;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-access-admin:'||NEW.party_id::text,0));
 controlling:=supply_operating_owned(NEW.tenant_id,NEW.party_id,NEW.premise_id,ARRAY['administrator','dispatcher']);
 IF TG_OP='INSERT' THEN
  IF NOT controlling OR NEW.version<>1 OR NEW.status NOT IN ('draft','assigned') OR NEW.assignment_generation<>(CASE WHEN NEW.driver_subject='' THEN 0 ELSE 1 END) THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_delivery_identity'; END IF;
  changed_assignment:=NEW.driver_subject<>'';
 ELSE
  IF (NEW.tenant_id,NEW.party_id,NEW.premise_id,NEW.id,NEW.code,NEW.source_kind,NEW.source_reference,NEW.country_code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.premise_id,OLD.id,OLD.code,OLD.source_kind,OLD.source_reference,OLD.country_code,OLD.created_at) OR NEW.version<>OLD.version+1 OR OLD.status IN ('delivered','cancelled') THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_delivery_identity'; END IF;
  changed_assignment:=(NEW.driver_subject,NEW.assignment_expires_at) IS DISTINCT FROM (OLD.driver_subject,OLD.assignment_expires_at);
  IF NEW.assignment_generation<>OLD.assignment_generation+(CASE WHEN changed_assignment THEN 1 ELSE 0 END) THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_delivery_assignment'; END IF;
  IF NEW.status IN ('in_transit','delivered','exception') THEN
   IF NOT supply_delivery_assigned(OLD.tenant_id,OLD.party_id,OLD.premise_id,OLD.driver_subject,OLD.assignment_expires_at)
    OR (to_jsonb(NEW)-ARRAY['status','version','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','version','updated_at'])
    OR NOT ((OLD.status='assigned' AND NEW.status IN ('in_transit','exception')) OR (OLD.status='in_transit' AND NEW.status IN ('delivered','exception')))
   THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_delivery_authority'; END IF;
  ELSE
   IF NOT controlling THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_delivery_authority'; END IF;
   IF NEW.status NOT IN ('draft','assigned','cancelled') OR (OLD.status IN ('in_transit','exception') AND NEW.status='draft') THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_delivery_transition'; END IF;
   IF OLD.status IN ('in_transit','exception') AND NEW.status='assigned' AND NOT changed_assignment THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_delivery_assignment'; END IF;
  END IF;
 END IF;
 PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.premise_id AND country_code=NEW.country_code AND kind IN ('warehouse','depot') AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_delivery_origin'; END IF;
 IF changed_assignment AND NEW.driver_subject<>'' AND (NEW.assignment_expires_at<=clock_timestamp() OR NOT supply_effective_role(NEW.tenant_id,NEW.party_id,NEW.driver_subject,ARRAY['driver'],NEW.premise_id)) THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_delivery_driver'; END IF;
 NEW.updated_at:=clock_timestamp();RETURN NEW;
END $$;
ALTER TABLE supply_delivery_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_delivery_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_delivery_tasks USING(hms_tenant_visible(tenant_id) AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','dispatcher','auditor','regulator']) OR supply_delivery_assigned(tenant_id,party_id,premise_id,driver_subject,assignment_expires_at))) WITH CHECK(supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','dispatcher']) OR supply_delivery_assigned(tenant_id,party_id,premise_id,driver_subject,assignment_expires_at));
CREATE TRIGGER supply_delivery_guard BEFORE INSERT OR UPDATE ON supply_delivery_tasks FOR EACH ROW EXECUTE FUNCTION supply_delivery_guard();
CREATE TRIGGER supply_delivery_audit AFTER INSERT OR UPDATE ON supply_delivery_tasks FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_delivery_retain BEFORE DELETE OR TRUNCATE ON supply_delivery_tasks FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_delivery_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,delivery_id uuid NOT NULL,version bigint NOT NULL,snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 5 AND 500),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,delivery_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id,premise_id,delivery_id) REFERENCES supply_delivery_tasks(tenant_id,party_id,premise_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_delivery_tasks ADD CONSTRAINT supply_delivery_revision_head FOREIGN KEY(tenant_id,party_id,premise_id,id,version) REFERENCES supply_delivery_revisions(tenant_id,party_id,premise_id,delivery_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_delivery_revisions ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_delivery_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_delivery_revisions USING(hms_tenant_visible(tenant_id) AND supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','dispatcher','auditor','regulator'])) WITH CHECK(supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','dispatcher']) OR EXISTS(SELECT 1 FROM supply_delivery_tasks t WHERE t.tenant_id=supply_delivery_revisions.tenant_id AND t.party_id=supply_delivery_revisions.party_id AND t.premise_id=supply_delivery_revisions.premise_id AND t.id=supply_delivery_revisions.delivery_id AND supply_delivery_assigned(t.tenant_id,t.party_id,t.premise_id,t.driver_subject,t.assignment_expires_at)));
CREATE INDEX supply_delivery_revision_page ON supply_delivery_revisions(tenant_id,party_id,premise_id,delivery_id,created_at DESC,id DESC);
CREATE TRIGGER supply_delivery_revision_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_delivery_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_delivery_commands (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,delivery_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),request_hash text NOT NULL CHECK(request_hash ~ '^[a-f0-9]{64}$'),response jsonb NOT NULL CHECK(jsonb_typeof(response)='object'),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,delivery_id,id),FOREIGN KEY(tenant_id,party_id,premise_id,delivery_id) REFERENCES supply_delivery_tasks(tenant_id,party_id,premise_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_delivery_commands ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_delivery_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_delivery_commands USING(hms_tenant_visible(tenant_id) AND (supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','dispatcher','auditor','regulator']) OR EXISTS(SELECT 1 FROM supply_delivery_tasks t WHERE t.tenant_id=supply_delivery_commands.tenant_id AND t.party_id=supply_delivery_commands.party_id AND t.premise_id=supply_delivery_commands.premise_id AND t.id=supply_delivery_commands.delivery_id AND supply_delivery_assigned(t.tenant_id,t.party_id,t.premise_id,t.driver_subject,t.assignment_expires_at)))) WITH CHECK(EXISTS(SELECT 1 FROM supply_delivery_tasks t WHERE t.tenant_id=supply_delivery_commands.tenant_id AND t.party_id=supply_delivery_commands.party_id AND t.premise_id=supply_delivery_commands.premise_id AND t.id=supply_delivery_commands.delivery_id AND supply_delivery_assigned(t.tenant_id,t.party_id,t.premise_id,t.driver_subject,t.assignment_expires_at)));
CREATE TRIGGER supply_delivery_command_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_delivery_commands FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_delivery_drivers(row_tenant uuid,row_party uuid,row_premise uuid) RETURNS TABLE(id uuid,subject text,created_at timestamptz) LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_lookup text;
BEGIN
 IF NOT supply_operating_owned(row_tenant,row_party,row_premise,ARRAY['administrator','dispatcher']) THEN RETURN; END IF;
 previous_lookup:=current_setting('app.supply_authority_lookup',true);PERFORM set_config('app.supply_authority_lookup','on',true);
 BEGIN
  RETURN QUERY SELECT DISTINCT ON (g.subject) g.id,g.subject,g.created_at FROM (
   SELECT a.id,a.subject,a.created_at FROM supply_party_access a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND a.role='driver'
   UNION ALL SELECT a.id,a.subject,a.created_at FROM supply_access_grants a WHERE a.tenant_id=row_tenant AND a.party_id=row_party AND (a.premise_id IS NULL OR a.premise_id=row_premise) AND a.role='driver' AND a.status='active' AND a.valid_from<=clock_timestamp() AND a.expires_at>clock_timestamp()
  ) g ORDER BY g.subject,g.created_at,g.id;
 EXCEPTION WHEN OTHERS THEN PERFORM set_config('app.supply_authority_lookup',COALESCE(previous_lookup,''),true);RAISE;
 END;
 PERFORM set_config('app.supply_authority_lookup',COALESCE(previous_lookup,''),true);
END $$;
