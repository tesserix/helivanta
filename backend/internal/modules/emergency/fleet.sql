SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE emergency_vehicles (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9-]{1,40}$'),
 vehicle_class text NOT NULL CHECK(vehicle_class IN('patient_transport','bls','als')),
 certified_until timestamptz NOT NULL,
 policy_reference text NOT NULL CHECK(policy_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 oxygen_min_bar int NOT NULL CHECK(oxygen_min_bar BETWEEN 1 AND 300),
 readiness_minutes int NOT NULL CHECK(readiness_minutes BETWEEN 1 AND 1440),
 retired boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,code),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_vehicles(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_vehicles ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_vehicles FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_vehicles
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'close'));
CREATE FUNCTION emergency_fleet_version() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.base_id,NEW.id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.id,OLD.created_at)
 OR NEW.version<>OLD.version+1 THEN
 RAISE EXCEPTION 'fleet identity or version change invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_vehicle_version BEFORE UPDATE ON emergency_vehicles FOR EACH ROW EXECUTE FUNCTION emergency_fleet_version();
CREATE TRIGGER emergency_vehicle_no_removal BEFORE DELETE OR TRUNCATE ON emergency_vehicles FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE TABLE emergency_crew (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 qualification text NOT NULL CHECK(qualification IN('driver','emt','paramedic')),
 certified_until timestamptz NOT NULL,
 evidence_reference text NOT NULL CHECK(evidence_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 suspended boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,subject),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_crew(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_crew ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_crew FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_crew
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'close'));
CREATE TRIGGER emergency_crew_version BEFORE UPDATE ON emergency_crew FOR EACH ROW EXECUTE FUNCTION emergency_fleet_version();
CREATE TRIGGER emergency_crew_no_removal BEFORE DELETE OR TRUNCATE ON emergency_crew FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_crew_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.subject<>OLD.subject THEN RAISE EXCEPTION 'crew identity is immutable' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_crew_identity BEFORE UPDATE ON emergency_crew FOR EACH ROW EXECUTE FUNCTION emergency_crew_identity();

CREATE TABLE emergency_duties (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 vehicle_id uuid NOT NULL,
 driver_subject text NOT NULL,
 clinician_subject text NOT NULL,
 ends_at timestamptz NOT NULL,
 ended_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,id,vehicle_id),
 CHECK(driver_subject<>clinician_subject),
 CHECK(ends_at>created_at AND ends_at<=created_at+interval '24 hours'),
 CHECK(ended_at IS NULL OR ended_at>=created_at),
 FOREIGN KEY(tenant_id,base_id,vehicle_id) REFERENCES emergency_vehicles(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,driver_subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,clinician_subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX emergency_duty_vehicle_open ON emergency_duties(tenant_id,base_id,vehicle_id) WHERE ended_at IS NULL;
CREATE INDEX ON emergency_duties(tenant_id,base_id,created_at DESC,id DESC);
CREATE INDEX ON emergency_duties(tenant_id,base_id,driver_subject);
CREATE INDEX ON emergency_duties(tenant_id,base_id,clinician_subject);
ALTER TABLE emergency_duties ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_duties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_duties
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'close'));
CREATE TRIGGER emergency_duty_no_removal BEFORE DELETE OR TRUNCATE ON emergency_duties FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_duty_end() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.tenant_id,NEW.base_id,NEW.id,NEW.vehicle_id,NEW.driver_subject,NEW.clinician_subject,NEW.ends_at,NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.id,OLD.vehicle_id,OLD.driver_subject,OLD.clinician_subject,OLD.ends_at,OLD.created_at)
 OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR NEW.ended_at>now() THEN
 RAISE EXCEPTION 'duty can only be ended once' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_duty_end BEFORE UPDATE ON emergency_duties FOR EACH ROW EXECUTE FUNCTION emergency_duty_end();

CREATE TABLE emergency_duty_members (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 duty_id uuid NOT NULL,
 subject text NOT NULL,
 ended_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,duty_id,subject),
 FOREIGN KEY(tenant_id,base_id,duty_id) REFERENCES emergency_duties(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,subject) REFERENCES emergency_crew(tenant_id,base_id,subject) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX emergency_duty_member_open ON emergency_duty_members(tenant_id,subject) WHERE ended_at IS NULL;
CREATE INDEX ON emergency_duty_members(tenant_id,base_id,subject);
ALTER TABLE emergency_duty_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_duty_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_duty_members
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'close'));
CREATE TRIGGER emergency_duty_member_no_removal BEFORE DELETE OR TRUNCATE ON emergency_duty_members FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_duty_member_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.base_id,NEW.duty_id,NEW.subject,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.duty_id,OLD.subject,OLD.created_at) OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL) THEN
 RAISE EXCEPTION 'duty member can only be ended once' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM emergency_duties d WHERE d.tenant_id=NEW.tenant_id AND d.base_id=NEW.base_id AND d.id=NEW.duty_id AND NEW.subject IN(d.driver_subject,d.clinician_subject) AND d.ended_at IS NOT DISTINCT FROM NEW.ended_at) THEN
 RAISE EXCEPTION 'duty membership mismatch' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_duty_member_guard BEFORE INSERT OR UPDATE ON emergency_duty_members FOR EACH ROW EXECUTE FUNCTION emergency_duty_member_guard();
CREATE FUNCTION emergency_duty_members_required() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (SELECT count(*) FROM emergency_duty_members m WHERE m.tenant_id=NEW.tenant_id AND m.base_id=NEW.base_id AND m.duty_id=NEW.id AND m.ended_at IS NOT DISTINCT FROM NEW.ended_at)<>2 THEN
 RAISE EXCEPTION 'duty requires both members' USING ERRCODE='23514'; END IF; RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER emergency_duty_members_required AFTER INSERT OR UPDATE ON emergency_duties DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION emergency_duty_members_required();

CREATE TABLE emergency_readiness (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 vehicle_id uuid NOT NULL,
 vehicle_version bigint NOT NULL CHECK(vehicle_version>0),
 duty_id uuid NOT NULL,
 oxygen_bar int NOT NULL CHECK(oxygen_bar BETWEEN 0 AND 300),
 defibrillator_passed boolean NOT NULL,
 equipment_complete boolean NOT NULL,
 roadworthy boolean NOT NULL,
 actor_subject text NOT NULL,
 valid_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 FOREIGN KEY(tenant_id,base_id,duty_id,vehicle_id) REFERENCES emergency_duties(tenant_id,base_id,id,vehicle_id) ON DELETE RESTRICT,
 CHECK(valid_until>created_at)
);
CREATE INDEX ON emergency_readiness(tenant_id,base_id,vehicle_id,created_at DESC,id DESC);
CREATE INDEX ON emergency_readiness(tenant_id,base_id,duty_id,vehicle_id);
ALTER TABLE emergency_readiness ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_readiness FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_readiness
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake') AND actor_subject=NULLIF(current_setting('app.emergency_subject',true),''));
CREATE TRIGGER emergency_readiness_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_readiness FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE FUNCTION emergency_readiness_valid() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM emergency_vehicles v
 JOIN emergency_duties d ON d.tenant_id=v.tenant_id AND d.base_id=v.base_id AND d.vehicle_id=v.id
 JOIN emergency_crew driver ON driver.tenant_id=d.tenant_id AND driver.base_id=d.base_id AND driver.subject=d.driver_subject
 JOIN emergency_crew medic ON medic.tenant_id=d.tenant_id AND medic.base_id=d.base_id AND medic.subject=d.clinician_subject
 WHERE v.tenant_id=NEW.tenant_id AND v.base_id=NEW.base_id AND v.id=NEW.vehicle_id AND v.version=NEW.vehicle_version
 AND NOT v.retired AND d.id=NEW.duty_id AND d.ended_at IS NULL AND driver.qualification='driver' AND medic.qualification IN('emt','paramedic')
 AND NOT driver.suspended AND NOT medic.suspended AND NEW.created_at=now()
 AND NEW.valid_until=LEAST(now()+v.readiness_minutes*interval '1 minute',d.ends_at,v.certified_until,driver.certified_until,medic.certified_until)) THEN
 RAISE EXCEPTION 'readiness must match current policy and duty' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_readiness_valid BEFORE INSERT ON emergency_readiness FOR EACH ROW EXECUTE FUNCTION emergency_readiness_valid();

CREATE TABLE emergency_fleet_constraints (
 tenant_id uuid NOT NULL,
 base_id uuid NOT NULL,
 id uuid NOT NULL,
 code text NOT NULL CHECK(code IN('oxygen_low','defibrillator_failed','equipment_incomplete','not_roadworthy')),
 vehicle_id uuid NOT NULL,
 cleared_at timestamptz,
 cleared_by text,
 evidence_reference text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id,code),
 CHECK((cleared_at IS NULL AND cleared_by IS NULL AND evidence_reference IS NULL) OR
 (cleared_at IS NOT NULL AND cleared_by IS NOT NULL AND evidence_reference IS NOT NULL AND length(btrim(cleared_by)) BETWEEN 1 AND 200 AND evidence_reference ~ '^[A-Za-z0-9._/-]{1,120}$')),
 FOREIGN KEY(tenant_id,base_id,id) REFERENCES emergency_readiness(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,vehicle_id) REFERENCES emergency_vehicles(tenant_id,base_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX emergency_fleet_constraint_open ON emergency_fleet_constraints(tenant_id,base_id,vehicle_id,code) WHERE cleared_at IS NULL;
CREATE INDEX ON emergency_fleet_constraints(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_fleet_constraints ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_fleet_constraints FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_fleet_constraints
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake'));
CREATE TRIGGER emergency_fleet_constraint_no_removal BEFORE DELETE OR TRUNCATE ON emergency_fleet_constraints FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_fleet_constraint_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN
 IF NEW.cleared_at IS NOT NULL OR NOT EXISTS(SELECT 1 FROM emergency_readiness r JOIN emergency_vehicles v ON v.tenant_id=r.tenant_id AND v.base_id=r.base_id AND v.id=r.vehicle_id
 WHERE r.tenant_id=NEW.tenant_id AND r.base_id=NEW.base_id AND r.id=NEW.id AND r.vehicle_id=NEW.vehicle_id AND (
 (NEW.code='oxygen_low' AND r.oxygen_bar<v.oxygen_min_bar) OR
 (NEW.code='defibrillator_failed' AND NOT r.defibrillator_passed AND v.vehicle_class='als') OR
 (NEW.code='equipment_incomplete' AND NOT r.equipment_complete) OR
 (NEW.code='not_roadworthy' AND NOT r.roadworthy))) THEN
 RAISE EXCEPTION 'constraint must match failed readiness' USING ERRCODE='23514'; END IF;
 ELSE
 IF NOT emergency_allowed(NEW.tenant_id,NEW.base_id,'close') OR OLD.cleared_at IS NOT NULL OR NEW.cleared_at IS NULL
 OR NEW.cleared_at<>now() OR NEW.cleared_by IS DISTINCT FROM NULLIF(current_setting('app.emergency_subject',true),'') OR NEW.evidence_reference IS NULL
 OR (NEW.tenant_id,NEW.base_id,NEW.id,NEW.code,NEW.vehicle_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.base_id,OLD.id,OLD.code,OLD.vehicle_id,OLD.created_at) THEN
 RAISE EXCEPTION 'constraint clearance requires supervisor evidence' USING ERRCODE='23514'; END IF;
 END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_fleet_constraint_guard BEFORE INSERT OR UPDATE ON emergency_fleet_constraints FOR EACH ROW EXECUTE FUNCTION emergency_fleet_constraint_guard();
CREATE FUNCTION emergency_readiness_constraints() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO emergency_fleet_constraints(tenant_id,base_id,id,code,vehicle_id)
 SELECT NEW.tenant_id,NEW.base_id,NEW.id,c.code,NEW.vehicle_id FROM emergency_vehicles v
 CROSS JOIN LATERAL(VALUES
 ('oxygen_low',NEW.oxygen_bar<v.oxygen_min_bar),
 ('defibrillator_failed',NOT NEW.defibrillator_passed AND v.vehicle_class='als'),
 ('equipment_incomplete',NOT NEW.equipment_complete),
 ('not_roadworthy',NOT NEW.roadworthy)) c(code,failed)
 WHERE v.tenant_id=NEW.tenant_id AND v.base_id=NEW.base_id AND v.id=NEW.vehicle_id AND c.failed
 ON CONFLICT(tenant_id,base_id,vehicle_id,code) WHERE cleared_at IS NULL DO NOTHING;
 RETURN NULL;
END $$;
CREATE TRIGGER emergency_readiness_constraints AFTER INSERT ON emergency_readiness FOR EACH ROW EXECUTE FUNCTION emergency_readiness_constraints();

CREATE VIEW emergency_fleet_status WITH(security_invoker=true) AS
 SELECT f.*, cardinality(f.blocker_list)=0 AS dispatchable, to_jsonb(f.blocker_list) AS blockers,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'code',c.code) ORDER BY c.code) FROM emergency_fleet_constraints c WHERE c.tenant_id=f.tenant_id AND c.base_id=f.base_id AND c.vehicle_id=f.id AND c.cleared_at IS NULL),'[]'::jsonb) AS open_constraints,
 CASE WHEN cardinality(f.blocker_list)=0 THEN f.capability ELSE NULL END AS effective_capability
 FROM (
 SELECT v.*, now() AS computed_at, d.id AS duty_id,
 CASE WHEN v.vehicle_class='als' AND medic.qualification='emt' THEN 'bls' ELSE v.vehicle_class END AS capability,
 ARRAY(SELECT DISTINCT reason FROM unnest(array_remove(ARRAY[
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
