package pharmacy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVerificationFulfilmentBindsExactSourceAndRequestedQuantities(t *testing.T) {
	source, err := prescriptionReviewContext(uuid.MustParse("11111111-1111-7111-8111-111111111111"), uuid.MustParse("0199e000-0000-7000-8000-000000000004"), "synthetic-preparer", reviewSourceFixture())
	require.NoError(t, err)
	input := PrescriptionFulfilmentReviewInput{SourceStateHash: source.SourceStateHash, Lines: []PrescriptionFulfilmentReviewLineInput{{LineNumber: 1, QuantityUnits: 5}}}
	view, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.Equal(t, "proposed", view.State)
	require.Equal(t, "not_evaluated", view.ClinicalAuthority)
	require.Equal(t, "not_reserved", view.StockReservation)
	require.Equal(t, source.Record.Lines[0].ProductID, view.Lines[0].ProductID)
	require.EqualValues(t, 5, view.Lines[0].QuantityUnits)
	require.Len(t, view.FulfilmentStateHash, 64)
	identical, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.Equal(t, view.FulfilmentStateHash, identical.FulfilmentStateHash)
	input.Lines[0].QuantityUnits = 6
	changed, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.NotEqual(t, view.FulfilmentStateHash, changed.FulfilmentStateHash)
	source.ReaderSubject = "another-authorized-reader"
	another, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.Equal(t, changed.FulfilmentStateHash, another.FulfilmentStateHash)
}

func TestVerificationFulfilmentRejectsStaleOrUnavailablePrescription(t *testing.T) {
	source, err := prescriptionReviewContext(uuid.MustParse("11111111-1111-7111-8111-111111111111"), uuid.MustParse("0199e000-0000-7000-8000-000000000004"), "synthetic-preparer", reviewSourceFixture())
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*PrescriptionFulfilmentReviewInput)
	}{
		{"stale source", func(in *PrescriptionFulfilmentReviewInput) { in.SourceStateHash = strings.Repeat("a", 64) }},
		{"missing lines", func(in *PrescriptionFulfilmentReviewInput) { in.Lines = nil }},
		{"duplicate line", func(in *PrescriptionFulfilmentReviewInput) { in.Lines = append(in.Lines, in.Lines[0]) }},
		{"unknown line", func(in *PrescriptionFulfilmentReviewInput) { in.Lines[0].LineNumber = 2 }},
		{"overfill", func(in *PrescriptionFulfilmentReviewInput) { in.Lines[0].QuantityUnits = 16 }},
		{"zero quantity", func(in *PrescriptionFulfilmentReviewInput) { in.Lines[0].QuantityUnits = 0 }},
		{"negative quantity", func(in *PrescriptionFulfilmentReviewInput) { in.Lines[0].QuantityUnits = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := PrescriptionFulfilmentReviewInput{SourceStateHash: source.SourceStateHash, Lines: []PrescriptionFulfilmentReviewLineInput{{LineNumber: 1, QuantityUnits: 5}}}
			tc.change(&in)
			_, err := prescriptionFulfilmentReview(source, in)
			require.Error(t, err)
		})
	}
	source.Record.Lines[0].DispensingsRemaining = 0
	source, err = prescriptionReviewContext(source.OwnerTenantID, source.PharmacyID, source.ReaderSubject, source.Record)
	require.NoError(t, err)
	_, err = prescriptionFulfilmentReview(source, PrescriptionFulfilmentReviewInput{SourceStateHash: source.SourceStateHash, Lines: []PrescriptionFulfilmentReviewLineInput{{LineNumber: 1, QuantityUnits: 5}}})
	require.ErrorIs(t, err, errFulfilmentUnavailable)
}

func TestVerificationFulfilmentCanonicalOrderAndUnchangedInputs(t *testing.T) {
	record := reviewSourceFixture()
	second := record.Lines[0]
	second.LineNumber = 2
	second.ProductID = uuid.MustParse("0199e000-0000-7000-8000-000000000007")
	record.Lines = append(record.Lines, second)
	source, err := prescriptionReviewContext(uuid.MustParse("11111111-1111-7111-8111-111111111111"), uuid.MustParse("0199e000-0000-7000-8000-000000000004"), "synthetic-preparer", record)
	require.NoError(t, err)
	input := PrescriptionFulfilmentReviewInput{SourceStateHash: source.SourceStateHash, Lines: []PrescriptionFulfilmentReviewLineInput{{LineNumber: 2, QuantityUnits: 2}, {LineNumber: 1, QuantityUnits: 5}}}
	one, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.Equal(t, 2, input.Lines[0].LineNumber)
	input.Lines[0], input.Lines[1] = input.Lines[1], input.Lines[0]
	two, err := prescriptionFulfilmentReview(source, input)
	require.NoError(t, err)
	require.Equal(t, one.FulfilmentStateHash, two.FulfilmentStateHash)
	require.Equal(t, 1, two.Lines[0].LineNumber)
}
