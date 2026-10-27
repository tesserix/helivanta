package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserix/helivanta/internal/bootstrap"
)

func TestReferenceCoversRuntimeModulesAndAuthenticationBoundaries(t *testing.T) {
	doc := inventory()
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Modules []struct {
			Name   string `json:"name"`
			Routes []struct {
				Method         string `json:"method"`
				Path           string `json:"path"`
				Authentication bool   `json:"authentication_required"`
				Membership     bool   `json:"membership_required"`
			} `json:"routes"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Modules) != len(bootstrap.Modules(nil, nil)) {
		t.Fatal("reference omits a runtime module")
	}
	found := map[string]bool{}
	for _, mod := range wire.Modules {
		for _, route := range mod.Routes {
			key := route.Method + " " + route.Path
			if found[key] {
				t.Fatalf("duplicate route %s", key)
			}
			found[key] = true
			switch key {
			case "GET /v1/workspace/branding":
				if !route.Authentication || !route.Membership {
					t.Fatal("authz.Public must still require authentication and membership")
				}
			case "POST /v1/iam/me/sign-out":
				if !route.Authentication || route.Membership {
					t.Fatal("sign-out must require authentication without membership")
				}
			}
		}
	}
	for _, key := range []string{"GET /v1/workspace/branding", "POST /v1/iam/me/sign-out", "GET /v1/medicore/visits", "GET /v1/pharmacy/dispenses", "POST /v1/patients/search"} {
		if !found[key] {
			t.Errorf("missing route %s", key)
		}
	}
	if !strings.Contains(string(encoded), "helivanta.in.medicore.visit_created.v1") || !strings.Contains(string(encoded), "lab-visit-intake") {
		t.Fatal("reference omits the clinical event handoff")
	}
}

func TestCheckReportsDriftWithoutOverwritingReference(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "inventory.json")
	if err := save(file, []byte("old\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := save(file, []byte("new\n"), true); err == nil {
		t.Fatal("stale reference passed")
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatal("check overwrote the reference")
	}
	if err := save(file, got, true); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceIncludesConsumersBehindWorkflowConfiguration(t *testing.T) {
	data, err := json.Marshal(inventory())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"privacy-deadline", "emergency-dispatch", "emergency-receiving", "supply-evidence-scan", "workforce-document-scan", "workforce-credential-reminders"} {
		if !strings.Contains(string(data), `"name":"`+name+`"`) {
			t.Errorf("conditional consumer %s omitted", name)
		}
	}
}

func TestEventSchemasCoverDeclaredSubjectsAndPreserveWireFields(t *testing.T) {
	schemas, err := eventSchemas(inventory(), "../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range inventory().Modules {
		for _, subject := range mod.Publishes {
			if _, ok := schemas[subject]; !ok {
				t.Errorf("no schema for %s", subject)
			}
		}
	}
	data, err := json.Marshal(schemas)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]struct {
		Schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	visit := wire["helivanta.in.medicore.visit_created.v1"].Schema
	if _, ok := visit.Properties["patient_name"]; !ok {
		t.Fatal("current PHI-bearing field omitted")
	}
	evidence := wire["helivanta.in.supply.evidence_changed.v1"].Schema
	for _, field := range []string{"party_id", "license_id", "file_id", "scan_attempt", "status"} {
		if _, ok := evidence.Properties[field]; !ok {
			t.Errorf("embedded field %s omitted", field)
		}
	}
	policy := wire["helivanta.in.workspace.country_policy_changed.v1"].Schema
	if !strings.Contains(string(policy.Properties["version_id"]), `"null"`) {
		t.Fatal("nullable version_id changed")
	}
}

func TestReferenceIncludesSharedAuditAndPlatformMigrations(t *testing.T) {
	doc := inventory()
	if len(doc.PlatformMigrations) != len(bootstrap.PlatformMigrations()) {
		t.Fatal("platform migration reference differs from runtime bootstrap")
	}
	found := false
	for _, migration := range doc.PlatformMigrations {
		for _, table := range migration.Tables {
			if table == "audit_events" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("shared append-only audit schema omitted")
	}
}

func TestReferenceIncludesConcurrentIndexDefinitionsAndOrdering(t *testing.T) {
	encoded, err := json.Marshal(inventory())
	if err != nil {
		t.Fatal(err)
	}
	expected := `"concurrent_indexes":[{"name":"patient_clinical_consent_proxy_authority","table":"patient_clinical_consents","columns":[{"name":"tenant_id","descending":false},{"name":"proxy_authority_id","descending":false}]},{"name":"patient_sharing_notice_capacity_page"`
	if !strings.Contains(string(encoded), expected) {
		t.Fatal("reference omits concurrent index names, tables or ordered key definitions")
	}
	if !strings.Contains(string(encoded), `{"name":"created_at","descending":true}`) {
		t.Fatal("reference omits descending index key order")
	}
	rendered := string(markdown(inventory()))
	if !strings.Contains(rendered, "patient_sharing_notice_capacity_page") {
		t.Fatal("rendered migration reference omits concurrent indexes")
	}
}

func TestEventSchemasIncludeGeneratedContractFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backend/internal/modules/example/contract")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"events.go":       `package contract; const SubjectChecked="helivanta.in.example.checked.v1"`,
		"generated.go":    "package contract\ntype CheckedData struct { Allowed bool `json:\"allowed\"` }",
		"ignored_test.go": "package contract\ntype CheckedData struct { Wrong string `json:\"wrong\"` }",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	schemas, err := eventSchemas(reference{Modules: []module{{Name: "example", Publishes: []string{"helivanta.in.example.checked.v1"}}}}, root)
	if err != nil {
		t.Fatal(err)
	}
	got := schemas["helivanta.in.example.checked.v1"]
	if got.Source != "backend/internal/modules/example/contract/generated.go:2" {
		t.Fatalf("wrong source: %s", got.Source)
	}
	properties := got.Schema["properties"].(map[string]any)
	if _, ok := properties["allowed"]; !ok {
		t.Fatalf("missing generated field: %#v", properties)
	}
	if _, ok := properties["wrong"]; ok {
		t.Fatal("test fixture type must not enter published schema")
	}
}
