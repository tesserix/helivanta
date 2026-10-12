package archtest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renewalEvidenceAllowlist is every file outside pkg/session that may
// reference session.Verifier.RenewalEvidence (#859). The method reads a
// session token WITHOUT refusing it for being expired, so it must never
// sit on an authentication path: a caller that treated its result as "this
// request is authenticated" would accept any cookie this key ever signed,
// however long ago. Its one legitimate use is the login exchange's
// idle-deadline decision, which only ever carries a deadline forward.
//
// Adding a file here is a security review, not a refactor.
var renewalEvidenceAllowlist = map[string]bool{
	"internal/modules/iam/login.go": true,
}

// TestRenewalEvidenceIsOnlyReadByTheLoginExchange pins every reference to
// RenewalEvidence — as a call or a bare method value — to
// renewalEvidenceAllowlist, the way TestWithAdminIsOnlyCalledFromTheAllowlist
// pins the RLS bypass. pkg/session owns the method and its tests; test files
// elsewhere are checked too, so a helper cannot smuggle it into a harness
// that production code later copies.
func TestRenewalEvidenceIsOnlyReadByTheLoginExchange(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/archtest/") || strings.HasPrefix(rel, "pkg/session/") {
			return nil
		}
		if renewalEvidenceAllowlist[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceReferencesMethod(src, "RenewalEvidence")
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s references session.Verifier.RenewalEvidence, which reads a session token "+
				"WITHOUT refusing it as expired and must never authenticate a request (#859). "+
				"Authentication is session.Verifier.Verify. If you believe another caller needs "+
				"it, bring it to security review — don't add the file to renewalEvidenceAllowlist "+
				"on your own.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

// The allowlist must name a file that still exists and still calls the
// method; a stale entry would silently pre-approve whatever file is later
// created at that path.
func TestRenewalEvidenceAllowlistHasNoDeadEntries(t *testing.T) {
	for rel := range renewalEvidenceAllowlist {
		src, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Errorf("renewalEvidenceAllowlist names %s, which cannot be read: %v", rel, err)
			continue
		}
		found, err := sourceReferencesMethod(src, "RenewalEvidence")
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		if !found {
			t.Errorf("renewalEvidenceAllowlist names %s, which no longer references RenewalEvidence: remove the entry", rel)
		}
	}
}
