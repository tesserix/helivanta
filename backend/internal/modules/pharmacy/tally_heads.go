package pharmacy

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Tally account heads (ADR-0076 §2): the closed grammar a ledger map names.
var (
	tallyVoucherLabels = map[string]string{"sale": "sales", "credit_note": "credit notes", "supplier_payment": "supplier payments", "supplier_purchase": "supplier purchases", "supplier_payment_reversal": "reversed supplier payments"}
	tallyTenderLabels  = map[string]string{"cash": "Counter cash", "upi": "Counter UPI", "upi_verified": "Counter verified UPI", "card": "Counter card", "admission_account": "Admission accounts"}
	tallyMethodLabels  = map[string]string{"cash": "cash", "bank": "bank transfer", "card": "card", "upi": "UPI", "other": "other means"}
	tallyNatureLabels  = map[string]string{"taxable": "taxable", "nil_rated": "nil rated", "exempt": "exempt"}
)

// tallyHeadLabel names a head for staff; false is any string outside the grammar.
func tallyHeadLabel(head string) (string, bool) {
	kind, rest, ok := strings.Cut(head, ":")
	if !ok {
		return "", false
	}
	switch kind {
	case "voucher_type":
		label, ok := tallyVoucherLabels[rest]
		return "Voucher type for " + label, ok
	case "tender":
		label, ok := tallyTenderLabels[rest]
		return label, ok
	case "payment_method":
		label, ok := tallyMethodLabels[rest]
		return "Supplier payments by " + label, ok
	case "purchases":
		category, supply, ok := strings.Cut(rest, ":")
		label, known := tallyPurchaseCategories[category]
		if !ok || !known {
			return "", false
		}
		nature, rate, ok := strings.Cut(supply, ":")
		kind, known := tallyNatureLabels[nature]
		if nature == "outside_gst" {
			kind = "outside GST"
			known = true
		}
		ppm, valid := tallyRate(rate)
		if !ok || !known || !valid || (nature != "taxable" && ppm != 0) {
			return "", false
		}
		if nature == "taxable" {
			return label + " purchases taxable at " + tallyPercent(ppm), true
		}
		return label + " purchases, " + kind, true
	case "input_cgst", "input_sgst", "input_igst", "input_utgst", "nonrecoverable_cgst", "nonrecoverable_sgst", "nonrecoverable_igst", "nonrecoverable_utgst":
		prefix, component, _ := strings.Cut(kind, "_")
		label := "Recoverable input "
		if prefix == "nonrecoverable" {
			label = "Nonrecoverable "
		}
		ppm, ok := tallyRate(rest)
		return label + strings.ToUpper(component) + " on " + tallyPercent(ppm) + " supplies", ok
	case "sales_goods", "sales_service":
		nature, rate, ok := strings.Cut(rest, ":")
		label, known := tallyNatureLabels[nature]
		ppm, valid := tallyRate(rate)
		if !ok || !known || !valid || (nature != "taxable" && ppm != 0) {
			return "", false
		}
		what := "Sales of goods"
		if kind == "sales_service" {
			what = "Sales of own services"
		}
		if nature == "taxable" {
			return what + " taxable at " + tallyPercent(ppm), true
		}
		return what + ", " + label, true
	case "cgst", "sgst", "igst", "utgst":
		ppm, ok := tallyRate(rest)
		return "Output " + strings.ToUpper(kind) + " on " + tallyPercent(ppm) + " supplies", ok
	case "supplier":
		ok := tallyText(rest) && utf8.RuneCountInString(rest) <= 100 && strings.TrimSpace(rest) == rest && strings.ToUpper(rest) == rest
		return "Supplier " + rest, ok
	}
	return "", false
}

// tallyRate is a canonical rate in parts per million, as lines record it.
func tallyRate(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil && v >= 0 && v <= 1000000 && strconv.FormatInt(v, 10) == s
}

func tallyPercent(ppm int64) string {
	return strings.TrimRight(strings.TrimRight(majorUnits(ppm, 4), "0"), ".") + "%"
}

// tallySalesHead posts a line by its recorded nature and rate; a charge for
// the pharmacy's own service is apart from goods and principal-supply charges.
func tallySalesHead(treatment, nature string, ppm int64) string {
	kind := "sales_goods"
	if treatment == "own_service" {
		kind = "sales_service"
	}
	return kind + ":" + nature + ":" + strconv.FormatInt(ppm, 10)
}

func tallyTaxHead(component string, ppm int64) string {
	return component + ":" + strconv.FormatInt(ppm, 10)
}

var tallyPurchaseCategories = map[string]string{"inventory": "Inventory", "utilities": "Utilities", "rent": "Rent", "salary": "Salary", "transport": "Transport", "maintenance": "Maintenance", "professional": "Professional services", "other": "Other"}
