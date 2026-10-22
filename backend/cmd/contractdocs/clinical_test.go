package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClinicalOpenAPIReferencesRegisteredOperationsAndPermissions(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/api/clinical.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Permission  string `json:"x-permission"`
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{}
	for _, mod := range inventory().Modules {
		for _, route := range mod.Routes {
			routes[route.Method+" "+route.Path] = string(route.Permission)
		}
	}
	seen := map[string]bool{}
	for path, operations := range spec.Paths {
		for method, operation := range operations {
			key := strings.ToUpper(method) + " " + strings.ReplaceAll(path, "{id}", ":id")
			permission, ok := routes[key]
			if !ok || permission != operation.Permission {
				t.Errorf("contract drift: %s permission=%q, runtime=%q", key, operation.Permission, permission)
			}
			if operation.OperationID == "" || seen[operation.OperationID] {
				t.Errorf("missing/duplicate operation ID for %s", key)
			}
			seen[operation.OperationID] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("clinical contract contains no operations")
	}
}
