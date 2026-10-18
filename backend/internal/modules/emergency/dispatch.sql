SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION emergency_dispatch_clock() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT clock_timestamp() $$;

ALTER POLICY tenant_isolation ON emergency_clinical_reviewers
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND base_id=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND (current_setting('app.emergency_admin',true)='true' OR subject=NULLIF(current_setting('app.emergency_subject',true),'')
 OR emergency_allowed(tenant_id,base_id,'intake')));

CREATE TABLE emergency_dispatches (
 tenant_id uuid NOT NULL, base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 case_id uuid NOT NULL, case_version bigint NOT NULL CHECK(case_version>0),
 assessment_id uuid NOT NULL, vehicle_id uuid NOT NULL, duty_id uuid NOT NULL,
 driver_subject text NOT NULL, clinician_subject text NOT NULL,
 acknowledgement_deadline timestamptz,
 driver_acknowledged boolean NOT NULL DEFAULT false, clinician_acknowledged boolean NOT NULL DEFAULT false,
 acknowledgement_seconds int NOT NULL CHECK(acknowledgement_seconds BETWEEN 15 AND 300),
 requested_by text NOT NULL,
 state text NOT NULL DEFAULT 'workflow_pending' CHECK(state IN('workflow_pending','offered','acknowledged','en_route','on_scene','transporting','arrived','handover_complete','cleared','reconciliation_required','cancel_requested','cancelled')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 FOREIGN KEY(tenant_id,base_id,case_id) REFERENCES emergency_cases(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,assessment_id) REFERENCES emergency_transport_assessments(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,vehicle_id) REFERENCES emergency_vehicles(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,duty_id) REFERENCES emergency_duties(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,driver_subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,clinician_subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT,
 CHECK(driver_subject<>clinician_subject)
);
ALTER POLICY tenant_isolation ON emergency_cases
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND base_id=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND (emergency_allowed(tenant_id,base_id,'read') OR EXISTS(SELECT 1 FROM emergency_dispatches d
 WHERE d.tenant_id=emergency_cases.tenant_id AND d.base_id=emergency_cases.base_id AND d.case_id=emergency_cases.id
 AND current_setting('app.emergency_subject',true) IN(d.driver_subject,d.clinician_subject)
 AND d.acknowledgement_deadline IS NOT NULL AND d.state NOT IN('workflow_pending','cleared','cancelled'))));

CREATE UNIQUE INDEX emergency_dispatch_case_active ON emergency_dispatches(tenant_id,case_id) WHERE state NOT IN('cleared','cancelled');
CREATE UNIQUE INDEX emergency_dispatch_vehicle_active ON emergency_dispatches(tenant_id,vehicle_id) WHERE state NOT IN('cleared','cancelled');
CREATE INDEX ON emergency_dispatches(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_dispatches ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_dispatches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_dispatches
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND ((base_id=NULLIF(current_setting('app.emergency_base',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'read'))
 OR (current_setting('app.emergency_subject',true) IN(driver_subject,clinician_subject) AND acknowledgement_deadline IS NOT NULL AND state NOT IN('workflow_pending','cleared','cancelled'))))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (emergency_allowed(tenant_id,base_id,'intake') OR current_setting('app.emergency_subject',true) IN(driver_subject,clinician_subject)));
CREATE TRIGGER emergency_dispatch_no_removal BEFORE DELETE OR TRUNCATE ON emergency_dispatches FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE TABLE emergency_dispatch_members (
 tenant_id uuid NOT NULL, base_id uuid NOT NULL, dispatch_id uuid NOT NULL,
 subject text NOT NULL, released_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,dispatch_id,subject),
 FOREIGN KEY(tenant_id,base_id,dispatch_id) REFERENCES emergency_dispatches(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX emergency_dispatch_member_active ON emergency_dispatch_members(tenant_id,subject) WHERE released_at IS NULL;
ALTER TABLE emergency_dispatch_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_dispatch_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_dispatch_members
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake'));
CREATE TRIGGER emergency_dispatch_member_no_removal BEFORE DELETE OR TRUNCATE ON emergency_dispatch_members FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE FUNCTION emergency_dispatch_reservation_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE eligible boolean;
BEGIN
 PERFORM 1 FROM emergency_bases WHERE tenant_id=NEW.tenant_id AND id=NEW.base_id FOR SHARE;
 PERFORM 1 FROM emergency_cases WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.case_id FOR UPDATE;
 PERFORM 1 FROM emergency_vehicles WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.vehicle_id FOR UPDATE;
 PERFORM 1 FROM emergency_duties WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.duty_id FOR UPDATE;
 PERFORM 1 FROM emergency_crew WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND subject IN(NEW.driver_subject,NEW.clinician_subject) ORDER BY subject FOR SHARE;
 SELECT EXISTS(
  SELECT 1 FROM emergency_cases c
  JOIN emergency_transport_assessments a ON a.tenant_id=c.tenant_id AND a.base_id=c.base_id AND a.case_id=c.id
  JOIN emergency_clinical_reviewers r ON r.tenant_id=a.tenant_id AND r.base_id=a.base_id AND r.id=a.reviewer_grant_id
  JOIN emergency_fleet_status v ON v.tenant_id=c.tenant_id AND v.base_id=c.base_id AND v.id=NEW.vehicle_id
  JOIN emergency_duties d ON d.tenant_id=v.tenant_id AND d.base_id=v.base_id AND d.id=v.duty_id
  WHERE c.tenant_id=NEW.tenant_id AND c.base_id=NEW.base_id AND c.id=NEW.case_id
   AND c.status='received' AND c.version=NEW.case_version
   AND a.id=NEW.assessment_id AND a.case_version=c.version AND a.valid_until>clock_timestamp()
   AND NOT EXISTS(SELECT 1 FROM emergency_transport_assessments newer WHERE newer.tenant_id=a.tenant_id AND newer.base_id=a.base_id AND newer.case_id=a.case_id AND newer.sequence>a.sequence)
   AND r.enabled AND r.expires_at>clock_timestamp()
   AND NOT EXISTS(SELECT 1 FROM emergency_clinical_reviewers newer WHERE newer.tenant_id=r.tenant_id AND newer.base_id=r.base_id AND newer.subject=r.subject AND newer.version>r.version)
   AND v.dispatchable AND d.id=NEW.duty_id AND d.driver_subject=NEW.driver_subject AND d.clinician_subject=NEW.clinician_subject
   AND emergency_class_supports(v.effective_capability,a.required_class)
 ) INTO eligible;
 IF NOT COALESCE(eligible,false) OR NEW.state<>'workflow_pending' OR NEW.version<>1 OR NEW.created_at<>now()
 OR NEW.requested_by<>NULLIF(current_setting('app.emergency_subject',true),'') THEN
  RAISE EXCEPTION 'dispatch eligibility changed' USING ERRCODE='40001';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_reservation_valid BEFORE INSERT ON emergency_dispatches FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_reservation_valid();

CREATE FUNCTION emergency_dispatch_members_reserve() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO emergency_dispatch_members(tenant_id,base_id,dispatch_id,subject) VALUES
 (NEW.tenant_id,NEW.base_id,NEW.id,NEW.driver_subject),(NEW.tenant_id,NEW.base_id,NEW.id,NEW.clinician_subject);
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_members_reserve AFTER INSERT ON emergency_dispatches FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_members_reserve();

CREATE OR REPLACE VIEW emergency_fleet_status WITH(security_invoker=true) AS
 SELECT f.*, cardinality(f.blocker_list)=0 AS dispatchable, to_jsonb(f.blocker_list) AS blockers,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'code',c.code) ORDER BY c.code) FROM emergency_fleet_constraints c WHERE c.tenant_id=f.tenant_id AND c.base_id=f.base_id AND c.vehicle_id=f.id AND c.cleared_at IS NULL),'[]'::jsonb) AS open_constraints,
 CASE WHEN cardinality(f.blocker_list)=0 THEN f.capability ELSE NULL END AS effective_capability
 FROM (
 SELECT v.*, now() AS computed_at, d.id AS duty_id,
 CASE WHEN v.vehicle_class='als' AND medic.qualification='emt' THEN 'bls' ELSE v.vehicle_class END AS capability,
 ARRAY(SELECT DISTINCT reason FROM unnest(array_remove(ARRAY[
 CASE WHEN EXISTS(SELECT 1 FROM emergency_dispatches active WHERE active.tenant_id=v.tenant_id AND active.base_id=v.base_id AND active.vehicle_id=v.id AND active.state NOT IN('cleared','cancelled')) THEN 'active_dispatch' END,
 CASE WHEN v.retired THEN 'retired' END,
 CASE WHEN v.certified_until<=now() THEN 'vehicle_certification_expired' END,
 CASE WHEN d.id IS NULL OR d.ends_at<=now() THEN 'no_current_duty' END,
 CASE WHEN driver.id IS NULL OR driver.suspended OR driver.certified_until<=now() OR driver.qualification<>'driver' THEN 'driver_not_qualified' END,
 CASE WHEN medic.id IS NULL OR medic.suspended OR medic.certified_until<=now() OR medic.qualification NOT IN('emt','paramedic') THEN 'clinician_not_qualified' END,
 CASE WHEN r.id IS NULL OR r.valid_until<=now() OR r.vehicle_version<>v.version OR r.duty_id IS DISTINCT FROM d.id THEN 'readiness_missing_or_expired' END,
 CASE WHEN r.oxygen_bar<v.oxygen_min_bar THEN 'oxygen_low' END,
 CASE WHEN NOT r.defibrillator_passed AND v.vehicle_class='als' THEN 'defibrillator_failed' END,
 CASE WHEN NOT r.equipment_complete THEN 'equipment_incomplete' END,
 CASE WHEN NOT r.roadworthy THEN 'not_roadworthy' END
 ]::text[],NULL) || ARRAY(SELECT c.code FROM emergency_fleet_constraints c WHERE c.tenant_id=v.tenant_id AND c.base_id=v.base_id AND c.vehicle_id=v.id AND c.cleared_at IS NULL ORDER BY c.code)) reason ORDER BY reason) AS blocker_list
 FROM emergency_vehicles v
 LEFT JOIN emergency_duties d ON d.tenant_id=v.tenant_id AND d.base_id=v.base_id AND d.vehicle_id=v.id AND d.ended_at IS NULL
 LEFT JOIN emergency_crew driver ON driver.tenant_id=d.tenant_id AND driver.base_id=d.base_id AND driver.subject=d.driver_subject
 LEFT JOIN emergency_crew medic ON medic.tenant_id=d.tenant_id AND medic.base_id=d.base_id AND medic.subject=d.clinician_subject
 LEFT JOIN LATERAL(SELECT r.id,r.valid_until,r.vehicle_version,r.duty_id,r.oxygen_bar,r.defibrillator_passed,r.equipment_complete,r.roadworthy FROM emergency_readiness r
 WHERE r.tenant_id=v.tenant_id AND r.base_id=v.base_id AND r.vehicle_id=v.id ORDER BY r.created_at DESC,r.id DESC LIMIT 1) r ON true
 ) f;


CREATE FUNCTION emergency_case_dispatch_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.status='closed' AND OLD.status<>'closed' AND EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=OLD.tenant_id AND d.base_id=OLD.base_id AND d.case_id=OLD.id AND d.state NOT IN('cleared','cancelled')) THEN
  RAISE EXCEPTION 'active dispatch requires reconciliation' USING ERRCODE='40001';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_case_active_dispatch BEFORE UPDATE ON emergency_cases FOR EACH ROW EXECUTE FUNCTION emergency_case_dispatch_guard();
CREATE FUNCTION emergency_duty_dispatch_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.ended_at IS NOT NULL AND OLD.ended_at IS NULL AND EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=OLD.tenant_id AND d.base_id=OLD.base_id AND d.duty_id=OLD.id AND d.state NOT IN('cleared','cancelled')) THEN
  RAISE EXCEPTION 'active dispatch requires duty reconciliation' USING ERRCODE='40001';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_duty_active_dispatch BEFORE UPDATE ON emergency_duties FOR EACH ROW EXECUTE FUNCTION emergency_duty_dispatch_guard();

CREATE TABLE emergency_dispatch_commands (
 tenant_id uuid NOT NULL, base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7), dispatch_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version>0),
 actor_subject text NOT NULL,
 operation text NOT NULL CHECK(operation IN('request_cancel','reconcile_cancelled','acknowledge','en_route','on_scene','offer','acknowledgement_timeout')),
 evidence_reference text NOT NULL CHECK(evidence_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 result_state text NOT NULL, result_version bigint NOT NULL,
 result_acknowledgement_deadline timestamptz,
 result_driver_acknowledged boolean NOT NULL DEFAULT false, result_clinician_acknowledged boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,dispatch_id,result_version),
 FOREIGN KEY(tenant_id,base_id,dispatch_id) REFERENCES emergency_dispatches(tenant_id,base_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_dispatch_commands(tenant_id,base_id,dispatch_id,created_at DESC,id DESC);
ALTER TABLE emergency_dispatch_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_dispatch_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_dispatch_commands
 USING(hms_tenant_visible(tenant_id) AND (emergency_allowed(tenant_id,base_id,'read') OR actor_subject=NULLIF(current_setting('app.emergency_subject',true),'')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=NULLIF(current_setting('app.emergency_subject',true),'')
 AND (emergency_allowed(tenant_id,base_id,'intake') OR EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_dispatch_commands.tenant_id AND d.base_id=emergency_dispatch_commands.base_id AND d.id=emergency_dispatch_commands.dispatch_id AND current_setting('app.emergency_subject',true) IN(d.driver_subject,d.clinician_subject))));
CREATE TRIGGER emergency_dispatch_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_dispatch_commands FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE FUNCTION emergency_dispatch_command_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved emergency_dispatches%ROWTYPE;
BEGIN
 PERFORM 1 FROM emergency_bases WHERE tenant_id=NEW.tenant_id AND id=NEW.base_id FOR NO KEY UPDATE;
 SELECT * INTO saved FROM emergency_dispatches WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.dispatch_id FOR UPDATE;
 IF NOT FOUND OR saved.version<>NEW.expected_version THEN
  RAISE EXCEPTION 'dispatch version changed' USING ERRCODE='40001';
 END IF;
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.emergency_subject',true),'') THEN
  RAISE EXCEPTION 'dispatch actor mismatch' USING ERRCODE='42501';
 END IF;
 NEW.result_driver_acknowledged=saved.driver_acknowledged; NEW.result_clinician_acknowledged=saved.clinician_acknowledged;
 IF NEW.operation='acknowledge' AND saved.state='offered' AND saved.acknowledgement_deadline>emergency_dispatch_clock()
 AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NEW.actor_subject=saved.driver_subject AND NOT saved.driver_acknowledged THEN NEW.result_driver_acknowledged=true;
  ELSIF NEW.actor_subject=saved.clinician_subject AND NOT saved.clinician_acknowledged THEN NEW.result_clinician_acknowledged=true;
  ELSE RAISE EXCEPTION 'crew acknowledgement already recorded' USING ERRCODE='40001'; END IF;
  IF NOT emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN RAISE EXCEPTION 'dispatch eligibility changed' USING ERRCODE='40001'; END IF;
  INSERT INTO emergency_crew_commitments(tenant_id,base_id,dispatch_id,subject) VALUES(NEW.tenant_id,NEW.base_id,NEW.dispatch_id,NEW.actor_subject);
  IF NEW.result_driver_acknowledged AND NEW.result_clinician_acknowledged THEN NEW.result_state='acknowledged'; ELSE NEW.result_state='offered'; END IF;
 ELSIF NEW.operation='en_route' AND saved.state='acknowledged' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NOT emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN RAISE EXCEPTION 'dispatch eligibility changed' USING ERRCODE='40001'; END IF;
  NEW.result_state='en_route';
 ELSIF NEW.operation='on_scene' AND saved.state='en_route' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN NEW.result_state='on_scene';
 ELSIF NEW.operation='offer' AND emergency_worker_allowed(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) AND saved.state='workflow_pending' THEN
  IF emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN
   NEW.result_state='offered'; NEW.result_acknowledgement_deadline=emergency_dispatch_clock()+saved.acknowledgement_seconds*interval '1 second';
  ELSE NEW.result_state='reconciliation_required'; END IF;
 ELSIF NEW.operation='acknowledgement_timeout' AND emergency_worker_allowed(NEW.tenant_id,NEW.base_id,NEW.dispatch_id)
 AND saved.state='offered' AND saved.acknowledgement_deadline<=emergency_dispatch_clock() THEN
  NEW.result_state='reconciliation_required';
 ELSIF NEW.operation='request_cancel' AND emergency_allowed(NEW.tenant_id,NEW.base_id,'intake')
 AND saved.state NOT IN('cleared','cancelled','cancel_requested') THEN
  NEW.result_state='cancel_requested';
 ELSIF NEW.operation='reconcile_cancelled' AND emergency_allowed(NEW.tenant_id,NEW.base_id,'close')
 AND saved.state IN('cancel_requested','reconciliation_required') THEN
  NEW.result_state='cancelled';
 ELSIF NEW.operation IN('acknowledge','en_route','on_scene') AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  RAISE EXCEPTION 'crew command state changed' USING ERRCODE='40001';
 ELSE
  RAISE EXCEPTION 'dispatch command not permitted' USING ERRCODE='42501';
 END IF;
 NEW.result_acknowledgement_deadline=COALESCE(NEW.result_acknowledgement_deadline,saved.acknowledgement_deadline);

 NEW.result_version=saved.version+1;
 NEW.created_at=now(); NEW.updated_at=now();
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_command_valid BEFORE INSERT ON emergency_dispatch_commands FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_command_valid();

CREATE FUNCTION emergency_dispatch_update_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command emergency_dispatch_commands%ROWTYPE;
BEGIN
 SELECT * INTO command FROM emergency_dispatch_commands WHERE tenant_id=OLD.tenant_id AND base_id=OLD.base_id AND dispatch_id=OLD.id
 AND id=NULLIF(current_setting('app.emergency_command',true),'')::uuid;
 IF NOT FOUND OR command.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.emergency_subject',true),'')
 OR command.expected_version<>OLD.version OR command.result_version<>NEW.version OR command.result_state<>NEW.state
 OR (NEW.tenant_id,NEW.base_id,NEW.id,NEW.case_id,NEW.case_version,NEW.assessment_id,NEW.vehicle_id,NEW.duty_id,NEW.driver_subject,NEW.clinician_subject,NEW.acknowledgement_seconds,NEW.requested_by,NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.id,OLD.case_id,OLD.case_version,OLD.assessment_id,OLD.vehicle_id,OLD.duty_id,OLD.driver_subject,OLD.clinician_subject,OLD.acknowledgement_seconds,OLD.requested_by,OLD.created_at)
 OR NEW.acknowledgement_deadline IS DISTINCT FROM command.result_acknowledgement_deadline
 OR NEW.driver_acknowledged IS DISTINCT FROM command.result_driver_acknowledged OR NEW.clinician_acknowledged IS DISTINCT FROM command.result_clinician_acknowledged
 OR NEW.updated_at<>now() THEN
  RAISE EXCEPTION 'dispatch update requires committed command' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_update_guard BEFORE UPDATE ON emergency_dispatches FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_update_valid();

CREATE FUNCTION emergency_dispatch_member_release_valid() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.base_id,NEW.dispatch_id,NEW.subject,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.dispatch_id,OLD.subject,OLD.created_at)
 OR OLD.released_at IS NOT NULL OR NEW.released_at IS DISTINCT FROM now() OR NEW.updated_at<>now()
 OR NOT EXISTS(SELECT 1 FROM emergency_dispatches d JOIN emergency_dispatch_commands c ON c.tenant_id=d.tenant_id AND c.base_id=d.base_id AND c.dispatch_id=d.id
 WHERE d.tenant_id=NEW.tenant_id AND d.base_id=NEW.base_id AND d.id=NEW.dispatch_id AND d.state IN('cleared','cancelled')
 AND c.id=NULLIF(current_setting('app.emergency_command',true),'')::uuid AND c.result_version=d.version) THEN
  RAISE EXCEPTION 'dispatch capacity release requires terminal receipt' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_member_update_guard BEFORE UPDATE ON emergency_dispatch_members FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_member_release_valid();

CREATE FUNCTION emergency_dispatch_command_apply() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous text;
BEGIN
 previous=current_setting('app.emergency_command',true);
 PERFORM set_config('app.emergency_command',NEW.id::text,true);
 UPDATE emergency_dispatches SET state=NEW.result_state,version=NEW.result_version,updated_at=now(),acknowledgement_deadline=NEW.result_acknowledgement_deadline,driver_acknowledged=NEW.result_driver_acknowledged,clinician_acknowledged=NEW.result_clinician_acknowledged
 WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.dispatch_id AND version=NEW.expected_version;
 IF NOT FOUND THEN RAISE EXCEPTION 'dispatch version changed' USING ERRCODE='40001'; END IF;
 IF NEW.result_state IN('cleared','cancelled') THEN
  UPDATE emergency_crew_commitments SET released_at=now(),updated_at=now() WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND dispatch_id=NEW.dispatch_id AND released_at IS NULL;
  UPDATE emergency_dispatch_members SET released_at=now(),updated_at=now()
  WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND dispatch_id=NEW.dispatch_id AND released_at IS NULL;
 END IF;
 PERFORM set_config('app.emergency_command',COALESCE(previous,''),true);
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_dispatch_command_apply AFTER INSERT ON emergency_dispatch_commands FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_command_apply();


CREATE TABLE emergency_crew_commitments (
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,dispatch_id uuid NOT NULL,subject text NOT NULL,
 released_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,dispatch_id,subject),
 FOREIGN KEY(tenant_id,base_id,dispatch_id,subject) REFERENCES emergency_dispatch_members(tenant_id,base_id,dispatch_id,subject) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX emergency_crew_global_active ON emergency_crew_commitments(subject) WHERE released_at IS NULL;
ALTER TABLE emergency_crew_commitments ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_crew_commitments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_crew_commitments
 USING(hms_tenant_visible(tenant_id) AND (emergency_allowed(tenant_id,base_id,'read') OR subject=current_setting('app.emergency_subject',true)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (
 (released_at IS NULL AND subject=current_setting('app.emergency_subject',true) AND EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_crew_commitments.tenant_id AND d.base_id=emergency_crew_commitments.base_id AND d.id=emergency_crew_commitments.dispatch_id AND subject IN(d.driver_subject,d.clinician_subject)))
 OR (released_at IS NOT NULL AND EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_crew_commitments.tenant_id AND d.base_id=emergency_crew_commitments.base_id AND d.id=emergency_crew_commitments.dispatch_id AND d.state IN('cleared','cancelled')))));
CREATE TRIGGER emergency_crew_commitment_no_removal BEFORE DELETE OR TRUNCATE ON emergency_crew_commitments FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE TRIGGER emergency_crew_commitment_release BEFORE UPDATE ON emergency_crew_commitments FOR EACH ROW EXECUTE FUNCTION emergency_dispatch_member_release_valid();
