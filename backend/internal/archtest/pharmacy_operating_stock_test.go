package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Postgres cannot see the Go guarded command, so only the IAM-admitted path may mark pharmacy stock access authorized.
func TestPharmacyOperatingStockAccessHasOneProductionWriter(t *testing.T) {
	const setting = "app.pharmacy_stock_access"
	const writer = "internal/modules/pharmacy/operating_stock.go"
	writers := map[string]bool{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".sql") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(source)
		if !strings.Contains(text, setting) {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, "../../"))
		if strings.HasSuffix(path, ".sql") {
			if strings.Count(text, setting) != strings.Count(text, "current_setting('"+setting+"',true)") {
				t.Errorf("%s must only read %s; SQL cannot authorize operating stock access", rel, setting)
			}
			return nil
		}
		writers[rel] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(writers) != 1 || !writers[writer] {
		t.Errorf("only %s may set %s; found %v", writer, setting, writers)
	}
}
