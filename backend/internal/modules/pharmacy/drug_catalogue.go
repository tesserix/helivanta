package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) searchDrugCatalogue(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	if err := h.transaction(c.Request.Context(), p, outlet, false, func(*gorm.DB) error { return nil }); err != nil {
		catalogueError(c, err)
		return
	}
	if len(c.Request.URL.Query()) != 1 || len(c.QueryArray("q")) != 1 {
		respond.BadRequest(c, stockproduct.ErrInvalidSearch)
		return
	}
	if h.drugCatalogue == nil {
		drugCatalogueError(c, stockproduct.ErrUnavailable)
		return
	}
	rows, err := h.drugCatalogue.SearchCatalogue(c.Request.Context(), c.Query("q"))
	if err != nil {
		drugCatalogueError(c, err)
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

func (h *catalogueHandlers) importDrugProduct(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	// Authorize before resolving global reference data, then reauthorize under
	// the source's publication lock through the outlet commit.
	if err := h.transaction(c.Request.Context(), p, outlet, true, func(*gorm.DB) error { return nil }); err != nil {
		catalogueError(c, err)
		return
	}
	var in stockproduct.CatalogueImport
	if !decodeCatalogue(c, &in) {
		return
	}
	if in.Currency != "INR" || in.MrpMinor == nil || *in.MrpMinor < 0 || *in.MrpMinor > 1_000_000_000_000 {
		respond.BadRequest(c, errors.New("enter a valid INR pack MRP"))
		return
	}
	var replay *catalogueProduct
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var rows []catalogueProduct
		if err := tx.Select(productColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		var revision catalogueRevision
		if err := tx.Where("tenant_id=? AND outlet_id=? AND product_id=? AND version=1", p.TenantID, outlet, id).First(&revision).Error; err != nil {
			return err
		}
		var original catalogueProduct
		if err := json.Unmarshal(revision.Snapshot, &original); err != nil {
			return err
		}
		if original.DrugMaster == nil || original.DrugMaster.VersionID != in.VersionID || original.Currency != in.Currency || original.MRPMinor != *in.MrpMinor {
			return errCatalogueVersion
		}
		replay = &original
		return nil
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	if replay != nil {
		respond.OK(c, replay)
		return
	}
	if h.drugCatalogue == nil {
		drugCatalogueError(c, stockproduct.ErrUnavailable)
		return
	}
	var result catalogueProduct
	err = h.drugCatalogue.WithCatalogueProduct(c.Request.Context(), in.VersionID, func(ctx context.Context, source stockproduct.CatalogueProduct) error {
		fields := importedProductFields(source, in)
		if !fields.normalize() || binding.Validator.ValidateStruct(&fields) != nil {
			return stockproduct.ErrIneligible
		}
		zero := int64(0)
		return h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
			var err error
			result, err = h.persistProduct(ctx, tx, p, outlet, id, productInput{ProductFields: fields, ExpectedVersion: &zero}, &source)
			return err
		})
	})
	if err != nil {
		drugCatalogueError(c, err)
		return
	}
	respond.OK(c, result)
}

func importedProductFields(source stockproduct.CatalogueProduct, in stockproduct.CatalogueImport) ProductFields {
	fields := ProductFields{SKU: "DM-" + source.ProductID.String(), BrandName: source.BrandName,
		GenericName: source.FormulationLabel, DosageForm: source.DoseForm, Route: source.Route,
		Manufacturer: source.Manufacturer, Ingredients: []ingredient{}, PackSize: source.PackSize,
		UnitOfIssue: source.UnitOfIssue, Currency: in.Currency, MRPMinor: *in.MrpMinor, Status: "active"}
	for _, x := range source.Ingredients {
		unit := x.StrengthUnit
		if x.PerValue != nil && x.PerUnit != nil {
			unit += "/" + *x.PerValue + " " + *x.PerUnit
		}
		fields.Ingredients = append(fields.Ingredients, ingredient{Name: x.InnName, Strength: x.StrengthValue, Unit: unit})
	}
	fields.SKU = strings.ToUpper(fields.SKU)
	return fields
}

func drugCatalogueError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, stockproduct.ErrInvalidSearch):
		respond.BadRequest(c, err)
	case errors.Is(err, stockproduct.ErrIneligible):
		respond.Conflict(c, "the medicine version is no longer current and active; search again")
	case errors.Is(err, stockproduct.ErrUnavailable):
		respond.Error(c, 503, "catalogue_unavailable", "The published medicine catalogue could not be verified.")
	default:
		catalogueError(c, err)
	}
}
