package pharmacy

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"
)

// errTallyVoucher is a voucher the builder should never produce; it is an internal fault.
var errTallyVoucher = errors.New("tally voucher cannot be written faithfully")

type tallyEntry struct {
	Ledger string
	Debit  bool
	Party  bool
	Minor  int64
}

type tallyVoucher struct {
	Date      string
	Type      string
	Number    string
	RemoteID  string
	Narration string
	Entries   []tallyEntry
}

// tallyText is single-line text an XML 1.0 file carries unchanged.
func tallyText(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == 0xFFFE || r == 0xFFFF {
			return false
		}
	}
	return true
}

// encodeTallyVouchers writes Tally's Import Data envelope (ADR-0076 §1), or nothing.
func encodeTallyVouchers(vouchers []tallyVoucher) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("<ENVELOPE>\n<HEADER>\n<TALLYREQUEST>Import Data</TALLYREQUEST>\n</HEADER>\n<BODY>\n<IMPORTDATA>\n" +
		"<REQUESTDESC>\n<REPORTNAME>Vouchers</REPORTNAME>\n</REQUESTDESC>\n<REQUESTDATA>\n")
	for i, v := range vouchers {
		if err := writeTallyVoucher(&b, v); err != nil {
			return nil, fmt.Errorf("voucher %d: %w", i, err)
		}
		if b.Len() > tallyExportBytes {
			return nil, errTallyTooLarge
		}
	}
	b.WriteString("</REQUESTDATA>\n</IMPORTDATA>\n</BODY>\n</ENVELOPE>\n")
	if b.Len() > tallyExportBytes {
		return nil, errTallyTooLarge
	}
	return b.Bytes(), nil
}

func writeTallyVoucher(b *bytes.Buffer, v tallyVoucher) error {
	date, err := time.Parse(time.DateOnly, v.Date)
	if err != nil || date.Format(time.DateOnly) != v.Date {
		return errTallyVoucher
	}
	for _, s := range []string{v.Type, v.Number, v.RemoteID, v.Narration} {
		if !tallyText(s) {
			return errTallyVoucher
		}
	}
	entries, err := mergedTallyEntries(v.Entries)
	if err != nil {
		return err
	}
	b.WriteString("<TALLYMESSAGE xmlns:UDF=\"TallyUDF\">\n<VOUCHER REMOTEID=\"")
	tallyEscape(b, v.RemoteID)
	b.WriteString(`" VCHTYPE="`)
	tallyEscape(b, v.Type)
	b.WriteString("\" ACTION=\"Create\" OBJVIEW=\"Accounting Voucher View\">\n<DATE>" + date.Format("20060102") + "</DATE>\n<VOUCHERTYPENAME>")
	tallyEscape(b, v.Type)
	b.WriteString("</VOUCHERTYPENAME>\n<VOUCHERNUMBER>")
	tallyEscape(b, v.Number)
	b.WriteString("</VOUCHERNUMBER>\n<PERSISTEDVIEW>Accounting Voucher View</PERSISTEDVIEW>\n<ISINVOICE>No</ISINVOICE>\n<NARRATION>")
	tallyEscape(b, v.Narration)
	b.WriteString("</NARRATION>\n")
	for _, e := range entries {
		// Tally's samples: a debit is deemed positive and carries a negative amount.
		positive, amount := "No", majorUnits(e.Minor, 2)
		if e.Debit {
			positive, amount = "Yes", "-"+amount
		}
		b.WriteString("<LEDGERENTRIES.LIST>\n<LEDGERNAME>")
		tallyEscape(b, e.Ledger)
		b.WriteString("</LEDGERNAME>\n<ISDEEMEDPOSITIVE>" + positive + "</ISDEEMEDPOSITIVE>\n<ISPARTYLEDGER>" + tallyYesNo(e.Party) +
			"</ISPARTYLEDGER>\n<AMOUNT>" + amount + "</AMOUNT>\n</LEDGERENTRIES.LIST>\n")
	}
	b.WriteString("</VOUCHER>\n</TALLYMESSAGE>\n")
	return nil
}

// mergedTallyEntries joins same-side entries on one ledger, debits first, then by
// ledger name, and refuses a voucher that does not balance to the paisa.
func mergedTallyEntries(in []tallyEntry) ([]tallyEntry, error) {
	var out []tallyEntry
	var debits, credits int64
	for _, e := range in {
		if e.Minor <= 0 || !tallyText(e.Ledger) {
			return nil, errTallyVoucher
		}
		if e.Debit {
			debits += e.Minor
		} else {
			credits += e.Minor
		}
		i := slices.IndexFunc(out, func(o tallyEntry) bool { return o.Debit == e.Debit && o.Ledger == e.Ledger })
		if i < 0 {
			out = append(out, e)
			continue
		}
		out[i].Minor += e.Minor
		out[i].Party = out[i].Party || e.Party
	}
	if debits == 0 || debits != credits {
		return nil, errTallyVoucher
	}
	slices.SortFunc(out, func(a, b tallyEntry) int {
		if a.Debit != b.Debit {
			if a.Debit {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Ledger, b.Ledger)
	})
	return out, nil
}

func tallyEscape(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s)) // bytes.Buffer writes cannot fail
}

func tallyYesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}
