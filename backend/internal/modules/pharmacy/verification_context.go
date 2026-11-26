package pharmacy

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

var verificationReader = regexp.MustCompile(verificationReaderPattern)

var errPrescriptionReviewSource = errors.New("prescription review source unavailable")

// prescriptionReviewContext fingerprints recorded source facts, not a proposed supply or a clinical approval.
func prescriptionReviewContext(tenant, pharmacy uuid.UUID, reader string, record prescription) (PrescriptionReviewContext, error) {
	var empty PrescriptionReviewContext
	if tenant == uuid.Nil || pharmacy == uuid.Nil || (!prescriptionText(reader, 200) || !verificationReader.MatchString(reader)) || record.ID == uuid.Nil || record.OutletID == uuid.Nil || record.CreatedAt.IsZero() || !prescriptionText(record.RecordedBy, 200) || len(record.Lines) < 1 || len(record.Lines) > 20 {
		return empty, errPrescriptionReviewSource
	}
	in := prescriptionInput{PrescriberName: record.PrescriberName, PrescriberRegistration: record.PrescriberRegistration, PrescriberCouncil: record.PrescriberCouncil, PrescribedOn: record.PrescribedOn, PatientName: record.PatientName, PatientAddress: record.PatientAddress, PrescriberAddress: record.PrescriberAddress}
	if !validPrescription(in) || strings.HasPrefix(record.PrescribedOn, "0000-") {
		return empty, errPrescriptionReviewSource
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.Lines = slices.Clone(record.Lines)
	slices.SortFunc(record.Lines, func(a, b prescriptionLine) int { return cmp.Compare(a.LineNumber, b.LineNumber) })
	for i, line := range record.Lines {
		if line.LineNumber != i+1 || line.ProductID == uuid.Nil || !prescriptionText(line.PrescribedAs, 200) || line.QuantityUnits < 1 || line.QuantityUnits > 100000 || line.DispensingsAllowed < 1 || line.DispensingsAllowed > 12 || line.UnitsRemaining < 0 || line.UnitsRemaining > line.QuantityUnits || line.DispensingsRemaining < 0 || line.DispensingsRemaining > line.DispensingsAllowed {
			return empty, errPrescriptionReviewSource
		}
	}
	encoded, err := json.Marshal(struct {
		Domain   string       `json:"domain"`
		Tenant   uuid.UUID    `json:"tenant"`
		Pharmacy uuid.UUID    `json:"pharmacy"`
		Record   prescription `json:"record"`
	}{"helivanta.pharmacy.prescription-source.v1", tenant, pharmacy, record})
	if err != nil {
		return empty, errPrescriptionReviewSource
	}
	digest := sha256.Sum256(encoded)
	return PrescriptionReviewContext{
		OwnerTenantID: tenant, PharmacyID: pharmacy, ReaderSubject: reader, SourceSchemaVersion: 1,
		SourceKind: "recorded_prescription", SourceAuthenticity: "not_evaluated",
		SourceStateHash: hex.EncodeToString(digest[:]), ClinicalAuthority: "not_evaluated",
		ClinicalContext: UnavailablePrescriptionClinicalContext{Allergies: "unavailable", ActiveMedications: "unavailable", PatientAge: "unavailable", PatientWeight: "unavailable", ClinicalFlags: "unavailable"},
		Record:          record,
	}, nil
}

func (h *catalogueHandlers) getPrescriptionReviewContext(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var view PrescriptionReviewContext
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		record, err := loadPrescription(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		tenant, err := uuid.Parse(p.TenantID)
		if err != nil {
			return errPrescriptionReviewSource
		}
		view, err = prescriptionReviewContext(tenant, currentPharmacy(c.Request.Context()), p.Subject, record)
		if err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "prescription", p.TenantID, p.Subject, id, "record_read")
	})
	if errors.Is(err, errPrescriptionReviewSource) {
		respond.Error(c, http.StatusServiceUnavailable, "review_context_unavailable", "The recorded prescription could not be checked. No clinical verification has been recorded.")
		return
	}
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, view)
}
