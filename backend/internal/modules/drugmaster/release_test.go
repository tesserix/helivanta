package drugmaster_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	file := raw(t, f)
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, file, "Ravi Approver", true, "reviewed against source"))
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
	file := raw(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)

	require.Empty(t, s.searchLabels(t, "amox"))
	code, _ := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, http.StatusNotFound, code)
	s.quiet(t)

	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, file, "Ravi Approver", true, "reviewed against source"))

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
	file := raw(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)

	err = drugmaster.Decide(t.Context(), s.approver, id, file, "  asha stager ", true, "self-approval attempt")
	require.ErrorContains(t, err, "drugmaster_release_four_eyes")
	require.Equal(t, "staged", s.releaseStatus(t, id))
	require.Empty(t, s.searchLabels(t, "amox"))
}

// The stager's credential cannot publish, and the approver's cannot add
// content: one stolen login is never enough.
func TestEachStewardRoleDoesOnlyItsOwnStep(t *testing.T) {
	s := newStack(t)
	file := raw(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)

	err = drugmaster.Decide(t.Context(), s.stager, id, file, "Ravi Approver", true, "stager publishing")
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
	require.ErrorContains(t, s.stager.WithSystem(t.Context(), formulation), "staged in this transaction")
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
	code, _ = s.get(t, "/formulations/"+formulation+"?at=0000-01-01")
	require.Equal(t, http.StatusBadRequest, code, "a year Postgres cannot hold is the caller's error (review M8)")
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

	file := raw(t, amoxicillin("v2", "2024-01-01", "X", "active"))
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)
	err = drugmaster.Decide(t.Context(), s.approver, id, file, "Ravi Approver", true, "backdated")
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
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, nil, "Ravi Approver", false, "500 mg is not Schedule X"))
	require.Equal(t, "rejected", s.releaseStatus(t, id))
	require.Empty(t, s.searchLabels(t, "amox"))
	s.quiet(t)
	require.ErrorIs(t, drugmaster.Decide(t.Context(), s.approver, id, raw(t, amoxicillin("v1", "2020-01-01", "X", "active")), "Ravi Approver", true, "second try"), drugmaster.ErrNotStaged)

	s.publish(t, amoxicillin("v1-corrected", "2020-01-01", "H", "active"))
	_, body := s.get(t, "/formulations/"+body0(t, s, "amox"))
	require.Equal(t, "H", body["regulatory_class"])
}

// Review I1: what a rejected release got wrong (an ATC code, a pack's
// formulation) never binds the corrected release.
func TestARejectedReleaseBindsNothing(t *testing.T) {
	s := newStack(t)
	typo := amoxicillin("v1", "2020-01-01", "H", "active")
	typo.Molecules[0].ATCCode = ptr("J01CA01")
	typo.Products[0].Formulation = "TEST-AMOX-250-CAP"
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, typo), "Asha Stager")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), s.approver, id, nil, "Ravi Approver", false, "wrong ATC code and pack"))

	s.publish(t, amoxicillin("v1-corrected", "2020-01-01", "H", "active"))
	_, product := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, body0(t, s, "amox"), product["formulation_id"], "the pack belongs to the 500 mg, as corrected")
}

// Review C1: once staged, a release is sealed, so nothing joins it later.
func TestAStagedReleaseIsSealed(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)
	err = s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO drugmaster_formulations (id,release_id,code) VALUES (?,?,'TEST-LATE')`, uuid.Must(uuid.NewV7()), id).Error
	})
	require.ErrorContains(t, err, "staged in this transaction")
}

// Review C1: a release whose rows differ from the reviewed file is not
// published, even when its recorded checksum is that file's.
func TestAReleaseIsPublishedOnlyAsItsFileSays(t *testing.T) {
	s := newStack(t)
	reviewed := raw(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	sum := sha256.Sum256(reviewed)
	release := uuid.Must(uuid.NewV7())
	// Forged by hand with the stager login: the reviewed file's checksum,
	// but the 500 mg capsule recorded as needing no prescription.
	forged := amoxicillin("v1", "2020-01-01", "none", "active")
	require.NoError(t, s.stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		molecule, formulation, version := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO drugmaster_releases (id,country,source_name,licence_ref,source_version,file_sha256,staged_by) VALUES (?,'IN','synthetic-test-fixture','TEST-LICENCE','v1',?,'Asha Stager')`, []any{release, hex.EncodeToString(sum[:])}},
			{`INSERT INTO drugmaster_molecules (id,release_id,inn_name,atc_code) VALUES (?,?,'Amoxicillin','J01CA04')`, []any{molecule, release}},
			{`INSERT INTO drugmaster_formulations (id,release_id,code) VALUES (?,?,'TEST-AMOX-500-CAP')`, []any{formulation, release}},
			{`INSERT INTO drugmaster_formulation_versions (id,formulation_id,release_id,valid_from,label,dose_form,route,regulatory_class,ndps,teleconsult_prescribable) VALUES (?,?,?,'2020-01-01','Amoxicillin 500 mg capsule','capsule','oral',?,false,true)`, []any{version, formulation, release, forged.Formulations[0].RegulatoryClass}},
			{`INSERT INTO drugmaster_formulation_ingredients (version_id,position,molecule_id,strength_value,strength_unit) VALUES (?,1,?,500,'mg')`, []any{version, molecule}},
		} {
			if err := tx.Exec(q.sql, q.args...).Error; err != nil {
				return err
			}
		}
		return nil
	}))

	err := drugmaster.Decide(t.Context(), s.approver, release, reviewed, "Ravi Approver", true, "checked the file")
	require.ErrorIs(t, err, drugmaster.ErrReleaseMismatch)
	err = drugmaster.Decide(t.Context(), s.approver, release, raw(t, forged), "Ravi Approver", true, "checked another file")
	require.ErrorIs(t, err, drugmaster.ErrReleaseMismatch, "only the staged file can be the reviewed one")
	require.Equal(t, "staged", s.releaseStatus(t, release))
	require.Empty(t, s.searchLabels(t, "amox"))
}

// Review I2: decisions are serialised, so two releases decided at once
// cannot both give one barcode to different products.
func TestConcurrentDecisionsCannotShareABarcode(t *testing.T) {
	s := newStack(t)
	base := amoxicillin("base", "2020-01-01", "H", "active")
	base.Products = nil
	s.publish(t, base)
	files := make([][]byte, 2)
	ids := make([]uuid.UUID, 2)
	for i := range files {
		f := amoxicillin(fmt.Sprintf("v%d", i), "2020-01-01", "H", "active")
		f.Molecules, f.Formulations = nil, nil
		f.Products[0].Code = fmt.Sprintf("TEST-MOXA-%d", i)
		files[i] = raw(t, f)
		id, err := drugmaster.Stage(t.Context(), s.stager, files[i], "Asha Stager")
		require.NoError(t, err)
		ids[i] = id
	}
	// Hold the first publication open; the second must wait for it, then
	// see its barcode. Without the lock it would publish alongside.
	second := make(chan error, 1)
	require.NoError(t, s.approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE drugmaster_releases SET status='published',decided_by='Ravi Approver',decision_reason='first' WHERE id=?`, ids[0]).Error; err != nil {
			return err
		}
		go func() {
			second <- drugmaster.Decide(t.Context(), s.approver, ids[1], files[1], "Ravi Approver", true, "second")
		}()
		select {
		case err := <-second:
			second <- err
			return fmt.Errorf("the second decision did not wait for the first: %v", err)
		case <-time.After(time.Second):
			return nil
		}
	}))
	require.ErrorContains(t, <-second, "would name two products")
	code, _ := s.get(t, "/products/by-gtin/8901234567890")
	require.Equal(t, http.StatusOK, code)
}

// Review I3: a recall takes effect today even when a version for a later
// date is already published.
func TestARecallIsNotHeldBackByAFutureVersion(t *testing.T) {
	s := newStack(t)
	s.publish(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	future := amoxicillin("v2", time.Now().In(loc).AddDate(0, 1, 0).Format(time.DateOnly), "H", "active")
	future.Formulations = nil
	future.Molecules = nil
	future.Products[0].PackSize = 20
	s.publish(t, future)

	recall := amoxicillin("v3", todayIST(t), "H", "recalled")
	recall.Formulations = nil
	recall.Molecules = nil
	s.publish(t, recall)
	_, body := s.get(t, "/products/by-gtin/08901234567890")
	require.Equal(t, "recalled", body["status"], "the GTIN-14 form finds the same pack (review M4)")
}

// Review I4: a pack cannot be published before its formulation has rules.
func TestAProductNeedsItsFormulationInForce(t *testing.T) {
	s := newStack(t)
	early := amoxicillin("v1", "2021-01-01", "H", "active")
	early.Products[0].ValidFrom = "2020-06-01"
	file := raw(t, early)
	id, err := drugmaster.Stage(t.Context(), s.stager, file, "Asha Stager")
	require.NoError(t, err)
	err = drugmaster.Decide(t.Context(), s.approver, id, file, "Ravi Approver", true, "pack before its rules")
	require.ErrorContains(t, err, "formulation in force from its first day")

	orphan := amoxicillin("v2", "2020-01-01", "H", "active")
	orphan.Formulations, orphan.Molecules = nil, nil
	_, err = drugmaster.Stage(t.Context(), s.stager, raw(t, orphan), "Asha Stager")
	require.ErrorIs(t, err, drugmaster.ErrInvalidRelease, "a staged-only formulation is not one a pack can name")
}

// The approver decides; the database alone dates and orders decisions
// (review M5).
func TestTheApproverCannotSetDecisionMetadata(t *testing.T) {
	s := newStack(t)
	id, err := drugmaster.Stage(t.Context(), s.stager, raw(t, amoxicillin("v1", "2020-01-01", "H", "active")), "Asha Stager")
	require.NoError(t, err)
	err = s.approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE drugmaster_releases SET status='published',decided_by='Ravi',decision_reason='x',sequence=999,decided_at='2001-01-01' WHERE id=?`, id).Error
	})
	require.ErrorContains(t, err, "permission denied")
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
	nonASCII := amoxicillin("v1", "2020-01-01", "H", "active")
	nonASCII.Molecules = append(nonASCII.Molecules, drugmaster.MoleculeIn{INNName: "β-Carotene"})
	paddedSource := amoxicillin("v1", "2020-01-01", "H", "active")
	paddedSource.Source.Name = "synthetic-test-fixture "
	for name, file := range map[string][]byte{
		"missing ndps":     raw(t, missingFlag),
		"unknown molecule": raw(t, unknownMolecule),
		"non-ASCII name":   raw(t, nonASCII),
		"padded source":    raw(t, paddedSource),
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
	var allowed struct{ Test, Production, Disguised, Real, AppSequence bool }
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT drugmaster_source_allowed('synthetic-test-fixture','hms_test_7') AS test,
			drugmaster_source_allowed('synthetic-test-fixture','helivanta') AS production,
			drugmaster_source_allowed(' Synthetic-Test-Fixture ','helivanta') AS disguised,
			drugmaster_source_allowed('Licensed Source','helivanta') AS real,
			has_sequence_privilege('hms_app','drugmaster_release_sequence','USAGE') AS app_sequence`).Scan(&allowed).Error
	}))
	require.Equal(t, struct{ Test, Production, Disguised, Real, AppSequence bool }{true, false, false, true, false}, allowed)
}
