package drugmaster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// ingredientFact is one ingredient as a comparable value: the molecule by
// key and ATC code, so two versions naming different molecule rows for the
// same substance still compare equal, and a different substance never does.
type ingredientFact struct{ Key, ATC, Value, Unit, PerValue, PerUnit string }

func formulationFact(label, form, route, class string, ndps, tele bool, ings []ingredientFact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s|%t|%t", label, form, route, class, ndps, tele)
	for _, i := range ings {
		fmt.Fprintf(&b, "|%s,%s,%s,%s,%s,%s", i.Key, i.ATC, i.Value, i.Unit, i.PerValue, i.PerUnit)
	}
	return b.String()
}

func productFact(formulationCode, brand, manufacturer string, pack int, unit, gtin, hsn, status string) string {
	return fmt.Sprintf("%s|%s|%s|%d|%s|%s|%s|%s", formulationCode, brand, manufacturer, pack, unit, gtin, hsn, status)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// fileIngredients is a file formulation's ingredients as facts. atc gives
// each molecule key's ATC code from the file or the published catalogue.
func fileIngredients(fm FormulationIn, atc func(string) string) []ingredientFact {
	out := make([]ingredientFact, len(fm.Ingredients))
	for i, x := range fm.Ingredients {
		per := ""
		if x.PerValue != nil {
			per = canonicalDecimal(x.PerValue.String())
		}
		k := searchKey(x.Molecule)
		out[i] = ingredientFact{k, atc(k), canonicalDecimal(x.StrengthValue.String()), x.StrengthUnit, per, deref(x.PerUnit)}
	}
	return out
}

func fileFormulationFact(fm FormulationIn, atc func(string) string) string {
	return formulationFact(fm.Label, fm.DoseForm, fm.Route, fm.RegulatoryClass, *fm.NDPS, *fm.TeleconsultPrescribable, fileIngredients(fm, atc))
}

func fileProductFact(p ProductIn) string {
	gtin := ""
	if p.GTIN != nil {
		gtin = gtin14(*p.GTIN)
	}
	return productFact(p.Formulation, p.BrandName, p.Manufacturer, p.PackSize, p.UnitOfIssue, gtin, p.HSNCode, p.Status)
}

// dated is one published version: its date, its release order and its fact.
type dated struct {
	ValidFrom string
	Sequence  int64
	Fact      string
}

// inForce is the fact in force on date, or "" before the first version:
// the latest valid_from on or before it, a later release winning a tie.
func inForce(versions []dated, date string) string {
	fact, from, seq := "", "", int64(-1)
	for _, v := range versions {
		if v.ValidFrom <= date && (v.ValidFrom > from || (v.ValidFrom == from && v.Sequence > seq)) {
			fact, from, seq = v.Fact, v.ValidFrom, v.Sequence
		}
	}
	return fact
}

type publishedMolecule struct {
	ID  uuid.UUID
	ATC string
}

type publishedProduct struct{ ID, FormulationID uuid.UUID }

// catalogue is what is published, loaded once per staging or decision so
// a 60,000-product release costs a handful of queries, not one per row.
type catalogue struct {
	molecules    map[string]publishedMolecule
	formulations map[string]uuid.UUID
	fversions    map[uuid.UUID][]dated
	products     map[string]publishedProduct
	pversions    map[uuid.UUID][]dated
}

func (c catalogue) atc(key string) string { return c.molecules[key].ATC }

// loadCatalogue reads published content only. An identity is published
// when a published version uses it, whichever release created it.
func loadCatalogue(tx *gorm.DB) (catalogue, error) {
	c := catalogue{map[string]publishedMolecule{}, map[string]uuid.UUID{}, map[uuid.UUID][]dated{}, map[string]publishedProduct{}, map[uuid.UUID][]dated{}}
	var molecules []struct {
		ID        uuid.UUID
		SearchKey string
		ATCCode   *string
	}
	if err := tx.Raw(`SELECT DISTINCT m.id, m.search_key, m.atc_code FROM drugmaster_molecules m
		JOIN drugmaster_formulation_ingredients i ON i.molecule_id=m.id
		JOIN drugmaster_formulation_versions v ON v.id=i.version_id
		JOIN drugmaster_releases r ON r.id=v.release_id WHERE r.status='published'`).Scan(&molecules).Error; err != nil {
		return c, err
	}
	for _, m := range molecules {
		c.molecules[m.SearchKey] = publishedMolecule{m.ID, deref(m.ATCCode)}
	}
	var ings []struct {
		VersionID                   uuid.UUID
		SearchKey, ATCCode          string
		StrengthValue, StrengthUnit string
		PerValue, PerUnit           *string
	}
	if err := tx.Raw(`SELECT i.version_id, m.search_key, COALESCE(m.atc_code,'') AS atc_code, i.strength_value::text AS strength_value,
		i.strength_unit, i.per_value::text AS per_value, i.per_unit
		FROM drugmaster_formulation_ingredients i JOIN drugmaster_molecules m ON m.id=i.molecule_id
		JOIN drugmaster_formulation_versions v ON v.id=i.version_id JOIN drugmaster_releases r ON r.id=v.release_id
		WHERE r.status='published' ORDER BY i.version_id, i.position`).Scan(&ings).Error; err != nil {
		return c, err
	}
	byVersion := map[uuid.UUID][]ingredientFact{}
	for _, i := range ings {
		per := ""
		if i.PerValue != nil {
			per = canonicalDecimal(*i.PerValue)
		}
		byVersion[i.VersionID] = append(byVersion[i.VersionID], ingredientFact{i.SearchKey, i.ATCCode, canonicalDecimal(i.StrengthValue), i.StrengthUnit, per, deref(i.PerUnit)})
	}
	var fvs []struct {
		ID, FormulationID                       uuid.UUID
		Code, ValidFrom                         string
		Sequence                                int64
		Label, DoseForm, Route, RegulatoryClass string
		NDPS, TeleconsultPrescribable           bool
	}
	if err := tx.Raw(`SELECT v.id, v.formulation_id, f.code, v.valid_from::text AS valid_from, r.sequence, v.label, v.dose_form, v.route,
		v.regulatory_class, v.ndps, v.teleconsult_prescribable
		FROM drugmaster_formulation_versions v JOIN drugmaster_formulations f ON f.id=v.formulation_id
		JOIN drugmaster_releases r ON r.id=v.release_id WHERE r.status='published'`).Scan(&fvs).Error; err != nil {
		return c, err
	}
	for _, v := range fvs {
		c.formulations[v.Code] = v.FormulationID
		c.fversions[v.FormulationID] = append(c.fversions[v.FormulationID], dated{v.ValidFrom, v.Sequence,
			formulationFact(v.Label, v.DoseForm, v.Route, v.RegulatoryClass, v.NDPS, v.TeleconsultPrescribable, byVersion[v.ID])})
	}
	var pvs []struct {
		ProductID, FormulationID                                    uuid.UUID
		Code, FormulationCode, ValidFrom                            string
		Sequence                                                    int64
		BrandName, Manufacturer, UnitOfIssue, GTIN, HSNCode, Status string
		PackSize                                                    int
	}
	if err := tx.Raw(`SELECT v.product_id, p.formulation_id, p.code, f.code AS formulation_code, v.valid_from::text AS valid_from, r.sequence,
		v.brand_name, v.manufacturer, v.unit_of_issue, COALESCE(v.gtin,'') AS gtin, v.hsn_code, v.status, v.pack_size
		FROM drugmaster_product_versions v JOIN drugmaster_products p ON p.id=v.product_id
		JOIN drugmaster_formulations f ON f.id=p.formulation_id
		JOIN drugmaster_releases r ON r.id=v.release_id WHERE r.status='published'`).Scan(&pvs).Error; err != nil {
		return c, err
	}
	for _, v := range pvs {
		c.products[v.Code] = publishedProduct{v.ProductID, v.FormulationID}
		c.pversions[v.ProductID] = append(c.pversions[v.ProductID], dated{v.ValidFrom, v.Sequence,
			productFact(v.FormulationCode, v.BrandName, v.Manufacturer, v.PackSize, v.UnitOfIssue, v.GTIN, v.HSNCode, v.Status)})
	}
	return c, nil
}

type moleculeRow struct {
	ID        uuid.UUID `gorm:"column:id"`
	ReleaseID uuid.UUID `gorm:"column:release_id"`
	INNName   string    `gorm:"column:inn_name"`
	ATCCode   *string   `gorm:"column:atc_code"`
}

type identityRow struct {
	ID        uuid.UUID `gorm:"column:id"`
	ReleaseID uuid.UUID `gorm:"column:release_id"`
	Code      string    `gorm:"column:code"`
}

type productRow struct {
	ID            uuid.UUID `gorm:"column:id"`
	ReleaseID     uuid.UUID `gorm:"column:release_id"`
	Code          string    `gorm:"column:code"`
	FormulationID uuid.UUID `gorm:"column:formulation_id"`
}

type formulationVersionRow struct {
	ID                      uuid.UUID `gorm:"column:id"`
	FormulationID           uuid.UUID `gorm:"column:formulation_id"`
	ReleaseID               uuid.UUID `gorm:"column:release_id"`
	ValidFrom               string    `gorm:"column:valid_from"`
	Label                   string    `gorm:"column:label"`
	DoseForm                string    `gorm:"column:dose_form"`
	Route                   string    `gorm:"column:route"`
	RegulatoryClass         string    `gorm:"column:regulatory_class"`
	NDPS                    bool      `gorm:"column:ndps"`
	TeleconsultPrescribable bool      `gorm:"column:teleconsult_prescribable"`
}

type ingredientRow struct {
	VersionID     uuid.UUID `gorm:"column:version_id"`
	ReleaseID     uuid.UUID `gorm:"column:release_id"`
	Position      int       `gorm:"column:position"`
	MoleculeID    uuid.UUID `gorm:"column:molecule_id"`
	StrengthValue string    `gorm:"column:strength_value"`
	StrengthUnit  string    `gorm:"column:strength_unit"`
	PerValue      *string   `gorm:"column:per_value"`
	PerUnit       *string   `gorm:"column:per_unit"`
}

type productVersionRow struct {
	ID           uuid.UUID `gorm:"column:id"`
	ProductID    uuid.UUID `gorm:"column:product_id"`
	ReleaseID    uuid.UUID `gorm:"column:release_id"`
	ValidFrom    string    `gorm:"column:valid_from"`
	BrandName    string    `gorm:"column:brand_name"`
	Manufacturer string    `gorm:"column:manufacturer"`
	PackSize     int       `gorm:"column:pack_size"`
	UnitOfIssue  string    `gorm:"column:unit_of_issue"`
	GTIN         *string   `gorm:"column:gtin"`
	HSNCode      string    `gorm:"column:hsn_code"`
	Status       string    `gorm:"column:status"`
}

// staged is every row one release adds, in insertion order.
type staged struct {
	molecules           []moleculeRow
	formulations        []identityRow
	formulationVersions []formulationVersionRow
	ingredients         []ingredientRow
	products            []productRow
	productVersions     []productVersionRow
}

// plan decides what a file adds: new identities for what is not
// published, and a new version only where the file differs from what is
// in force on its date.
func plan(release uuid.UUID, f ReleaseFile, c catalogue) (staged, error) {
	var out staged
	moleculeIDs := map[string]uuid.UUID{}
	fileATC := map[string]string{}
	for _, m := range f.Molecules {
		k := searchKey(m.INNName)
		fileATC[k] = deref(m.ATCCode)
		if pub, ok := c.molecules[k]; ok {
			if pub.ATC != deref(m.ATCCode) {
				return out, invalid("molecule %q is published with a different ATC code", m.INNName)
			}
			moleculeIDs[k] = pub.ID
			continue
		}
		id := newID()
		moleculeIDs[k] = id
		out.molecules = append(out.molecules, moleculeRow{id, release, m.INNName, m.ATCCode})
	}
	atc := func(k string) string {
		if a, ok := fileATC[k]; ok {
			return a
		}
		return c.atc(k)
	}
	formulationIDs := map[string]uuid.UUID{}
	for _, fm := range f.Formulations {
		id, published := c.formulations[fm.Code]
		if !published {
			id = newID()
			out.formulations = append(out.formulations, identityRow{id, release, fm.Code})
		}
		formulationIDs[fm.Code] = id
		if published && inForce(c.fversions[id], fm.ValidFrom) == fileFormulationFact(fm, atc) {
			continue
		}
		version := newID()
		out.formulationVersions = append(out.formulationVersions, formulationVersionRow{version, id, release, fm.ValidFrom, fm.Label,
			fm.DoseForm, fm.Route, fm.RegulatoryClass, *fm.NDPS, *fm.TeleconsultPrescribable})
		for i, x := range fm.Ingredients {
			k := searchKey(x.Molecule)
			mid, ok := moleculeIDs[k]
			if !ok {
				pub, found := c.molecules[k]
				if !found {
					return out, invalid("formulation %s names molecule %q, which is neither in this release nor published", fm.Code, x.Molecule)
				}
				mid = pub.ID
			}
			var per *string
			if x.PerValue != nil {
				v := x.PerValue.String()
				per = &v
			}
			out.ingredients = append(out.ingredients, ingredientRow{version, release, i + 1, mid, x.StrengthValue.String(), x.StrengthUnit, per, x.PerUnit})
		}
	}
	for _, p := range f.Products {
		formulation, ok := formulationIDs[p.Formulation]
		if !ok {
			if formulation, ok = c.formulations[p.Formulation]; !ok {
				return out, invalid("product %s names formulation %s, which is neither in this release nor published", p.Code, p.Formulation)
			}
		}
		pub, published := c.products[p.Code]
		id := pub.ID
		if published && pub.FormulationID != formulation {
			// A different composition is a different product (spec D4).
			return out, invalid("product %s is published as a pack of another formulation", p.Code)
		}
		if !published {
			id = newID()
			out.products = append(out.products, productRow{id, release, p.Code, formulation})
		}
		if published && inForce(c.pversions[id], p.ValidFrom) == fileProductFact(p) {
			continue
		}
		var gtin *string
		if p.GTIN != nil {
			g := gtin14(*p.GTIN)
			gtin = &g
		}
		out.productVersions = append(out.productVersions, productVersionRow{newID(), id, release, p.ValidFrom, p.BrandName,
			p.Manufacturer, p.PackSize, p.UnitOfIssue, gtin, p.HSNCode, p.Status})
	}
	return out, nil
}

const batch = 1000

func insertAll[T any](tx *gorm.DB, table string, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.Table(table).CreateInBatches(rows, batch).Error
}

// Stage records a release as staged, with all its rows, in one
// transaction: the database seals a release once that transaction ends.
// db must connect as helivanta_drug_stager; any other role is refused.
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
		c, err := loadCatalogue(tx)
		if err != nil {
			return err
		}
		rows, err := plan(release, f, c)
		if err != nil {
			return err
		}
		for _, step := range []func() error{
			func() error { return insertAll(tx, "drugmaster_molecules", rows.molecules) },
			func() error { return insertAll(tx, "drugmaster_formulations", rows.formulations) },
			func() error { return insertAll(tx, "drugmaster_formulation_versions", rows.formulationVersions) },
			func() error { return insertAll(tx, "drugmaster_formulation_ingredients", rows.ingredients) },
			func() error { return insertAll(tx, "drugmaster_products", rows.products) },
			func() error { return insertAll(tx, "drugmaster_product_versions", rows.productVersions) },
		} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return release, nil
}
