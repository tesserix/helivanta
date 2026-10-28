SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_staff ADD COLUMN employment_epoch bigint NOT NULL DEFAULT 1
    CHECK (employment_epoch BETWEEN 1 AND 9007199254740991);

CREATE FUNCTION workforce_staff_employment_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.employment_epoch <> 1 THEN
            RAISE EXCEPTION 'invalid initial employment epoch' USING ERRCODE = '23514';
        END IF;
    ELSE
        IF NEW.employment_epoch <> OLD.employment_epoch THEN
            RAISE EXCEPTION 'employment epoch is maintained by owner' USING ERRCODE = '23514';
        END IF;
        IF NEW.employment_status IS DISTINCT FROM OLD.employment_status THEN
            NEW.employment_epoch := OLD.employment_epoch + 1;
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_employment_epoch BEFORE INSERT OR UPDATE ON workforce_staff
    FOR EACH ROW EXECUTE FUNCTION workforce_staff_employment_epoch();

CREATE FUNCTION workforce_identity_source_context(row_tenant uuid, row_hospital uuid, row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT COALESCE(hms_tenant_visible(row_tenant)
        AND row_tenant = NULLIF(current_setting('app.tenant_id',true),'')::uuid
        AND row_hospital = NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
        AND row_staff = NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid
        AND current_setting('app.workforce_identity_source',true) = 'authorized'
        AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL, false)
$$;
DO $migration$
DECLARE item record; prior record;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('workforce_staff','id'),
        ('workforce_enrollment_profile','staff_id')
    ) AS scopes(table_name,staff_column) LOOP
        SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression
        INTO STRICT prior FROM pg_policy p
        WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass
            AND p.polpermissive;
        EXECUTE format('CREATE POLICY identity_source_delete_boundary ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
        EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR workforce_identity_source_context(tenant_id,hospital_id,%I))',
            prior.polname,item.table_name,prior.read_expression,item.staff_column);
    END LOOP;
END $migration$;
