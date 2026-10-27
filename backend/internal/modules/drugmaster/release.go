package drugmaster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ReleaseFile is one dataset from a named source (spec D3). It is a delta:
// whatever it names is compared with what is published, and only what
// differs becomes a new version.
type ReleaseFile struct {
	Country      string          `json:"country"`
	Source       ReleaseSource   `json:"source"`
	Molecules    []MoleculeIn    `json:"molecules"`
	Formulations []FormulationIn `json:"formulations"`
	Products     []ProductIn     `json:"products"`
}

type ReleaseSource struct {
	Name       string `json:"name"`
	LicenceRef string `json:"licence_ref"`
	Version    string `json:"version"`
}

type MoleculeIn struct {
	INNName string  `json:"inn_name"`
	ATCCode *string `json:"atc_code"`
}

type FormulationIn struct {
	Code            string `json:"code"`
	ValidFrom       string `json:"valid_from"`
	Label           string `json:"label"`
	DoseForm        string `json:"dose_form"`
	Route           string `json:"route"`
	RegulatoryClass string `json:"regulatory_class"`
	// Pointers: a missing safety flag is a rejected file, never false.
	NDPS                    *bool          `json:"ndps"`
	TeleconsultPrescribable *bool          `json:"teleconsult_prescribable"`
	Ingredients             []IngredientIn `json:"ingredients"`
}

type IngredientIn struct {
	Molecule      string       `json:"molecule"`
	StrengthValue json.Number  `json:"strength_value"`
	StrengthUnit  string       `json:"strength_unit"`
	PerValue      *json.Number `json:"per_value"`
	PerUnit       *string      `json:"per_unit"`
}

type ProductIn struct {
	Code         string  `json:"code"`
	Formulation  string  `json:"formulation"`
	ValidFrom    string  `json:"valid_from"`
	BrandName    string  `json:"brand_name"`
	Manufacturer string  `json:"manufacturer"`
	PackSize     int     `json:"pack_size"`
	UnitOfIssue  string  `json:"unit_of_issue"`
	GTIN         *string `json:"gtin"`
	HSNCode      string  `json:"hsn_code"`
	Status       string  `json:"status"`
}

// ErrInvalidRelease wraps every reason a release file is refused.
var ErrInvalidRelease = errors.New("invalid drug master release")

var codeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)

// ParseRelease decodes a release file strictly: unknown fields, trailing
// data and missing safety flags are refused rather than defaulted.
func ParseRelease(raw []byte) (ReleaseFile, error) {
	var f ReleaseFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("%w: %v", ErrInvalidRelease, err)
	}
	if dec.More() {
		return f, fmt.Errorf("%w: trailing data", ErrInvalidRelease)
	}
	return f, f.validate()
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidRelease}, args...)...)
}

func (f ReleaseFile) validate() error {
	if f.Country != "IN" {
		return invalid("country must be IN")
	}
	for _, v := range []string{f.Source.Name, f.Source.LicenceRef, f.Source.Version} {
		if strings.TrimSpace(v) == "" || v != strings.TrimSpace(v) {
			return invalid("source name, licence_ref and version are required, without surrounding spaces")
		}
	}
	if len(f.Molecules)+len(f.Formulations)+len(f.Products) == 0 {
		return invalid("release names nothing")
	}
	molecules := map[string]bool{}
	for _, m := range f.Molecules {
		k := searchKey(m.INNName)
		if len(k) < 2 || m.INNName != strings.TrimSpace(m.INNName) || !asciiRe.MatchString(m.INNName) {
			return invalid("molecule %q needs a trimmed name of at least two letters or digits", m.INNName)
		}
		if molecules[k] {
			return invalid("molecule %q appears twice", m.INNName)
		}
		molecules[k] = true
	}
	formulations := map[string]bool{}
	for _, fm := range f.Formulations {
		if !codeRe.MatchString(fm.Code) || formulations[fm.Code] {
			return invalid("formulation code %q is malformed or repeated", fm.Code)
		}
		formulations[fm.Code] = true
		if _, err := parseDate(fm.ValidFrom); err != nil {
			return invalid("formulation %s valid_from: %v", fm.Code, err)
		}
		if fm.NDPS == nil || fm.TeleconsultPrescribable == nil {
			return invalid("formulation %s must state ndps and teleconsult_prescribable", fm.Code)
		}
		if strings.TrimSpace(fm.Label) == "" || !doseForms[fm.DoseForm] || !routes[fm.Route] || !classes[fm.RegulatoryClass] {
			return invalid("formulation %s has an empty label or an unknown dose form, route or class", fm.Code)
		}
		if len(fm.Ingredients) == 0 || len(fm.Ingredients) > 20 {
			return invalid("formulation %s needs 1 to 20 ingredients", fm.Code)
		}
		seen := map[string]bool{}
		for _, in := range fm.Ingredients {
			k := searchKey(in.Molecule)
			// ASCII only, so a name's key is exactly the database's (review I5).
			if !asciiRe.MatchString(in.Molecule) || len(k) < 2 {
				return invalid("formulation %s names molecule %q, which is not a plain ASCII name", fm.Code, in.Molecule)
			}
			if seen[k] {
				return invalid("formulation %s names molecule %q twice", fm.Code, in.Molecule)
			}
			seen[k] = true
			if !positiveDecimal(in.StrengthValue.String()) || !strengthUnits[in.StrengthUnit] {
				return invalid("formulation %s: strength must be positive with a known unit", fm.Code)
			}
			if (in.PerValue == nil) != (in.PerUnit == nil) || (in.PerValue != nil && (!positiveDecimal(in.PerValue.String()) || !perUnits[*in.PerUnit])) {
				return invalid("formulation %s: per_value and per_unit go together, positive and known", fm.Code)
			}
		}
	}
	products := map[string]bool{}
	for _, p := range f.Products {
		if !codeRe.MatchString(p.Code) || products[p.Code] || !codeRe.MatchString(p.Formulation) {
			return invalid("product code %q or its formulation is malformed or repeated", p.Code)
		}
		products[p.Code] = true
		if _, err := parseDate(p.ValidFrom); err != nil {
			return invalid("product %s valid_from: %v", p.Code, err)
		}
		if strings.TrimSpace(p.BrandName) == "" || p.BrandName != strings.TrimSpace(p.BrandName) || searchKey(p.BrandName) == "" ||
			strings.TrimSpace(p.Manufacturer) == "" || strings.TrimSpace(p.UnitOfIssue) == "" {
			return invalid("product %s needs a brand, manufacturer and unit of issue", p.Code)
		}
		if p.PackSize < 1 || p.PackSize > 1_000_000 || !hsnRe.MatchString(p.HSNCode) || !productStatuses[p.Status] {
			return invalid("product %s has an invalid pack size, HSN code or status", p.Code)
		}
		if p.GTIN != nil && !validGTIN(*p.GTIN) {
			return invalid("product %s has an invalid GTIN", p.Code)
		}
	}
	return nil
}

var (
	asciiRe   = regexp.MustCompile(`^[ -~]+$`)
	hsnRe     = regexp.MustCompile(`^[0-9]{4}([0-9]{2}){0,2}$`)
	doseForms = set("tablet", "capsule", "syrup", "suspension", "solution", "drops", "injection", "infusion",
		"ointment", "cream", "gel", "lotion", "inhaler", "powder", "granules", "suppository", "patch", "spray", "lozenge")
	routes = set("oral", "sublingual", "buccal", "intravenous", "intramuscular", "subcutaneous", "topical",
		"transdermal", "inhalation", "nasal", "ophthalmic", "otic", "rectal", "vaginal")
	classes         = set("none", "G", "H", "H1", "X")
	strengthUnits   = set("mg", "g", "mcg", "IU", "mL", "%")
	perUnits        = set("mg", "g", "mL", "dose", "unit")
	productStatuses = set("active", "discontinued", "recalled")
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// decimalRe fits numeric(14,6): up to eight integer and six fraction digits.
var decimalRe = regexp.MustCompile(`^[0-9]{1,8}(\.[0-9]{1,6})?$`)

func positiveDecimal(s string) bool {
	return decimalRe.MatchString(s) && strings.Trim(s, "0.") != ""
}

// canonicalDecimal drops the zeros numeric(14,6) pads with, so "500",
// "0500" and "500.000000" compare equal.
func canonicalDecimal(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	s = strings.TrimLeft(s, "0")
	if s == "" || s[0] == '.' {
		s = "0" + s
	}
	return s
}

func numberOrNil(n *json.Number) any {
	if n == nil {
		return nil
	}
	return n.String()
}

// parseDate accepts YYYY-MM-DD within years Postgres and the catalogue
// can mean (review M8).
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err == nil && (t.Year() < 1900 || t.Year() > 9999) {
		err = errors.New("year out of range")
	}
	return t, err
}

// gtin14 left-pads a GTIN to 14 digits; the check digit is unchanged.
func gtin14(g string) string { return strings.Repeat("0", 14-len(g)) + g }

// searchKey mirrors the generated search_key columns: lower case, letters
// and digits only.
func searchKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validGTIN checks the GS1 mod-10 check digit of a GTIN-8/12/13/14.
func validGTIN(g string) bool {
	if l := len(g); l != 8 && (l < 12 || l > 14) {
		return false
	}
	sum := 0
	for i := len(g) - 2; i >= 0; i-- {
		d := int(g[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if (len(g)-2-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	last := int(g[len(g)-1] - '0')
	return last >= 0 && last <= 9 && (10-sum%10)%10 == last
}
