package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type payloadContract struct {
	Source string         `json:"source"`
	Type   string         `json:"go_type"`
	Schema map[string]any `json:"schema"`
}

func eventSchemas(doc reference, root string) (map[string]payloadContract, error) {
	out := map[string]payloadContract{}
	aliases := map[string]string{
		"mediconnect.SubjectAccountConsentRecorded":  "AccountConsentData",
		"mediconnect.SubjectAccountConsentWithdrawn": "AccountConsentData",
		"mediconnect.SubjectReviewRequested":         "RegistrationReviewData",
		"mediconnect.SubjectReviewWithdrawn":         "RegistrationReviewData",
		"iam.SubjectMemberGranted":                   "MemberChangedData", "iam.SubjectMemberRevoked": "MemberChangedData",
		"emergency.SubjectCaseReceived": "CaseChangedData", "emergency.SubjectCaseClosed": "CaseChangedData",
	}
	for _, mod := range doc.Modules {
		if len(mod.Publishes) == 0 {
			continue
		}
		directory := "backend/internal/modules/" + mod.Name + "/contract"
		files, err := filepath.Glob(filepath.Join(root, directory, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("find event contracts: %w", err)
		}
		fset := token.NewFileSet()
		types := map[string]*ast.StructType{}
		constants := map[string]string{}
		positions := map[string]int{}
		sources := map[string]string{}
		for _, filename := range files {
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filename, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("parse event contracts: %w", err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.TypeSpec:
					if body, ok := n.Type.(*ast.StructType); ok {
						types[n.Name.Name] = body
						positions[n.Name.Name] = fset.Position(n.Pos()).Line
						sources[n.Name.Name] = directory + "/" + filepath.Base(filename)
					}
				case *ast.ValueSpec:
					for i, name := range n.Names {
						if i >= len(n.Values) {
							continue
						}
						if literal, ok := n.Values[i].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							if value, err := strconv.Unquote(literal.Value); err == nil {
								constants[value] = name.Name
							}
						}
					}
				}
				return true
			})
		}
		for _, subject := range mod.Publishes {
			name := constants[subject]
			if !strings.HasPrefix(name, "Subject") {
				return nil, fmt.Errorf("missing subject constant %s", subject)
			}
			typeName := strings.TrimPrefix(name, "Subject") + "Data"
			if alias := aliases[mod.Name+"."+name]; alias != "" {
				typeName = alias
			}
			schema, err := structSchema(typeName, types, map[string]bool{})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", subject, err)
			}
			schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
			out[subject] = payloadContract{Source: fmt.Sprintf("%s:%d", sources[typeName], positions[typeName]), Type: typeName, Schema: schema}
		}
	}
	return out, nil
}

func structSchema(name string, types map[string]*ast.StructType, active map[string]bool) (map[string]any, error) {
	body, ok := types[name]
	if !ok || active[name] {
		return nil, fmt.Errorf("unsupported or recursive payload type %s", name)
	}
	active[name] = true
	defer delete(active, name)
	properties := map[string]any{}
	required := []string{}
	for _, field := range body.Fields.List {
		if len(field.Names) == 0 {
			embed, ok := field.Type.(*ast.Ident)
			if !ok {
				return nil, fmt.Errorf("unsupported embedded payload in %s", name)
			}
			schema, err := structSchema(embed.Name, types, active)
			if err != nil {
				return nil, err
			}
			for key, value := range schema["properties"].(map[string]any) {
				properties[key] = value
			}
			required = append(required, schema["required"].([]string)...)
			continue
		}
		if field.Tag == nil {
			return nil, fmt.Errorf("untagged payload field in %s", name)
		}
		tag, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(reflect.StructTag(tag).Get("json"), ",")
		if parts[0] == "-" {
			continue
		}
		if parts[0] == "" {
			return nil, fmt.Errorf("missing wire name in %s", name)
		}
		schema, err := fieldSchema(field.Type, types, active)
		if err != nil {
			return nil, err
		}
		properties[parts[0]] = schema
		if !strings.Contains(","+strings.Join(parts[1:], ",")+",", ",omitempty,") {
			required = append(required, parts[0])
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}, nil
}

func fieldSchema(expr ast.Expr, types map[string]*ast.StructType, active map[string]bool) (map[string]any, error) {
	switch value := expr.(type) {
	case *ast.Ident:
		switch value.Name {
		case "string":
			return map[string]any{"type": "string"}, nil
		case "int", "int64":
			return map[string]any{"type": "integer"}, nil
		case "bool":
			return map[string]any{"type": "boolean"}, nil
		default:
			return structSchema(value.Name, types, active)
		}
	case *ast.StarExpr:
		schema, err := fieldSchema(value.X, types, active)
		if err != nil {
			return nil, err
		}
		return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}, nil
	case *ast.ArrayType:
		// A slice only: a fixed-length array has no use on the wire yet.
		if value.Len == nil {
			items, err := fieldSchema(value.Elt, types, active)
			if err != nil {
				return nil, err
			}
			return map[string]any{"type": "array", "items": items}, nil
		}
	case *ast.SelectorExpr:
		pkg, ok := value.X.(*ast.Ident)
		if ok && pkg.Name == "time" && value.Sel.Name == "Time" {
			return map[string]any{"type": "string", "format": "date-time"}, nil
		}
		if ok && pkg.Name == "uuid" && value.Sel.Name == "UUID" {
			return map[string]any{"type": "string", "format": "uuid"}, nil
		}
	}
	return nil, fmt.Errorf("unsupported event field type %T; extend exporter before documenting it", expr)
}
