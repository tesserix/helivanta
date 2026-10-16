SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Role predicates cannot hide subquery privilege checks from other workers.
ALTER POLICY tenant_isolation ON outbox_events
 WITH CHECK(tenant_id IS NULL OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY workforce_worker_outbox_boundary ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_document_worker
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND tenant_id IS NOT NULL AND subject='helivanta.in.workforce.document_changed.v1'
 AND payload->>'tenant_id'=tenant_id::text AND payload->>'type'=subject
 AND payload->'data'->>'hospital_id'=current_setting('app.workforce_hospital',true)
 AND payload->'data'->>'staff_id'=current_setting('app.workforce_document_staff',true)
 AND payload->'data'->>'file_id'=current_setting('app.workforce_document_file',true)
 AND EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=outbox_events.tenant_id
 AND workforce_document_worker_allowed(d.tenant_id,d.hospital_id,d.staff_id,d.id)
 AND d.status=payload->'data'->>'status' AND d.scan_attempt::text=payload->'data'->>'scan_attempt'));
