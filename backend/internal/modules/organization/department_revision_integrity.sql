SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION organization_department_revision_reached_head() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM organization_departments d
   WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id
     AND d.id=NEW.node_id AND d.version>=NEW.version
 ) THEN
  RAISE EXCEPTION 'department revision requires its head transition'
   USING ERRCODE='23514',CONSTRAINT='organization_department_revision_reached_head';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER organization_department_revision_reached_head
 AFTER INSERT ON organization_department_revisions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION organization_department_revision_reached_head();
