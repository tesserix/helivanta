package drugmaster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/drugmaster" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type stack struct {
	events           *eventLog
	r                *gin.Engine
	db               *tenantdb.DB
	stager, approver *tenantdb.DB
}

func newStack(t *testing.T) stack {
	t.Helper()
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"doctor": testutil.TenantA, "clerk": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"doctor": {drugmaster.PermCatalogueRead}},
		Modules: []platform.Module{drugmaster.New()},
	})
	var name string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_database()`).Scan(&name).Error
	}))
	stager, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugStagerDSN(t, name))
	require.NoError(t, err)
	approver, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugApproverDSN(t, name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = stager.Close(); _ = approver.Close() })
	log := &eventLog{}
	var subs []events.Broadcast
	for _, subject := range drugmaster.New().Publishes() {
		subs = append(subs, events.Broadcast{Subject: subject, Handle: log.add})
	}
	require.NoError(t, bus.StartBroadcasts(ctx, subs))
	return stack{events: log, r: r, db: db, stager: stager, approver: approver}
}

func ptr[T any](v T) *T { return &v }

// todayIST is the catalogue's "today" (spec D2).
func todayIST(t *testing.T) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	return time.Now().In(loc).Format(time.DateOnly)
}

// amoxicillin is a synthetic release: one molecule, two strengths of one
// capsule, one branded pack of the 500 mg.
func amoxicillin(version, validFrom, class, status string) drugmaster.ReleaseFile {
	return drugmaster.ReleaseFile{
		Country:   "IN",
		Source:    drugmaster.ReleaseSource{Name: "synthetic-test-fixture", LicenceRef: "TEST-LICENCE", Version: version},
		Molecules: []drugmaster.MoleculeIn{{INNName: "Amoxicillin", ATCCode: ptr("J01CA04")}},
		Formulations: []drugmaster.FormulationIn{
			{Code: "TEST-AMOX-500-CAP", ValidFrom: validFrom, Label: "Amoxicillin 500 mg capsule", DoseForm: "capsule", Route: "oral",
				RegulatoryClass: class, NDPS: ptr(false), TeleconsultPrescribable: ptr(true),
				Ingredients: []drugmaster.IngredientIn{{Molecule: "Amoxicillin", StrengthValue: "500", StrengthUnit: "mg"}}},
			{Code: "TEST-AMOX-250-CAP", ValidFrom: "2020-01-01", Label: "Amoxicillin 250 mg capsule", DoseForm: "capsule", Route: "oral",
				RegulatoryClass: "H", NDPS: ptr(false), TeleconsultPrescribable: ptr(true),
				Ingredients: []drugmaster.IngredientIn{{Molecule: "Amoxicillin", StrengthValue: "250.000", StrengthUnit: "mg"}}},
		},
		Products: []drugmaster.ProductIn{{Code: "TEST-MOXA-10", Formulation: "TEST-AMOX-500-CAP", ValidFrom: validFrom,
			BrandName: "Testmox", Manufacturer: "TEST Pharma", PackSize: 10, UnitOfIssue: "capsule",
			GTIN: ptr("8901234567890"), HSNCode: "30041010", Status: status}},
	}
}

func raw(t *testing.T, f drugmaster.ReleaseFile) []byte {
	b, err := json.Marshal(f)
	require.NoError(t, err)
	return b
}

func (s stack) publish(t *testing.T, f drugmaster.ReleaseFile) uuid.UUID {
	t.Helper()
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, f), "Asha Stager")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, "Ravi Approver", true, "reviewed against source"))
	return id
}

func (s stack) get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	res := testutil.Do(s.r, http.MethodGet, "/v1/drugmaster"+path, "doctor", "")
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	return res.Code, body
}

func (s stack) searchLabels(t *testing.T, q string) []string {
	t.Helper()
	code, body := s.get(t, "/search?q="+q)
	require.Equal(t, http.StatusOK, code, body)
	var labels []string
	for _, row := range body["data"].([]any) {
		labels = append(labels, row.(map[string]any)["label"].(string))
	}
	return labels
}

// eventLog collects what the dispatcher delivers from the outbox, so the
// tests assert on published messages rather than on table rows.
type eventLog struct {
	mu   sync.Mutex
	seen []events.Event
}

func (l *eventLog) add(_ context.Context, evt events.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, evt)
}

func (l *eventLog) of(subject string) []events.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []events.Event
	for _, e := range l.seen {
		if e.Type == subject {
			out = append(out, e)
		}
	}
	return out
}

// await waits for n events of subject, then a moment more so a surplus
// one would be seen too.
func (s stack) await(t *testing.T, subject string, n int) []events.Event {
	t.Helper()
	require.Eventually(t, func() bool { return len(s.events.of(subject)) >= n }, 10*time.Second, 20*time.Millisecond, subject)
	time.Sleep(200 * time.Millisecond)
	got := s.events.of(subject)
	require.Len(t, got, n, subject)
	return got
}

func (s stack) quiet(t *testing.T) {
	t.Helper()
	time.Sleep(500 * time.Millisecond)
	s.events.mu.Lock()
	defer s.events.mu.Unlock()
	require.Empty(t, s.events.seen)
}

func (s stack) releaseStatus(t *testing.T, id uuid.UUID) string {
	var status string
	require.NoError(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT status FROM drugmaster_releases WHERE id=?`, id).Scan(&status).Error
	}))
	return status
}

// Review focus 1: a staged release is invisible until a second person
// approves it, and publishing announces itself.
func TestStagedReleaseIsInvisibleUntilApproved(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)

	require.Empty(t, s.searchLabels(t, "amox"))
	code, _ := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, http.StatusNotFound, code)
	s.quiet(t)

	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, "Ravi Approver", true, "reviewed against source"))

	code, body := s.get(t, "/search?q=amox")
	require.Equal(t, http.StatusOK, code)
	rows := body["data"].([]any)
	require.Len(t, rows, 2)
	first := rows[0].(map[string]any)
	require.Equal(t, "Amoxicillin 250 mg capsule", first["label"])
	require.Equal(t, "H", first["regulatory_class"])
	require.Equal(t, false, first["ndps"])
	require.Equal(t, true, first["teleconsult_prescribable"])
	require.Equal(t, []any{map[string]any{"molecule_id": first["ingredients"].([]any)[0].(map[string]any)["molecule_id"],
		"inn_name": "Amoxicillin", "strength_value": "250", "strength_unit": "mg", "per_value": nil, "per_unit": nil}}, first["ingredients"])

	events := s.await(t, contract.SubjectCataloguePublished, 1)
	require.Empty(t, events[0].TenantID, "catalogue events belong to no tenant")
	require.Empty(t, s.events.of(contract.SubjectFormulationReclassified), "a first version reclassifies nothing")
	var published contract.CataloguePublishedData
	require.NoError(t, json.Unmarshal(events[0].Data, &published))
	require.Equal(t, contract.CataloguePublishedData{ReleaseID: id.String(), Sequence: published.Sequence, FormulationVersions: 2, ProductVersions: 1}, published)
	require.Positive(t, published.Sequence)
}

// Review focus 4: whoever staged a release cannot decide it.
func TestTheStagerCannotApproveTheirOwnRelease(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)

	err = drugmaster.Decide(t.Context(), s.approver, id, "  asha stager ", true, "self-approval attempt")
	require.ErrorContains(t, err, "drugmaster_release_four_eyes")
	require.Equal(t, "staged", s.releaseStatus(t, id))
	require.Empty(t, s.searchLabels(t, "amox"))
}

// The stager's credential cannot publish, and the approver's cannot add
// content: one stolen login is never enough.
func TestEachStewardRoleDoesOnlyItsOwnStep(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)

	err = drugmaster.Decide(t.Context(), s.stager, id, "Ravi Approver", true, "stager publishing")
	require.ErrorContains(t, err, "permission denied")
	_, err = drugmaster.Stage(t.Context(), s.approver, raw(t, amoxicillin("v2", "2020-01-01", "H", "active")), "Ravi Approver")
	require.ErrorContains(t, err, "permission denied")
	require.Equal(t, "staged", s.releaseStatus(t, id))
}

// Review focus 3: nothing published can be appended to, changed or
// removed, by any role.
func TestPublishedContentIsNeverRewritten(t *testing.T) {
	s := newStack(t)
	id := s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))

	var version string
	require.NoError(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id::text FROM drugmaster_formulation_versions WHERE release_id=? LIMIT 1`, id).Scan(&version).Error
	}))
	formulation := func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO drugmaster_formulations (id,release_id,code) VALUES (?,?,'TEST-LATE')`, uuid.Must(uuid.NewV7()), id).Error
	}
	require.ErrorContains(t, s.stager.WithSystem(t.Context(), formulation), "joins only a staged release")
	require.ErrorContains(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE drugmaster_formulation_versions SET regulatory_class='X' WHERE id=?`, version).Error
	}), "permission denied")
	var redecided int64
	require.NoError(t, s.approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE drugmaster_releases SET status='rejected',decision_reason='undo' WHERE id=?`, id)
		redecided = res.RowsAffected
		return res.Error
	}))
	require.Zero(t, redecided, "a decided release is not decided again")
	require.Equal(t, "published", s.releaseStatus(t, id))
	require.ErrorContains(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM drugmaster_formulation_versions WHERE id=?`, version).Error
	}), "permission denied")
	require.ErrorContains(t, s.db.WithSystem(t.Context(), formulation), "permission denied")

	code, body := s.get(t, "/formulations/"+body0(t, s, "amox")+"?at=2021-01-01")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "H", body["regulatory_class"])
}

// body0 is the first search hit's formulation id.
func body0(t *testing.T, s stack, q string) string {
	_, body := s.get(t, "/search?q="+q+"&form=capsule")
	for _, row := range body["data"].([]any) {
		m := row.(map[string]any)
		if m["label"] == "Amoxicillin 500 mg capsule" {
			return m["formulation_id"].(string)
		}
	}
	t.Fatal("no 500 mg capsule")
	return ""
}

// Review focus 2: a reclassification answers past dates with the old
// rules, today with the new, and announces the change.
func TestReclassificationKeepsWhatApplied(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	formulation := body0(t, s, "amox")
	s.await(t, contract.SubjectCataloguePublished, 1)

	today := todayIST(t)
	second := s.publish(t, amoxicillin("v2", today, "H1", "active"))

	code, past := s.get(t, "/formulations/"+formulation+"?at=2024-06-30")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "H", past["regulatory_class"])
	require.Equal(t, "2020-01-01", past["valid_from"])
	code, now := s.get(t, "/formulations/"+formulation)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "H1", now["regulatory_class"])
	require.NotEqual(t, past["version_id"], now["version_id"])
	code, _ = s.get(t, "/formulations/"+formulation+"?at=2019-12-31")
	require.Equal(t, http.StatusNotFound, code, "a date before the first version is never answered with today's rules")

	events := s.await(t, contract.SubjectFormulationReclassified, 1)
	var r contract.FormulationReclassifiedData
	require.NoError(t, json.Unmarshal(events[0].Data, &r))
	require.Equal(t, contract.FormulationReclassifiedData{FormulationID: formulation, VersionID: now["version_id"].(string),
		ValidFrom: today, FromClass: "H", ToClass: "H1"}, r)
	var published contract.CataloguePublishedData
	require.NoError(t, json.Unmarshal(s.await(t, contract.SubjectCataloguePublished, 2)[1].Data, &published))
	require.Equal(t, second.String(), published.ReleaseID)
	require.Equal(t, 1, published.FormulationVersions)
	require.Equal(t, 0, published.ProductVersions)
}

// A change dated before today, or before what is published, would answer
// an already-answered date differently, so publication refuses it.
func TestBackdatedChangesAreRefused(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))

	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v2", "2024-01-01", "X", "active")), "Asha Stager")
	require.NoError(t, err)
	err = drugmaster.Decide(t.Context(), s.approver, id, "Ravi Approver", true, "backdated")
	require.ErrorContains(t, err, "rewrite a date already answered")
	require.Equal(t, "staged", s.releaseStatus(t, id))
	code, body := s.get(t, "/formulations/"+body0(t, s, "amox")+"?at=2024-06-30")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "H", body["regulatory_class"])
}

// Review focus 5: a recalled pack still resolves by barcode, marked.
func TestARecalledProductStillResolvesByGTIN(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	code, body := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "active", body["status"])
	product := body["product_id"].(string)
	s.await(t, contract.SubjectCataloguePublished, 1)

	s.publish(t, amoxicillin("v2", todayIST(t), "H", "recalled"))

	code, body = s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "recalled", body["status"])
	require.Equal(t, "Testmox", body["brand_name"])
	code, byID := s.get(t, "/products/"+product)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "recalled", byID["status"])

	events := s.await(t, contract.SubjectProductStatusChanged, 1)
	var changed contract.ProductStatusChangedData
	require.NoError(t, json.Unmarshal(events[0].Data, &changed))
	require.Equal(t, product, changed.ProductID)
	require.Equal(t, "recalled", changed.Status)

	code, _ = s.get(t, "/products/by-gtin/8901234567891")
	require.Equal(t, http.StatusBadRequest, code, "a wrong check digit is refused, not looked up")
	code, _ = s.get(t, "/products/by-gtin/96385074")
	require.Equal(t, http.StatusNotFound, code)
}

// A rejected release leaves nothing visible, and its codes can be staged
// again in a corrected release.
func TestARejectedReleaseCanBeCorrectedAndRestaged(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "X", "active")), "Asha Stager")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, "Ravi Approver", false, "500 mg is not Schedule X"))
	require.Equal(t, "rejected", s.releaseStatus(t, id))
	require.Empty(t, s.searchLabels(t, "amox"))
	s.quiet(t)
	require.ErrorIs(t, drugmaster.Decide(t.Context(), s.approver, id, "Ravi Approver", true, "second try"), drugmaster.ErrNotStaged)

	s.publish(t, amoxicillin("v1-corrected", "2020-01-01", "H", "active"))
	_, body := s.get(t, "/formulations/"+body0(t, s, "amox"))
	require.Equal(t, "H", body["regulatory_class"])
}

func TestSearchMatchesBrandsFlagsConfusablesAndValidates(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))

	require.Equal(t, []string{"Amoxicillin 500 mg capsule"}, s.searchLabels(t, "TEST-mox"), "a brand prefix finds its formulation, punctuation ignored")
	require.Empty(t, s.searchLabels(t, "moxa"), "prefix, not substring")

	_, body := s.get(t, "/search?q=amox")
	rows := body["data"].([]any)
	a, b := rows[0].(map[string]any), rows[1].(map[string]any)
	require.Equal(t, []any{b["formulation_id"]}, a["confusable_with"], "two strengths of one capsule are confusable")
	require.Equal(t, []any{a["formulation_id"]}, b["confusable_with"])

	_, body = s.get(t, "/search?q=amox&class=H1")
	require.Empty(t, body["data"])
	for _, q := range []string{"/search?q=a", "/search?q=-%20-", "/search?q=amox&class=Z", "/search?q=amox&form=elixir"} {
		code, _ := s.get(t, q)
		require.Equal(t, http.StatusBadRequest, code, q)
	}
	res := testutil.Do(s.r, http.MethodGet, "/v1/drugmaster/search?q=amox", "clerk", "")
	require.Equal(t, http.StatusForbidden, res.Code, "a member without the read permission is refused")
}

func TestStagingRefusesUnsafeFiles(t *testing.T) {
	s := newStack(t)
	missingFlag := amoxicillin("v1", "2020-01-01", "H", "active")
	missingFlag.Formulations[0].NDPS = nil
	unknownMolecule := amoxicillin("v1", "2020-01-01", "H", "active")
	unknownMolecule.Molecules = nil
	for name, file := range map[string][]byte{
		"missing ndps":     raw(t, missingFlag),
		"unknown molecule": raw(t, unknownMolecule),
		"unknown field":    []byte(`{"country":"IN","extra":1}`),
	} {
		_, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
		require.ErrorIs(t, err, drugmaster.ErrInvalidRelease, name)
	}
	var releases int64
	require.NoError(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM drugmaster_releases`).Scan(&releases).Error
	}))
	require.Zero(t, releases, "a refused file leaves no partial release")

	_, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)
	_, err = drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.ErrorContains(t, err, "file_sha256", "the same file is staged once")
}

// The schema's access rules, pinned (spec D1): the app reads and never
// writes; every table forces RLS; synthetic data is refused outside the
// harness's databases.
func TestTheCatalogueIsReadOnlyToTheApp(t *testing.T) {
	s := newStack(t)
	var rows []struct {
		Relname               string
		Forced, Select, Write bool
	}
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT c.relname, c.relforcerowsecurity AS forced,
			has_table_privilege('hms_app', c.oid, 'SELECT') AS select,
			has_table_privilege('hms_app', c.oid, 'INSERT,UPDATE,DELETE,TRUNCATE') AS write
			FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relkind='r' AND c.relname LIKE 'drugmaster\_%' ORDER BY c.relname`).Scan(&rows).Error
	}))
	require.Len(t, rows, 7)
	for _, r := range rows {
		require.True(t, r.Forced, r.Relname)
		require.True(t, r.Select, r.Relname)
		require.False(t, r.Write, r.Relname)
	}
	var allowed struct{ Test, Production, Real bool }
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT drugmaster_source_allowed('synthetic-test-fixture','hms_test_7') AS test,
			drugmaster_source_allowed('synthetic-test-fixture','helivanta') AS production,
			drugmaster_source_allowed('Licensed Source','helivanta') AS real`).Scan(&allowed).Error
	}))
	require.Equal(t, struct{ Test, Production, Real bool }{true, false, true}, allowed)
}
