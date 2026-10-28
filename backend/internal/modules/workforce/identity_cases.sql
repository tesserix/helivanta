SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION workforce_identity_case_context(row_tenant uuid, row_hospital uuid, row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT workforce_identity_source_context(row_tenant,row_hospital,row_staff)
        AND current_setting('app.workforce_identity_case',true) IN ('read','manage')
$$;

CREATE TABLE workforce_identity_cases (
    tenant_id uuid NOT NULL,
    hospital_id uuid NOT NULL,
    staff_id uuid NOT NULL,
    id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
    revision_id uuid NOT NULL CHECK (substring(revision_id::text,15,1)='7'),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    status text NOT NULL CHECK (status IN ('draft','pending','approved','rejected','withdrawn','revoked')),
    kind text NOT NULL CHECK (kind IN ('identity','background','photograph')),
    policy_reference text NOT NULL CHECK (length(btrim(policy_reference)) BETWEEN 1 AND 300),
    check_reference text NOT NULL CHECK (length(btrim(check_reference)) BETWEEN 1 AND 500),
    observed_on date NOT NULL,
    valid_until timestamptz NOT NULL CHECK (isfinite(valid_until)),
    identity_hash text NOT NULL CHECK (identity_hash ~ '^[a-f0-9]{64}$'),
    employment_epoch bigint NOT NULL CHECK (employment_epoch BETWEEN 1 AND 9007199254740991),
    proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
    actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200),
    input_hash text NOT NULL CHECK (input_hash ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (valid_until > observed_on::timestamp AT TIME ZONE 'UTC'),
    PRIMARY KEY(tenant_id,hospital_id,id),
    UNIQUE(tenant_id,hospital_id,staff_id,id),
    FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_identity_cases_page ON workforce_identity_cases(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE TABLE workforce_identity_case_history (LIKE workforce_identity_cases INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE workforce_identity_case_history ADD PRIMARY KEY(tenant_id,hospital_id,id,version);
ALTER TABLE workforce_identity_case_history ADD FOREIGN KEY(tenant_id,hospital_id,staff_id,id)
    REFERENCES workforce_identity_cases(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX workforce_identity_case_command ON workforce_identity_case_history(tenant_id,hospital_id,revision_id);
CREATE INDEX workforce_identity_case_history_page ON workforce_identity_case_history(tenant_id,hospital_id,staff_id,id,updated_at DESC);

CREATE FUNCTION workforce_identity_case_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('app.workforce_identity_case',true) IS DISTINCT FROM 'manage'
        OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'')
        OR NEW.status <> 'draft' THEN
        RAISE EXCEPTION 'identity case draft requires current manager authority' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.version<>1 OR NEW.proposed_by_subject<>NEW.actor_subject THEN
            RAISE EXCEPTION 'identity case starts as attributable draft' USING ERRCODE='23514';
        END IF;
        NEW.created_at:=clock_timestamp(); NEW.updated_at:=NEW.created_at;
    ELSE
        IF OLD.status<>'draft' OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id
            OR (to_jsonb(NEW)-ARRAY['revision_id','version','policy_reference','check_reference','observed_on','valid_until','identity_hash','employment_epoch','actor_subject','input_hash','updated_at'])
            IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['revision_id','version','policy_reference','check_reference','observed_on','valid_until','identity_hash','employment_epoch','actor_subject','input_hash','updated_at']) THEN
            RAISE EXCEPTION 'identity case identity is immutable and revisions advance once' USING ERRCODE='23514';
        END IF;
        NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
    END IF;
    IF NEW.observed_on > (clock_timestamp() AT TIME ZONE 'UTC')::date OR NOT EXISTS (
        SELECT 1 FROM workforce_staff WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id
            AND id=NEW.staff_id AND employment_status='active' AND employment_epoch=NEW.employment_epoch
    ) THEN
        RAISE EXCEPTION 'identity case requires current active employment source' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workforce_identity_case_write BEFORE INSERT OR UPDATE ON workforce_identity_cases
    FOR EACH ROW EXECUTE FUNCTION workforce_identity_case_write();

CREATE FUNCTION workforce_identity_case_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO workforce_identity_case_history SELECT NEW.*;
    RETURN NULL;
END $$;
CREATE TRIGGER workforce_identity_case_snapshot AFTER INSERT OR UPDATE ON workforce_identity_cases
    FOR EACH ROW EXECUTE FUNCTION workforce_identity_case_snapshot();

CREATE FUNCTION workforce_identity_case_history_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN
        RAISE EXCEPTION 'identity case history is owner-maintained and immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workforce_identity_case_history_write BEFORE INSERT OR UPDATE OR DELETE ON workforce_identity_case_history
    FOR EACH ROW EXECUTE FUNCTION workforce_identity_case_history_write();

ALTER TABLE workforce_identity_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_identity_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY identity_cases ON workforce_identity_cases
    USING (hms_tenant_visible(tenant_id) AND workforce_identity_case_context(tenant_id,hospital_id,staff_id))
    WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_identity_case_context(tenant_id,hospital_id,staff_id) AND current_setting('app.workforce_identity_case',true)='manage');
CREATE POLICY identity_cases_no_delete ON workforce_identity_cases AS RESTRICTIVE FOR DELETE USING (false);

ALTER TABLE workforce_identity_case_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_identity_case_history FORCE ROW LEVEL SECURITY;
CREATE POLICY identity_case_history ON workforce_identity_case_history
    USING (hms_tenant_visible(tenant_id) AND workforce_identity_case_context(tenant_id,hospital_id,staff_id))
    WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_identity_case_context(tenant_id,hospital_id,staff_id) AND current_setting('app.workforce_identity_case',true)='manage');
CREATE POLICY identity_case_history_no_delete ON workforce_identity_case_history AS RESTRICTIVE FOR DELETE USING (false);
