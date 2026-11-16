// Command pharmacy-schedule-review is Helivanta's decision on a pharmacy's
// request to lower a product's schedule class (#1681). It holds no
// authority of its own: it works only when SCHEDULE_REVIEW_DATABASE_URL
// logs in as helivanta_drug_approver, and the database refuses a decision
// on anything but a countersigned lowering, or by its declarer or
// countersigner.
//
//	pharmacy-schedule-review list
//	pharmacy-schedule-review decide -declaration <id> -by "Name" (-approve|-reject) -reason "..."
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
	"text/tabwriter"

	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv("SCHEDULE_REVIEW_DATABASE_URL"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pharmacy-schedule-review:", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage: pharmacy-schedule-review list | decide -declaration ID -by NAME (-approve | -reject) -reason TEXT")

func run(ctx context.Context, args []string, dsn string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	declaration := fs.String("declaration", "", "the declaration to decide")
	by := fs.String("by", "", "the person deciding, by name")
	approve := fs.Bool("approve", false, "approve the lower class")
	reject := fs.Bool("reject", false, "reject the lower class")
	reason := fs.String("reason", "", "why")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	var id uuid.UUID
	switch args[0] {
	case "list":
		if *declaration != "" || *by != "" || *approve || *reject || *reason != "" {
			return errUsage
		}
	case "decide":
		var err error
		// Exactly one of -approve and -reject: an unstated decision is no decision.
		if id, err = uuid.Parse(*declaration); err != nil || *by == "" || *reason == "" || *approve == *reject {
			return errUsage
		}
	default:
		return errUsage
	}
	if dsn == "" {
		return errors.New("SCHEDULE_REVIEW_DATABASE_URL is required")
	}
	db, err := tenantdb.OpenRuntime(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if args[0] == "decide" {
		return pharmacy.DecideScheduleLowering(ctx, db, id, *by, *approve, *reason)
	}
	waiting, err := pharmacy.ListScheduleLowerings(ctx, db)
	if err != nil {
		return err
	}
	return printLowerings(out, waiting)
}

func ndps(b bool) string {
	if b {
		return "+NDPS"
	}
	return ""
}

func printLowerings(out io.Writer, waiting []pharmacy.ScheduleLowering) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "DECLARATION\tTENANT\tOUTLET\tPRODUCT\tFROM\tTO\tSOURCE\tDECLARED BY\tCOUNTERSIGNED BY\tCOUNTERSIGN REASON"); err != nil {
		return err
	}
	for _, l := range waiting {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s%s\t%s%s\t%s\t%s\t%s\t%s\n", l.DeclarationID, l.TenantID, l.OutletID, l.ProductName,
			l.CurrentClass, ndps(l.CurrentNDPS), l.RequestedClass, ndps(l.RequestedNDPS), l.Source, l.DeclaredBy, l.CountersignedBy, l.CountersignReason); err != nil {
			return err
		}
	}
	return w.Flush()
}
