package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestScopedCommandEvidenceHasOnlyTheIAMIssuer(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{
		method: "WithScopedCommandEvidence",
		allowlist: map[string]bool{
			"internal/modules/iam/assignment_command.go": true,
		},
		allowDir: func(path string) bool { return strings.HasPrefix(path, "pkg/authz/") },
		message:  "Only IAM may issue checked command evidence; product owners consume its exact callback-bound scope.",
	})
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if imp.Name != nil && imp.Name.Name == "." && importPath == "github.com/tesserix/helivanta/pkg/authz" {
				t.Errorf("%s dot-imports authz, bypassing the checked-evidence issuer restriction", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
