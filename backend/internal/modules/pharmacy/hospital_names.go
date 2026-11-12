package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

// hospitalNameConsumer keeps the newest announced name of every hospital in
// the event's tenant (#1637). Redelivery and out-of-order delivery are
// no-ops: a stored row changes only for a strictly greater name_version.
func hospitalNameConsumer() events.Consumer {
	return events.Consumer{Name: "pharmacy-hospital-names", Subject: workforcecontract.SubjectHospitalNamed, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d workforcecontract.HospitalNamedData
		if evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid hospital name event")
		}
		hospital, err := uuid.Parse(d.HospitalID)
		name := strings.TrimSpace(d.Name)
		if err != nil || hospital == uuid.Nil || name == "" || utf8.RuneCountInString(name) > 150 || d.NameVersion < 1 {
			return errors.New("invalid hospital name event")
		}
		if err := tx.Exec(`INSERT INTO pharmacy_hospital_names (tenant_id,hospital_id,name,name_version) VALUES (?,?,?,?)
 ON CONFLICT (tenant_id,hospital_id) DO UPDATE SET name=EXCLUDED.name, name_version=EXCLUDED.name_version, updated_at=clock_timestamp()
 WHERE pharmacy_hospital_names.name_version < EXCLUDED.name_version`, evt.TenantID, hospital, name, d.NameVersion).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.HospitalNameReceivedData{HospitalID: d.HospitalID, NameVersion: d.NameVersion})
		if err != nil {
			return err
		}
		// A stale version is confirmed too, so its re-sending stops.
		subject := pharmacycontract.SubjectHospitalNameReceived
		return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: evt.TenantID, Data: data})
	}}
}

// fillHospitalNames sets HospitalName on verified affiliations only. Call it
// after fillVerification, which sets VerificationStatus.
func fillHospitalNames(tx *gorm.DB, tenant string, rows []hospitalAffiliation) error {
	var ids []uuid.UUID
	for i := range rows {
		// Fails closed: only the exact status "verified" discloses a name, so
		// any other status, including one added later, shows nothing.
		if rows[i].VerificationStatus == "verified" {
			ids = append(ids, rows[i].HospitalID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var named []struct {
		HospitalID uuid.UUID
		Name       string
	}
	if err := tx.Raw(`SELECT hospital_id, name FROM pharmacy_hospital_names WHERE tenant_id=? AND hospital_id IN ?`, tenant, ids).Scan(&named).Error; err != nil {
		return err
	}
	by := make(map[uuid.UUID]string, len(named))
	for _, n := range named {
		by[n.HospitalID] = n.Name
	}
	for i := range rows {
		if rows[i].VerificationStatus != "verified" {
			continue
		}
		if name, ok := by[rows[i].HospitalID]; ok {
			rows[i].HospitalName = &name
		}
	}
	return nil
}
