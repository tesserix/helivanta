package pharmacy

import (
	"bytes"
	"html/template"
	"strings"
	"time"
	"unicode/utf8"
)

type supplierReturnNote struct {
	PharmacyName, OutletName, OutletCode                                 string
	SupplierName, OriginalBill, Reference, Recipient, HandoffUTC, Reason string
	Number                                                               int64
	Lines                                                                []SupplierReturnLine
}

func renderSupplierReturnNote(note supplierReturnNote) ([]byte, error) {
	if note.Number < 1 || note.Number > 1000000000000 || len(note.Lines) < 1 || len(note.Lines) > 100 {
		return nil, errSupplierReturnEvidence
	}
	for _, field := range []struct {
		value string
		max   int
	}{
		{note.PharmacyName, 200}, {note.OutletName, 200}, {note.OutletCode, 40},
		{note.SupplierName, 200}, {note.OriginalBill, 100}, {note.Reference, 100},
		{note.Recipient, 200}, {note.HandoffUTC, 100}, {note.Reason, 500},
	} {
		if !validSupplierNoteText(field.value, field.max) {
			return nil, errSupplierReturnEvidence
		}
	}
	for _, line := range note.Lines {
		_, err := time.Parse(time.DateOnly, line.ExpiresOn)
		if err != nil || line.QuantityUnits == nil || *line.QuantityUnits < 1 || *line.QuantityUnits > 1000000000 ||
			!validSupplierNoteText(line.ProductName, 200) || !validSupplierNoteText(line.GenericName, 200) ||
			!validSupplierNoteText(line.UnitCode, 40) || !validSupplierNoteText(line.LotNumber, 100) {
			return nil, errSupplierReturnEvidence
		}
	}
	view, err := template.New("supplier-return-note").Parse(supplierReturnNoteHTML)
	if err != nil {
		return nil, err
	}
	var data bytes.Buffer
	if err := view.Execute(&data, note); err != nil {
		return nil, err
	}
	if data.Len() > 1024*1024 {
		return nil, errSupplierReturnEvidence
	}
	return data.Bytes(), nil
}

func validSupplierNoteText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}

const supplierReturnNoteHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src &#39;none&#39;; style-src &#39;unsafe-inline&#39;; base-uri &#39;none&#39;; form-action &#39;none&#39;">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Commercial supplier return note — Return {{.Number}}</title>
<style>
@page{size:A4;margin:16mm}*{box-sizing:border-box}body{font:14px/1.5 system-ui,sans-serif;color:#17212f;margin:0 auto;max-width:1000px;padding:24px}header{border-bottom:3px solid #176b67;padding-bottom:18px;margin-bottom:24px}h1{font-size:26px;line-height:1.2;margin:4px 0}h2{font-size:16px;margin:0 0 8px}.brand{color:#176b67;font-weight:700;letter-spacing:.06em}.columns{display:grid;grid-template-columns:1fr 1fr;gap:24px;margin:20px 0}p{margin:5px 0;overflow-wrap:anywhere}dl{display:grid;grid-template-columns:140px 1fr;gap:6px 16px}dt{color:#526171}dd{margin:0;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse;margin:24px 0;table-layout:fixed}th,td{text-align:left;vertical-align:top;padding:10px 8px;border-bottom:1px solid #cdd7df;overflow-wrap:anywhere}th{background:#edf5f4;font-size:12px;text-transform:uppercase}small{display:block;color:#526171}.units{width:16%;text-align:right}footer{margin-top:24px;border-top:1px solid #cdd7df;padding-top:16px;color:#526171;font-size:12px}thead{display:table-header-group}tr{break-inside:avoid}@media print{body{padding:0;max-width:none}th{print-color-adjust:exact}}
</style></head><body>
<header><div class="brand">HELIVANTA · COMMERCIAL RECORD</div><h1>Commercial supplier return note</h1><p>Return {{.Number}} · Dispatched</p></header>
<section class="columns"><div><h2>From</h2><p>{{.PharmacyName}}</p><p>{{.OutletName}} · {{.OutletCode}}</p></div><div><h2>Supplier</h2><p>{{.SupplierName}}</p><p>Received by: {{.Recipient}}</p></div></section>
<dl><dt>Outward reference</dt><dd>{{.Reference}}</dd><dt>Original bill</dt><dd>{{.OriginalBill}}</dd><dt>Handed over</dt><dd>{{.HandoffUTC}}</dd><dt>Return reason</dt><dd>{{.Reason}}</dd></dl>
<table aria-label="Returned batches"><thead><tr><th>Product / unit</th><th>Batch</th><th>Expiry</th><th class="units">Returned units</th></tr></thead><tbody>
{{range .Lines}}<tr><td>{{.ProductName}}<small>{{.GenericName}} · {{.UnitCode}}</small></td><td>{{.LotNumber}}</td><td>{{.ExpiresOn}}</td><td class="units">{{.QuantityUnits}}</td></tr>{{end}}
</tbody></table><footer><p>Not a tax invoice, statutory debit note, supplier credit acceptance or regulatory certificate.</p><p>This commercial record describes the retained outward handoff. It does not settle a bill, record payment or change stock on download.</p><p>Pharmacy and outlet names were retained at dispatch. Use your browser's Print command to print or save this note as PDF.</p></footer>
</body></html>`
