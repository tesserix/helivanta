package pharmacy

import (
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
)

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	// pharmacy-visit-intake is retired (#1680): new visits open no dispense.
	return append([]events.Consumer{PrescriptionImageScanConsumer(deps.Workflows), LicenceEvidenceScanConsumer(deps.Workflows)}, append(append(paymentOutcomeConsumers(), affiliationDecisionConsumer(), affiliationReceiptConsumer(), registeredPatientConsumer(), hospitalNameConsumer()), append(occupancyConsumers(), opdEncounterConsumers()...)...)...)
}

// Broadcasts declares none: pharmacy has no per-replica cache to
// invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
