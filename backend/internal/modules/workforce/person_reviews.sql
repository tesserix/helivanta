SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_document_contributors (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,document_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,document_id,subject),
 FOREIGN KEY(tenant_id,hospital_id,document_id) REFERENCES workforce_documents(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE FUNCTION workforce_document_contributor_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text:=NULLIF(current_setting('app.workforce_subject',true),'');
BEGIN
 IF NEW.record_kind<>'enrollment' OR NEW.category<>'identity_evidence' THEN RETURN NULL; END IF;
 IF workforce_document_worker_allowed(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) IS TRUE THEN RETURN NULL; END IF;
 IF actor IS NULL THEN RAISE EXCEPTION 'document contributor requires attributable owner command' USING ERRCODE='42501'; END IF;
 INSERT INTO workforce_document_contributors(tenant_id,hospital_id,staff_id,document_id,subject)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,actor) ON CONFLICT DO NOTHING;
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_document_contributor_record AFTER INSERT OR UPDATE ON workforce_documents
 FOR EACH ROW EXECUTE FUNCTION workforce_document_contributor_record();
CREATE FUNCTION workforce_document_contributor_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN
  RAISE EXCEPTION 'document provenance is immutable and owner-maintained' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_document_contributor_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_document_contributors
 FOR EACH ROW EXECUTE FUNCTION workforce_document_contributor_guard();

CREATE FUNCTION workforce_person_review_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND COALESCE(current_setting('app.workforce_person_review',true) IN ('read','manage','review'),false)
$$;
CREATE FUNCTION workforce_person_review_file_context(row_tenant uuid,row_hospital uuid,row_file uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_documents d WHERE (d.tenant_id,d.hospital_id,d.id)=(row_tenant,row_hospital,row_file)
 AND workforce_person_review_context(d.tenant_id,d.hospital_id,d.staff_id)
 AND d.record_kind='enrollment' AND d.record_id=d.staff_id AND d.category='identity_evidence'
 AND d.id::text=ANY(string_to_array(current_setting('app.workforce_person_review_documents',true),',')))
$$;

CREATE TABLE workforce_person_reviews (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 revision_id uuid NOT NULL CHECK(substring(revision_id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740990),
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','withdrawn','revoked')),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740990),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 1 AND 9007199254740990),
 identity_hash text NOT NULL CHECK(identity_hash ~ '^[a-f0-9]{64}$'),
 policy_reference text NOT NULL CHECK(length(btrim(policy_reference)) BETWEEN 1 AND 300),
 finding_reference text NOT NULL CHECK(length(btrim(finding_reference)) BETWEEN 1 AND 500),
 observed_on date NOT NULL,valid_until timestamptz NOT NULL CHECK(isfinite(valid_until)),
 access_intent text NOT NULL CHECK(access_intent IN ('digital','non_digital')),
 declared_subject text NOT NULL CHECK(length(declared_subject)<=200),
 identity_examined boolean NOT NULL CHECK(identity_examined),
 known_associations_disclosed boolean NOT NULL CHECK(known_associations_disclosed),
 proposed_by_subject text NOT NULL CHECK(length(proposed_by_subject) BETWEEN 1 AND 200),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 decision_reason text NOT NULL DEFAULT '' CHECK(length(decision_reason)<=500),
 reviewer_identity_examined boolean NOT NULL DEFAULT false,
 independent_person_confirmed boolean NOT NULL DEFAULT false,
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 submission_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),UNIQUE(tenant_id,hospital_id,staff_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK(valid_until > observed_on::timestamp AT TIME ZONE 'UTC'),
 CHECK((access_intent='non_digital' AND declared_subject='') OR (access_intent='digital' AND length(declared_subject)>0)),
 CHECK(status<>'approved' OR (reviewer_identity_examined AND independent_person_confirmed AND length(btrim(decision_reason))>0))
);
CREATE INDEX workforce_person_reviews_page ON workforce_person_reviews(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE TABLE workforce_person_review_history(LIKE workforce_person_reviews INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE workforce_person_review_history ADD PRIMARY KEY(tenant_id,hospital_id,id,version);
ALTER TABLE workforce_person_review_history ADD FOREIGN KEY(tenant_id,hospital_id,staff_id,id)
 REFERENCES workforce_person_reviews(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX workforce_person_review_command ON workforce_person_review_history(tenant_id,hospital_id,revision_id);
CREATE INDEX workforce_person_review_history_page ON workforce_person_review_history(tenant_id,hospital_id,staff_id,id,updated_at DESC,revision_id DESC);

CREATE TABLE workforce_person_review_evidence (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,review_id uuid NOT NULL,document_id uuid NOT NULL,
 record_version bigint NOT NULL CHECK(record_version>0),filename text NOT NULL,content_type text NOT NULL,
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 10485760),sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 source_generation bigint NOT NULL CHECK(source_generation>0),released_generation bigint NOT NULL CHECK(released_generation>0),
 scan_attempt bigint NOT NULL CHECK(scan_attempt>0),scanned_at timestamptz NOT NULL,
 engine_version text NOT NULL,definition_version bigint NOT NULL CHECK(definition_version>0),definitions_updated_at timestamptz NOT NULL,
 classification_version bigint NOT NULL CHECK(classification_version>=0),classification_revision_id text NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,review_id,document_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,review_id) REFERENCES workforce_person_reviews(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,document_id) REFERENCES workforce_documents(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_person_review_contributors (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,review_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 PRIMARY KEY(tenant_id,hospital_id,review_id,subject),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,review_id) REFERENCES workforce_person_reviews(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION workforce_person_review_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE mode text:=current_setting('app.workforce_person_review',true);
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') THEN
  RAISE EXCEPTION 'person review requires current attributable authority' USING ERRCODE='42501';
 END IF;
 IF TG_OP='INSERT' THEN
  IF mode IS DISTINCT FROM 'manage' OR NEW.status<>'pending' OR NEW.version<>1
    OR NEW.proposed_by_subject<>NEW.actor_subject OR NEW.decision_reason<>''
    OR NEW.reviewer_identity_examined OR NEW.independent_person_confirmed THEN
   RAISE EXCEPTION 'person review starts as an attributable pending finding' USING ERRCODE='23514';
  END IF;
  NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;
  NEW.submission_xid:=pg_current_xact_id();
 ELSE
  IF NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id OR
   (to_jsonb(NEW)-ARRAY['version','revision_id','status','actor_subject','decision_reason','reviewer_identity_examined','independent_person_confirmed','input_hash','updated_at'])
   IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['version','revision_id','status','actor_subject','decision_reason','reviewer_identity_examined','independent_person_confirmed','input_hash','updated_at']) THEN
   RAISE EXCEPTION 'person review source is immutable and revisions advance once' USING ERRCODE='23514';
  END IF;
  IF NOT ((mode='review' AND OLD.status='pending' AND NEW.status IN ('approved','rejected'))
    OR (mode='manage' AND OLD.status='pending' AND NEW.status='withdrawn' AND NEW.actor_subject=OLD.proposed_by_subject)
    OR (mode='review' AND OLD.status='approved' AND NEW.status='revoked')) THEN
   RAISE EXCEPTION 'invalid person review transition' USING ERRCODE='23514';
  END IF;
  IF length(btrim(NEW.decision_reason))=0 THEN RAISE EXCEPTION 'person review transition requires a reason' USING ERRCODE='23514'; END IF;
  IF NEW.status IN ('approved','rejected') AND (NEW.actor_subject IN (OLD.proposed_by_subject,OLD.declared_subject)
   OR EXISTS(SELECT 1 FROM workforce_person_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.review_id,c.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.actor_subject))
   OR EXISTS(SELECT 1 FROM workforce_login_links l WHERE (l.tenant_id,l.hospital_id,l.staff_id,l.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.actor_subject))) THEN
   RAISE EXCEPTION 'person review requires independent examination' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 IF NEW.status IN ('pending','approved') AND (NEW.valid_until<=clock_timestamp() OR NEW.observed_on>(clock_timestamp() AT TIME ZONE 'UTC')::date
  OR NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id,s.employment_epoch)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.employment_epoch)
    AND (TG_OP<>'INSERT' OR s.version=NEW.staff_version))) THEN
  RAISE EXCEPTION 'person review source or validity changed' USING ERRCODE='23514',CONSTRAINT='workforce_person_review_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_person_review_write BEFORE INSERT OR UPDATE ON workforce_person_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_person_review_write();
CREATE FUNCTION workforce_person_review_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO workforce_person_review_history SELECT NEW.*;RETURN NULL;END $$;
CREATE TRIGGER workforce_person_review_snapshot AFTER INSERT OR UPDATE ON workforce_person_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_person_review_snapshot();
CREATE FUNCTION workforce_person_review_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN RAISE EXCEPTION 'person review history is immutable and owner-maintained' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_person_review_history_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_person_review_history
 FOR EACH ROW EXECUTE FUNCTION workforce_person_review_history_guard();
CREATE FUNCTION workforce_person_review_child_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR current_setting('app.workforce_person_review',true) IS DISTINCT FROM 'manage'
  OR NOT EXISTS(SELECT 1 FROM workforce_person_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.id,r.status,r.version,r.actor_subject)
   =(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.review_id,'pending',1,NULLIF(current_setting('app.workforce_subject',true),''))
   AND r.submission_xid=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'person review evidence and contributors are frozen' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_person_review_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_person_review_evidence FOR EACH ROW EXECUTE FUNCTION workforce_person_review_child_guard();
CREATE TRIGGER workforce_person_review_contributor_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_person_review_contributors FOR EACH ROW EXECUTE FUNCTION workforce_person_review_child_guard();
CREATE FUNCTION workforce_person_review_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total bigint;
BEGIN
 SELECT count(*) INTO total FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF total NOT BETWEEN 1 AND 10 OR NOT EXISTS(SELECT 1 FROM workforce_person_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.review_id,c.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.proposed_by_subject)) THEN
  RAISE EXCEPTION 'person review requires frozen evidence and contributor provenance' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_person_review_complete AFTER INSERT ON workforce_person_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_person_review_complete();
CREATE TRIGGER workforce_person_reviews_no_delete BEFORE DELETE OR TRUNCATE ON workforce_person_reviews FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

DO $migration$
DECLARE item record;prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('workforce_documents','(workforce_person_review_context(tenant_id,hospital_id,staff_id) AND record_kind=''enrollment'' AND record_id=staff_id AND category=''identity_evidence'' AND id::text=ANY(string_to_array(current_setting(''app.workforce_person_review_documents'',true),'','')))'),
  ('workforce_login_links','workforce_person_review_context(tenant_id,hospital_id,staff_id)'),
  ('workforce_document_classifications','workforce_person_review_file_context(tenant_id,hospital_id,id)'),
  ('workforce_document_classification_revisions','workforce_person_review_file_context(tenant_id,hospital_id,file_id)')
 ) AS scope(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  EXECUTE format('CREATE POLICY person_review_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR %s)',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
 FOR item IN SELECT * FROM (VALUES
  ('workforce_person_reviews'),('workforce_person_review_history'),('workforce_person_review_evidence'),('workforce_person_review_contributors'),('workforce_document_contributors')
 ) AS private(table_name) LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',item.table_name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',item.table_name);
  IF item.table_name='workforce_document_contributors' THEN
   EXECUTE format('CREATE POLICY person_review_owner ON %I USING (hms_tenant_visible(tenant_id) AND (workforce_person_review_file_context(tenant_id,hospital_id,document_id) OR workforce_enrollment_allowed(tenant_id,hospital_id))) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id))',item.table_name);
  ELSE
   EXECUTE format('CREATE POLICY person_review_owner ON %I USING (hms_tenant_visible(tenant_id) AND workforce_person_review_context(tenant_id,hospital_id,staff_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_person_review_context(tenant_id,hospital_id,staff_id) AND current_setting(''app.workforce_person_review'',true) IN (''manage'',''review''))',item.table_name);
  END IF;
  EXECUTE format('CREATE POLICY person_review_no_delete ON %I AS RESTRICTIVE FOR DELETE USING(false)',item.table_name);
  EXECUTE format('CREATE TRIGGER person_review_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',item.table_name);
 END LOOP;
END $migration$;
