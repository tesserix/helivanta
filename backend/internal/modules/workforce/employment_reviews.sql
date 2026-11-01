SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_employment_review_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_hr_intake_context(row_tenant,row_hospital)
 AND workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND COALESCE(current_setting('app.workforce_employment_review',true)='manage',false)
 AND NULLIF(current_setting('app.workforce_employment_review_id',true),'') IS NOT NULL
$$;
CREATE FUNCTION workforce_employment_person_context(row_tenant uuid,row_hospital uuid,row_staff uuid,row_review uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_employment_review_context(row_tenant,row_hospital,row_staff)
 AND COALESCE(row_review::text=current_setting('app.workforce_employment_person_review',true),false)
$$;

CREATE TABLE workforce_employment_reviews (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 revision_id uuid NOT NULL CHECK(substring(revision_id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version=1),status text NOT NULL CHECK(status='pending'),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740990),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740990),
 identity_hash text NOT NULL CHECK(identity_hash ~ '^[a-f0-9]{64}$'),
 period_id uuid NOT NULL,period_revision_id uuid NOT NULL,period_version bigint NOT NULL,
 person_review_id uuid NOT NULL,person_review_revision_id uuid NOT NULL,person_review_version bigint NOT NULL,
 policy_reference text NOT NULL CHECK(policy_reference=btrim(policy_reference) AND length(policy_reference) BETWEEN 1 AND 300),
 reason text NOT NULL CHECK(reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 terms_examined boolean NOT NULL CHECK(terms_examined),known_associations_disclosed boolean NOT NULL CHECK(known_associations_disclosed),
 access_intent text NOT NULL CHECK(access_intent IN ('digital','non_digital')),
 declared_subject text NOT NULL CHECK(length(declared_subject)<=200 AND ((access_intent='digital' AND length(declared_subject)>0) OR (access_intent='non_digital' AND declared_subject=''))),
 proposed_by_subject text NOT NULL CHECK(length(proposed_by_subject) BETWEEN 1 AND 200),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),submission_xid xid8 NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,period_id,period_version) REFERENCES workforce_employment_period_history(tenant_id,hospital_id,id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,person_review_id,person_review_version) REFERENCES workforce_person_review_history(tenant_id,hospital_id,id,version) ON DELETE RESTRICT
);
CREATE TABLE workforce_employment_review_history(LIKE workforce_employment_reviews INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE workforce_employment_review_history ADD PRIMARY KEY(tenant_id,hospital_id,id,version);
ALTER TABLE workforce_employment_review_history ADD FOREIGN KEY(tenant_id,hospital_id,id) REFERENCES workforce_employment_reviews(tenant_id,hospital_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX workforce_employment_review_receipt ON workforce_employment_review_history(tenant_id,hospital_id,revision_id);
CREATE TABLE workforce_employment_review_evidence (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,review_id uuid NOT NULL,
 binding_id uuid NOT NULL,binding_revision_id uuid NOT NULL,binding_version bigint NOT NULL,
 document_id uuid NOT NULL,classification_version bigint NOT NULL CHECK(classification_version BETWEEN 0 AND 9007199254740990),classification_revision_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,review_id,binding_id),UNIQUE(tenant_id,hospital_id,review_id,document_id),
 FOREIGN KEY(tenant_id,hospital_id,review_id) REFERENCES workforce_employment_reviews(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,binding_id,binding_version) REFERENCES workforce_employment_evidence_history(tenant_id,hospital_id,id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,document_id) REFERENCES workforce_documents(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK((classification_version=0 AND classification_revision_id IS NULL) OR (classification_version>0 AND classification_revision_id IS NOT NULL))
);
CREATE TABLE workforce_employment_review_contributors (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,review_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,review_id,subject),
 FOREIGN KEY(tenant_id,hospital_id,review_id) REFERENCES workforce_employment_reviews(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE FUNCTION workforce_employment_review_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'employment submission is immutable' USING ERRCODE='23514'; END IF;
 IF workforce_employment_review_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) IS NOT TRUE
  OR NEW.id::text IS DISTINCT FROM current_setting('app.workforce_employment_review_id',true)
  OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'')
  OR NEW.proposed_by_subject IS DISTINCT FROM NEW.actor_subject OR NEW.version<>1 OR NEW.status<>'pending' THEN
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
CREATE TRIGGER workforce_employment_review_write BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_reviews FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_write();
CREATE FUNCTION workforce_employment_review_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO workforce_employment_review_history SELECT NEW.*;RETURN NULL;END $$;
CREATE TRIGGER workforce_employment_review_snapshot AFTER INSERT ON workforce_employment_reviews FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_snapshot();
CREATE FUNCTION workforce_employment_review_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN RAISE EXCEPTION 'employment review history is immutable' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE TRIGGER workforce_employment_review_history_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_review_history FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_history_guard();
CREATE FUNCTION workforce_employment_review_child_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR NOT EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.review_id) AND r.submission_xid=pg_current_xact_id() AND r.actor_subject=NULLIF(current_setting('app.workforce_subject',true),'')) THEN
  RAISE EXCEPTION 'employment review evidence and contributors are frozen' USING ERRCODE='23514';
 END IF;
 IF TG_TABLE_NAME='workforce_employment_review_evidence' THEN
 IF NOT EXISTS(
  SELECT 1 FROM workforce_employment_reviews r JOIN workforce_employment_evidence_bindings b
   ON (b.tenant_id,b.hospital_id,b.staff_id,b.period_id,b.period_revision_id,b.period_version)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id,r.period_revision_id,r.period_version)
  JOIN workforce_documents d ON (d.tenant_id,d.hospital_id,d.staff_id,d.id)=(b.tenant_id,b.hospital_id,b.staff_id,b.document_id)
  LEFT JOIN workforce_document_classifications c ON (c.tenant_id,c.hospital_id,c.id)=(d.tenant_id,d.hospital_id,d.id)
  WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.review_id)
   AND (b.id,b.revision_id,b.version,b.document_id,b.status)=(NEW.binding_id,NEW.binding_revision_id,NEW.binding_version,NEW.document_id,'attached')
   AND (d.record_kind,d.record_id,d.category,d.status)=('enrollment',r.staff_id,'employment_evidence','available')
   AND (d.record_version,d.filename,d.content_type,d.size,d.sha256,d.source_generation,d.released_generation,d.scan_attempt,d.scanned_at,d.engine_version,d.definition_version,d.definitions_updated_at)
    IS NOT DISTINCT FROM (b.document_record_version,b.filename,b.content_type,b.size,b.sha256,b.source_generation,b.released_generation,b.scan_attempt,b.scanned_at,b.engine_version,b.definition_version,b.definitions_updated_at)
   AND COALESCE(c.version,0)=NEW.classification_version AND c.revision_id IS NOT DISTINCT FROM NEW.classification_revision_id
 ) THEN RAISE EXCEPTION 'employment evidence source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_review_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_review_evidence FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_child_guard();
CREATE TRIGGER workforce_employment_review_contributor_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_review_contributors FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_child_guard();
CREATE FUNCTION workforce_employment_review_required_contributors(row_tenant uuid,row_hospital uuid,row_staff uuid,row_review uuid)
RETURNS TABLE(subject text) LANGUAGE sql STABLE AS $$
 WITH source AS (SELECT r.tenant_id,r.hospital_id,r.staff_id,r.id,r.proposed_by_subject,r.period_id,r.period_version,r.person_review_id FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id)=(row_tenant,row_hospital,row_staff,row_review))
 SELECT r.proposed_by_subject FROM source r
 UNION SELECT a.actor_subject FROM source r JOIN workforce_audit a ON (a.tenant_id,a.hospital_id,a.resource_id)=(r.tenant_id,r.hospital_id,r.staff_id) WHERE a.action IN ('workforce_staff.insert','workforce_staff.update','workforce.enrollment.changed')
 UNION SELECT p.actor_subject FROM source r JOIN workforce_employment_period_history p ON (p.tenant_id,p.hospital_id,p.staff_id,p.id)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id) WHERE p.version<=r.period_version
 UNION SELECT b.actor_subject FROM source r JOIN workforce_employment_review_evidence e ON (e.tenant_id,e.hospital_id,e.review_id)=(r.tenant_id,r.hospital_id,r.id) JOIN workforce_employment_evidence_history b ON (b.tenant_id,b.hospital_id,b.staff_id,b.id)=(e.tenant_id,e.hospital_id,e.staff_id,e.binding_id) WHERE b.version<=e.binding_version
 UNION SELECT c.subject FROM source r JOIN workforce_employment_review_evidence e ON (e.tenant_id,e.hospital_id,e.review_id)=(r.tenant_id,r.hospital_id,r.id) JOIN workforce_document_contributors c ON (c.tenant_id,c.hospital_id,c.staff_id,c.document_id)=(e.tenant_id,e.hospital_id,e.staff_id,e.document_id)
 UNION SELECT c.subject FROM source r JOIN workforce_person_review_contributors c ON (c.tenant_id,c.hospital_id,c.staff_id,c.review_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.person_review_id)
$$;
CREATE FUNCTION workforce_employment_review_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total bigint;expected text[];actual text[];
BEGIN
 SELECT count(*) INTO total FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF total NOT BETWEEN 1 AND 20 OR NOT EXISTS(SELECT 1 FROM workforce_employment_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.review_id,c.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.proposed_by_subject))
  OR NOT EXISTS(SELECT 1 FROM workforce_employment_period_drafts d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.id,d.version,d.revision_id,d.status)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.period_id,NEW.period_version,NEW.period_revision_id,'draft') AND (d.sponsor_id IS NULL OR EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.employment_status)=(d.tenant_id,d.hospital_id,d.sponsor_id,'active'))))
  OR NOT EXISTS(SELECT 1 FROM workforce_person_reviews p WHERE (p.tenant_id,p.hospital_id,p.id,p.version,p.revision_id,p.status)=(NEW.tenant_id,NEW.hospital_id,NEW.person_review_id,NEW.person_review_version,NEW.person_review_revision_id,'approved') AND p.valid_until>clock_timestamp())
  OR EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
   AND NOT EXISTS(SELECT 1 FROM workforce_document_contributors c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.document_id)=(e.tenant_id,e.hospital_id,e.staff_id,e.document_id) AND c.creation_captured)) THEN
  RAISE EXCEPTION 'employment submission requires complete evidence provenance and current person finding' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
 END IF;
 SELECT array_agg(subject ORDER BY subject) INTO expected FROM (SELECT subject FROM workforce_employment_review_required_contributors(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) ORDER BY subject LIMIT 201) required;
 SELECT array_agg(subject ORDER BY subject) INTO actual FROM workforce_employment_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF cardinality(expected) NOT BETWEEN 1 AND 200 OR expected IS DISTINCT FROM actual THEN
  RAISE EXCEPTION 'employment submission must freeze exact contributor provenance' USING ERRCODE='23514',CONSTRAINT='workforce_employment_review_source';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_employment_review_complete AFTER INSERT ON workforce_employment_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_complete();

DO $migration$ DECLARE item record;prior record;BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('workforce_person_reviews','id'),('workforce_person_review_history','id'),('workforce_person_review_evidence','review_id'),('workforce_person_review_contributors','review_id')
 ) AS rows(table_name,review_column) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  EXECUTE format('CREATE POLICY employment_person_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR workforce_employment_person_context(tenant_id,hospital_id,staff_id,%I))',prior.polname,item.table_name,prior.read_expression,item.review_column);
 END LOOP;
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior FROM pg_policy p WHERE p.polrelid='public.workforce_login_links'::regclass AND p.polpermissive;
 EXECUTE format('CREATE POLICY employment_review_link_delete_guard ON workforce_login_links AS RESTRICTIVE FOR DELETE USING (%s)',prior.read_expression);
 EXECUTE format('ALTER POLICY %I ON workforce_login_links USING ((%s) OR workforce_employment_review_context(tenant_id,hospital_id,staff_id))',prior.polname,prior.read_expression);
 FOR item IN SELECT * FROM (VALUES ('workforce_employment_reviews','id'),('workforce_employment_review_history','id'),('workforce_employment_review_evidence','review_id'),('workforce_employment_review_contributors','review_id')) AS rows(table_name,review_column) LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',item.table_name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',item.table_name);
  EXECUTE format('CREATE POLICY employment_review_owner ON %I USING (hms_tenant_visible(tenant_id) AND workforce_employment_review_context(tenant_id,hospital_id,staff_id) AND %I::text=current_setting(''app.workforce_employment_review_id'',true)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_employment_review_context(tenant_id,hospital_id,staff_id) AND %I::text=current_setting(''app.workforce_employment_review_id'',true))',item.table_name,item.review_column,item.review_column);
  EXECUTE format('CREATE POLICY employment_review_no_delete ON %I AS RESTRICTIVE FOR DELETE USING(false)',item.table_name);
  EXECUTE format('CREATE TRIGGER employment_review_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',item.table_name);
 END LOOP;
END $migration$;
