package main

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxXMLBytes = 8 << 20

type voucher struct {
	ID      string
	Date    string
	Type    string
	Number  string
	Entries map[string]int64
}

type xmlEntry struct {
	Ledger string `xml:"LEDGERNAME"`
	Amount string `xml:"AMOUNT"`
}

type xmlVoucher struct {
	ID         string     `xml:"REMOTEID,attr"`
	Date       string     `xml:"DATE"`
	Type       string     `xml:"VOUCHERTYPENAME"`
	Number     string     `xml:"VOUCHERNUMBER"`
	Entries    []xmlEntry `xml:"LEDGERENTRIES.LIST"`
	AllEntries []xmlEntry `xml:"ALLLEDGERENTRIES.LIST"`
	Cancelled  string     `xml:"ISCANCELLED"`
	Optional   string     `xml:"ISOPTIONAL"`
}

func readVouchers(r io.Reader) ([]voucher, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxXMLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read XML: %w", err)
	}
	if len(b) > maxXMLBytes {
		return nil, errors.New("XML exceeds 8 MiB")
	}
	if err := checkXMLShape(b); err != nil {
		return nil, err
	}
	d := xml.NewDecoder(strings.NewReader(string(b)))
	var result []voucher
	seen := make(map[string]bool)
	depth, roots, entries := 0, 0, 0
	for {
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode XML: %w", err)
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, errors.New("XML directives are not permitted")
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return nil, errors.New("text outside XML root")
			}
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || t.Name.Local != "ENVELOPE" {
					return nil, errors.New("expected one ENVELOPE root")
				}
			}
			if t.Name.Local != "VOUCHER" {
				depth++
				if depth > 64 {
					return nil, errors.New("XML nesting exceeds 64 levels")
				}
				continue
			}
			var raw xmlVoucher
			if err := d.DecodeElement(&raw, &t); err != nil {
				return nil, fmt.Errorf("decode voucher: %w", err)
			}
			v, err := validateVoucher(raw)
			if err != nil {
				return nil, fmt.Errorf("voucher %d: %w", len(result)+1, err)
			}
			if seen[v.ID] {
				return nil, errors.New("duplicate REMOTEID")
			}
			seen[v.ID] = true
			entries += len(raw.Entries) + len(raw.AllEntries)
			result = append(result, v)
			if len(result) > 5000 || entries > 50000 {
				return nil, errors.New("voucher or ledger-entry limit exceeded")
			}
		case xml.EndElement:
			depth--
		}
	}
	if roots != 1 || len(result) == 0 {
		return nil, errors.New("XML contains no vouchers")
	}
	return result, nil
}

func checkXMLShape(b []byte) error {
	d := xml.NewDecoder(strings.NewReader(string(b)))
	depth := 0
	for {
		t, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("validate XML: %w", err)
		}
		switch t.(type) {
		case xml.Directive:
			return errors.New("XML directives are not permitted")
		case xml.StartElement:
			depth++
			if depth > 64 {
				return errors.New("XML nesting exceeds 64 levels")
			}
		case xml.EndElement:
			depth--
		}
	}
}

func validateVoucher(raw xmlVoucher) (voucher, error) {
	v := voucher{ID: raw.ID, Date: raw.Date, Type: raw.Type, Number: raw.Number, Entries: make(map[string]int64)}
	if (raw.Cancelled != "" && raw.Cancelled != "No") || (raw.Optional != "" && raw.Optional != "No") {
		return v, errors.New("cancelled or optional voucher cannot establish a posted balance")
	}
	if strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.Type) == "" || strings.TrimSpace(v.Number) == "" {
		return v, errors.New("REMOTEID, voucher type and number are required")
	}
	if _, err := time.Parse("20060102", v.Date); err != nil {
		return v, errors.New("invalid voucher date")
	}
	if len(raw.Entries) != 0 && len(raw.AllEntries) != 0 {
		return v, errors.New("ambiguous ledger-entry representations")
	}
	all := append(raw.Entries, raw.AllEntries...)
	if len(all) < 2 {
		return v, errors.New("at least two ledger entries are required")
	}
	var total int64
	for _, e := range all {
		if strings.TrimSpace(e.Ledger) == "" {
			return v, errors.New("ledger name is required")
		}
		amount, err := minorUnits(e.Amount)
		if err != nil {
			return v, err
		}
		total += amount
		v.Entries[e.Ledger] += amount
	}
	if total != 0 {
		return v, errors.New("voucher is not balanced")
	}
	return v, nil
}

func minorUnits(s string) (int64, error) {
	negative := strings.HasPrefix(s, "-")
	value := strings.TrimPrefix(s, "-")
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[0]) < 1 || len(parts[0]) > 10 || len(parts[1]) != 2 {
		return 0, errors.New("amount must be a bounded decimal with two places")
	}
	for _, c := range parts[0] + parts[1] {
		if c < '0' || c > '9' {
			return 0, errors.New("amount contains a non-decimal character")
		}
	}
	n, err := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse amount: %w", err)
	}
	if negative {
		n = -n
	}
	return n, nil
}

type config struct {
	Version        int              `json:"version"`
	Company        string           `json:"company"`
	Currency       string           `json:"currency"`
	From           string           `json:"from"`
	To             string           `json:"to"`
	VoucherCounts  map[string]int   `json:"voucher_counts"`
	LedgerBalances map[string]int64 `json:"ledger_balances_minor"`
}

func decodeConfig(r io.Reader, c *config) error {
	d := json.NewDecoder(io.LimitReader(r, 64<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(c); err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("configuration must contain exactly one JSON object")
	}
	return nil
}

func checkExpectations(c config, vouchers []voucher) error {
	if c.Version != 1 || strings.TrimSpace(c.Company) == "" || c.Currency != "INR" {
		return errors.New("version 1, declared company and INR currency are required")
	}
	from, err := time.Parse("2006-01-02", c.From)
	if err != nil {
		return errors.New("invalid from date")
	}
	to, err := time.Parse("2006-01-02", c.To)
	if err != nil || to.Before(from) || to.Sub(from) > 365*24*time.Hour {
		return errors.New("expected period must span 1–366 days")
	}
	if len(c.VoucherCounts) == 0 || len(c.LedgerBalances) == 0 {
		return errors.New("expected voucher counts and ledger balances are required")
	}
	counts := make(map[string]int)
	balances := make(map[string]int64)
	for _, v := range vouchers {
		if v.Date < from.Format("20060102") || v.Date > to.Format("20060102") {
			return errors.New("voucher falls outside expected period")
		}
		counts[v.Type]++
		for ledger, amount := range v.Entries {
			balances[ledger] += amount
		}
	}
	if !maps.Equal(counts, c.VoucherCounts) {
		return errors.New("voucher-type counts do not match expectations")
	}
	for ledger, amount := range c.LedgerBalances {
		actual, exists := balances[ledger]
		if !exists || actual != amount {
			return errors.New("expected ledger balance is missing or different")
		}
	}
	return nil
}

func reconcile(source, observed []voucher) error {
	if len(source) != len(observed) {
		return errors.New("observed voucher count differs from source")
	}
	byID := make(map[string]voucher, len(observed))
	for _, v := range observed {
		byID[v.ID] = v
	}
	for _, want := range source {
		got, ok := byID[want.ID]
		if !ok || got.Date != want.Date || got.Type != want.Type || got.Number != want.Number || !maps.Equal(got.Entries, want.Entries) {
			return errors.New("observed voucher identity, date, type, number or ledger postings differ")
		}
	}
	return nil
}

func checkImportResponse(r io.Reader, expected int) error {
	b, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
	if err != nil || len(b) > 64<<10 {
		return errors.New("cannot read bounded import response")
	}
	var response struct {
		XMLName    xml.Name `xml:"RESPONSE"`
		Created    *int     `xml:"CREATED"`
		Altered    *int     `xml:"ALTERED"`
		Deleted    *int     `xml:"DELETED"`
		Errors     *int     `xml:"ERRORS"`
		Ignored    *int     `xml:"IGNORED"`
		Cancelled  int      `xml:"CANCELLED"`
		Combined   int      `xml:"COMBINED"`
		Exceptions int      `xml:"EXCEPTIONS"`
		LineErrors []string `xml:"LINEERROR"`
	}
	if err := xml.Unmarshal(b, &response); err != nil {
		return fmt.Errorf("decode import response: %w", err)
	}
	// Unmarshal permits trailing text; the evidence must be one complete XML document.
	d := xml.NewDecoder(strings.NewReader(string(b)))
	depth, roots := 0, 0
	seen := make(map[string]bool)
	for {
		t, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid import response XML")
		}
		switch token := t.(type) {
		case xml.Directive:
			return errors.New("import response directives are not permitted")
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			if depth == 1 {
				switch token.Name.Local {
				case "CREATED", "ALTERED", "DELETED", "ERRORS", "IGNORED", "CANCELLED", "COMBINED", "EXCEPTIONS":
					if seen[token.Name.Local] {
						return errors.New("duplicate import-result counter")
					}
					seen[token.Name.Local] = true
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return errors.New("text outside import response")
			}
		}
	}
	if roots != 1 || response.Created == nil || *response.Created != expected || len(response.LineErrors) != 0 || response.Cancelled != 0 || response.Combined != 0 || response.Exceptions != 0 {
		return errors.New("import did not create exactly the expected vouchers")
	}
	for _, counter := range []*int{response.Altered, response.Deleted, response.Errors, response.Ignored} {
		if counter == nil || *counter != 0 {
			return errors.New("import response reports missing counters or unexpected effects")
		}
	}
	return nil
}

type evidence struct {
	Status            string `json:"status"`
	DeclaredCompany   string `json:"declared_company"`
	CompanyVerified   bool   `json:"company_verified"`
	SourceSHA256      string `json:"source_sha256"`
	ObservedSHA256    string `json:"observed_sha256,omitempty"`
	ImportSHA256      string `json:"import_response_sha256,omitempty"`
	VoucherCount      int    `json:"voucher_count"`
	ConfigurationHash string `json:"configuration_sha256"`
}

func run(args []string, output io.Writer) error {
	f := flag.NewFlagSet("tallycheck", flag.ContinueOnError)
	f.SetOutput(output)
	configuration := f.String("config", "", "expectations JSON (required)")
	source := f.String("source", "", "unchanged Helivanta voucher XML (required)")
	observed := f.String("observed", "", "voucher XML exported independently from isolated Tally company")
	importResponse := f.String("import-response", "", "actual Tally XML import response; required with observed")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *configuration == "" || *source == "" || ((*observed == "") != (*importResponse == "")) {
		return errors.New("provide --config and --source; --observed and --import-response must be supplied together")
	}
	cb, err := readFile(*configuration, 64<<10)
	if err != nil {
		return err
	}
	var c config
	if err := decodeConfig(strings.NewReader(string(cb)), &c); err != nil {
		return err
	}
	sb, err := readFile(*source, maxXMLBytes)
	if err != nil {
		return err
	}
	vouchers, err := readVouchers(strings.NewReader(string(sb)))
	if err != nil {
		return err
	}
	if err := checkExpectations(c, vouchers); err != nil {
		return err
	}
	e := evidence{Status: "source_validated", DeclaredCompany: c.Company, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(sb)), ConfigurationHash: fmt.Sprintf("%x", sha256.Sum256(cb)), VoucherCount: len(vouchers)}
	if *observed != "" {
		ob, err := readFile(*observed, maxXMLBytes)
		if err != nil {
			return err
		}
		actual, err := readVouchers(strings.NewReader(string(ob)))
		if err != nil {
			return err
		}
		if err := reconcile(vouchers, actual); err != nil {
			return err
		}
		ib, err := readFile(*importResponse, 64<<10)
		if err != nil {
			return err
		}
		if err := checkImportResponse(strings.NewReader(string(ib)), len(vouchers)); err != nil {
			return err
		}
		e.Status = "vouchers_reconciled"
		e.ObservedSHA256 = fmt.Sprintf("%x", sha256.Sum256(ob))
		e.ImportSHA256 = fmt.Sprintf("%x", sha256.Sum256(ib))
	}
	if err := json.NewEncoder(output).Encode(e); err != nil {
		return fmt.Errorf("write evidence: %w", err)
	}
	return nil
}

func readFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open evidence file: %w", err)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("evidence file unreadable or exceeds limit")
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close evidence file: %w", closeErr)
	}
	return b, nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "tallycheck:", err)
		os.Exit(1)
	}
}
