package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
)

var (
	errFulfilmentInput         = errors.New("invalid proposed prescription fulfilment")
	errFulfilmentSourceChanged = errors.New("prescription source changed")
	errFulfilmentUnavailable   = errors.New("prescription allowance unavailable")
)

func validPrescriptionFulfilmentInput(in PrescriptionFulfilmentReviewInput) bool {
	if !verificationHash.MatchString(in.SourceStateHash) || len(in.Lines) < 1 || len(in.Lines) > verificationFulfilmentMaxLines {
		return false
	}
	seen := map[int]bool{}
	for _, line := range in.Lines {
		if line.LineNumber < 1 || line.LineNumber > verificationFulfilmentMaxLines || line.QuantityUnits < 1 || line.QuantityUnits > verificationFulfilmentMaxQuantity || seen[line.LineNumber] {
			return false
		}
		seen[line.LineNumber] = true
	}
	return true
}

func prescriptionFulfilmentReview(source PrescriptionReviewContext, in PrescriptionFulfilmentReviewInput) (PrescriptionFulfilmentReviewContext, error) {
	var empty PrescriptionFulfilmentReviewContext
	if !validPrescriptionFulfilmentInput(in) {
		return empty, errFulfilmentInput
	}
	checked, err := prescriptionReviewContext(source.OwnerTenantID, source.PharmacyID, source.ReaderSubject, source.Record)
	if err != nil || checked.SourceStateHash != source.SourceStateHash {
		return empty, errPrescriptionReviewSource
	}
	if in.SourceStateHash != checked.SourceStateHash {
		return empty, errFulfilmentSourceChanged
	}
	requested := slices.Clone(in.Lines)
	slices.SortFunc(requested, func(a, b PrescriptionFulfilmentReviewLineInput) int { return a.LineNumber - b.LineNumber })
	lines := make([]PrescriptionFulfilmentReviewLine, 0, len(requested))
	for _, line := range requested {
		if line.LineNumber > len(checked.Record.Lines) {
			return empty, errFulfilmentUnavailable
		}
		recorded := checked.Record.Lines[line.LineNumber-1]
		if recorded.DispensingsRemaining < 1 || line.QuantityUnits > recorded.UnitsRemaining {
			return empty, errFulfilmentUnavailable
		}
		lines = append(lines, PrescriptionFulfilmentReviewLine{LineNumber: line.LineNumber, ProductID: recorded.ProductID, QuantityUnits: line.QuantityUnits})
	}
	encoded, err := json.Marshal(struct {
		Domain          string                             `json:"domain"`
		Tenant          uuid.UUID                          `json:"tenant"`
		Pharmacy        uuid.UUID                          `json:"pharmacy"`
		Outlet          uuid.UUID                          `json:"outlet"`
		Prescription    uuid.UUID                          `json:"prescription"`
		SourceStateHash string                             `json:"source_state_hash"`
		Lines           []PrescriptionFulfilmentReviewLine `json:"lines"`
	}{"helivanta.pharmacy.proposed-prescription-fulfilment.v1", checked.OwnerTenantID, checked.PharmacyID, checked.Record.OutletID, checked.Record.ID, checked.SourceStateHash, lines})
	if err != nil {
		return empty, errPrescriptionReviewSource
	}
	digest := sha256.Sum256(encoded)
	return PrescriptionFulfilmentReviewContext{Source: checked, Lines: lines, FulfilmentStateHash: hex.EncodeToString(digest[:]), State: "proposed", StockReservation: "not_reserved", ClinicalAuthority: "not_evaluated"}, nil
}
