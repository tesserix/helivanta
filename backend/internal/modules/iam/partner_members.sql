SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Evidence is append-only; forward repair preserves nominations and decisions.
CREATE TABLE iam_partner_members(
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,group_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(char_length(subject) BETWEEN 1 AND 200),staff_id uuid NOT NULL,
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'),
 status text NOT NULL CHECK(status IN('pending_review','approved','rejected','withdrawn','suspended')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,hospital_id,group_id,id),
 FOREIGN KEY(tenant_id,group_id)REFERENCES iam_partner_groups(tenant_id,id)ON DELETE RESTRICT,
 CHECK(document->>'id'=id::text AND document->>'group_id'=group_id::text AND document->>'hospital_id'=hospital_id::text AND document->>'status'=status AND document->>'version'=version::text AND document#>>'{input,subject}'=subject AND document->>'member_staff_id'=staff_id::text)
);
CREATE INDEX ON iam_partner_members(tenant_id,hospital_id,group_id,created_at DESC,id DESC);
CREATE UNIQUE INDEX iam_partner_member_subject_active ON iam_partner_members(tenant_id,group_id,subject)WHERE status='approved';
CREATE UNIQUE INDEX iam_partner_member_staff_active ON iam_partner_members(tenant_id,group_id,staff_id)WHERE status='approved';
CREATE TABLE iam_partner_group_cohorts(
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,group_id uuid NOT NULL,epoch bigint NOT NULL CHECK(epoch>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,group_id),FOREIGN KEY(tenant_id,group_id)REFERENCES iam_partner_groups(tenant_id,id)ON DELETE RESTRICT
);
CREATE TABLE iam_partner_member_commands(
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,group_id uuid NOT NULL,member_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 expected_version bigint NOT NULL CHECK(expected_version>0),result_version bigint NOT NULL CHECK(result_version=expected_version+1),
 result_status text NOT NULL CHECK(result_status IN('approved','rejected','withdrawn','suspended')),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,member_id,expected_version),
 FOREIGN KEY(tenant_id,hospital_id,group_id,member_id)REFERENCES iam_partner_members(tenant_id,hospital_id,group_id,id)ON DELETE RESTRICT
);
CREATE INDEX ON iam_partner_member_commands(tenant_id,hospital_id,group_id,member_id,result_version DESC);
DO $$DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['iam_partner_members','iam_partner_member_commands','iam_partner_group_cohorts']LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format($p$CREATE POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND hospital_id=NULLIF(current_setting('app.iam_partner_hospital',true),'')::uuid AND current_setting('app.iam_partner_members',true)='write') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND hospital_id=NULLIF(current_setting('app.iam_partner_hospital',true),'')::uuid AND current_setting('app.iam_partner_members',true)='write')$p$,tab);
 END LOOP;
END $$;
CREATE TRIGGER iam_partner_member_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_partner_member_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_partner_member_no_delete BEFORE DELETE OR TRUNCATE ON iam_partner_members FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_partner_cohort_no_delete BEFORE DELETE OR TRUNCATE ON iam_partner_group_cohorts FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE FUNCTION iam_partner_member_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m iam_partner_members%ROWTYPE;op text;actor text;actor_staff text;
BEGIN
 SELECT * INTO m FROM iam_partner_members WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND group_id=NEW.group_id AND id=NEW.member_id FOR UPDATE;
 op:=NEW.receipt#>>'{input,operation}';actor:=NEW.receipt->>'actor_subject';actor_staff:=NEW.receipt->>'actor_staff_id';
 IF NOT FOUND OR NEW.expected_version<>m.version
 OR actor IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NEW.receipt->>'id' IS DISTINCT FROM NEW.id::text
 OR NEW.receipt->>'member_id' IS DISTINCT FROM m.id::text
 OR NEW.receipt->>'result_version' IS DISTINCT FROM NEW.result_version::text
 OR NEW.receipt#>>'{member,id}' IS DISTINCT FROM m.id::text
 OR NEW.receipt#>>'{member,status}' IS DISTINCT FROM NEW.result_status
 OR NEW.receipt#>>'{member,version}' IS DISTINCT FROM NEW.result_version::text
 OR (NEW.receipt->'member')-'status'-'version'-'updated_at' IS DISTINCT FROM m.document-'status'-'version'-'updated_at'
 OR (op IN('approve','reject') AND (m.status<>'pending_review' OR actor=m.document->>'prepared_by' OR actor=m.subject OR actor_staff=m.staff_id::text OR actor_staff=m.document->>'prepared_by_staff_id'))
 OR (op='withdraw' AND (m.status<>'pending_review' OR actor IS DISTINCT FROM m.document->>'prepared_by' OR actor_staff IS DISTINCT FROM m.document->>'prepared_by_staff_id'))
 OR (op='suspend' AND m.status<>'approved')
 OR NEW.result_status IS DISTINCT FROM (CASE op WHEN 'approve' THEN 'approved' WHEN 'reject' THEN 'rejected' WHEN 'withdraw' THEN 'withdrawn' WHEN 'suspend' THEN 'suspended' ELSE '' END)
 THEN RAISE EXCEPTION 'invalid partner member transition' USING ERRCODE='23514',CONSTRAINT='iam_partner_member_command_guard';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_partner_member_command_guard BEFORE INSERT ON iam_partner_member_commands FOR EACH ROW EXECUTE FUNCTION iam_partner_member_command_guard();
CREATE FUNCTION iam_partner_member_projection_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending_review' OR NEW.version<>1 OR NEW.document->>'prepared_by' IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NEW.document->>'prepared_by_staff_id'=NEW.staff_id::text OR NEW.document#>>'{input,kind}' IS DISTINCT FROM 'subject' THEN
   RAISE EXCEPTION 'invalid partner member nomination' USING ERRCODE='23514';END IF;
 ELSE
  IF NEW.tenant_id<>OLD.tenant_id OR NEW.id<>OLD.id OR NEW.hospital_id<>OLD.hospital_id OR NEW.group_id<>OLD.group_id OR NEW.subject<>OLD.subject OR NEW.staff_id<>OLD.staff_id OR NEW.input_hash<>OLD.input_hash OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 OR NOT EXISTS(SELECT 1 FROM iam_partner_member_commands c WHERE c.tenant_id=NEW.tenant_id AND c.member_id=NEW.id AND c.expected_version=OLD.version AND c.result_version=NEW.version AND c.receipt->'member'=NEW.document)THEN
   RAISE EXCEPTION 'member projection requires immutable command' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_partner_member_projection_guard BEFORE INSERT OR UPDATE ON iam_partner_members FOR EACH ROW EXECUTE FUNCTION iam_partner_member_projection_guard();
