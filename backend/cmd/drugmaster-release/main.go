// Command drugmaster-release stages and decides drug master releases
// (#476 spec D3). It holds no authority of its own: staging works only
// when DRUGMASTER_DATABASE_URL logs in as helivanta_drug_stager, deciding
// only as helivanta_drug_approver, and the database refuses a decision by
// the person who staged the release.
//
//	drugmaster-release stage  -file release.json -by "Name"
//	drugmaster-release decide -release <id> -file release.json -by "Name" -publish -reason "..."
//	drugmaster-release decide -release <id> -by "Name" -reject -reason "..."
//	drugmaster-release replay-molecules   (as the approver; outbox only)
//
// Publishing takes the file the approver reviewed and refuses a release
// whose rows differ from it.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
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

var errUsage = errors.New("usage: drugmaster-release stage -file F -by NAME | decide -release ID -by NAME (-publish -file F | -reject) -reason TEXT | sync -source NAME -licence REF -archive DIR -by NAME | replay-molecules")

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
	source := fs.String("source", "", "licensed source name")
	licence := fs.String("licence", "", "approved licence reference")
	archive := fs.String("archive", "", "private durable release archive directory")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	if dsn == "" {
		return errors.New("DRUGMASTER_DATABASE_URL is required")
	}
	switch args[0] {
	case "sync":
		if *by == "" || *source == "" || *licence == "" || *archive == "" {
			return errUsage
		}
		raw, err := drugmaster.FetchRelease(ctx, os.Getenv("DRUGMASTER_FEED_URL"), *source, *licence, os.Getenv("DRUGMASTER_FEED_TOKEN"))
		if err != nil {
			return err
		}
		file, err := archiveRelease(*archive, raw)
		if err != nil {
			return err
		}
		db, err := tenantdb.OpenRuntime(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		id, err := drugmaster.StageOnce(ctx, db, raw, *by)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "staged release %s; approval file %s\n", id, file)
		return err
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
		if err != nil || *by == "" || *publish == *reject || (*publish && *file == "") {
			return errUsage
		}
		var raw []byte
		if *publish {
			if raw, err = os.ReadFile(*file); err != nil { //nolint:gosec // G703: the operator names the file they reviewed
				return err
			}
		}
		db, err := tenantdb.OpenRuntime(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		return drugmaster.Decide(ctx, db, id, raw, *by, *publish, *reason)
	case "replay-molecules":
		db, err := tenantdb.OpenRuntime(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		n, err := drugmaster.ReplayMolecules(ctx, db)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "re-announced %d molecules\n", n)
		return err
	}
	return errUsage
}

func archiveRelease(dir string, raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	target := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	// The directory is an operator-controlled private durable volume.
	temp, err := os.CreateTemp(dir, ".release-*")
	if err != nil {
		return "", errors.New("release archive unavailable")
	}
	defer func() {
		_ = os.Remove(temp.Name()) //nolint:gosec // only the private temporary file created above is removed
	}()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return "", errors.New("archive write failed")
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", errors.New("archive sync failed")
	}
	if err := temp.Close(); err != nil {
		return "", errors.New("archive close failed")
	}
	if err := os.Link(temp.Name(), target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", errors.New("archive retain failed")
		}
		saved, err := os.ReadFile(target) //nolint:gosec // operator-controlled archive path, content hash verified below
		if err != nil || sha256.Sum256(saved) != sum {
			return "", errors.New("archive content does not match release")
		}
	}
	directory, err := os.Open(dir) //nolint:gosec // operator-controlled archive directory
	if err != nil {
		return "", errors.New("archive directory unavailable")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return "", errors.New("archive directory sync failed")
	}
	return target, nil
}
