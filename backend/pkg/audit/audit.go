// Package audit owns the append-only change trail (#54): tables opt in with an
// AFTER trigger calling audit_record_change(), which records the actor
// tenantdb.WithTenant stamps and rejects writes that carry none.
package audit

import (
	"context"
	"log/slog"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// LogKey marks a log record as an audit event; filter on it for retention and SIEM export.
const LogKey = "audit_event"

// Log tags an authentication or session event to the audit log stream.
func Log(ctx context.Context, logger *slog.Logger, level slog.Level, event, msg string, attrs ...any) {
	logger.Log(ctx, level, msg, append([]any{LogKey, event}, attrs...)...)
}

func Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_audit_events",
		// Changes records column names only, never values, so no patient data reaches the audit store.
		SQL: `
			CREATE TABLE audit_events (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  actor_subject text NOT NULL CHECK (length(actor_subject) > 0),
			  action text NOT NULL,
			  resource_type text NOT NULL,
			  resource_id uuid NOT NULL,
			  changed_fields text[] NOT NULL DEFAULT '{}',
			  occurred_at timestamptz NOT NULL DEFAULT now()
			);
			CREATE INDEX ON audit_events (tenant_id, occurred_at DESC, id DESC);
			CREATE INDEX ON audit_events (tenant_id, resource_type, resource_id);
			ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
			ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON audit_events
			  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
			  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

			CREATE FUNCTION audit_events_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			  RAISE EXCEPTION 'audit events are append only' USING ERRCODE = '23514';
			END $$;
			CREATE TRIGGER audit_events_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_events
			  FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable();

			CREATE FUNCTION audit_record_change() RETURNS trigger LANGUAGE plpgsql AS $$
			DECLARE
			  actor text := NULLIF(current_setting('app.actor_subject', true), '');
			  rec jsonb := CASE WHEN TG_OP = 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
			  fields text[] := '{}';
			BEGIN
			  IF actor IS NULL THEN
			    RAISE EXCEPTION 'audit actor not set for % on %', TG_OP, TG_TABLE_NAME USING ERRCODE = '42501';
			  END IF;
			  IF TG_OP = 'UPDATE' THEN
			    SELECT COALESCE(array_agg(n.key ORDER BY n.key), '{}') INTO fields
			      FROM jsonb_each(to_jsonb(NEW)) n
			     WHERE n.value IS DISTINCT FROM to_jsonb(OLD)->n.key;
			  END IF;
			  INSERT INTO audit_events (tenant_id, actor_subject, action, resource_type, resource_id, changed_fields)
			  VALUES ((rec->>'tenant_id')::uuid, actor, lower(TG_OP), TG_TABLE_NAME, (rec->>'id')::uuid, fields);
			  RETURN NULL;
			END $$;`,
	}}
}
