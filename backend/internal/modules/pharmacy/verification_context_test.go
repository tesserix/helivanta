package pharmacy

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func reviewSourceFixture() prescription {
	return prescription{
		ID:             uuid.MustParse("0199e000-0000-7000-8000-000000000001"),
		OutletID:       uuid.MustParse("0199e000-0000-7000-8000-000000000002"),
		PrescriberName: "Synthetic Prescriber", PrescriberRegistration: "QA/123",
		PrescriberCouncil: "Synthetic council", PrescribedOn: "2026-11-25",
		PatientName: "Synthetic Patient", RecordedBy: "synthetic-pharmacist",
		CreatedAt: time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC),
		Lines:     []prescriptionLine{{LineNumber: 1, PrescribedAs: "Synthetic product", ProductID: uuid.MustParse("0199e000-0000-7000-8000-000000000003"), QuantityUnits: 20, DispensingsAllowed: 2, UnitsRemaining: 15, DispensingsRemaining: 1}},
	}
}

func TestPrescriptionReviewSourceIsScopedAndNotClinicalAuthority(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	pharmacy := uuid.MustParse("0199e000-0000-7000-8000-000000000004")
	record := reviewSourceFixture()
	view, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", record)
	if err != nil {
		t.Fatal(err)
	}
	if view.SourceKind != "recorded_prescription" || view.SourceAuthenticity != "not_evaluated" || view.ClinicalAuthority != "not_evaluated" {
		t.Fatal("record capture must not assert authenticity or clinical authority")
	}
	if view.ClinicalContext.Allergies != "unavailable" || view.ClinicalContext.ActiveMedications != "unavailable" || view.ClinicalContext.PatientAge != "unavailable" || view.ClinicalContext.PatientWeight != "unavailable" || view.ClinicalContext.ClinicalFlags != "unavailable" {
		t.Fatal("absent clinical data must not become clear findings")
	}
	if view.SourceStateHash != "08d8c47d523b97edc83cd69023273f0a44ca8d6596571a5535f8bb1e40b2d78b" {
		t.Fatal("versioned source fingerprint changed")
	}
	if len(view.SourceStateHash) != 64 || strings.Trim(view.SourceStateHash, "0123456789abcdef") != "" {
		t.Fatal("source digest must be canonical SHA-256")
	}
	if view.Record.ID != record.ID || view.Record.PatientName != "Synthetic Patient" || view.Record.Lines[0].UnitsRemaining != 15 {
		t.Fatal("source context changed its recorded facts")
	}
	again, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", record)
	if err != nil || again.SourceStateHash != view.SourceStateHash {
		t.Fatal("same source must have stable digest")
	}
	other, err := prescriptionReviewContext(uuid.MustParse("22222222-2222-7222-8222-222222222222"), pharmacy, "synthetic-pharmacist", record)
	if err != nil || other.SourceStateHash == view.SourceStateHash {
		t.Fatal("digest must bind the owning tenant")
	}
	other, err = prescriptionReviewContext(tenant, uuid.MustParse("0199e000-0000-7000-8000-000000000005"), "synthetic-pharmacist", record)
	if err != nil || other.SourceStateHash == view.SourceStateHash {
		t.Fatal("digest must bind the owning pharmacy")
	}
	for name, change := range map[string]func(*prescription){
		"outlet":              func(p *prescription) { p.OutletID = uuid.MustParse("0199e000-0000-7000-8000-000000000006") },
		"remaining units":     func(p *prescription) { p.Lines[0].UnitsRemaining-- },
		"repeat balance":      func(p *prescription) { p.Lines[0].DispensingsRemaining-- },
		"prescribed quantity": func(p *prescription) { p.Lines[0].QuantityUnits++ },
		"prescribed text":     func(p *prescription) { p.Lines[0].PrescribedAs = "Synthetic changed source" },
	} {
		t.Run(name, func(t *testing.T) {
			p := reviewSourceFixture()
			change(&p)
			changed, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", p)
			if err != nil || changed.SourceStateHash == view.SourceStateHash {
				t.Fatal("changed source reused prior digest")
			}
		})
	}
}

func TestPrescriptionReviewSourceCanonicalOrderAndTime(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	pharmacy := uuid.MustParse("0199e000-0000-7000-8000-000000000004")
	p := reviewSourceFixture()
	second := p.Lines[0]
	second.LineNumber = 2
	second.ProductID = uuid.MustParse("0199e000-0000-7000-8000-000000000007")
	p.Lines = append(p.Lines, second)
	one, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", p)
	if err != nil {
		t.Fatal(err)
	}
	p.Lines[0], p.Lines[1] = p.Lines[1], p.Lines[0]
	p.CreatedAt = p.CreatedAt.In(time.FixedZone("synthetic", 19800))
	two, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", p)
	if err != nil || one.SourceStateHash != two.SourceStateHash {
		t.Fatal("ordering or time-zone presentation changed source identity")
	}
	if p.Lines[0].LineNumber != 2 {
		t.Fatal("context construction mutated caller's source")
	}
}

func TestPrescriptionReviewSourceRejectsIncompleteOrInconsistentFacts(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	pharmacy := uuid.MustParse("0199e000-0000-7000-8000-000000000004")
	for name, change := range map[string]func(*prescription){
		"missing id":            func(p *prescription) { p.ID = uuid.Nil },
		"missing outlet":        func(p *prescription) { p.OutletID = uuid.Nil },
		"missing recorded time": func(p *prescription) { p.CreatedAt = time.Time{} },
		"missing lines":         func(p *prescription) { p.Lines = nil },
		"invalid product":       func(p *prescription) { p.Lines[0].ProductID = uuid.Nil },
		"negative balance":      func(p *prescription) { p.Lines[0].UnitsRemaining = -1 },
		"excess balance":        func(p *prescription) { p.Lines[0].UnitsRemaining = 21 },
		"excess repeats":        func(p *prescription) { p.Lines[0].DispensingsRemaining = 3 },
		"duplicate line":        func(p *prescription) { p.Lines = append(p.Lines, p.Lines[0]) },
		"nonpositive line":      func(p *prescription) { p.Lines[0].LineNumber = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			p := reviewSourceFixture()
			change(&p)
			if _, err := prescriptionReviewContext(tenant, pharmacy, "synthetic-pharmacist", p); err == nil {
				t.Fatal("inconsistent source produced review context")
			}
		})
	}
	for _, reader := range []string{"", "invalid reader", "subject@example.com", "subject\n"} {
		if _, err := prescriptionReviewContext(tenant, pharmacy, reader, reviewSourceFixture()); err == nil {
			t.Fatal("invalid reader identity accepted")
		}
	}
	if _, err := prescriptionReviewContext(uuid.Nil, pharmacy, "synthetic-pharmacist", reviewSourceFixture()); err == nil {
		t.Fatal("missing tenant accepted")
	}
	if _, err := prescriptionReviewContext(tenant, uuid.Nil, "synthetic-pharmacist", reviewSourceFixture()); err == nil {
		t.Fatal("missing pharmacy accepted")
	}
}
