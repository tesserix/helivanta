package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleLog = `
Importing Data from 'C:\QA\vouchers.xml' on 26-Nov-26 at 01:33:13

Importing Data to company: 'Isolated QA'

Import Summary
Created : 20
Altered : 0
Deleted : 0
Combined : 0
Ignored : 0
Errors : 0
Exceptions: 0
----------------------------------------------
`

func TestNativeLogRequiresUniqueSuccessfulImportInDeclaredCompany(t *testing.T) {
	if err := checkImportLog([]byte(sampleLog), `C:\QA\vouchers.xml`, "Isolated QA", 20); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]string{
		"other company":     strings.ReplaceAll(sampleLog, "Isolated QA", "Other"),
		"other file":        strings.ReplaceAll(sampleLog, "vouchers.xml", "masters.xml"),
		"partial":           strings.ReplaceAll(sampleLog, "Created : 20", "Created : 19"),
		"errors":            strings.ReplaceAll(sampleLog, "Errors : 0", "Errors : 1"),
		"alterations":       strings.ReplaceAll(sampleLog, "Altered : 0", "Altered : 1"),
		"duplicate import":  sampleLog + sampleLog,
		"missing counter":   strings.ReplaceAll(sampleLog, "Ignored : 0", ""),
		"duplicate counter": strings.ReplaceAll(sampleLog, "Ignored : 0", "Ignored : 0\nIgnored : 0"),
		"truncated":         strings.ReplaceAll(sampleLog, "----------------------------------------------", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkImportLog([]byte(s), `C:\QA\vouchers.xml`, "Isolated QA", 20); err == nil {
				t.Fatal("unsafe import log accepted")
			}
		})
	}
}

func TestCLINativeEvidenceRetainsRawHashesAndRequiresAllInputs(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"config.json":  []byte(`{"version":1,"company":"Isolated QA","currency":"INR","from":"2026-11-25","to":"2026-11-25","voucher_counts":{"Purchase":1},"ledger_balances_minor":{"Supplier & Co":11800}}`),
		"source.xml":   []byte(sampleVoucher),
		"observed.xml": utf16Evidence(strings.Replace(sampleVoucher, "<DATE>", "<GSTCLASS>&#4; Not Applicable</GSTCLASS><DATE>", 1)),
		"tally.imp":    utf16Evidence(strings.Replace(sampleLog, "Created : 20", "Created : 1", 1)),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--config", filepath.Join(dir, "config.json"), "--source", filepath.Join(dir, "source.xml"), "--observed", filepath.Join(dir, "observed.xml"), "--observed-format", "tallyprime", "--import-log", filepath.Join(dir, "tally.imp"), "--imported-file", `C:\QA\vouchers.xml`}
	var output bytes.Buffer
	if err := run(args, &output); err != nil {
		t.Fatal(err)
	}
	var e evidence
	if err := json.Unmarshal(output.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Status != "vouchers_reconciled" || e.CompanyVerified || e.ImportEvidence != "native_log" || e.EscapedSentinels != 1 || e.ObservedFormat != "tallyprime" || len(e.ObservedSHA256) != 64 || len(e.ImportSHA256) != 64 {
		t.Fatalf("unexpected evidence: %#v", e)
	}
	for _, invalid := range [][]string{args[:len(args)-2], append(append([]string{}, args...), "--import-response", "other.xml")} {
		if err := run(invalid, &bytes.Buffer{}); err == nil {
			t.Fatal("incomplete or ambiguous evidence accepted")
		}
	}
}
