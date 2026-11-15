package workforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func assignmentGovernanceScopeRequest(evt events.Event) (iamcontract.AssignmentGovernanceScopeRequestedData, error) {
	var request iamcontract.AssignmentGovernanceScopeRequestedData
	tenant, err := uuid.Parse(evt.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Type != iamcontract.SubjectAssignmentGovernanceScopeRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 || !utf8.Valid(evt.Data) {
		return request, errors.New("invalid native assignment request envelope")
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return request, errors.New("native assignment request must be an object")
	}
	seen := map[string]bool{"proposal_id": false, "request_id": false, "hospital_id": false}
	count := 0
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		previous, allowed := seen[key]
		if err != nil || !ok || !allowed || previous {
			return request, errors.New("ambiguous native assignment request")
		}
		var field json.RawMessage
		if decoder.Decode(&field) != nil || bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return request, errors.New("invalid native assignment field")
		}
		seen[key] = true
		count++
	}
	end, err := decoder.Token()
	var extra any
	if err != nil || end != json.Delim('}') || count != len(seen) || !errors.Is(decoder.Decode(&extra), io.EOF) || json.Unmarshal(evt.Data, &request) != nil {
		return request, errors.New("incomplete native assignment request")
	}
	if request.ProposalID.Version() != 7 || request.ProposalID.Variant() != uuid.RFC4122 || request.RequestID.Version() != 7 || request.RequestID.Variant() != uuid.RFC4122 || request.HospitalID == uuid.Nil || request.HospitalID.Variant() != uuid.RFC4122 {
		return request, errors.New("invalid native assignment scope identity")
	}

	return request, nil
}

func assignmentGovernanceScopeConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "workforce-assignment-governance-scope", Subject: iamcontract.SubjectAssignmentGovernanceScopeRequested, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		request, err := assignmentGovernanceScopeRequest(evt)
		if err != nil {
			return err
		}
		if bus == nil {
			return errors.New("native assignment scope transport unavailable")
		}
		var eligible bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind='hospital')`, evt.TenantID, request.HospitalID).Scan(&eligible).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.AssignmentGovernanceScopeVerifiedData{ProposalID: request.ProposalID, RequestID: request.RequestID, HospitalID: request.HospitalID, Eligible: eligible})
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return bus.Publish(tx, workforcecontract.SubjectAssignmentGovernanceScopeVerified, events.Event{ID: id.String(), Type: workforcecontract.SubjectAssignmentGovernanceScopeVerified, Version: 1, TenantID: evt.TenantID, OccurredAt: time.Now(), Data: data})
	}}
}
