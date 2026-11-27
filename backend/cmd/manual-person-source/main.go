// Command manual-person-source validates or imports an externally reviewed source.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("manual-person-source", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "Path to a private reviewed source document")
	apply := flags.Bool("apply", false, "Import using the configured schema-owner connection")
	if flags.Parse(args) != nil || *file == "" || flags.NArg() != 0 {
		return errors.New("usage: manual-person-source --file <private-json> [--apply]")
	}
	input, err := os.Open(*file)
	if err != nil {
		return errors.New("cannot open source document")
	}
	source, err := iam.DecodeManualIdentitySource(input)
	if closeErr := input.Close(); closeErr != nil {
		return errors.New("cannot close source document")
	}
	if err != nil {
		return errors.New("invalid source document: use the canonical manual-person identity contract")
	}
	if !*apply {
		if iam.ValidateManualIdentitySource(source, time.Now().UTC()) != nil {
			return errors.New("source evidence is future-dated, expired or invalid")
		}
		return json.NewEncoder(output).Encode(struct {
			Status            string `json:"status"`
			MembershipChecked bool   `json:"membership_checked"`
			ClinicalAuthority string `json:"clinical_authority"`
		}{"valid_document_only", false, "not_evaluated"})
	}
	cfg := config.Load()
	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return errors.New("owner database unavailable; import was not confirmed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var receipt iam.ManualIdentityImportReceipt
	err = db.WithAdmin(ctx, func(tx *gorm.DB) error {
		var err error
		receipt, err = iam.ImportManualIdentitySource(tx, source)
		return err
	})
	if err != nil {
		return errors.New("import not confirmed: check current memberships, retained source intent, schema integrity and audit availability")
	}
	return json.NewEncoder(output).Encode(receipt)
}
