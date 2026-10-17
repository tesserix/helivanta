SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_support_access (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 premise_id uuid NOT NULL,requester_subject text NOT NULL,beneficiary_subject text NOT NULL,
 ticket_reference text NOT NULL CHECK(length(btrim(ticket_reference)) BETWEEN 1 AND 100),reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 5 AND 500),
 valid_from timestamptz NOT NULL,expires_at timestamptz NOT NULL CHECK(expires_at>valid_from AND expires_at<=valid_from+interval '24 hours'),
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','revoked')),approver_subject text,
 decision_actor text NOT NULL DEFAULT '',decision_reason text NOT NULL DEFAULT '',
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,premise_id) REFERENCES supply_premises(tenant_id,party_id,id) ON DELETE RESTRICT,
 CHECK(requester_subject ~ '^[A-Za-z0-9._:@|+\-]{1,200}$' AND beneficiary_subject ~ '^[A-Za-z0-9._:@|+\-]{1,200}$'),
 CHECK(approver_subject IS NULL OR (approver_subject<>requester_subject AND approver_subject<>beneficiary_subject)),
 CHECK((status='pending' AND approver_subject IS NULL AND decision_actor='' AND decision_reason='') OR (status<>'pending' AND length(btrim(decision_reason)) BETWEEN 5 AND 500 AND (decision_actor<>beneficiary_subject OR status='revoked') AND length(decision_actor) BETWEEN 1 AND 200)),
 CHECK(status<>'approved' OR approver_subject IS NOT NULL)
);
CREATE INDEX supply_support_access_page ON supply_support_access(tenant_id,party_id,created_at DESC,id DESC);
CREATE INDEX supply_support_beneficiary ON supply_support_access(tenant_id,beneficiary_subject,party_id,expires_at);
ALTER TABLE supply_support_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_support_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_support_access USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND (beneficiary_subject=NULLIF(current_setting('app.supply_subject',true),'') OR supply_roles_allowed(tenant_id,party_id,ARRAY['administrator'])))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND (current_setting('app.supply_access_admin',true)='on' OR (status='revoked' AND beneficiary_subject=NULLIF(current_setting('app.supply_subject',true),'') AND id=NULLIF(current_setting('app.supply_support_revoke',true),'')::uuid)));
CREATE TRIGGER supply_support_access_audit AFTER INSERT OR UPDATE ON supply_support_access FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE supply_support_access_revisions (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,support_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,support_id,version),UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,party_id,support_id) REFERENCES supply_support_access(tenant_id,party_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_support_access ADD CONSTRAINT supply_support_current_revision FOREIGN KEY(tenant_id,party_id,id,version) REFERENCES supply_support_access_revisions(tenant_id,party_id,support_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_support_access_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_support_access_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_support_access_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator'])) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND party_id=NULLIF(current_setting('app.supply_party',true),'')::uuid AND (current_setting('app.supply_access_admin',true)='on' OR (support_id=NULLIF(current_setting('app.supply_support_revoke',true),'')::uuid AND EXISTS(SELECT 1 FROM supply_support_access s WHERE s.tenant_id=supply_support_access_revisions.tenant_id AND s.party_id=supply_support_access_revisions.party_id AND s.id=supply_support_access_revisions.support_id AND s.beneficiary_subject=NULLIF(current_setting('app.supply_subject',true),'') AND s.status='revoked'))));
CREATE INDEX supply_support_revisions_page ON supply_support_access_revisions(tenant_id,party_id,support_id,created_at DESC,id DESC);
CREATE TRIGGER supply_support_revisions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_support_access_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_support_access_retain BEFORE DELETE OR TRUNCATE ON supply_support_access FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_support_access_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending' OR NEW.version<>1 OR NEW.approver_subject IS NOT NULL OR NEW.requester_subject IS DISTINCT FROM NULLIF(current_setting('app.supply_subject',true),'') THEN RAISE EXCEPTION 'support request must begin pending for current actor' USING ERRCODE='23514',CONSTRAINT='supply_support_transition'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.party_id,NEW.id,NEW.premise_id,NEW.requester_subject,NEW.beneficiary_subject,NEW.ticket_reference,NEW.reason,NEW.valid_from,NEW.expires_at,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.id,OLD.premise_id,OLD.requester_subject,OLD.beneficiary_subject,OLD.ticket_reference,OLD.reason,OLD.valid_from,OLD.expires_at,OLD.created_at) OR NEW.version<>OLD.version+1 OR NOT((OLD.status='pending' AND NEW.status IN ('approved','rejected','revoked')) OR (OLD.status='approved' AND NEW.status='revoked')) THEN RAISE EXCEPTION 'support identity or transition immutable' USING ERRCODE='23514',CONSTRAINT='supply_support_transition'; END IF;
  IF NEW.decision_actor IS DISTINCT FROM NULLIF(current_setting('app.supply_subject',true),'') OR (NEW.decision_actor=OLD.beneficiary_subject AND NEW.status<>'revoked') OR (NEW.status IN ('approved','rejected') AND NEW.decision_actor=OLD.requester_subject) OR (NEW.status='approved' AND NEW.approver_subject IS DISTINCT FROM NEW.decision_actor) OR (NEW.status<>'approved' AND NEW.approver_subject IS DISTINCT FROM OLD.approver_subject) THEN RAISE EXCEPTION 'support decision requires independent authorised actor' USING ERRCODE='23514',CONSTRAINT='supply_support_transition'; END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER supply_support_access_guard BEFORE INSERT OR UPDATE ON supply_support_access FOR EACH ROW EXECUTE FUNCTION supply_support_access_guard();
CREATE FUNCTION supply_support_allowed(row_tenant uuid,row_party uuid,row_premise uuid) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND row_party=NULLIF(current_setting('app.supply_party',true),'')::uuid AND EXISTS(SELECT 1 FROM supply_support_access s WHERE s.tenant_id=row_tenant AND s.party_id=row_party AND s.premise_id=row_premise AND s.id=NULLIF(current_setting('app.supply_support_id',true),'')::uuid AND s.beneficiary_subject=NULLIF(current_setting('app.supply_subject',true),'') AND s.status='approved' AND s.valid_from<=clock_timestamp() AND s.expires_at>clock_timestamp() AND supply_effective_role(s.tenant_id,s.party_id,s.approver_subject,ARRAY['administrator'],NULL))
$$;
ALTER POLICY tenant_isolation ON supply_premises USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']) OR (kind IN ('warehouse','depot') AND supply_operating_owned(tenant_id,party_id,id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','driver','auditor','regulator'])) OR supply_support_allowed(tenant_id,party_id,id)));
ALTER POLICY tenant_isolation ON supply_bins USING(hms_tenant_visible(tenant_id) AND (supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance','auditor']) OR supply_operating_owned(tenant_id,party_id,premise_id,ARRAY['administrator','warehouse','inventory','picker','packer','dispatcher','auditor','regulator']) OR supply_support_allowed(tenant_id,party_id,premise_id)));
CREATE FUNCTION supply_support_directory_allowed(row_tenant uuid,row_party uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE previous_party text;allowed boolean;
BEGIN
 IF row_tenant IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false; END IF;
 previous_party:=current_setting('app.supply_party',true);
 PERFORM set_config('app.supply_party',row_party::text,true);
 BEGIN
  SELECT EXISTS(SELECT 1 FROM supply_support_access s WHERE s.tenant_id=row_tenant AND s.party_id=row_party AND s.beneficiary_subject=NULLIF(current_setting('app.supply_subject',true),'') AND s.status='approved' AND s.valid_from<=clock_timestamp() AND s.expires_at>clock_timestamp() AND supply_effective_role(s.tenant_id,s.party_id,s.approver_subject,ARRAY['administrator'],NULL)) INTO allowed;
 EXCEPTION WHEN OTHERS THEN PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);RAISE;END;
 PERFORM set_config('app.supply_party',COALESCE(previous_party,''),true);RETURN allowed;
END $$;
ALTER POLICY tenant_isolation ON supply_parties USING(
 hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (supply_party_allowed(tenant_id,id,false)
  OR (current_setting('app.supply_directory',true)='on'
   AND (current_setting('app.supply_manager',true)='on'
    OR supply_effective_role(tenant_id,id,NULLIF(current_setting('app.supply_subject',true),''),ARRAY['administrator','purchasing','finance','compliance','auditor','warehouse','sales','driver'],NULL)
    OR supply_operating_directory_allowed(tenant_id,id)
    OR supply_support_directory_allowed(tenant_id,id)
   )
  )
 )
);
