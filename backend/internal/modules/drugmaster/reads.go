package drugmaster

import (
	"errors"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// The catalogue's dates are India's (spec D2): "today" is today in IST.
const todayIST = `(now() AT TIME ZONE 'Asia/Kolkata')::date`

// searchLimit bounds the typeahead. It is a pick list, not a collection a
// client pages through, so the best matches are all it ever returns.
const searchLimit = 20

type readHandlers struct{ db *tenantdb.DB }

type Ingredient struct {
	MoleculeID    uuid.UUID `json:"molecule_id"`
	INNName       string    `json:"inn_name"`
	StrengthValue string    `json:"strength_value"`
	StrengthUnit  string    `json:"strength_unit"`
	PerValue      *string   `json:"per_value"`
	PerUnit       *string   `json:"per_unit"`
}

// Formulation is one formulation version: what a prescription stores
// (version_id) and the facts that applied under it.
type Formulation struct {
	FormulationID           uuid.UUID    `json:"formulation_id"`
	VersionID               uuid.UUID    `json:"version_id"`
	ValidFrom               string       `json:"valid_from"`
	Label                   string       `json:"label"`
	DoseForm                string       `json:"dose_form"`
	Route                   string       `json:"route"`
	RegulatoryClass         string       `json:"regulatory_class"`
	NDPS                    bool         `json:"ndps"`
	TeleconsultPrescribable bool         `json:"teleconsult_prescribable"`
	Ingredients             []Ingredient `json:"ingredients" gorm:"-"`
}

type SearchResult struct {
	Formulation
	// ConfusableWith lists the other results a hurried pick could mix up
	// with this one, so the UI forces a deliberate choice (spec D5).
	ConfusableWith []uuid.UUID `json:"confusable_with"`
}

const formulationColumns = `v.formulation_id, v.id AS version_id, v.valid_from::text AS valid_from, v.label, v.dose_form, v.route,
	v.regulatory_class, v.ndps, v.teleconsult_prescribable`

// upperBound is the first key after every key starting with prefix.
// Keys hold only [a-z0-9] (searchKey and the generated columns agree),
// so bumping the last byte cannot overflow.
func upperBound(prefix string) string {
	b := []byte(prefix)
	b[len(b)-1]++
	return string(b)
}

func (h *readHandlers) search(c *gin.Context) {
	key := searchKey(c.Query("q"))
	if len(key) < 2 {
		respond.BadRequest(c, errors.New("q needs at least two letters or digits"))
		return
	}
	form, class := c.Query("form"), c.Query("class")
	if (form != "" && !doseForms[form]) || (class != "" && !classes[class]) {
		respond.BadRequest(c, errors.New("unknown form or class"))
		return
	}
	var rows []Formulation
	err := h.db.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
		// Range predicates rather than LIKE, so the text_pattern_ops
		// indexes serve every plan, generic ones included.
		q := `WITH candidates AS (
			SELECT v.formulation_id FROM drugmaster_molecules m
			 JOIN drugmaster_formulation_ingredients i ON i.molecule_id=m.id
			 JOIN drugmaster_formulation_versions v ON v.id=i.version_id
			 WHERE m.search_key ~>=~ ? AND m.search_key ~<~ ?
			UNION
			SELECT p.formulation_id FROM drugmaster_product_versions pv
			 JOIN drugmaster_products p ON p.id=pv.product_id
			 WHERE pv.search_key ~>=~ ? AND pv.search_key ~<~ ?)
			SELECT ` + formulationColumns + ` FROM candidates c
			JOIN LATERAL (SELECT x.* FROM drugmaster_formulation_versions x JOIN drugmaster_releases r ON r.id=x.release_id
			 WHERE x.formulation_id=c.formulation_id AND x.valid_from<=` + todayIST + `
			 ORDER BY x.valid_from DESC, r.sequence DESC LIMIT 1) v ON true
			WHERE (?='' OR v.dose_form=?) AND (?='' OR v.regulatory_class=?)
			ORDER BY v.label, v.formulation_id LIMIT ?`
		if err := tx.Raw(q, key, upperBound(key), key, upperBound(key), form, form, class, class, searchLimit).
			Scan(&rows).Error; err != nil {
			return err
		}
		return attachIngredients(tx, rows)
	})
	if err != nil {
		respond.InternalErr(c, err, "could not search the drug master")
		return
	}
	out := make([]SearchResult, len(rows))
	for i, r := range rows {
		out[i] = SearchResult{Formulation: r, ConfusableWith: []uuid.UUID{}}
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if confusable(out[i].Label, out[j].Label) {
				out[i].ConfusableWith = append(out[i].ConfusableWith, out[j].FormulationID)
				out[j].ConfusableWith = append(out[j].ConfusableWith, out[i].FormulationID)
			}
		}
	}
	respond.OK(c, gin.H{"data": out})
}

func attachIngredients(tx *gorm.DB, rows []Formulation) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	at := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		ids[i], at[r.VersionID] = r.VersionID, i
		rows[i].Ingredients = []Ingredient{}
	}
	var in []struct {
		VersionID uuid.UUID
		Ingredient
	}
	if err := tx.Raw(`SELECT i.version_id, i.molecule_id, m.inn_name, i.strength_value::text AS strength_value, i.strength_unit,
		i.per_value::text AS per_value, i.per_unit
		FROM drugmaster_formulation_ingredients i JOIN drugmaster_molecules m ON m.id=i.molecule_id
		WHERE i.version_id IN ? ORDER BY i.version_id, i.position`, ids).Scan(&in).Error; err != nil {
		return err
	}
	for _, x := range in {
		x.StrengthValue = canonicalDecimal(x.StrengthValue)
		if x.PerValue != nil {
			v := canonicalDecimal(*x.PerValue)
			x.PerValue = &v
		}
		rows[at[x.VersionID]].Ingredients = append(rows[at[x.VersionID]].Ingredients, x.Ingredient)
	}
	return nil
}

// confusable is true for labels identical apart from their numbers
// (strengths) or within two edits of each other.
func confusable(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return true
	}
	if withoutNumbers(a) == withoutNumbers(b) {
		return true
	}
	return levenshtein(a, b) <= 2
}

var numberRe = regexp.MustCompile(`[0-9]+([.,][0-9]+)?`)

func withoutNumbers(s string) string {
	return strings.Join(strings.Fields(numberRe.ReplaceAllString(s, "")), " ")
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// formulation answers with the version in force on ?at= (default today).
// A date before the first version is 404: a past audit is never answered
// with rules that did not exist yet (spec D5).
func (h *readHandlers) formulation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "formulation")
		return
	}
	at := c.Query("at")
	if at != "" {
		if _, err := parseDate(at); err != nil {
			respond.BadRequest(c, errors.New("at must be a date, YYYY-MM-DD"))
			return
		}
	}
	var rows []Formulation
	err = h.db.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT `+formulationColumns+` FROM drugmaster_formulation_versions v
			JOIN drugmaster_releases r ON r.id=v.release_id
			WHERE v.formulation_id=? AND v.valid_from<=COALESCE(NULLIF(?,'')::date,`+todayIST+`)
			ORDER BY v.valid_from DESC, r.sequence DESC LIMIT 1`, id, at).Scan(&rows).Error; err != nil {
			return err
		}
		return attachIngredients(tx, rows)
	})
	if err != nil {
		respond.InternalErr(c, err, "could not read the formulation")
		return
	}
	if len(rows) == 0 {
		respond.NotFound(c, "formulation")
		return
	}
	respond.OK(c, rows[0])
}

type Product struct {
	ProductID     uuid.UUID `json:"product_id"`
	VersionID     uuid.UUID `json:"version_id"`
	FormulationID uuid.UUID `json:"formulation_id"`
	ValidFrom     string    `json:"valid_from"`
	BrandName     string    `json:"brand_name"`
	Manufacturer  string    `json:"manufacturer"`
	PackSize      int       `json:"pack_size"`
	UnitOfIssue   string    `json:"unit_of_issue"`
	GTIN          *string   `json:"gtin"`
	HSNCode       string    `json:"hsn_code"`
	// Status is reported, never filtered on: a recalled product must still
	// resolve so the counter sees why it cannot dispense (spec D5).
	Status string `json:"status"`
}

const productQuery = `SELECT p.id AS product_id, v.id AS version_id, p.formulation_id, v.valid_from::text AS valid_from, v.brand_name,
	v.manufacturer, v.pack_size, v.unit_of_issue, v.gtin, v.hsn_code, v.status
	FROM drugmaster_products p JOIN LATERAL (SELECT x.* FROM drugmaster_product_versions x JOIN drugmaster_releases r ON r.id=x.release_id
	 WHERE x.product_id=p.id AND x.valid_from<=` + todayIST + ` ORDER BY x.valid_from DESC, r.sequence DESC LIMIT 1) v ON true `

func (h *readHandlers) product(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "product")
		return
	}
	h.oneProduct(c, productQuery+`WHERE p.id=?`, id)
}

func (h *readHandlers) productByGTIN(c *gin.Context) {
	gtin := c.Param("gtin")
	if !validGTIN(gtin) {
		respond.BadRequest(c, errors.New("not a valid GTIN"))
		return
	}
	// The subquery reaches the gtin index; the version in force must still hold it.
	gtin = gtin14(gtin)
	h.oneProduct(c, productQuery+`WHERE p.id IN (SELECT product_id FROM drugmaster_product_versions WHERE gtin=?) AND v.gtin=?`, gtin, gtin)
}

func (h *readHandlers) oneProduct(c *gin.Context, q string, args ...any) {
	var rows []Product
	if err := h.db.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
		return tx.Raw(q+` LIMIT 2`, args...).Scan(&rows).Error
	}); err != nil {
		respond.InternalErr(c, err, "could not read the product")
		return
	}
	switch len(rows) {
	case 0:
		respond.NotFound(c, "product")
	case 1:
		respond.OK(c, rows[0])
	default:
		// Publication refuses a GTIN on two products; two here means that
		// guard failed, and guessing which pack was scanned is unsafe.
		respond.InternalErr(c, errors.New("GTIN resolves to more than one product"), "ambiguous GTIN")
	}
}
