SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- ADR-0055: a department grant binds every posting row to the locked target, or in the inbox to its locked subtree.
CREATE FUNCTION workforce_posting_department_allows(department uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN COALESCE(current_setting('app.workforce_posting_scope_kind',true),'')<>'department'
  AND COALESCE(current_setting('app.workforce_posting_department_target',true),'')='' THEN true
 ELSE COALESCE(current_setting('app.workforce_posting_scope_kind',true)='department'
  AND (department=NULLIF(current_setting('app.workforce_posting_department_target',true),'')::uuid
  OR current_setting('app.workforce_posting_inbox',true)='read'
  AND department=ANY(string_to_array(NULLIF(current_setting('app.workforce_posting_department_subtree',true),''),',')::uuid[])),false) END
$$;

CREATE POLICY posting_department_scope ON workforce_posting_proposal_revisions AS RESTRICTIVE FOR ALL
 USING (workforce_posting_department_allows(department_id)) WITH CHECK (workforce_posting_department_allows(department_id));
CREATE POLICY posting_department_scope ON workforce_posting_draft_revisions AS RESTRICTIVE FOR ALL
 USING (workforce_posting_department_allows(department_id)) WITH CHECK (workforce_posting_department_allows(department_id));

-- Staff source rows stay hidden unless the Staff has a posting proposal in the bound department, or is the actor.
CREATE POLICY posting_department_scope ON workforce_staff AS RESTRICTIVE FOR ALL USING (
 COALESCE(current_setting('app.workforce_posting_scope_kind',true),'')<>'department'
 AND COALESCE(current_setting('app.workforce_posting_department_target',true),'')=''
 OR id=NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid
 OR EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
  WHERE (r.tenant_id,r.hospital_id,r.staff_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id)
  AND r.version=1 AND workforce_posting_department_allows(r.department_id)));
