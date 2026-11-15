SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $prerequisites$
BEGIN
 IF to_regprocedure('public.workforce_posting_root_approval_context(uuid,uuid,uuid,uuid,uuid)') IS NULL
 OR to_regprocedure('public.workforce_posting_chain_approval_context(uuid,uuid,uuid,uuid,uuid)') IS NULL THEN
  RAISE EXCEPTION 'posting approval planning requires existing root and chain guards' USING ERRCODE='55000';
 END IF;
END $prerequisites$;

CREATE OR REPLACE FUNCTION workforce_posting_root_approval_context(t uuid,h uuid,s uuid,p uuid,initial uuid)
RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF current_setting('app.workforce_posting_root_approval',true) IS DISTINCT FROM 'verified' THEN RETURN false;END IF;
 RETURN (
 SELECT COALESCE(workforce_posting_proposal_decider(t,h,s,p,'approve')
 AND current_setting('app.workforce_posting_root_approval',true)='verified'
 AND NULLIF(current_setting('app.workforce_posting_management_assignment',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_management_assignment',true),'')::uuid<>NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid
 AND NULLIF(current_setting('app.workforce_posting_management_assignment_version',true),'')::bigint>0
 AND NULLIF(current_setting('app.workforce_posting_management_scope_epoch',true),'')::bigint=NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint
 AND NULLIF(current_setting('app.workforce_posting_management_membership_epoch',true),'')::bigint=NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint
 AND EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
 JOIN workforce_posting_supervision_intents i ON
 (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)
 JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(r.tenant_id,r.hospital_id,r.staff_id)
 WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(t,h,s,p,initial,1)
 AND r.supervision_requested AND i.kind='hospital_management' AND staff.employment_status='active'
 AND (staff.version,staff.employment_epoch)=(r.staff_version,r.employment_epoch)
 AND r.generation_id=NULLIF(current_setting('app.workforce_posting_target_generation',true),'')::uuid
 AND r.eligibility_until=NULLIF(current_setting('app.workforce_posting_target_until',true),'')::timestamptz
 AND r.department_id=NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 AND r.department_revision_id=NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid
 AND r.eligibility_until>clock_timestamp()),false)
 );
END $$;

CREATE OR REPLACE FUNCTION workforce_posting_chain_approval_context(t uuid,h uuid,s uuid,p uuid,initial uuid)
RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF current_setting('app.workforce_posting_chain_approval',true) IS DISTINCT FROM 'verified' THEN RETURN false;END IF;
 RETURN (
 SELECT COALESCE(workforce_posting_proposal_decider(t,h,s,p,'approve')
 AND current_setting('app.workforce_posting_chain_approval',true)='verified'
 AND NULLIF(current_setting('app.workforce_posting_chain_until',true),'')::timestamptz>clock_timestamp()
 AND EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
 JOIN workforce_posting_supervision_intents i ON (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)
 JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(r.tenant_id,r.hospital_id,r.staff_id)
 WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(t,h,s,p,initial,1)
 AND r.supervision_requested AND i.kind IN ('department_supervisor','department_member') AND staff.employment_status='active'
 AND (staff.version,staff.employment_epoch)=(r.staff_version,r.employment_epoch)
 AND r.generation_id=NULLIF(current_setting('app.workforce_posting_target_generation',true),'')::uuid
 AND r.eligibility_until=NULLIF(current_setting('app.workforce_posting_target_until',true),'')::timestamptz
 AND r.department_id=NULLIF(current_setting('app.workforce_posting_department',true),'')::uuid
 AND r.department_revision_id=NULLIF(current_setting('app.workforce_posting_department_revision',true),'')::uuid
 AND NULLIF(current_setting('app.workforce_posting_chain_until',true),'')::timestamptz<=r.eligibility_until),false)
 );
END $$;
