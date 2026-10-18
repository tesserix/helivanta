package emergency_test

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/mocks"
	"gorm.io/gorm"
	"testing"
)

func TestReceivingConsumerChecksCommittedDirectionAndOnlySignalsItsProvider(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	var raw string
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Select("payload").Where("subject=?", emergencycontract.SubjectReceivingChanged).Limit(1).Scan(&raw).Error
	}))
	var evt events.Event
	require.NoError(t, json.Unmarshal([]byte(raw), &evt))
	c := &mocks.Client{}
	consumer := emergency.ReceivingConsumer(c)
	invalid := evt
	invalid.TenantID = testutil.TenantA
	require.Error(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, invalid) }))
	require.Error(t, f.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, evt) }))
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, evt) }))
	input := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: a.dispatch}
	id, err := emergency.DispatchWorkflowID(input)
	require.NoError(t, err)
	c.On("ExecuteWorkflow", mock.Anything, mock.Anything, emergency.DispatchWorkflowName, input).Return(nil, nil).Once()
	c.On("SignalWorkflow", mock.Anything, id, "", emergency.DispatchVersionSignal, int64(2)).Return(nil).Once()
	require.NoError(t, consumer.AfterCommit(context.Background(), evt))
	c.AssertExpectations(t)
}
