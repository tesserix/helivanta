package pharmacy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func tallySaleVoucher() tallyVoucher {
	return tallyVoucher{
		Date: "2026-11-01", Type: "Sales", Number: "INV/2026/0001",
		RemoteID: "helivanta-01933b5e-0000-7000-8000-000000000001", Narration: "Invoice INV/2026/0001",
		Entries: []tallyEntry{
			{Ledger: "Sales 12%", Minor: 10000},
			{Ledger: "Counter UPI", Debit: true, Minor: 1200},
			{Ledger: "Output SGST 6%", Minor: 600},
			{Ledger: "Cash", Debit: true, Minor: 10000},
			{Ledger: "Output CGST 6%", Minor: 600},
		},
	}
}

func tallyEntryXML(ledger, positive, party, amount string) string {
	return "<LEDGERENTRIES.LIST>\n<LEDGERNAME>" + ledger + "</LEDGERNAME>\n<ISDEEMEDPOSITIVE>" + positive +
		"</ISDEEMEDPOSITIVE>\n<ISPARTYLEDGER>" + party + "</ISPARTYLEDGER>\n<AMOUNT>" + amount + "</AMOUNT>\n</LEDGERENTRIES.LIST>\n"
}

func TestTallyXMLWritesTheImportDataEnvelope(t *testing.T) {
	t.Parallel()
	got, err := encodeTallyVouchers([]tallyVoucher{tallySaleVoucher()})
	require.NoError(t, err)
	want := "<ENVELOPE>\n<HEADER>\n<TALLYREQUEST>Import Data</TALLYREQUEST>\n</HEADER>\n<BODY>\n<IMPORTDATA>\n" +
		"<REQUESTDESC>\n<REPORTNAME>Vouchers</REPORTNAME>\n</REQUESTDESC>\n<REQUESTDATA>\n" +
		"<TALLYMESSAGE xmlns:UDF=\"TallyUDF\">\n" +
		"<VOUCHER REMOTEID=\"helivanta-01933b5e-0000-7000-8000-000000000001\" VCHTYPE=\"Sales\" ACTION=\"Create\" OBJVIEW=\"Accounting Voucher View\">\n" +
		"<DATE>20261101</DATE>\n<VOUCHERTYPENAME>Sales</VOUCHERTYPENAME>\n<VOUCHERNUMBER>INV/2026/0001</VOUCHERNUMBER>\n" +
		"<PERSISTEDVIEW>Accounting Voucher View</PERSISTEDVIEW>\n<ISINVOICE>No</ISINVOICE>\n<NARRATION>Invoice INV/2026/0001</NARRATION>\n" +
		tallyEntryXML("Cash", "Yes", "No", "-100.00") +
		tallyEntryXML("Counter UPI", "Yes", "No", "-12.00") +
		tallyEntryXML("Output CGST 6%", "No", "No", "6.00") +
		tallyEntryXML("Output SGST 6%", "No", "No", "6.00") +
		tallyEntryXML("Sales 12%", "No", "No", "100.00") +
		"</VOUCHER>\n</TALLYMESSAGE>\n</REQUESTDATA>\n</IMPORTDATA>\n</BODY>\n</ENVELOPE>\n"
	require.Equal(t, want, string(got))
}

func TestTallyXMLWritesAnEmptyPeriodAsAnEmptyEnvelope(t *testing.T) {
	t.Parallel()
	got, err := encodeTallyVouchers(nil)
	require.NoError(t, err)
	require.Equal(t, "<ENVELOPE>\n<HEADER>\n<TALLYREQUEST>Import Data</TALLYREQUEST>\n</HEADER>\n<BODY>\n<IMPORTDATA>\n"+
		"<REQUESTDESC>\n<REPORTNAME>Vouchers</REPORTNAME>\n</REQUESTDESC>\n<REQUESTDATA>\n</REQUESTDATA>\n</IMPORTDATA>\n</BODY>\n</ENVELOPE>\n", string(got))
}

func TestTallyXMLMergesSameSideEntriesOnOneLedger(t *testing.T) {
	t.Parallel()
	v := tallyVoucher{Date: "2026-11-02", Type: "Payment", Number: "PAY-7", RemoteID: "helivanta-x", Narration: "Bill B-1",
		Entries: []tallyEntry{
			{Ledger: "Output GST", Minor: 50}, {Ledger: "Sales", Minor: 1}, {Ledger: "Output GST", Minor: 50},
			{Ledger: "Acme", Debit: true, Party: true, Minor: 100}, {Ledger: "Sales", Debit: true, Minor: 1},
		}}
	got, err := encodeTallyVouchers([]tallyVoucher{v})
	require.NoError(t, err)
	body := string(got)
	require.Contains(t, body, tallyEntryXML("Acme", "Yes", "Yes", "-1.00")+tallyEntryXML("Sales", "Yes", "No", "-0.01")+
		tallyEntryXML("Output GST", "No", "No", "1.00")+tallyEntryXML("Sales", "No", "No", "0.01")+"</VOUCHER>")
	require.Equal(t, 4, strings.Count(body, "<LEDGERENTRIES.LIST>"))
}

func TestTallyXMLEscapesText(t *testing.T) {
	t.Parallel()
	v := tallySaleVoucher()
	v.Type, v.Number, v.Narration = `Sales "B&B"`, "INV<1>", "Invoice 'A' & <B>"
	v.Entries[0].Ledger = "R&D <Sales> Ä"
	got, err := encodeTallyVouchers([]tallyVoucher{v})
	require.NoError(t, err)
	body := string(got)
	require.Contains(t, body, `VCHTYPE="Sales &#34;B&amp;B&#34;"`)
	require.Contains(t, body, "<VOUCHERTYPENAME>Sales &#34;B&amp;B&#34;</VOUCHERTYPENAME>")
	require.Contains(t, body, "<VOUCHERNUMBER>INV&lt;1&gt;</VOUCHERNUMBER>")
	require.Contains(t, body, "<NARRATION>Invoice &#39;A&#39; &amp; &lt;B&gt;</NARRATION>")
	require.Contains(t, body, "<LEDGERNAME>R&amp;D &lt;Sales&gt; Ä</LEDGERNAME>")
}

func TestTallyXMLIsByteIdenticalWhateverTheEntryOrder(t *testing.T) {
	t.Parallel()
	a, err := encodeTallyVouchers([]tallyVoucher{tallySaleVoucher()})
	require.NoError(t, err)
	v := tallySaleVoucher()
	for i, j := 0, len(v.Entries)-1; i < j; i, j = i+1, j-1 {
		v.Entries[i], v.Entries[j] = v.Entries[j], v.Entries[i]
	}
	b, err := encodeTallyVouchers([]tallyVoucher{v})
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestTallyXMLRefusesVouchersItCannotWriteFaithfully(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*tallyVoucher){
		"unbalanced":            func(v *tallyVoucher) { v.Entries[0].Minor++ },
		"zero entry":            func(v *tallyVoucher) { v.Entries[0].Minor = 0 },
		"negative entry":        func(v *tallyVoucher) { v.Entries[0].Minor, v.Entries[3].Minor = -10000, -10000 },
		"one sided":             func(v *tallyVoucher) { v.Entries = []tallyEntry{{Ledger: "Cash", Debit: true, Minor: 1}} },
		"no entries":            func(v *tallyVoucher) { v.Entries = nil },
		"empty ledger":          func(v *tallyVoucher) { v.Entries[0].Ledger = "" },
		"control in ledger":     func(v *tallyVoucher) { v.Entries[0].Ledger = "Sales\t12%" },
		"control in narration":  func(v *tallyVoucher) { v.Narration = "Invoice\n1" },
		"c1 control in number":  func(v *tallyVoucher) { v.Number = "INV\u0085" },
		"noncharacter in type":  func(v *tallyVoucher) { v.Type = "Sales￿" },
		"invalid utf8 remoteid": func(v *tallyVoucher) { v.RemoteID = "helivanta-\xff" },
		"empty number":          func(v *tallyVoucher) { v.Number = "" },
		"empty type":            func(v *tallyVoucher) { v.Type = "" },
		"bad date":              func(v *tallyVoucher) { v.Date = "2026-02-30" },
		"date without dashes":   func(v *tallyVoucher) { v.Date = "20261101" },
	} {
		v := tallySaleVoucher()
		mutate(&v)
		got, err := encodeTallyVouchers([]tallyVoucher{tallySaleVoucher(), v})
		require.ErrorIs(t, err, errTallyVoucher, name)
		require.Nil(t, got, name)
	}
}

func TestTallyTextRefusesWhatXMLCannotCarry(t *testing.T) {
	t.Parallel()
	require.True(t, tallyText("Sales 12% — Ä & <x>"))
	for _, s := range []string{"", "a\x00", "a\x1f", "a\x7f", "a\u0085", "a￾", "a￿", "\xff"} {
		require.False(t, tallyText(s), "%q", s)
	}
}

func TestTallyXMLRefusesAnOversizedFileWithoutReturningPartialBytes(t *testing.T) {
	previous := tallyExportBytes
	tallyExportBytes = 256
	t.Cleanup(func() { tallyExportBytes = previous })
	data, err := encodeTallyVouchers([]tallyVoucher{tallySaleVoucher()})
	require.ErrorIs(t, err, errTallyTooLarge)
	require.Nil(t, data)
}
