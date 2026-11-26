package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadVouchersPreservesExactAccountingValues(t *testing.T) {
	got, err := readVouchers(strings.NewReader(sampleVoucher))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "helivanta-qa-1" || got[0].Date != "20261125" || got[0].Entries["Supplier & Co"] != 11800 || got[0].Entries["Purchases"] != -11800 {
		t.Fatalf("unexpected accounting values: %#v", got)
	}
}

func TestReadVouchersRejectsInvalidAccountingEvidence(t *testing.T) {
	for name, input := range map[string]string{
		"unbalanced":       strings.Replace(sampleVoucher, "118.00</AMOUNT>", "118.01</AMOUNT>", 1),
		"fractional paisa": strings.ReplaceAll(sampleVoucher, "118.00", "118.001"),
		"missing identity": strings.ReplaceAll(sampleVoucher, `REMOTEID="helivanta-qa-1"`, ""),
		"invalid date":     strings.ReplaceAll(sampleVoucher, "20261125", "20260230"),
		"empty ledger":     strings.ReplaceAll(sampleVoucher, "Purchases", ""),
		"cancelled":        strings.Replace(sampleVoucher, "<DATE>", "<ISCANCELLED>Yes</ISCANCELLED><DATE>", 1),
		"optional":         strings.Replace(sampleVoucher, "<DATE>", "<ISOPTIONAL>Yes</ISOPTIONAL><DATE>", 1),
		"directive":        `<!DOCTYPE ENVELOPE>` + sampleVoucher,
		"nested directive": strings.Replace(sampleVoucher, "<DATE>", "<!DOCTYPE bad><DATE>", 1),
		"multiple roots":   sampleVoucher + sampleVoucher,
		"trailing garbage": sampleVoucher + "not xml",
		"oversized":        strings.Repeat(" ", maxXMLBytes+1),
		"no vouchers":      `<ENVELOPE/>`,
		"malformed":        `<ENVELOPE>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readVouchers(strings.NewReader(input)); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}

func TestReconcileComparesIdentitiesAndEveryPosting(t *testing.T) {
	source, err := readVouchers(strings.NewReader(sampleVoucher))
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"changed ledger":   strings.ReplaceAll(sampleVoucher, "Purchases", "Other purchases"),
		"changed number":   strings.ReplaceAll(sampleVoucher, "QA/1", "QA/2"),
		"changed date":     strings.ReplaceAll(sampleVoucher, "20261125", "20261126"),
		"changed identity": strings.ReplaceAll(sampleVoucher, "helivanta-qa-1", "helivanta-qa-2"),
		"changed value":    strings.ReplaceAll(sampleVoucher, "118.00", "119.00"),
	} {
		t.Run(name, func(t *testing.T) {
			observed, err := readVouchers(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if err := reconcile(source, observed); err == nil {
				t.Fatal("changed voucher accepted")
			}
		})
	}
	observed, err := readVouchers(strings.NewReader(strings.ReplaceAll(sampleVoucher, "LEDGERENTRIES.LIST", "ALLLEDGERENTRIES.LIST")))
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(source, observed); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(source, nil); err == nil {
		t.Fatal("missing vouchers accepted")
	}
}

func TestCheckRequiresDeclaredScopeAndExactExpectedBalances(t *testing.T) {
	c := config{Version: 1, Company: "Isolated QA", Currency: "INR", From: "2026-11-25", To: "2026-11-25", VoucherCounts: map[string]int{"Purchase": 1}, LedgerBalances: map[string]int64{"Supplier & Co": 11800}}
	v, err := readVouchers(strings.NewReader(sampleVoucher))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkExpectations(c, v); err != nil {
		t.Fatal(err)
	}
	c.LedgerBalances["Supplier & Co"] = 11801
	if err := checkExpectations(c, v); err == nil {
		t.Fatal("incorrect balance accepted")
	}
	c.Company = ""
	if err := checkExpectations(c, v); err == nil {
		t.Fatal("missing scope accepted")
	}
}

func TestImportResponseRequiresOnlyExpectedCreates(t *testing.T) {
	valid := `<RESPONSE><CREATED>1</CREATED><ALTERED>0</ALTERED><DELETED>0</DELETED><ERRORS>0</ERRORS><IGNORED>0</IGNORED><CANCELLED>0</CANCELLED><COMBINED>0</COMBINED><EXCEPTIONS>0</EXCEPTIONS></RESPONSE>`
	if err := checkImportResponse(strings.NewReader(valid), 1); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		strings.ReplaceAll(valid, "<ERRORS>0", "<ERRORS>1"),
		strings.ReplaceAll(valid, "<ALTERED>0", "<ALTERED>1"),
		strings.ReplaceAll(valid, "<CREATED>1", "<CREATED>0"),
		strings.ReplaceAll(valid, "</RESPONSE>", "<LINEERROR>rejected</LINEERROR></RESPONSE>"),
		`<RESPONSE/>`,
		valid + "garbage",
		strings.ReplaceAll(valid, "<ERRORS>0", "<ERRORS>1</ERRORS><ERRORS>0"),
		valid + valid,
	} {
		if err := checkImportResponse(strings.NewReader(input), 1); err == nil {
			t.Fatal("unsuccessful or incomplete import response accepted")
		}
	}
}

func TestCLISeparatesSourceValidationFromRemoteReconciliation(t *testing.T) {
	dir := t.TempDir()
	configuration := filepath.Join(dir, "config.json")
	source := filepath.Join(dir, "source.xml")
	observed := filepath.Join(dir, "observed.xml")
	response := filepath.Join(dir, "response.xml")
	for path, content := range map[string]string{
		configuration: `{"version":1,"company":"Isolated QA","currency":"INR","from":"2026-11-25","to":"2026-11-25","voucher_counts":{"Purchase":1},"ledger_balances_minor":{"Supplier & Co":11800}}`,
		source:        sampleVoucher,
		observed:      strings.ReplaceAll(sampleVoucher, "LEDGERENTRIES.LIST", "ALLLEDGERENTRIES.LIST"),
		response:      `<RESPONSE><CREATED>1</CREATED><ALTERED>0</ALTERED><DELETED>0</DELETED><ERRORS>0</ERRORS><IGNORED>0</IGNORED></RESPONSE>`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--config", configuration, "--source", source}
	var output bytes.Buffer
	if err := run(args, &output); err != nil {
		t.Fatal(err)
	}
	var result evidence
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "source_validated" || result.CompanyVerified || result.VoucherCount != 1 || len(result.SourceSHA256) != 64 || result.ObservedSHA256 != "" {
		t.Fatalf("source-only run overstated evidence: %+v", result)
	}
	output.Reset()
	if err := run(append(args, "--observed", observed), &output); err == nil {
		t.Fatal("missing import receipt accepted")
	}
	if err := run(append(args, "--observed", observed, "--import-response", response), &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "vouchers_reconciled" || result.CompanyVerified || len(result.ObservedSHA256) != 64 || len(result.ImportSHA256) != 64 {
		t.Fatalf("missing reconciliation evidence: %+v", result)
	}
	if err := os.WriteFile(response, []byte(`<RESPONSE><CREATED>0</CREATED></RESPONSE>`), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(append(args, "--observed", observed, "--import-response", response), &output); err == nil || output.Len() != 0 {
		t.Fatal("failed reconciliation emitted success evidence")
	}
}

func TestMinorUnitsRejectsAmbiguousOrUnboundedValues(t *testing.T) {
	for _, input := range []string{"1", "1.001", "+1.00", " 1.00", "1,000.00", "NaN", "1e3", "10000000000.00", "--1.00", ""} {
		if _, err := minorUnits(input); err == nil {
			t.Errorf("accepted ambiguous amount %q", input)
		}
	}
}

func TestConfigRejectsUnknownFields(t *testing.T) {
	var c config
	if err := decodeConfig(bytes.NewBufferString(`{"version":1,"companny":"QA"}`), &c); err == nil {
		t.Fatal("misspelled configuration accepted")
	}
	if err := decodeConfig(bytes.NewBufferString(`{} {}`), &c); err == nil {
		t.Fatal("multiple configuration values accepted")
	}
	b, err := json.Marshal(config{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeConfig(bytes.NewReader(b), &c); err != nil {
		t.Fatal(err)
	}
}

const sampleVoucher = `<ENVELOPE><BODY><IMPORTDATA><REQUESTDATA><TALLYMESSAGE>
<VOUCHER REMOTEID="helivanta-qa-1" ACTION="Create" VCHTYPE="Purchase">
<DATE>20261125</DATE><VOUCHERTYPENAME>Purchase</VOUCHERTYPENAME><VOUCHERNUMBER>QA/1</VOUCHERNUMBER>
<LEDGERENTRIES.LIST><LEDGERNAME>Supplier &amp; Co</LEDGERNAME><AMOUNT>118.00</AMOUNT></LEDGERENTRIES.LIST>
<LEDGERENTRIES.LIST><LEDGERNAME>Purchases</LEDGERNAME><AMOUNT>-118.00</AMOUNT></LEDGERENTRIES.LIST>
</VOUCHER></TALLYMESSAGE></REQUESTDATA></IMPORTDATA></BODY></ENVELOPE>`
