SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_supplier_grants(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,
 hospital_id uuid NOT NULL,relationship_id uuid NOT NULL,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL CHECK(updated_at>=created_at),
 PRIMARY KEY(tenant_id,id),
 CHECK(origin_record_id=id AND origin_tenant_id::text=document#>>'{scope,source_tenant_id}'),
 CHECK(COALESCE(document->>'owner_tenant_id'=tenant_id::text AND document->>'id'=id::text AND(document->>'version')::bigint=version AND document#>>'{scope,hospital_id}'=hospital_id::text AND document#>>'{input,relationship_id}'=relationship_id::text AND document->>'input_sha256'=input_hash,false)),
 CHECK(COALESCE((document->>'authority_epoch')::bigint BETWEEN 1 AND 9007199254740991 AND(document->>'remote_epoch')::bigint BETWEEN 0 AND 9007199254740991,false)),
 CHECK(COALESCE(document->>'status'IN('pending_review','offered','incoming','accepted_pending_ack','approved','rejected','suspended'),false)),
 CHECK(COALESCE((document->>'side'='source'AND document#>>'{scope,source_tenant_id}'=tenant_id::text)OR(document->>'side'='hospital'AND document#>>'{scope,recipient_tenant_id}'=tenant_id::text),false)),
 CHECK(COALESCE(document#>>'{scope,source_tenant_id}'<>document#>>'{scope,recipient_tenant_id}',false))
);
CREATE INDEX iam_supplier_grants_page ON iam_supplier_grants(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX iam_supplier_grants_relationship ON iam_supplier_grants(tenant_id,relationship_id,id);
CREATE INDEX iam_supplier_grants_source_page ON iam_supplier_grants(tenant_id,(document#>>'{scope,source_product}'),(document#>>'{scope,source_organization_id}'),(document#>>'{scope,source_location_id}'),created_at DESC,id DESC)WHERE document->>'side'='source';
CREATE TABLE iam_supplier_grant_commands(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),grant_id uuid NOT NULL,hospital_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,grant_id)REFERENCES iam_supplier_grants(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX iam_supplier_grant_commands_grant ON iam_supplier_grant_commands(tenant_id,grant_id,created_at DESC,id DESC);
CREATE TABLE iam_supplier_grant_history(
 tenant_id uuid NOT NULL,origin_tenant_id uuid NOT NULL,origin_record_id uuid NOT NULL,grant_id uuid NOT NULL,hospital_id uuid NOT NULL,
 version bigint NOT NULL CHECK(version>0),document jsonb NOT NULL,actor_subject text NOT NULL,source_event_id uuid,
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,grant_id,version),FOREIGN KEY(tenant_id,grant_id)REFERENCES iam_supplier_grants(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE iam_supplier_grant_bases(
 tenant_id uuid NOT NULL,grant_id uuid NOT NULL,hospital_id uuid NOT NULL,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'),basis_hash text NOT NULL CHECK(basis_hash ~ '^[a-f0-9]{64}$'),
 actor_subject text NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,grant_id),FOREIGN KEY(tenant_id,grant_id)REFERENCES iam_supplier_grants(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE iam_supplier_grant_projection_acks(
 tenant_id uuid NOT NULL,grant_id uuid NOT NULL,hospital_id uuid NOT NULL,
 projection_hash text NOT NULL CHECK(projection_hash ~ '^[a-f0-9]{64}$'),subject text NOT NULL CHECK(length(subject)BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK(version>0),authority_epoch bigint NOT NULL CHECK(authority_epoch>0),
 created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,grant_id,projection_hash),FOREIGN KEY(tenant_id,grant_id)REFERENCES iam_supplier_grants(tenant_id,id) ON DELETE RESTRICT
);
DO $$DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['iam_supplier_grants','iam_supplier_grant_commands','iam_supplier_grant_history','iam_supplier_grant_bases','iam_supplier_grant_projection_acks']LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format($p$CREATE POLICY tenant_isolation ON %I
   USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND(current_setting('app.iam_supplier_access',true)IN('source','observe','consume')OR(current_setting('app.iam_supplier_access',true)='hospital'AND hospital_id=NULLIF(current_setting('app.iam_supplier_hospital',true),'')::uuid)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND(current_setting('app.iam_supplier_access',true)IN('source','observe')OR(current_setting('app.iam_supplier_access',true)='hospital'AND hospital_id=NULLIF(current_setting('app.iam_supplier_hospital',true),'')::uuid)))$p$,tab);
 END LOOP;
END $$;
DROP POLICY tenant_isolation ON iam_supplier_grant_bases;
CREATE POLICY tenant_isolation ON iam_supplier_grant_bases USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_supplier_access',true)='hospital'AND hospital_id=NULLIF(current_setting('app.iam_supplier_hospital',true),'')::uuid)WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_supplier_access',true)='hospital'AND hospital_id=NULLIF(current_setting('app.iam_supplier_hospital',true),'')::uuid);
DROP POLICY tenant_isolation ON iam_supplier_grant_projection_acks;
CREATE POLICY tenant_isolation ON iam_supplier_grant_projection_acks USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_supplier_access',true)IN('source','consume'))WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_supplier_access',true)='consume');
CREATE TRIGGER iam_supplier_grant_commands_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_supplier_grant_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_supplier_grant_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_supplier_grant_history FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_supplier_grant_bases_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_supplier_grant_bases FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE TRIGGER iam_supplier_grant_acks_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_supplier_grant_projection_acks FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
CREATE FUNCTION iam_supplier_grant_preserve()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'supplier grant evidence is retained' USING ERRCODE='23514';END IF;
 IF(NEW.tenant_id,NEW.id,NEW.origin_tenant_id,NEW.origin_record_id,NEW.hospital_id,NEW.relationship_id,NEW.input_hash,NEW.created_at)IS DISTINCT FROM(OLD.tenant_id,OLD.id,OLD.origin_tenant_id,OLD.origin_record_id,OLD.hospital_id,OLD.relationship_id,OLD.input_hash,OLD.created_at)
 OR NEW.document->'scope'IS DISTINCT FROM OLD.document->'scope'OR NEW.document->'input'IS DISTINCT FROM OLD.document->'input'OR NEW.document->>'prepared_by'IS DISTINCT FROM OLD.document->>'prepared_by'
 OR NEW.version<>OLD.version+1 OR(NEW.document->>'authority_epoch')::bigint<(OLD.document->>'authority_epoch')::bigint OR(NEW.document->>'remote_epoch')::bigint<(OLD.document->>'remote_epoch')::bigint
 OR(OLD.document->>'status'IN('rejected','suspended')AND NEW.document->>'status'IS DISTINCT FROM OLD.document->>'status')THEN RAISE EXCEPTION 'immutable supplier grant or stale revision' USING ERRCODE='23514';END IF;
 RETURN NEW;END $$;
CREATE TRIGGER iam_supplier_grant_preserve BEFORE UPDATE OR DELETE ON iam_supplier_grants FOR EACH ROW EXECUTE FUNCTION iam_supplier_grant_preserve();
CREATE TRIGGER iam_supplier_grant_audit AFTER INSERT OR UPDATE ON iam_supplier_grants FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER iam_supplier_grant_command_audit AFTER INSERT ON iam_supplier_grant_commands FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TRIGGER iam_supplier_grant_no_truncate BEFORE TRUNCATE ON iam_supplier_grants FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
