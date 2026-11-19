package drugmaster

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"gorm.io/gorm"
)

const catalogueQuery = `SELECT p.product_id,p.version_id,p.formulation_id,f.id AS formulation_version_id,
 r.id AS release_id,r.source_name,r.source_version,r.licence_ref,p.brand_name,p.manufacturer,
 f.label AS formulation_label,f.dose_form,f.route,f.regulatory_class,f.ndps,p.valid_from,
 p.pack_size,p.unit_of_issue,p.gtin,p.hsn_code
 FROM (` + productQuery + `) p
 JOIN drugmaster_product_versions pv ON pv.id=p.version_id JOIN drugmaster_releases r ON r.id=pv.release_id
 JOIN LATERAL (SELECT x.* FROM drugmaster_formulation_versions x JOIN drugmaster_releases fr ON fr.id=x.release_id
 WHERE x.formulation_id=p.formulation_id AND x.valid_from<=` + todayIST + `
 ORDER BY x.valid_from DESC,fr.sequence DESC LIMIT 1) f ON true `

func catalogueProducts(tx *gorm.DB, predicate string, args ...any) ([]stockproduct.CatalogueProduct, error) {
	rows := []stockproduct.CatalogueProduct{}
	if err := tx.Raw(catalogueQuery+predicate+` ORDER BY p.brand_name,p.product_id LIMIT 20`, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	forms := []Formulation{}
	at := map[uuid.UUID]int{}
	for _, row := range rows {
		if _, found := at[row.FormulationVersionID]; !found {
			at[row.FormulationVersionID] = len(forms)
			forms = append(forms, Formulation{VersionID: row.FormulationVersionID})
		}
	}
	if err := attachIngredients(tx, forms); err != nil {
		return nil, err
	}
	for i := range rows {
		ingredients := forms[at[rows[i].FormulationVersionID]].Ingredients
		rows[i].Ingredients = make([]stockproduct.CatalogueIngredient, len(ingredients))
		for j, in := range ingredients {
			rows[i].Ingredients[j] = stockproduct.CatalogueIngredient{MoleculeID: in.MoleculeID, InnName: in.INNName,
				StrengthValue: in.StrengthValue, StrengthUnit: in.StrengthUnit, PerValue: in.PerValue, PerUnit: in.PerUnit}
		}
	}
	return rows, nil
}

func (g stockProductGuard) SearchCatalogue(ctx context.Context, query string) ([]stockproduct.CatalogueProduct, error) {
	key := searchKey(query)
	if len(query) > 200 || len(key) < 2 {
		return nil, stockproduct.ErrInvalidSearch
	}
	if g.db == nil {
		return nil, stockproduct.ErrUnavailable
	}
	var rows []stockproduct.CatalogueProduct
	err := g.db.WithSystem(ctx, func(tx *gorm.DB) error {
		predicate := `WHERE pv.status='active' AND p.product_id IN (
 SELECT product_id FROM drugmaster_product_versions WHERE search_key ~>=~ ? AND search_key ~<~ ?
 UNION SELECT dp.id FROM drugmaster_products dp WHERE dp.formulation_id IN (
 SELECT v.formulation_id FROM drugmaster_molecules m JOIN drugmaster_formulation_ingredients i ON i.molecule_id=m.id
 JOIN drugmaster_formulation_versions v ON v.id=i.version_id
 WHERE m.search_key ~>=~ ? AND m.search_key ~<~ ?))`
		args := []any{key, upperBound(key), key, upperBound(key)}
		if validGTIN(strings.TrimSpace(query)) {
			predicate = `WHERE pv.status='active' AND p.product_id IN (SELECT product_id FROM drugmaster_product_versions WHERE gtin=?) AND p.gtin=?`
			args = []any{gtin14(strings.TrimSpace(query)), gtin14(strings.TrimSpace(query))}
		}
		var err error
		rows, err = catalogueProducts(tx, predicate, args...)
		return err
	})
	if err != nil {
		return nil, stockproduct.ErrUnavailable
	}
	return rows, nil
}

func (g stockProductGuard) WithCatalogueProduct(ctx context.Context, version uuid.UUID, action func(context.Context, stockproduct.CatalogueProduct) error) error {
	if version == uuid.Nil {
		return stockproduct.ErrIneligible
	}
	if g.db == nil || action == nil {
		return stockproduct.ErrUnavailable
	}
	return g.db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(?)`, publicationLock).Error; err != nil {
			return stockproduct.ErrUnavailable
		}
		rows, err := catalogueProducts(tx, `WHERE p.version_id=? AND pv.status='active'`, version)
		if err != nil {
			return stockproduct.ErrUnavailable
		}
		if len(rows) != 1 {
			return stockproduct.ErrIneligible
		}
		return action(ctx, rows[0])
	})
}
