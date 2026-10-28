SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Result delivery may read only its original request; writes remain subject-owned.
ALTER POLICY tenant_isolation ON mediconnect_link_requests
 USING(hms_tenant_visible(tenant_id)
 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR
 (id=NULLIF(current_setting('app.mediconnect_review_request',true),'')::uuid
 AND current_setting('app.actor_subject',true)='system:mediconnect-registration-reviewed')));
