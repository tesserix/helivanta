package testinfra

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
)

// Owner selects which role owns a migrated database's schema.
type Owner int

const (
	// HarnessOwner is hms_owner, the harness's non-superuser owner.
	HarnessOwner Owner = iota
	// ProductionOwner is helivanta, CNPG's owner name.
	ProductionOwner
)

// TemplateBuild migrates a template through its (app, admin, system) DSNs and closes every pool it opened.
type TemplateBuild func(app, admin, system string) error

type templateKey struct {
	owner Owner
	key   string
}

type template struct {
	once sync.Once
	name string
	err  error
}

// templates holds one migrated template per (owner, key) for this test binary.
var templates sync.Map

// StartMigratedPostgres returns DSNs for a fresh clone of the template build produced for key (#1620).
func StartMigratedPostgres(t *testing.T, owner Owner, key string, build TemplateBuild) (string, string, string) {
	t.Helper()
	name, err := migratedDatabase(t, owner, key, build)
	if err != nil {
		t.Fatal(err)
	}
	return databaseDSNs(owner, name)
}

func migratedDatabase(t *testing.T, owner Owner, key string, build TemplateBuild) (string, error) {
	t.Helper()
	sharedPostgres(t)
	v, _ := templates.LoadOrStore(templateKey{owner, key}, &template{})
	tpl := v.(*template)
	tpl.once.Do(func() { tpl.build(owner, build) })
	if tpl.err != nil {
		return "", tpl.err
	}
	ctx := context.Background()
	name := fmt.Sprintf("hms_test_%d", pgSeq.Add(1))
	if err := runSQL(ctx, "hms", `CREATE DATABASE `+name+` TEMPLATE `+tpl.name+` OWNER `+ownerRole(owner)+`;`); err != nil {
		return "", fmt.Errorf("clone %s from %s: %w%s", name, tpl.name, err, pgForensics(ctx, name))
	}
	dropOnCleanup(t, name)
	return name, nil
}

// build fails closed: the error is set before build runs, so a panic or FailNow leaves it.
func (tpl *template) build(owner Owner, build TemplateBuild) {
	ctx := context.Background()
	tpl.name = fmt.Sprintf("hms_test_template_%d", pgSeq.Add(1))
	tpl.err = fmt.Errorf("template %s did not finish building", tpl.name)
	if err := prepareDatabase(ctx, tpl.name); err != nil {
		tpl.err = err
		return
	}
	if owner == ProductionOwner {
		if err := ownByProduction(ctx, tpl.name); err != nil {
			tpl.err = err
			return
		}
	}
	app, admin, system := databaseDSNs(owner, tpl.name)
	if err := build(app, admin, system); err != nil {
		tpl.err = fmt.Errorf("build template %s: %w", tpl.name, err)
		return
	}
	// CREATE DATABASE copies the database's contents, not its pg_database ACL, settings or comment.
	if err := runSQL(ctx, "hms", `DO $$
DECLARE d oid := (SELECT oid FROM pg_database WHERE datname = '`+tpl.name+`');
BEGIN
 IF EXISTS (SELECT 1 FROM pg_db_role_setting WHERE setdatabase = d) THEN
  RAISE EXCEPTION 'template % has a database setting, which a clone does not inherit', '`+tpl.name+`';
 END IF;
 IF (SELECT datacl FROM pg_database WHERE oid = d) IS NOT NULL THEN
  RAISE EXCEPTION 'template % has database privileges, which a clone does not inherit', '`+tpl.name+`';
 END IF;
 IF EXISTS (SELECT 1 FROM pg_shdescription WHERE classoid = 'pg_database'::regclass AND objoid = d) THEN
  RAISE EXCEPTION 'template % has a database comment, which a clone does not inherit', '`+tpl.name+`';
 END IF;
END $$;`); err != nil {
		tpl.err = err
		return
	}
	// Refuse new connections first, then end any the build left, so cloning never meets a session.
	if err := runSQL(ctx, "hms", `ALTER DATABASE `+tpl.name+` WITH ALLOW_CONNECTIONS false IS_TEMPLATE true;
 SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '`+tpl.name+`';`); err != nil {
		tpl.err = fmt.Errorf("close template %s: %w", tpl.name, err)
		return
	}
	tpl.err = nil
}

func ownerRole(owner Owner) string {
	if owner == ProductionOwner {
		return "helivanta"
	}
	return "hms_owner"
}

// roleLogin mirrors sharedPostgres's init SQL: each harness role's password is its name; helivanta's is per process.
func roleLogin(role string) *url.Userinfo {
	if role == ownerRole(ProductionOwner) {
		return url.UserPassword(role, productionOwnerPassword)
	}
	return url.UserPassword(role, role)
}

func databaseDSNs(owner Owner, name string) (string, string, string) {
	dsn := func(role string) string {
		u := url.URL{Scheme: "postgres", User: roleLogin(role), Host: pgShared.host + ":" + pgShared.port, Path: "/" + name, RawQuery: "sslmode=disable"}
		return u.String()
	}
	return dsn("hms_app"), dsn(ownerRole(owner)), dsn("helivanta_system")
}
