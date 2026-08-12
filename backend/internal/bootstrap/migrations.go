package bootstrap

import (
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// PlatformMigrations returns the migrations owned by platform packages, in
// apply order, before any module's.
//
// It exists because events.Migrations() was hand-written in five places.
// Adding a second platform source meant remembering all five: missing the
// test harness breaks every module test at db.Migrate, and missing the RLS
// arch test silently stops the lint covering the new schema.
func PlatformMigrations() []tenantdb.Migration {
	migs := tenantdb.Migrations()
	return append(migs, events.Migrations()...)
}
