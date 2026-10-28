SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION emergency_worker_allowed(t uuid,b uuid,d uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (current_user='helivanta_emergency_worker' OR (current_user='hms_owner' AND current_setting('app.emergency_eligibility',true)='true'))
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND b=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND d=NULLIF(current_setting('app.emergency_dispatch',true),'')::uuid
 AND (current_setting('app.emergency_subject',true)='service:emergency-dispatch-worker' OR (current_user='hms_owner' AND current_setting('app.emergency_eligibility',true)='true'));
$$;

DO $$
DECLARE previous text;
BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_bases'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_bases USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND id=NULLIF(current_setting(''app.emergency_base'',true),'''')::uuid) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_dispatches'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_dispatches USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (emergency_worker_allowed(tenant_id,base_id,id)) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_dispatch_commands'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_dispatch_commands USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (emergency_worker_allowed(tenant_id,base_id,dispatch_id)) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_cases'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_cases USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_cases.tenant_id AND d.base_id=emergency_cases.base_id AND d.case_id=emergency_cases.id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_vehicles'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_vehicles USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_vehicles.tenant_id AND d.base_id=emergency_vehicles.base_id AND d.vehicle_id=emergency_vehicles.id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_duties'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_duties USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_duties.tenant_id AND d.base_id=emergency_duties.base_id AND d.duty_id=emergency_duties.id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_crew'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_crew USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_crew.tenant_id AND d.base_id=emergency_crew.base_id AND emergency_crew.subject IN(d.driver_subject,d.clinician_subject) AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_readiness'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_readiness USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_readiness.tenant_id AND d.base_id=emergency_readiness.base_id AND d.vehicle_id=emergency_readiness.vehicle_id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_fleet_constraints'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_fleet_constraints USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_fleet_constraints.tenant_id AND d.base_id=emergency_fleet_constraints.base_id AND d.vehicle_id=emergency_fleet_constraints.vehicle_id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_transport_assessments'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_transport_assessments USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=emergency_transport_assessments.tenant_id AND d.base_id=emergency_transport_assessments.base_id AND d.case_id=emergency_transport_assessments.case_id AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_clinical_reviewers'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_clinical_reviewers USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN (EXISTS(SELECT 1 FROM emergency_dispatches d JOIN emergency_transport_assessments a ON a.tenant_id=d.tenant_id AND a.base_id=d.base_id AND a.id=d.assessment_id WHERE d.tenant_id=emergency_clinical_reviewers.tenant_id AND d.base_id=emergency_clinical_reviewers.base_id AND a.reviewer_subject=emergency_clinical_reviewers.subject AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id))) ELSE (%s) END)',previous);
END $$;

DO $$ DECLARE previous text; BEGIN
 SELECT pg_get_expr(polwithcheck,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_dispatches'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_dispatches WITH CHECK(CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN emergency_worker_allowed(tenant_id,base_id,id) ELSE (%s) END)',previous);
END $$;

DO $$ DECLARE previous text; BEGIN
 SELECT pg_get_expr(polwithcheck,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_dispatch_commands'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_dispatch_commands WITH CHECK(CASE WHEN current_user=''helivanta_emergency_worker'' OR (current_user=''hms_owner'' AND current_setting(''app.emergency_eligibility'',true)=''true'') THEN emergency_worker_allowed(tenant_id,base_id,dispatch_id) ELSE (%s) END)',previous);
END $$;

GRANT USAGE ON SCHEMA public TO helivanta_emergency_worker;
GRANT SELECT ON emergency_dispatches,emergency_vehicles,emergency_fleet_status TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,id) ON emergency_bases TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,version,status) ON emergency_cases TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,vehicle_id,driver_subject,clinician_subject,ends_at,ended_at) ON emergency_duties TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,subject,qualification,certified_until,suspended) ON emergency_crew TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,vehicle_id,vehicle_version,duty_id,valid_until,oxygen_bar,defibrillator_passed,equipment_complete,roadworthy,created_at) ON emergency_readiness TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,vehicle_id,code,cleared_at) ON emergency_fleet_constraints TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,case_id,case_version,sequence,reviewer_grant_id,reviewer_subject,required_class,valid_until) ON emergency_transport_assessments TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,id,subject,version,enabled,expires_at) ON emergency_clinical_reviewers TO helivanta_emergency_worker;
GRANT SELECT ON emergency_dispatch_commands TO helivanta_emergency_worker;
GRANT UPDATE(id) ON emergency_bases,emergency_cases,emergency_vehicles,emergency_duties,emergency_crew TO helivanta_emergency_worker;
GRANT UPDATE(state,version,updated_at,acknowledgement_deadline,driver_acknowledged,clinician_acknowledged) ON emergency_dispatches TO helivanta_emergency_worker;
GRANT INSERT(tenant_id,base_id,id,dispatch_id,expected_version,actor_subject,operation,evidence_reference) ON emergency_dispatch_commands TO helivanta_emergency_worker;
GRANT INSERT(tenant_id,base_id,actor_subject,action,resource_id,target_subject) ON emergency_audit TO helivanta_emergency_worker;
GRANT INSERT ON outbox_events TO helivanta_emergency_worker;
GRANT SELECT(id) ON outbox_events TO helivanta_emergency_worker;

CREATE POLICY emergency_worker_audit_guard ON emergency_audit AS RESTRICTIVE FOR INSERT TO helivanta_emergency_worker
 WITH CHECK(emergency_worker_allowed(tenant_id,base_id,resource_id)
 AND actor_subject='service:emergency-dispatch-worker' AND target_subject IS NULL
 AND action IN('dispatch.offer','dispatch.acknowledgement_timeout'));
CREATE POLICY emergency_worker_outbox_guard ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_emergency_worker
 WITH CHECK(tenant_id IS NOT NULL AND subject='helivanta.in.emergency.dispatch_changed.v1'
 AND payload->>'tenant_id'=tenant_id::text AND payload->>'type'=subject
 AND (payload->'data')-ARRAY['base_id','dispatch_id','version']='{}'::jsonb
 AND EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=outbox_events.tenant_id
 AND emergency_worker_allowed(d.tenant_id,d.base_id,d.id)
 AND d.base_id::text=payload->'data'->>'base_id' AND d.id::text=payload->'data'->>'dispatch_id'
 AND d.version::text=payload->'data'->>'version'));

CREATE FUNCTION emergency_dispatch_offer_eligible(t uuid,b uuid,dispatch uuid) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE saved emergency_dispatches%ROWTYPE; previous_dispatch text; previous_purpose text; eligible boolean;
BEGIN
 IF t IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid
 OR b IS DISTINCT FROM NULLIF(current_setting('app.emergency_base',true),'')::uuid THEN RETURN false; END IF;
 IF NOT(session_user='helivanta_emergency_worker' AND current_setting('app.emergency_subject',true)='service:emergency-dispatch-worker' AND dispatch=NULLIF(current_setting('app.emergency_dispatch',true),'')::uuid) THEN
  SELECT * INTO saved FROM emergency_dispatches WHERE tenant_id=t AND base_id=b AND id=dispatch;
  IF NOT FOUND OR NOT(current_setting('app.emergency_subject',true) IN(saved.driver_subject,saved.clinician_subject) OR emergency_allowed(t,b,'intake')) THEN RETURN false; END IF;
 END IF;
 previous_dispatch=current_setting('app.emergency_dispatch',true); previous_purpose=current_setting('app.emergency_eligibility',true);
 PERFORM set_config('app.emergency_dispatch',dispatch::text,true),set_config('app.emergency_eligibility','true',true);
 SELECT * INTO saved FROM emergency_dispatches WHERE tenant_id=t AND base_id=b AND id=dispatch;
 IF NOT FOUND THEN
  PERFORM set_config('app.emergency_dispatch',COALESCE(previous_dispatch,''),true),set_config('app.emergency_eligibility',COALESCE(previous_purpose,''),true);
  RETURN false;
 END IF;
 PERFORM id FROM emergency_cases WHERE tenant_id=t AND base_id=b AND id=saved.case_id FOR UPDATE;
 PERFORM id FROM emergency_vehicles WHERE tenant_id=t AND base_id=b AND id=saved.vehicle_id FOR UPDATE;
 PERFORM id FROM emergency_duties WHERE tenant_id=t AND base_id=b AND id=saved.duty_id FOR UPDATE;
 PERFORM id FROM emergency_crew WHERE tenant_id=t AND base_id=b AND subject IN(saved.driver_subject,saved.clinician_subject) ORDER BY subject FOR SHARE;
 SELECT EXISTS(SELECT 1 FROM emergency_cases c
 JOIN emergency_transport_assessments a ON a.tenant_id=c.tenant_id AND a.base_id=c.base_id AND a.id=saved.assessment_id
 JOIN emergency_clinical_reviewers r ON r.tenant_id=a.tenant_id AND r.base_id=a.base_id AND r.id=a.reviewer_grant_id
 JOIN emergency_fleet_status v ON v.tenant_id=c.tenant_id AND v.base_id=c.base_id AND v.id=saved.vehicle_id
 WHERE c.tenant_id=t AND c.base_id=b AND c.id=saved.case_id AND c.status='received' AND c.version=saved.case_version
 AND a.case_id=c.id AND a.case_version=c.version AND a.valid_until>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM emergency_transport_assessments newer WHERE newer.tenant_id=t AND newer.base_id=b AND newer.case_id=c.id AND newer.sequence>a.sequence)
 AND r.enabled AND r.expires_at>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM emergency_clinical_reviewers newer WHERE newer.tenant_id=t AND newer.base_id=b AND newer.subject=r.subject AND newer.version>r.version)
 AND v.blockers='["active_dispatch"]'::jsonb AND v.duty_id=saved.duty_id
 AND emergency_class_supports(v.capability,a.required_class)) INTO eligible;
 PERFORM set_config('app.emergency_dispatch',COALESCE(previous_dispatch,''),true),set_config('app.emergency_eligibility',COALESCE(previous_purpose,''),true);
 RETURN eligible;
END $$;
