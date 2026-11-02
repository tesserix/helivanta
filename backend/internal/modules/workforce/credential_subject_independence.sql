CREATE OR REPLACE FUNCTION workforce_credential_subject_conflict(t uuid, h uuid, s uuid, actor text)
RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE prior_scope text; conflict boolean;
BEGIN
    IF t IS DISTINCT FROM NULLIF(current_setting('app.tenant_id', true), '')::uuid
       OR h IS DISTINCT FROM NULLIF(current_setting('app.workforce_hospital', true), '')::uuid
       OR actor IS NULL OR actor = '' THEN
        RAISE EXCEPTION 'credential subject scope is unavailable'
            USING ERRCODE='23514', CONSTRAINT='credential_review_subject_scope';
    END IF;
    PERFORM 1 FROM workforce_staff WHERE tenant_id=t AND hospital_id=h AND id=s FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'credential Staff scope is unavailable'
            USING ERRCODE='23514', CONSTRAINT='credential_review_subject_scope';
    END IF;
    prior_scope := current_setting('app.workforce_login_link_access', true);
    PERFORM set_config('app.workforce_login_link_access', 'authorized', true);
    SELECT EXISTS(SELECT 1 FROM workforce_login_links
        WHERE tenant_id=t AND hospital_id=h AND staff_id=s AND subject=actor) INTO conflict;
    PERFORM set_config('app.workforce_login_link_access', COALESCE(prior_scope, ''), true);
    RETURN conflict;
END $$;

CREATE OR REPLACE FUNCTION workforce_credential_subject_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.decided_by_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject', true), '') THEN
        RAISE EXCEPTION 'credential decision actor must be authenticated'
            USING ERRCODE='23514', CONSTRAINT='credential_review_actor';
    END IF;
    IF NEW.status IN ('approved', 'rejected')
       AND workforce_credential_subject_conflict(NEW.tenant_id, NEW.hospital_id, NEW.staff_id, NEW.decided_by_subject) THEN
        RAISE EXCEPTION 'credential subject cannot decide own review'
            USING ERRCODE='23514', CONSTRAINT='credential_review_subject_independent';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_subject_independent
    BEFORE UPDATE ON workforce_credential_reviews
    FOR EACH ROW EXECUTE FUNCTION workforce_credential_subject_guard();

CREATE OR REPLACE FUNCTION workforce_credential_link_subject_lock()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM workforce_staff
        WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.staff_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'login-link Staff scope is unavailable'
            USING ERRCODE='23514', CONSTRAINT='credential_review_subject_scope';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_link_subject_lock
    BEFORE INSERT ON workforce_login_links
    FOR EACH ROW EXECUTE FUNCTION workforce_credential_link_subject_lock();
