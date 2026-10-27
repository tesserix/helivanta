package drugmaster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	// ErrNotStaged means the release does not exist or was already decided.
	ErrNotStaged = errors.New("drug master release is not staged")
	// ErrReleaseMismatch means the release's rows are not what the
	// reviewed file says, so publishing would publish something unreviewed.
	ErrReleaseMismatch = errors.New("drug master release does not match the reviewed file")
)

// publicationLock serialises decisions; drugmaster_release_guard takes the
// same lock, so a caller that skips this still waits there.
const publicationLock = 476001

// Decide publishes or rejects a staged release. db must connect as
// helivanta_drug_approver. Publishing needs the release file the approver
// reviewed: the release must hold exactly what that file says (review
// C1). Its events are written in the same transaction, so a published
// release is never silent (spec D6). Rejecting needs no file, since
// nothing becomes visible.
func Decide(ctx context.Context, db *tenantdb.DB, release uuid.UUID, file []byte, decidedBy string, publish bool, reason string) error {
	if strings.TrimSpace(decidedBy) == "" || strings.TrimSpace(reason) == "" || len(reason) > 500 {
		return invalid("decided_by and a reason of at most 500 characters are required")
	}
	var f ReleaseFile
	if publish {
		var err error
		if f, err = ParseRelease(file); err != nil {
			return err
		}
	}
	return db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, publicationLock).Error; err != nil {
			return err
		}
		var r struct{ Status, FileSha256 string }
		if err := tx.Raw(`SELECT status, file_sha256 FROM drugmaster_releases WHERE id=? FOR UPDATE`, release).Scan(&r).Error; err != nil {
			return err
		}
		if r.Status != "staged" {
			return ErrNotStaged
		}
		if !publish {
			return tx.Exec(`UPDATE drugmaster_releases SET status='rejected',decided_by=?,decision_reason=? WHERE id=?`,
				decidedBy, reason, release).Error
		}
		sum := sha256.Sum256(file)
		if hex.EncodeToString(sum[:]) != r.FileSha256 {
			return errors.Join(ErrReleaseMismatch, errors.New("this is not the file that was staged"))
		}
		c, err := loadCatalogue(tx)
		if err != nil {
			return err
		}
		if err := verify(tx, release, f, c); err != nil {
			return err
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
		if err := tx.Raw(`UPDATE drugmaster_releases SET status='published',decided_by=?,decision_reason=? WHERE id=? RETURNING sequence`,
			decidedBy, reason, release).Scan(&sequence).Error; err != nil {
			return err
		}
		if err := enqueue(tx, contract.SubjectCataloguePublished, contract.CataloguePublishedData{
			ReleaseID: release.String(), Sequence: sequence, FormulationVersions: counts.Formulations, ProductVersions: counts.Products,
		}); err != nil {
			return err
		}
		var molecules []contract.MoleculePublishedData
		if err := tx.Raw(`SELECT id::text AS molecule_id, inn_name, COALESCE(atc_code,'') AS atc_code, release_id::text AS release_id
			FROM drugmaster_molecules WHERE release_id=? ORDER BY inn_name`, release).Scan(&molecules).Error; err != nil {
			return err
		}
		for _, m := range molecules {
			if err := enqueue(tx, contract.SubjectMoleculePublished, m); err != nil {
				return err
			}
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

func mismatch(what string) error { return errors.Join(ErrReleaseMismatch, errors.New(what)) }

// verify proves every row of the release is what the file says: each
// version equals the file's entry for its code, date included, and each
// identity the release created is one the file names. A file entry with no
// row is fine: staging skips what is already in force.
func verify(tx *gorm.DB, release uuid.UUID, f ReleaseFile, c catalogue) error {
	fileATC := map[string]string{}
	for _, m := range f.Molecules {
		fileATC[searchKey(m.INNName)] = deref(m.ATCCode)
	}
	atc := func(k string) string {
		if a, ok := fileATC[k]; ok {
			return a
		}
		return c.atc(k)
	}
	formulations := map[string]string{}
	for _, fm := range f.Formulations {
		formulations[fm.Code] = fm.ValidFrom + "@" + fileFormulationFact(fm, atc)
	}
	products := map[string]ProductIn{}
	for _, p := range f.Products {
		products[p.Code] = p
	}

	var molecules []struct{ SearchKey, ATCCode string }
	if err := tx.Raw(`SELECT search_key, COALESCE(atc_code,'') AS atc_code FROM drugmaster_molecules WHERE release_id=?`, release).Scan(&molecules).Error; err != nil {
		return err
	}
	for _, m := range molecules {
		if a, ok := fileATC[m.SearchKey]; !ok || a != m.ATCCode {
			return mismatch("molecule " + m.SearchKey + " is not in the file as staged")
		}
	}
	var codes []string
	if err := tx.Raw(`SELECT code FROM drugmaster_formulations WHERE release_id=?`, release).Scan(&codes).Error; err != nil {
		return err
	}
	for _, code := range codes {
		if _, ok := formulations[code]; !ok {
			return mismatch("formulation " + code + " is not in the file")
		}
	}
	var productIdentities []struct{ Code, FormulationCode string }
	if err := tx.Raw(`SELECT p.code, f.code AS formulation_code FROM drugmaster_products p JOIN drugmaster_formulations f ON f.id=p.formulation_id
		WHERE p.release_id=?`, release).Scan(&productIdentities).Error; err != nil {
		return err
	}
	for _, p := range productIdentities {
		if in, ok := products[p.Code]; !ok || in.Formulation != p.FormulationCode {
			return mismatch("product " + p.Code + " is not in the file as staged")
		}
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
		JOIN drugmaster_formulation_versions v ON v.id=i.version_id WHERE v.release_id=? ORDER BY i.version_id, i.position`, release).Scan(&ings).Error; err != nil {
		return err
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
		ID                                      uuid.UUID
		Code, ValidFrom                         string
		Label, DoseForm, Route, RegulatoryClass string
		NDPS, TeleconsultPrescribable           bool
	}
	if err := tx.Raw(`SELECT v.id, f.code, v.valid_from::text AS valid_from, v.label, v.dose_form, v.route, v.regulatory_class, v.ndps, v.teleconsult_prescribable
		FROM drugmaster_formulation_versions v JOIN drugmaster_formulations f ON f.id=v.formulation_id WHERE v.release_id=?`, release).Scan(&fvs).Error; err != nil {
		return err
	}
	for _, v := range fvs {
		got := v.ValidFrom + "@" + formulationFact(v.Label, v.DoseForm, v.Route, v.RegulatoryClass, v.NDPS, v.TeleconsultPrescribable, byVersion[v.ID])
		if formulations[v.Code] != got {
			return mismatch("formulation " + v.Code + " differs from the file")
		}
	}
	var pvs []struct {
		Code, FormulationCode, ValidFrom                            string
		BrandName, Manufacturer, UnitOfIssue, GTIN, HSNCode, Status string
		PackSize                                                    int
	}
	if err := tx.Raw(`SELECT p.code, f.code AS formulation_code, v.valid_from::text AS valid_from, v.brand_name, v.manufacturer, v.unit_of_issue,
		COALESCE(v.gtin,'') AS gtin, v.hsn_code, v.status, v.pack_size
		FROM drugmaster_product_versions v JOIN drugmaster_products p ON p.id=v.product_id JOIN drugmaster_formulations f ON f.id=p.formulation_id
		WHERE v.release_id=?`, release).Scan(&pvs).Error; err != nil {
		return err
	}
	for _, v := range pvs {
		in, ok := products[v.Code]
		got := v.ValidFrom + "@" + productFact(v.FormulationCode, v.BrandName, v.Manufacturer, v.PackSize, v.UnitOfIssue, v.GTIN, v.HSNCode, v.Status)
		if !ok || in.ValidFrom+"@"+fileProductFact(in) != got {
			return mismatch("product " + v.Code + " differs from the file")
		}
	}
	return nil
}

func enqueue(tx *gorm.DB, subject string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, Data: raw})
}

// inForceBefore selects the published version in force on n's date, a later
// release winning a tie: what n changes.
func inForceBefore(same string) string {
	return `JOIN drugmaster_releases r ON r.id=o.release_id WHERE r.status='published' AND o.valid_from<=n.valid_from AND ` + same +
		` ORDER BY o.valid_from DESC, r.sequence DESC LIMIT 1`
}

// reclassifications compares each new formulation version with the one in
// force on its date.
func reclassifications(tx *gorm.DB, release uuid.UUID) ([]contract.FormulationReclassifiedData, error) {
	var out []contract.FormulationReclassifiedData
	err := tx.Raw(`SELECT n.formulation_id::text AS formulation_id, n.id::text AS version_id, n.valid_from::text AS valid_from,
		p.regulatory_class AS from_class, n.regulatory_class AS to_class, p.ndps AS from_ndps, n.ndps AS to_ndps
		FROM drugmaster_formulation_versions n
		JOIN LATERAL (SELECT o.regulatory_class, o.ndps FROM drugmaster_formulation_versions o `+inForceBefore("o.formulation_id=n.formulation_id")+`) p ON true
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
		LEFT JOIN LATERAL (SELECT o.status FROM drugmaster_product_versions o `+inForceBefore("o.product_id=n.product_id")+`) p ON true
		WHERE n.release_id=? AND n.status IS DISTINCT FROM COALESCE(p.status,'active')
		ORDER BY n.product_id`, release).Scan(&out).Error
	return out, err
}

// ReplayMolecules re-announces every published molecule, for a consumer
// that started after they were published (#1373 A4). It changes nothing
// but the outbox. db must connect as helivanta_drug_approver.
func ReplayMolecules(ctx context.Context, db *tenantdb.DB) (int, error) {
	var molecules []contract.MoleculePublishedData
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		// The same set publication announced: every molecule a published
		// release introduced (review M8).
		if err := tx.Raw(`SELECT m.id::text AS molecule_id, m.inn_name, COALESCE(m.atc_code,'') AS atc_code, m.release_id::text AS release_id
			FROM drugmaster_molecules m JOIN drugmaster_releases r ON r.id=m.release_id WHERE r.status='published' ORDER BY m.inn_name`).Scan(&molecules).Error; err != nil {
			return err
		}
		for _, m := range molecules {
			if err := enqueue(tx, contract.SubjectMoleculePublished, m); err != nil {
				return err
			}
		}
		return nil
	})
	return len(molecules), err
}
