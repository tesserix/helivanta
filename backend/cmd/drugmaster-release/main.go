// Command drugmaster-release stages and decides drug master releases
// (#476 spec D3). It holds no authority of its own: staging works only
// when DRUGMASTER_DATABASE_URL logs in as helivanta_drug_stager, deciding
// only as helivanta_drug_approver, and the database refuses a decision by
// the person who staged the release.
//
//	drugmaster-release stage  -file release.json -by "Name"
//	drugmaster-release decide -release <id> -by "Name" -publish|-reject -reason "..."
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv("DRUGMASTER_DATABASE_URL"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "drugmaster-release:", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage: drugmaster-release stage -file F -by NAME | decide -release ID -by NAME (-publish|-reject) -reason TEXT")

func run(ctx context.Context, args []string, dsn string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "release file (JSON)")
	by := fs.String("by", "", "the person acting, by name")
	release := fs.String("release", "", "release id")
	publish := fs.Bool("publish", false, "publish the release")
	reject := fs.Bool("reject", false, "reject the release")
	reason := fs.String("reason", "", "why")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	if dsn == "" {
		return errors.New("DRUGMASTER_DATABASE_URL is required")
	}
	switch args[0] {
	case "stage":
		if *file == "" || *by == "" {
			return errUsage
		}
		raw, err := os.ReadFile(*file) //nolint:gosec // G703: the operator names the file to stage; reading it is the point
		if err != nil {
			return err
		}
		db, err := tenantdb.OpenRuntime(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		id, err := drugmaster.Stage(ctx, db, raw, *by)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, id)
		return err
	case "decide":
		id, err := uuid.Parse(*release)
		// Exactly one of -publish and -reject: an unstated decision is no decision.
		if err != nil || *by == "" || *publish == *reject {
			return errUsage
		}
		db, err := tenantdb.OpenRuntime(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		return drugmaster.Decide(ctx, db, id, *by, *publish, *reason)
	}
	return errUsage
}
