SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_employment_reviews
 ADD COLUMN decision_reason text NOT NULL DEFAULT '' CHECK(length(decision_reason)<=500),
 ADD COLUMN reviewer_terms_examined boolean NOT NULL DEFAULT false,
 ADD COLUMN independent_person_confirmed boolean NOT NULL DEFAULT false;
ALTER TABLE workforce_employment_review_history
 ADD COLUMN decision_reason text NOT NULL DEFAULT '' CHECK(length(decision_reason)<=500),
 ADD COLUMN reviewer_terms_examined boolean NOT NULL DEFAULT false,
 ADD COLUMN independent_person_confirmed boolean NOT NULL DEFAULT false;
DO $migration$ DECLARE t text;item record;BEGIN
 FOREACH t IN ARRAY ARRAY['workforce_employment_reviews','workforce_employment_review_history'] LOOP
  FOR item IN SELECT conname FROM pg_constraint WHERE conrelid=('public.'||quote_ident(t))::regclass
   AND contype='c' AND pg_get_constraintdef(oid) IN ('CHECK ((version = 1))','CHECK ((status = ''pending''::text))') LOOP
   EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',t,item.conname);
  END LOOP;
  EXECUTE format('ALTER TABLE %I ADD CHECK(version BETWEEN 1 AND 9007199254740990), ADD CHECK(status IN (''pending'',''approved'',''rejected'',''withdrawn'',''revoked''))',t);
 END LOOP;
END $migration$;

CREATE OR REPLACE FUNCTION workforce_employment_review_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND NULLIF(current_setting('app.workforce_employment_review_id',true),'') IS NOT NULL
 AND ((workforce_hr_intake_context(row_tenant,row_hospital) AND current_setting('app.workforce_employment_review',true)='manage')
 OR (current_setting('app.workforce_employment_review',true)='review'
 AND NULLIF(current_setting('app.workforce_employment_review_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_employment_review_link',true),'') IS NOT NULL))
$$;
CREATE FUNCTION workforce_employment_review_file_context(row_tenant uuid,row_hospital uuid,row_file uuid)
RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF current_setting('app.workforce_employment_review',true) IS DISTINCT FROM 'review' THEN RETURN false; END IF;
 RETURN EXISTS(SELECT 1 FROM workforce_employment_reviews r
 WHERE r.tenant_id=row_tenant AND r.hospital_id=row_hospital
 AND r.id::text=current_setting('app.workforce_employment_review_id',true)
 AND workforce_employment_review_context(r.tenant_id,r.hospital_id,r.staff_id)
 AND (EXISTS(SELECT 1 FROM workforce_employment_review_evidence e
 WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.id,row_file))
 OR EXISTS(SELECT 1 FROM workforce_person_review_evidence e
 WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.person_review_id,row_file))));
END $$;
DO $migration$ DECLARE item record;prior record;BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('workforce_staff',$read$EXISTS(SELECT 1 FROM workforce_employment_reviews r JOIN workforce_employment_period_history p ON (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.version,p.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id,r.period_version,r.period_revision_id) WHERE (r.tenant_id,r.hospital_id,p.sponsor_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id))$read$),
 ('workforce_employment_period_drafts',$read$EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.period_id)=(workforce_employment_period_drafts.tenant_id,workforce_employment_period_drafts.hospital_id,workforce_employment_period_drafts.staff_id,workforce_employment_period_drafts.id))$read$),
 ('workforce_employment_period_history',$read$EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.period_id)=(workforce_employment_period_history.tenant_id,workforce_employment_period_history.hospital_id,workforce_employment_period_history.staff_id,workforce_employment_period_history.id) AND workforce_employment_period_history.version<=r.period_version)$read$),
 ('workforce_employment_evidence_bindings',$read$EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.binding_id)=(workforce_employment_evidence_bindings.tenant_id,workforce_employment_evidence_bindings.hospital_id,workforce_employment_evidence_bindings.staff_id,workforce_employment_evidence_bindings.id))$read$),
 ('workforce_employment_evidence_history',$read$EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.binding_id)=(workforce_employment_evidence_history.tenant_id,workforce_employment_evidence_history.hospital_id,workforce_employment_evidence_history.staff_id,workforce_employment_evidence_history.id) AND workforce_employment_evidence_history.version<=e.binding_version)$read$),
 ('workforce_documents',$read$record_kind='enrollment' AND record_id=staff_id AND category IN ('identity_evidence','employment_evidence') AND workforce_employment_review_file_context(tenant_id,hospital_id,id)$read$),
 ('workforce_document_classifications',$read$workforce_employment_review_file_context(tenant_id,hospital_id,id)$read$),
 ('workforce_document_classification_revisions',$read$workforce_employment_review_file_context(tenant_id,hospital_id,file_id)$read$),
 ('workforce_document_contributors',$read$workforce_employment_review_file_context(tenant_id,hospital_id,document_id)$read$)
 ) AS scopes(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  EXECUTE format('CREATE POLICY employment_reviewer_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
END $migration$;
CREATE OR REPLACE FUNCTION workforce_employment_review_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'employment review cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF workforce_employment_review_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) IS NOT TRUE
   OR NEW.id::text IS DISTINCT FROM current_setting('app.workforce_employment_review_id',true)
   OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') THEN
   RAISE EXCEPTION 'employment decision requires current scoped authority' USING ERRCODE='42501';
  END IF;
  IF (to_jsonb(NEW)-ARRAY['revision_id','version','status','actor_subject','input_hash','decision_reason','reviewer_terms_examined','independent_person_confirmed','updated_at'])
   IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['revision_id','version','status','actor_subject','input_hash','decision_reason','reviewer_terms_examined','independent_person_confirmed','updated_at'])
   OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id
   OR length(btrim(NEW.decision_reason)) NOT BETWEEN 1 AND 500 THEN
   RAISE EXCEPTION 'employment review source is immutable; decision version must advance' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
  END IF;
  IF OLD.status='pending' AND NEW.status='withdrawn' THEN
   IF current_setting('app.workforce_employment_review',true) IS DISTINCT FROM 'manage' OR NEW.actor_subject<>OLD.proposed_by_subject
    OR NEW.reviewer_terms_examined OR NEW.independent_person_confirmed THEN
    RAISE EXCEPTION 'only the preparer may withdraw employment terms' USING ERRCODE='42501';
   END IF;
  ELSIF (OLD.status='pending' AND NEW.status IN ('approved','rejected')) OR (OLD.status='approved' AND NEW.status='revoked') THEN
   IF current_setting('app.workforce_employment_review',true) IS DISTINCT FROM 'review' THEN
    RAISE EXCEPTION 'employment decision requires reviewer authority' USING ERRCODE='42501';
   END IF;
   IF NEW.actor_subject IN (OLD.proposed_by_subject,OLD.declared_subject)
    OR OLD.staff_id::text=current_setting('app.workforce_employment_review_actor',true)
    OR EXISTS(SELECT 1 FROM workforce_employment_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.review_id,c.subject)=(OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id,NEW.actor_subject))
    OR EXISTS(SELECT 1 FROM workforce_login_links l WHERE (l.tenant_id,l.hospital_id,l.staff_id,l.subject)=(OLD.tenant_id,OLD.hospital_id,OLD.staff_id,NEW.actor_subject)) THEN
    RAISE EXCEPTION 'employment decision requires an independent actor' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_independence';
   END IF;
   IF (NEW.status IN ('approved','rejected') AND (NOT NEW.reviewer_terms_examined OR NOT NEW.independent_person_confirmed))
    OR (NEW.status='revoked' AND (NEW.reviewer_terms_examined OR NEW.independent_person_confirmed)) THEN
    RAISE EXCEPTION 'employment decision requires current action attestations' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'illegal employment review transition' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
  END IF;
  IF NEW.status='approved' THEN
   PERFORM 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) FOR SHARE;
   PERFORM 1 FROM workforce_employment_period_drafts p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.period_id) FOR SHARE;
   PERFORM 1 FROM workforce_person_reviews p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.person_review_id) FOR SHARE;
   PERFORM 1 FROM workforce_employment_evidence_bindings b JOIN workforce_employment_review_evidence e
    ON (e.tenant_id,e.hospital_id,e.staff_id,e.binding_id)=(b.tenant_id,b.hospital_id,b.staff_id,b.id)
    WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id) ORDER BY b.id FOR SHARE OF b;
   PERFORM 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.staff_id=NEW.staff_id
    AND workforce_employment_review_file_context(d.tenant_id,d.hospital_id,d.id) ORDER BY d.id FOR SHARE;
   IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.version,s.employment_epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.staff_version,NEW.employment_epoch))
    OR NOT EXISTS(SELECT 1 FROM workforce_employment_period_drafts p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.staff_version,p.employment_epoch,p.status)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.period_id,NEW.period_revision_id,NEW.period_version,NEW.staff_version,NEW.employment_epoch,'draft') AND (p.sponsor_id IS NULL OR EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.employment_status)=(p.tenant_id,p.hospital_id,p.sponsor_id,'active'))))
    OR NOT EXISTS(SELECT 1 FROM workforce_person_reviews p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.status,p.employment_epoch,p.identity_hash,p.access_intent,p.declared_subject)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.person_review_id,NEW.person_review_revision_id,NEW.person_review_version,'approved',NEW.employment_epoch,NEW.identity_hash,NEW.access_intent,NEW.declared_subject) AND p.valid_until>clock_timestamp())
    OR EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id)
     AND NOT EXISTS(SELECT 1 FROM workforce_employment_evidence_bindings b
      JOIN workforce_documents d ON (d.tenant_id,d.hospital_id,d.staff_id,d.id)=(b.tenant_id,b.hospital_id,b.staff_id,b.document_id)
      LEFT JOIN workforce_document_classifications c ON (c.tenant_id,c.hospital_id,c.id)=(d.tenant_id,d.hospital_id,d.id)
      WHERE (b.tenant_id,b.hospital_id,b.staff_id,b.id,b.revision_id,b.version,b.document_id,b.status,b.period_id,b.period_revision_id,b.period_version,b.staff_version,b.employment_epoch)
       =(e.tenant_id,e.hospital_id,e.staff_id,e.binding_id,e.binding_revision_id,e.binding_version,e.document_id,'attached',NEW.period_id,NEW.period_revision_id,NEW.period_version,NEW.staff_version,NEW.employment_epoch)
      AND (d.record_kind,d.record_id,d.category,d.status)=('enrollment',NEW.staff_id,'employment_evidence','available')
      AND (d.record_version,d.filename,d.content_type,d.size,d.sha256,d.source_generation,d.released_generation,d.scan_attempt,d.scanned_at,d.engine_version,d.definition_version,d.definitions_updated_at)
       IS NOT DISTINCT FROM (b.document_record_version,b.filename,b.content_type,b.size,b.sha256,b.source_generation,b.released_generation,b.scan_attempt,b.scanned_at,b.engine_version,b.definition_version,b.definitions_updated_at)
      AND COALESCE(c.version,0)=e.classification_version AND c.revision_id IS NOT DISTINCT FROM e.classification_revision_id))
    OR EXISTS(SELECT 1 FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.person_review_id)
     AND NOT EXISTS(SELECT 1 FROM workforce_documents d LEFT JOIN workforce_document_classifications c ON (c.tenant_id,c.hospital_id,c.id)=(d.tenant_id,d.hospital_id,d.id)
      WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.id)=(e.tenant_id,e.hospital_id,e.staff_id,e.document_id)
      AND (d.record_kind,d.record_id,d.category,d.status)=('enrollment',NEW.staff_id,'identity_evidence','available')
      AND (d.record_version,d.filename,d.content_type,d.size,d.sha256,d.source_generation,d.released_generation,d.scan_attempt,d.scanned_at,d.engine_version,d.definition_version,d.definitions_updated_at)
       IS NOT DISTINCT FROM (e.record_version,e.filename,e.content_type,e.size,e.sha256,e.source_generation,e.released_generation,e.scan_attempt,e.scanned_at,e.engine_version,e.definition_version,e.definitions_updated_at)
      AND COALESCE(c.version,0)=e.classification_version AND COALESCE(c.revision_id::text,'')=e.classification_revision_id
      AND EXISTS(SELECT 1 FROM workforce_document_contributors c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.document_id)=(d.tenant_id,d.hospital_id,d.staff_id,d.id) AND c.creation_captured))) THEN
    RAISE EXCEPTION 'employment approval requires exact current source and released evidence' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
   END IF;
  END IF;
  NEW.updated_at:=clock_timestamp();RETURN NEW;
 END IF;
 IF workforce_employment_review_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) IS NOT TRUE
  OR NEW.id::text IS DISTINCT FROM current_setting('app.workforce_employment_review_id',true)
  OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'')
  OR current_setting('app.workforce_employment_review',true) IS DISTINCT FROM 'manage' OR NEW.proposed_by_subject IS DISTINCT FROM NEW.actor_subject OR NEW.version<>1 OR NEW.status<>'pending' OR NEW.decision_reason<>'' OR NEW.reviewer_terms_examined OR NEW.independent_person_confirmed THEN
  RAISE EXCEPTION 'employment submission requires current scoped preparation authority' USING ERRCODE='42501';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.version,s.employment_epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.staff_version,NEW.employment_epoch))
  OR NOT EXISTS(SELECT 1 FROM workforce_employment_period_drafts p WHERE (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version,p.staff_version,p.employment_epoch,p.status)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.period_id,NEW.period_revision_id,NEW.period_version,NEW.staff_version,NEW.employment_epoch,'draft') AND (p.sponsor_id IS NULL OR EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.employment_status)=(p.tenant_id,p.hospital_id,p.sponsor_id,'active'))))
  OR NOT EXISTS(SELECT 1 FROM workforce_person_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id,r.revision_id,r.version,r.status,r.employment_epoch,r.identity_hash,r.access_intent,r.declared_subject)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.person_review_id,NEW.person_review_revision_id,NEW.person_review_version,'approved',NEW.employment_epoch,NEW.identity_hash,NEW.access_intent,NEW.declared_subject) AND r.valid_until>clock_timestamp()) THEN
  RAISE EXCEPTION 'employment submission source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
 END IF;
 IF NEW.actor_subject=NEW.declared_subject OR EXISTS(SELECT 1 FROM workforce_login_links l WHERE (l.tenant_id,l.hospital_id,l.staff_id,l.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.actor_subject)) THEN
  RAISE EXCEPTION 'employment submission cannot be self prepared' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_independence';
 END IF;
 NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;NEW.submission_xid:=pg_current_xact_id();RETURN NEW;
END $$;
DROP TRIGGER workforce_employment_review_snapshot ON workforce_employment_reviews;
CREATE TRIGGER workforce_employment_review_snapshot AFTER INSERT OR UPDATE ON workforce_employment_reviews FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_snapshot();
CREATE CONSTRAINT TRIGGER workforce_employment_review_approval_complete AFTER UPDATE ON workforce_employment_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.status='approved') EXECUTE FUNCTION workforce_employment_review_complete();
