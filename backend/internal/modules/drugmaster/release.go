package drugmaster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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
		if strings.TrimSpace(v) == "" {
			return invalid("source name, licence_ref and version are required")
		}
	}
	if len(f.Molecules)+len(f.Formulations)+len(f.Products) == 0 {
		return invalid("release names nothing")
	}
	molecules := map[string]bool{}
	for _, m := range f.Molecules {
		k := searchKey(m.INNName)
		if len(k) < 2 || m.INNName != strings.TrimSpace(m.INNName) {
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

// canonicalDecimal drops the zeros numeric(14,6) pads with, so "500" and
// "500.000000" compare equal.
func canonicalDecimal(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if t := strings.TrimLeft(s, "0"); t != "" && t[0] != '.' {
		s = t
	}
	return s
}

func numberOrNil(n *json.Number) any {
	if n == nil {
		return nil
	}
	return n.String()
}

func parseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }

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

// scanID reads one id column, or uuid.Nil when no row matches.
func scanID(tx *gorm.DB, q string, args ...any) (uuid.UUID, error) {
	var row struct{ ID uuid.UUID }
	err := tx.Raw(q, args...).Scan(&row).Error
	return row.ID, err
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// Stage records a release as staged. db must connect as
// helivanta_drug_stager; any other role is refused by the database.
func Stage(ctx context.Context, db *tenantdb.DB, raw []byte, stagedBy string) (uuid.UUID, error) {
	f, err := ParseRelease(raw)
	if err != nil {
		return uuid.Nil, err
	}
	if strings.TrimSpace(stagedBy) == "" {
		return uuid.Nil, invalid("staged_by is required")
	}
	sum := sha256.Sum256(raw)
	release := newID()
	err = db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO drugmaster_releases (id,country,source_name,licence_ref,source_version,file_sha256,staged_by)
			VALUES (?,?,?,?,?,?,?)`, release, f.Country, f.Source.Name, f.Source.LicenceRef, f.Source.Version,
			hex.EncodeToString(sum[:]), stagedBy).Error; err != nil {
			return err
		}
		molecules, err := stageMolecules(tx, release, f.Molecules)
		if err != nil {
			return err
		}
		formulations, err := stageFormulations(tx, release, f.Formulations, molecules)
		if err != nil {
			return err
		}
		return stageProducts(tx, release, f.Products, formulations)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return release, nil
}

// stageMolecules returns every molecule id by search key, reusing an
// existing identity and refusing one whose ATC code differs.
func stageMolecules(tx *gorm.DB, release uuid.UUID, in []MoleculeIn) (map[string]uuid.UUID, error) {
	ids := map[string]uuid.UUID{}
	for _, m := range in {
		var existing struct {
			ID      uuid.UUID
			ATCCode *string
		}
		err := tx.Raw(`SELECT id, atc_code FROM drugmaster_molecules WHERE search_key=?`, searchKey(m.INNName)).Scan(&existing).Error
		if err != nil {
			return nil, err
		}
		if existing.ID != uuid.Nil {
			if !sameString(existing.ATCCode, m.ATCCode) {
				return nil, invalid("molecule %q already exists with a different ATC code", m.INNName)
			}
			ids[searchKey(m.INNName)] = existing.ID
			continue
		}
		id := newID()
		if err := tx.Exec(`INSERT INTO drugmaster_molecules (id,release_id,inn_name,atc_code) VALUES (?,?,?,?)`,
			id, release, m.INNName, m.ATCCode).Error; err != nil {
			return nil, err
		}
		ids[searchKey(m.INNName)] = id
	}
	return ids, nil
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// formulationRecord is what makes two versions the same clinical fact.
type formulationRecord struct {
	Label, DoseForm, Route, RegulatoryClass string
	NDPS, Teleconsult                       bool
	Ingredients                             string
}

func ingredientsKey(molecules []uuid.UUID, in []IngredientIn) string {
	var b strings.Builder
	for i, x := range in {
		per := ""
		if x.PerValue != nil {
			per = canonicalDecimal(x.PerValue.String()) + *x.PerUnit
		}
		fmt.Fprintf(&b, "%d|%s|%s|%s|%s;", i+1, molecules[i], canonicalDecimal(x.StrengthValue.String()), x.StrengthUnit, per)
	}
	return b.String()
}

// latestPublishedFormulation is the newest published version, or nil.
func latestPublishedFormulation(tx *gorm.DB, formulation uuid.UUID) (*formulationRecord, error) {
	var v struct {
		ID                                      uuid.UUID
		Label, DoseForm, Route, RegulatoryClass string
		NDPS, TeleconsultPrescribable           bool
	}
	if err := tx.Raw(`SELECT id,label,dose_form,route,regulatory_class,ndps,teleconsult_prescribable
		FROM drugmaster_formulation_versions WHERE formulation_id=? AND drugmaster_published(release_id)
		ORDER BY valid_from DESC LIMIT 1`, formulation).Scan(&v).Error; err != nil {
		return nil, err
	}
	if v.ID == uuid.Nil {
		return nil, nil
	}
	var rows []struct {
		Position      int
		MoleculeID    uuid.UUID
		StrengthValue string
		StrengthUnit  string
		PerValue      *string
		PerUnit       *string
	}
	if err := tx.Raw(`SELECT position,molecule_id,strength_value::text AS strength_value,strength_unit,per_value::text AS per_value,per_unit
		FROM drugmaster_formulation_ingredients WHERE version_id=? ORDER BY position`, v.ID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	in := make([]IngredientIn, len(rows))
	for i, r := range rows {
		ids[i] = r.MoleculeID
		in[i] = IngredientIn{StrengthValue: json.Number(r.StrengthValue), StrengthUnit: r.StrengthUnit, PerUnit: r.PerUnit}
		if r.PerValue != nil {
			n := json.Number(*r.PerValue)
			in[i].PerValue = &n
		}
	}
	return &formulationRecord{v.Label, v.DoseForm, v.Route, v.RegulatoryClass, v.NDPS, v.TeleconsultPrescribable, ingredientsKey(ids, in)}, nil
}

func moleculeID(tx *gorm.DB, staged map[string]uuid.UUID, name string) (uuid.UUID, error) {
	if id, ok := staged[searchKey(name)]; ok {
		return id, nil
	}
	id, err := scanID(tx, `SELECT id FROM drugmaster_molecules WHERE search_key=?`, searchKey(name))
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil {
		return uuid.Nil, invalid("molecule %q is neither in this release nor in the catalogue", name)
	}
	return id, nil
}

func stageFormulations(tx *gorm.DB, release uuid.UUID, in []FormulationIn, molecules map[string]uuid.UUID) (map[string]uuid.UUID, error) {
	ids := map[string]uuid.UUID{}
	for _, fm := range in {
		mids := make([]uuid.UUID, len(fm.Ingredients))
		for i, x := range fm.Ingredients {
			id, err := moleculeID(tx, molecules, x.Molecule)
			if err != nil {
				return nil, err
			}
			mids[i] = id
		}
		formulation, err := scanID(tx, `SELECT id FROM drugmaster_formulations WHERE code=?`, fm.Code)
		if err != nil {
			return nil, err
		}
		if formulation == uuid.Nil {
			formulation = newID()
			if err := tx.Exec(`INSERT INTO drugmaster_formulations (id,release_id,code) VALUES (?,?,?)`, formulation, release, fm.Code).Error; err != nil {
				return nil, err
			}
		}
		ids[fm.Code] = formulation
		want := formulationRecord{fm.Label, fm.DoseForm, fm.Route, fm.RegulatoryClass, *fm.NDPS, *fm.TeleconsultPrescribable, ingredientsKey(mids, fm.Ingredients)}
		current, err := latestPublishedFormulation(tx, formulation)
		if err != nil {
			return nil, err
		}
		if current != nil && *current == want {
			continue
		}
		version := newID()
		if err := tx.Exec(`INSERT INTO drugmaster_formulation_versions
			(id,formulation_id,release_id,valid_from,label,dose_form,route,regulatory_class,ndps,teleconsult_prescribable)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, version, formulation, release, fm.ValidFrom, fm.Label, fm.DoseForm, fm.Route,
			fm.RegulatoryClass, *fm.NDPS, *fm.TeleconsultPrescribable).Error; err != nil {
			return nil, err
		}
		for i, x := range fm.Ingredients {
			if err := tx.Exec(`INSERT INTO drugmaster_formulation_ingredients
				(version_id,position,molecule_id,strength_value,strength_unit,per_value,per_unit) VALUES (?,?,?,?,?,?,?)`,
				version, i+1, mids[i], x.StrengthValue.String(), x.StrengthUnit, numberOrNil(x.PerValue), x.PerUnit).Error; err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

type productRecord struct {
	BrandName, Manufacturer, UnitOfIssue, HSNCode, Status string
	PackSize                                              int
	GTIN                                                  string
}

func stageProducts(tx *gorm.DB, release uuid.UUID, in []ProductIn, formulations map[string]uuid.UUID) error {
	for _, p := range in {
		formulation, ok := formulations[p.Formulation]
		if !ok {
			var err error
			if formulation, err = scanID(tx, `SELECT id FROM drugmaster_formulations WHERE code=?`, p.Formulation); err != nil {
				return err
			}
			if formulation == uuid.Nil {
				return invalid("product %s names unknown formulation %s", p.Code, p.Formulation)
			}
		}
		var existing struct{ ID, FormulationID uuid.UUID }
		if err := tx.Raw(`SELECT id, formulation_id FROM drugmaster_products WHERE code=?`, p.Code).Scan(&existing).Error; err != nil {
			return err
		}
		product := existing.ID
		if product == uuid.Nil {
			product = newID()
			if err := tx.Exec(`INSERT INTO drugmaster_products (id,release_id,code,formulation_id) VALUES (?,?,?,?)`,
				product, release, p.Code, formulation).Error; err != nil {
				return err
			}
		} else if existing.FormulationID != formulation {
			// A different composition is a different product (spec D4).
			return invalid("product %s is already a pack of another formulation", p.Code)
		}
		gtin := ""
		if p.GTIN != nil {
			gtin = *p.GTIN
		}
		want := productRecord{p.BrandName, p.Manufacturer, p.UnitOfIssue, p.HSNCode, p.Status, p.PackSize, gtin}
		var cur struct {
			ID                                                    uuid.UUID
			BrandName, Manufacturer, UnitOfIssue, HSNCode, Status string
			PackSize                                              int
			GTIN                                                  string
		}
		if err := tx.Raw(`SELECT id,brand_name,manufacturer,unit_of_issue,hsn_code,status,pack_size,COALESCE(gtin,'') AS gtin
			FROM drugmaster_product_versions WHERE product_id=? AND drugmaster_published(release_id)
			ORDER BY valid_from DESC LIMIT 1`, product).Scan(&cur).Error; err != nil {
			return err
		}
		if cur.ID != uuid.Nil && (productRecord{cur.BrandName, cur.Manufacturer, cur.UnitOfIssue, cur.HSNCode, cur.Status, cur.PackSize, cur.GTIN}) == want {
			continue
		}
		if err := tx.Exec(`INSERT INTO drugmaster_product_versions
			(id,product_id,release_id,valid_from,brand_name,manufacturer,pack_size,unit_of_issue,gtin,hsn_code,status)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, newID(), product, release, p.ValidFrom, p.BrandName, p.Manufacturer,
			p.PackSize, p.UnitOfIssue, p.GTIN, p.HSNCode, p.Status).Error; err != nil {
			return err
		}
	}
	return nil
}

// ErrNotStaged means the release does not exist or was already decided.
var ErrNotStaged = errors.New("drug master release is not staged")

// Decide publishes or rejects a staged release. db must connect as
// helivanta_drug_approver. Publishing writes its events in the same
// transaction, so a published release is never silent (spec D6).
func Decide(ctx context.Context, db *tenantdb.DB, release uuid.UUID, decidedBy string, publish bool, reason string) error {
	if strings.TrimSpace(decidedBy) == "" || strings.TrimSpace(reason) == "" || len(reason) > 500 {
		return invalid("decided_by and a reason of at most 500 characters are required")
	}
	return db.WithSystem(ctx, func(tx *gorm.DB) error {
		var status string
		if err := tx.Raw(`SELECT status FROM drugmaster_releases WHERE id=? FOR UPDATE`, release).Scan(&status).Error; err != nil {
			return err
		}
		if status != "staged" {
			return ErrNotStaged
		}
		if !publish {
			return tx.Exec(`UPDATE drugmaster_releases SET status='rejected',decided_by=?,decided_at=now(),decision_reason=? WHERE id=?`,
				decidedBy, reason, release).Error
		}
		// Changes are read before publishing, while "published" still
		// means "the versions this release supersedes".
		reclassified, err := reclassifications(tx, release)
		if err != nil {
			return err
		}
		statuses, err := statusChanges(tx, release)
		if err != nil {
			return err
		}
		var counts struct{ Formulations, Products int }
		if err := tx.Raw(`SELECT (SELECT count(*) FROM drugmaster_formulation_versions WHERE release_id=?) AS formulations,
			(SELECT count(*) FROM drugmaster_product_versions WHERE release_id=?) AS products`, release, release).Scan(&counts).Error; err != nil {
			return err
		}
		var sequence int64
		if err := tx.Raw(`UPDATE drugmaster_releases SET status='published',decided_by=?,decided_at=now(),decision_reason=?,
			sequence=nextval('drugmaster_release_sequence') WHERE id=? RETURNING sequence`, decidedBy, reason, release).Scan(&sequence).Error; err != nil {
			return err
		}
		if err := enqueue(tx, contract.SubjectCataloguePublished, contract.CataloguePublishedData{
			ReleaseID: release.String(), Sequence: sequence, FormulationVersions: counts.Formulations, ProductVersions: counts.Products,
		}); err != nil {
			return err
		}
		for _, r := range reclassified {
			if err := enqueue(tx, contract.SubjectFormulationReclassified, r); err != nil {
				return err
			}
		}
		for _, s := range statuses {
			s.ReleaseID = release.String()
			if err := enqueue(tx, contract.SubjectProductStatusChanged, s); err != nil {
				return err
			}
		}
		return nil
	})
}

func enqueue(tx *gorm.DB, subject string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, Data: raw})
}

// reclassifications compares each new formulation version with the
// published one before it.
func reclassifications(tx *gorm.DB, release uuid.UUID) ([]contract.FormulationReclassifiedData, error) {
	var out []contract.FormulationReclassifiedData
	err := tx.Raw(`SELECT n.formulation_id::text AS formulation_id, n.id::text AS version_id, n.valid_from::text AS valid_from,
		p.regulatory_class AS from_class, n.regulatory_class AS to_class, p.ndps AS from_ndps, n.ndps AS to_ndps
		FROM drugmaster_formulation_versions n
		JOIN LATERAL (SELECT regulatory_class, ndps FROM drugmaster_formulation_versions o
		 WHERE o.formulation_id=n.formulation_id AND drugmaster_published(o.release_id) ORDER BY o.valid_from DESC LIMIT 1) p ON true
		WHERE n.release_id=? AND (p.regulatory_class<>n.regulatory_class OR p.ndps<>n.ndps)
		ORDER BY n.formulation_id`, release).Scan(&out).Error
	return out, err
}

// statusChanges lists products whose status this release changes, and
// new products that arrive already recalled or discontinued.
func statusChanges(tx *gorm.DB, release uuid.UUID) ([]contract.ProductStatusChangedData, error) {
	var out []contract.ProductStatusChangedData
	err := tx.Raw(`SELECT n.product_id::text AS product_id, n.id::text AS version_id, n.valid_from::text AS valid_from, n.status
		FROM drugmaster_product_versions n
		LEFT JOIN LATERAL (SELECT status FROM drugmaster_product_versions o
		 WHERE o.product_id=n.product_id AND drugmaster_published(o.release_id) ORDER BY o.valid_from DESC LIMIT 1) p ON true
		WHERE n.release_id=? AND n.status IS DISTINCT FROM COALESCE(p.status,'active')
		ORDER BY n.product_id`, release).Scan(&out).Error
	return out, err
}
