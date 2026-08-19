// Command bootstrap grants one role to one subject in one tenant by
// writing a single iam_members row, and exits. It exists for exactly one
// situation: the first tenant_admin of a fresh deployment (#893). Every
// later grant goes through the HTTP route, which requires
// iam.member.manage — a permission only an existing member holds, so the
// first member cannot be created that way.
//
// # Why a binary and not a documented INSERT
//
// Because an INSERT cannot validate the role, and an unvalidated role
// fails SILENTLY. platform.applyGrants (internal/platform/reconcile.go)
// skips any iam_members row whose role_key is not a known authz.Role
// with a slog.Warn, and leaves it out of the desired tuple set — so a
// hand-typed `tenant_admn` produces a row that looks correct in psql,
// reconciles to nothing, and hands the operator an account that signs in
// successfully and then sees an empty application. The failure surfaces
// nowhere near its cause. authz.KnownRole below is the whole point of
// this program; the database write is the easy part.
//
// The tenant is checked as a UUID for the same class of reason: there is
// no tenants table (#893), so tenant_id is a value nothing provisions and
// nothing looks up. Be honest about the limit of that check — the column
// is uuid, so Postgres already rejects non-UUID TEXT (SQLSTATE 22P02);
// what this adds is a named flag in the message instead of a raw driver
// error, and a refusal that happens before any write is attempted.
// Neither check can catch the mistake that actually matters: a
// well-formed but WRONG UUID is indistinguishable from a right one, and
// silently becomes a separate, empty tenant. Only recording the chosen
// tenant UUID somewhere durable fixes that.
//
// Out of scope, deliberately: creating the Zitadel human user. That
// needs a management PAT and is a separate one-time human act; -subject
// takes the user ID it produces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/logging"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	if err := run(); err != nil {
		slog.Error("bootstrap failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Named for the three columns of the row being written
	// (tenant_id, subject, role_key), so the command line reads as the row
	// it produces. -subject rather than -user or -email: it is the Zitadel
	// subject claim of an account that must already exist, and a name
	// suggesting an address would invite input this program cannot use.
	tenantID := flag.String("tenant", "", "tenant UUID to grant membership in")
	subject := flag.String("subject", "", "Zitadel subject (user ID) of an existing account")
	role := flag.String("role", string(authz.RoleTenantAdmin), "role key to grant")
	flag.Parse()

	cfg := config.Load()
	slog.SetDefault(logging.New(cfg.LogLevel))
	ctx := context.Background()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	inserted, err := grant(ctx, db, *tenantID, *subject, authz.Role(*role))
	if err != nil {
		return err
	}
	// The audit line #893 asks for: a direct INSERT leaves no trace beyond
	// the row, and this is the highest-privilege act in the system. It
	// reports what actually happened — "existed" is not "granted", and
	// printing "granted" for a no-op would tell an operator their fix
	// landed when it landed on a previous run.
	outcome := "already existed"
	if inserted {
		outcome = "granted"
	}
	slog.Info("bootstrap membership "+outcome,
		"tenant_id", *tenantID, "subject", *subject, "role_key", *role, "inserted", inserted)
	return nil
}

// grant validates its inputs and then writes the membership row,
// reporting whether the row was new. Validation happens BEFORE the write
// and every failure returns before touching the database, so a rejected
// role or tenant leaves nothing behind to be found later and mistaken
// for a real grant.
func grant(ctx context.Context, db *tenantdb.DB, tenantID, subject string, role authz.Role) (bool, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return false, fmt.Errorf("-tenant %q is not a UUID: %w", tenantID, err)
	}
	if strings.TrimSpace(subject) == "" {
		return false, errors.New("-subject is required (the Zitadel user ID of an existing account)")
	}
	if !authz.KnownRole(role) {
		valid := make([]string, 0, len(authz.SystemRoles()))
		for _, r := range authz.SystemRoles() {
			valid = append(valid, string(r))
		}
		return false, fmt.Errorf("-role %q is not a known role: valid roles are %s", role, strings.Join(valid, ", "))
	}

	// WithTenant, NOT WithAdmin — and this is the one thing about this
	// binary worth reading before changing it.
	//
	// iam_members is FORCE ROW LEVEL SECURITY with a policy of
	// `tenant_id = current_setting('app.tenant_id', true)::uuid`. FORCE
	// means the policy binds the table OWNER too, so "connect as the admin
	// role" does not evade it. An earlier revision used WithAdmin on the
	// theory that the admin role holds BYPASSRLS; against production that
	// is false and the insert is refused outright:
	//
	//   ERROR: new row violates row-level security policy for table
	//   "iam_members" (SQLSTATE 42501)
	//
	// Checked against the live cluster: role `helivanta` has
	// rolsuper=false, rolbypassrls=false. Only `postgres` bypasses, and
	// bootstrapping as the superuser to sidestep a tenancy control would
	// be exactly the wrong instinct.
	//
	// WithTenant sets app.tenant_id to the tenant being written, so the
	// policy is SATISFIED rather than bypassed — the write is scoped to
	// the one tenant this command names, and a row for any other tenant is
	// impossible by construction. That is a stronger guarantee than the
	// admin pool offered, so this is the correct pool on the merits, not a
	// workaround.
	var inserted bool
	err := db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		res := tx.Exec(`
			INSERT INTO iam_members (tenant_id, subject, role_key)
			VALUES (?, ?, ?)
			ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
			tenantID, subject, string(role))
		inserted = res.RowsAffected == 1
		return res.Error
	})
	if err != nil {
		return false, fmt.Errorf("insert membership: %w", err)
	}
	return inserted, nil
}
