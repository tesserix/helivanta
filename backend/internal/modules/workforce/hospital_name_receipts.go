package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

// An announced hospital name is re-sent until pharmacy confirms it (#1653).
// The bus dead-letters after five failed deliveries and nothing replays the
// dead-letter subject, so without this a lost name would leave pharmacy
// without it for good.

//go:embed hospital_name_receipts.sql
var hospitalNameReceiptsSchema string

const (
	// firstRedelivery leaves time for the normal delivery and its confirmation.
	firstRedelivery = 2 * time.Minute
	maxRedelivery   = time.Hour
)

// redeliveryDelay doubles from two minutes and stops growing at an hour;
// a name is never given up on.
func redeliveryDelay(attempts int) time.Duration {
	d := firstRedelivery
	for i := 1; i < attempts && d < maxRedelivery; i++ {
		d *= 2
	}
	return min(d, maxRedelivery)
}

// hospitalNameReceiptConsumer stops re-sending a name pharmacy has stored. A
// confirmation older than the announced version, or for an unknown hospital,
// matches no row and changes nothing.
func hospitalNameReceiptConsumer() events.Consumer {
	subject := pharmacycontract.SubjectHospitalNameReceived
	return events.Consumer{Name: "workforce-hospital-name-receipt", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d pharmacycontract.HospitalNameReceivedData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil || d.NameVersion < 1 {
			return errors.New("invalid hospital name receipt event")
		}
		hospital, err := uuid.Parse(d.HospitalID)
		if err != nil || hospital == uuid.Nil {
			return errors.New("invalid hospital name receipt event")
		}
		return tx.Exec(`UPDATE workforce_hospital_name_announcements SET next_attempt_at=NULL,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND announced_version<=? AND next_attempt_at IS NOT NULL`,
			evt.TenantID, hospital, d.NameVersion).Error
	}}
}
