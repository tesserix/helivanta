package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"testing"
	"time"
)

func TestReviewedCorrectionPreservesOriginalReceiptAndReconcilesCustody(t *testing.T) {
	shipment, input, receiver, at := shipmentFixture(t)
	original, err := Receive(uuid.Must(uuid.NewV7()), shipment, input, ReceivingTotals{}, receiver, at)
	require.NoError(t, err)
	reviewer := supplierorder.Actor{Subject: "TEST-independent-reviewer", StaffID: uuid.New()}
	totals := ReceivingTotals{Counted: 30, Accepted: 20, Rejected: 10}
	command := ReceivingCorrectionInput{ExpectedSummaryVersion: 2, ExpectedReceiptVersion: 1, CountedUnits: 28, AcceptedUnits: 18, RejectedUnits: 10, Reason: "TEST independent recount found two fewer accepted units"}
	result, err := CorrectReceiving(uuid.Must(uuid.NewV7()), shipment, original, ReceiptCorrectionState{}, totals, 2, command, reviewer, at.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, original.ID, result.ReceiptID)
	require.Equal(t, int64(2), result.Version)
	require.Equal(t, int64(-2), result.AcceptedDeltaUnits)
	require.Equal(t, int64(28), result.CumulativeCountedUnits)
	require.Equal(t, int64(18), result.CumulativeAcceptedUnits)
	require.Equal(t, int64(10), result.CumulativeRejectedUnits)
	require.NotNil(t, result.MovementID)
	require.Equal(t, int64(30), original.Input.CountedUnits)
	require.Equal(t, int64(20), original.Input.AcceptedUnits)
	_, err = CorrectReceiving(uuid.Must(uuid.NewV7()), shipment, original, ReceiptCorrectionState{}, totals, 2, command, receiver, at.Add(time.Minute))
	require.ErrorIs(t, err, ErrConflict)
	command.ExpectedSummaryVersion = 1
	_, err = CorrectReceiving(uuid.Must(uuid.NewV7()), shipment, original, ReceiptCorrectionState{}, totals, 2, command, reviewer, at.Add(time.Minute))
	require.ErrorIs(t, err, ErrConflict)
}

func TestCorrectionsCannotRemapRejectedCustodyOrExceedShipment(t *testing.T) {
	shipment, input, receiver, at := shipmentFixture(t)
	input.AcceptedUnits = 0
	input.RejectedUnits = input.CountedUnits
	input.IdentityConfirmed = false
	input.DestinationProductID = nil
	input.DestinationProductVersion = nil
	input.DestinationBatchID = nil
	input.QuarantineLocationID = nil
	original, err := Receive(uuid.Must(uuid.NewV7()), shipment, input, ReceivingTotals{}, receiver, at)
	require.NoError(t, err)
	reviewer := supplierorder.Actor{Subject: "TEST-independent-reviewer", StaffID: uuid.New()}
	totals := ReceivingTotals{Counted: 30, Rejected: 30}
	command := ReceivingCorrectionInput{ExpectedSummaryVersion: 1, ExpectedReceiptVersion: 1, CountedUnits: 30, AcceptedUnits: 1, RejectedUnits: 29, IdentityConfirmed: true, Reason: "TEST cannot map previously rejected custody"}
	_, err = CorrectReceiving(uuid.Must(uuid.NewV7()), shipment, original, ReceiptCorrectionState{}, totals, 1, command, reviewer, at)
	require.ErrorIs(t, err, ErrConflict)
	command.CountedUnits = 0
	command.AcceptedUnits = 0
	command.RejectedUnits = 0
	command.IdentityConfirmed = false
	result, err := CorrectReceiving(uuid.Must(uuid.NewV7()), shipment, original, ReceiptCorrectionState{}, totals, 1, command, reviewer, at)
	require.NoError(t, err)
	require.Nil(t, result.MovementID)
	require.Zero(t, result.CumulativeCountedUnits)
	// After the correction, a new verified receipt is the independent stock posting.
	_, verified, _, _ := shipmentFixture(t)
	verified.CountedUnits = 10
	verified.AcceptedUnits = 10
	verified.RejectedUnits = 0
	verified.ObservedLotNumber = shipment.LotNumber
	verified.ObservedExpiresOn = shipment.ExpiresOn
	counted, err := Receive(uuid.Must(uuid.NewV7()), shipment, verified, ReceivingTotals{}, receiver, at)
	require.NoError(t, err)
	require.Equal(t, int64(10), counted.CumulativeAcceptedUnits)
}
