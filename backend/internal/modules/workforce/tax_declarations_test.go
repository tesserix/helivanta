package workforce_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/transit"
)

// Synthetic third-party PANs only; none belongs to a real landlord or lender.
const (
	synLandlordPAN = "SYNFA1234A"
	synOtherPAN    = "SYNHB5678B"
	synLenderPAN   = "SYNTC9012C"
	declYear       = "2026-27"
)

func panNone() map[string]any { return map[string]any{"action": "none"} }

func landlord(name string, rent int64, pan map[string]any) map[string]any {
	return map[string]any{"name": name, "address": "1 Synthetic Lane", "rent_paise": rent, "pan": pan}
}

func lender(name string, pan map[string]any) map[string]any {
	return map[string]any{"name": name, "address": "2 Synthetic Bank Road", "pan": pan}
}

func declBody(version int, landlords []any, lenders []any) map[string]any {
	body := map[string]any{"expected_version": version, "reason": "Synthetic Form 124", "claims": []any{map[string]any{"code": " savings ", "amount_paise": 15000000}}}
	if landlords != nil {
		body["hra"] = map[string]any{"metro": true, "landlords": landlords}
	}
	if lenders != nil {
		body["home_loan"] = map[string]any{"interest_paise": 20000000, "lenders": lenders}
	}
	return body
}

func (f *identifierFixture) declPath(staff, suffix string) string {
	return "/staff/" + staff + "/tax-declarations/" + declYear + suffix
}

func (f *identifierFixture) declare(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", f.declPath(staff, ""), user, body)
}

func (f *identifierFixture) decideDecl(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("POST", f.declPath(staff, "/decision"), user, body)
}

func (f *identifierFixture) revealParty(staff, user, revision, role string, position int) (int, map[string]any) {
	return f.call("POST", f.declPath(staff, "/reveal"), user, map[string]any{"revision_id": revision, "role": role, "position": position, "reason": "Synthetic Form 124 check"})
}

// approvedDecl proposes as admin and approves as approver, returning the approved revision id.
func (f *identifierFixture) approvedDecl(staff string, body map[string]any) string {
	f.t.Helper()
	code, out := f.declare(staff, "admin", body)
	require.Equal(f.t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"]
	code, out = f.decideDecl(staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": body["expected_version"], "decision": "approve", "note": "Synthetic receipts checked"})
	require.Equal(f.t, 200, code, out)
	return proposal.(string)
}

// withLandlordRule approves a new-regime income-tax rule whose HRA head needs a landlord PAN above threshold, and elects that regime.
func (f *identifierFixture) withLandlordRule(staff string, threshold int64) string {
	f.t.Helper()
	rule := uuid.NewString()
	body := incomeTaxRuleWithClaims("income_tax_new", 0)
	body["income_tax"].(map[string]any)["claims"].([]any)[0].(map[string]any)["landlord_pan_rent_above_paise"] = threshold
	f.approved(rule, body)
	code, out := f.call("PUT", "/staff/"+staff+"/tax-regimes/"+declYear, "admin", regimeBody(0, "new", "2026-04-01", ""))
	require.Equal(f.t, 200, code, out)
	return rule
}

func (f *identifierFixture) parties(revision string) []map[string]any {
	f.t.Helper()
	var rows []map[string]any
	require.NoError(f.t, f.asManager(func(tx *gorm.DB) error {
		return tx.Table("workforce_staff_tax_declaration_parties").Select("role,position,pan_ciphertext,pan_masked").Where("revision_id=?", revision).Order("role,position").Find(&rows).Error
	}))
	return rows
}

// payrollWrite runs statements in one transaction past the payroll write gate, as the handlers do.
func (f *identifierFixture) payrollWrite(statements ...string) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, f.hospital).Error; err != nil {
			return err
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// requireGuard accepts the guard's constraint name or its message, so each case names the guard that refused it.
func requireGuard(t *testing.T, err error, guard, name string) {
	t.Helper()
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg, name)
	if pg.ConstraintName != guard {
		require.Contains(t, pg.Message, guard, name)
	}
}

func TestTaxDeclarationsNeedASecondPerson(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	code, out := f.call("GET", f.declPath(f.staff, ""), "reader", nil)
	require.Equal(t, 404, code, "nothing before a proposal: %v", out)

	body := declBody(0, []any{landlord("  Synthetic   Landlord ", 9000000, set("synfa1234a"))}, []any{lender("Synthetic Bank", set(synLenderPAN))})
	code, out = f.declare(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 0, out["version"])
	require.Nil(t, out["current"], "nothing is in force before approval")
	pending := out["pending"].(map[string]any)
	proposal := pending["id"].(string)
	require.Equal(t, declYear, pending["tax_year"])
	require.Equal(t, true, pending["hra_metro"])
	require.EqualValues(t, 20000000, pending["home_loan_interest_paise"])
	require.Equal(t, []any{map[string]any{"code": "SAVINGS", "amount_paise": float64(15000000)}}, pending["claims"])
	require.Equal(t, []any{
		map[string]any{"role": "landlord", "position": float64(1), "name": "Synthetic Landlord", "address": "1 Synthetic Lane", "rent_paise": float64(9000000), "pan_masked": "XXXXXX234A"},
		map[string]any{"role": "lender", "position": float64(1), "name": "Synthetic Bank", "address": "2 Synthetic Bank Road", "pan_masked": "XXXXXX012C"},
	}, pending["parties"])
	require.NotContains(t, out, "pan_ciphertext")

	code, out = f.declare(f.staff, "admin", body)
	requireSalaryConflict(t, code, out, "proposal_pending", "", "", "")
	code, out = f.declare(f.enrol("DECL-02"), "admin", declBody(1, nil, nil))
	require.Equal(t, 409, code, "a first proposal starts at version 0: %v", out)
	code, out = f.declare(uuid.NewString(), "admin", declBody(0, nil, nil))
	require.Equal(t, 404, code, "an unknown staff member: %v", out)

	approve := map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "approve", "note": ""}
	code, out = f.decideDecl(f.staff, "admin", approve)
	require.Equal(t, 403, code, "the proposer cannot approve: %v", out)
	code, out = f.decideDecl(f.staff, "approver", with(approve, map[string]any{"expected_version": 1}))
	require.Equal(t, 409, code, "a stale version: %v", out)
	code, out = f.decideDecl(f.staff, "approver", with(approve, map[string]any{"decision": "reject", "note": " "}))
	require.Equal(t, 400, code, "a rejection needs a note: %v", out)
	code, out = f.decideDecl(f.staff, "approver", approve)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Nil(t, out["pending"])
	current := out["current"].(map[string]any)
	require.Equal(t, proposal, current["id"])
	require.Equal(t, "approved", current["status"])
	code, again := f.decideDecl(f.staff, "approver", approve)
	require.Equal(t, 200, code, "a replayed decision returns the result: %v", again)
	code, _ = f.decideDecl(f.staff, "auditor", approve)
	require.Equal(t, 409, code, "a decided proposal is not decided twice")

	code, out = f.call("GET", f.declPath(f.staff, "/history"), "reader", nil)
	require.Equal(t, 200, code, out)
	history := out["data"].([]any)
	require.Len(t, history, 1)
	require.Len(t, history[0].(map[string]any)["parties"], 2, "history carries the masked parties")

	var audits []string
	var events []json.RawMessage
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		if err := tx.Table("workforce_audit").Where("action LIKE 'workforce.tax_declaration.%'").Order("created_at").Pluck("action", &audits).Error; err != nil {
			return err
		}
		return tx.Table("outbox_events").Where("subject=?", workforce.TaxDeclarationChanged).Order("created_at").Pluck("payload", &events).Error
	}))
	require.Equal(t, []string{"workforce.tax_declaration.proposed", "workforce.tax_declaration.approved"}, audits)
	require.Len(t, events, 2)
	var last struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(events[1], &last))
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "staff_id": f.staff, "tax_year": declYear, "revision_id": proposal, "status": "approved", "version": float64(1)}, last.Data,
		"events carry no amounts, parties or PANs")
}

func TestTaxDeclarationRejectAndWithdrawErasePANs(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	body := declBody(0, []any{landlord("Synthetic Landlord", 9000000, set(synLandlordPAN))}, []any{lender("Synthetic Bank", set(synLenderPAN))})
	code, out := f.declare(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	rejected := out["pending"].(map[string]any)["id"].(string)
	for _, p := range f.parties(rejected) {
		require.NotNil(t, p["pan_ciphertext"])
	}
	code, out = f.decideDecl(f.staff, "approver", map[string]any{"proposal_id": rejected, "expected_version": 0, "decision": "reject", "note": "Synthetic receipt missing"})
	require.Equal(t, 200, code, out)
	for _, p := range f.parties(rejected) {
		require.Nil(t, p["pan_ciphertext"], "a rejection erases the PAN ciphertext")
		require.NotNil(t, p["pan_masked"], "the mask stays for history")
	}

	code, out = f.declare(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	withdrawn := out["pending"].(map[string]any)["id"].(string)
	code, out = f.call("POST", f.declPath(f.staff, "/withdraw"), "approver", map[string]any{"proposal_id": withdrawn, "note": ""})
	require.Equal(t, 403, code, "only the proposer withdraws: %v", out)
	code, out = f.call("POST", f.declPath(f.staff, "/withdraw"), "admin", map[string]any{"proposal_id": withdrawn, "note": ""})
	require.Equal(t, 200, code, out)
	for _, p := range f.parties(withdrawn) {
		require.Nil(t, p["pan_ciphertext"], "a withdrawal erases the PAN ciphertext")
	}
	code, out = f.revealParty(f.staff, "approver", withdrawn, "landlord", 1)
	require.Equal(t, 409, code, "a decided-against proposal is never revealed: %v", out)
}

func TestTaxDeclarationKeepCopiesTheInForcePAN(t *testing.T) {
	vault := vaultClient(t)
	f := newIdentifierFixture(t, vault)
	first := f.approvedDecl(f.staff, declBody(0, []any{landlord("Synthetic Landlord", 9000000, set(synLandlordPAN))}, []any{lender("Synthetic Bank", panNone())}))
	inForce := f.parties(first)[0]["pan_ciphertext"].(string)

	code, out := f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9000000, keep())}, []any{lender("Synthetic Bank", panNone())}))
	requireSalaryConflict(t, code, out, "nothing_to_change", "", "", "")
	code, out = f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9000000, keep())}, []any{lender("Synthetic Bank", keep())}))
	requireSalaryConflict(t, code, out, "nothing_to_keep", "", "", "")
	code, out = f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9000000, keep()), landlord("Synthetic Second", 1000000, keep())}, nil))
	requireSalaryConflict(t, code, out, "nothing_to_keep", "", "", "")

	code, out = f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9500000, keep())}, nil))
	require.Equal(t, 200, code, out)
	second := out["pending"].(map[string]any)["id"].(string)
	require.Equal(t, inForce, f.parties(second)[0]["pan_ciphertext"], "keep copies the ciphertext in force")
	code, out = f.revealParty(f.staff, "approver", second, "landlord", 1)
	require.Equal(t, 200, code, out)
	require.Equal(t, synLandlordPAN, out["value"])

	key := "helivanta-staff-" + testutil.TenantA
	context := func(role, position string) []byte {
		return []byte("helivanta/workforce/tax-declaration/v1|" + f.hospital + "|" + f.staff + "|" + declYear + "|" + role + "|" + position)
	}
	plain, err := vault.Decrypt(t.Context(), key, context("landlord", "1"), inForce)
	require.NoError(t, err)
	require.Equal(t, synLandlordPAN, string(plain))
	for _, moved := range [][]byte{context("landlord", "2"), context("lender", "1"), []byte(strings.Replace(string(context("landlord", "1")), declYear, "2027-28", 1))} {
		_, err = vault.Decrypt(t.Context(), key, moved, inForce)
		require.ErrorIs(t, err, transit.ErrIntegrity, "a ciphertext moved to %s fails authentication", moved)
	}
}

func TestTaxDeclarationLandlordPANFollowsTheRuleInForce(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	unknown := f.enrol("DECL-UNKNOWN")
	code, out := f.declare(unknown, "admin", declBody(0, []any{landlord("Synthetic Landlord", 100, panNone())}, nil))
	require.Equal(t, 400, code, "with no rule or election every landlord needs a PAN: %v", out)
	require.Equal(t, "landlord_pan_required", out["error"], out)

	rule := f.withLandlordRule(f.staff, 10000000)
	code, out = f.declare(f.staff, "admin", declBody(0, []any{landlord("Synthetic One", 6000000, panNone()), landlord("Synthetic Two", 6000000, panNone())}, nil))
	require.Equal(t, 400, code, "rent paid is the total across landlords: %v", out)
	require.Equal(t, "landlord_pan_required", out["error"], out)
	code, out = f.declare(f.staff, "admin", declBody(0, []any{landlord("Synthetic One", 5000000, panNone()), landlord("Synthetic Two", 5000000, panNone())}, nil))
	require.Equal(t, 200, code, "rent at the threshold needs no PAN: %v", out)
	proposal := out["pending"].(map[string]any)["id"]

	lower := incomeTaxRuleWithClaims("income_tax_new", 1)
	lower["effective_on"] = "2026-05-01"
	lower["income_tax"].(map[string]any)["claims"].([]any)[0].(map[string]any)["landlord_pan_rent_above_paise"] = 5000000
	f.approved(rule, lower)
	code, out = f.decideDecl(f.staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "approve", "note": ""})
	require.Equal(t, 400, code, "approval applies the rule in force at decision: %v", out)
	require.Equal(t, "landlord_pan_required", out["error"], out)
	code, out = f.call("GET", f.declPath(f.staff, ""), "reader", nil)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 0, out["version"], "the refused approval changed nothing")
	require.Equal(t, "pending", out["pending"].(map[string]any)["status"])
}

func TestTaxDeclarationRevealIsAuditedAndRestricted(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	approved := f.approvedDecl(f.staff, declBody(0, []any{landlord("Synthetic Landlord", 9000000, set(synLandlordPAN))}, []any{lender("Synthetic Bank", set(synLenderPAN))}))
	code, out := f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9000000, set(synOtherPAN))}, nil))
	require.Equal(t, 200, code, out)
	pending := out["pending"].(map[string]any)["id"].(string)

	code, out = f.revealParty(f.staff, "approver", approved, "lender", 1)
	require.Equal(t, 200, code, out)
	require.Equal(t, map[string]any{"revision_id": approved, "role": "lender", "position": float64(1), "value": synLenderPAN}, out)
	code, out = f.revealParty(f.staff, "approver", pending, "landlord", 1)
	require.Equal(t, 200, code, "a checker sees the proposed PAN: %v", out)
	require.Equal(t, synOtherPAN, out["value"])
	code, out = f.revealParty(f.staff, "admin", pending, "landlord", 1)
	require.Equal(t, 403, code, "the proposer cannot check their own values: %v", out)
	code, out = f.revealParty(f.staff, "viewer", approved, "landlord", 1)
	require.Equal(t, 403, code, "reveal needs its own permission: %v", out)
	code, out = f.revealParty(f.staff, "approver", pending, "lender", 1)
	require.Equal(t, 404, code, "a party the revision does not name: %v", out)
	code, out = f.call("POST", f.declPath(f.staff, "/reveal"), "approver", map[string]any{"revision_id": approved, "role": "landlord", "position": 1, "reason": " "})
	require.Equal(t, 400, code, "a reveal needs a reason: %v", out)
	code, raw := f.do("POST", f.base+"/hr/payroll"+f.declPath(f.staff, "/reveal"), "approver", `{"revision_id":"`+approved+`","role":"landlord","position":1,"reason":"Synthetic"}`)
	require.Equal(t, 200, code, raw)
	w := testutil.Do(f.router, "POST", f.base+"/hr/payroll"+f.declPath(f.staff, "/reveal"), "approver", `{"revision_id":"`+approved+`","role":"landlord","position":1,"reason":"Synthetic"}`)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var audits []string
	var events []json.RawMessage
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		if err := tx.Table("workforce_audit").Where("action=?", "workforce.tax_declaration.party_revealed").Order("created_at").Pluck("changes::text", &audits).Error; err != nil {
			return err
		}
		return tx.Table("outbox_events").Where("subject=?", workforce.TaxDeclarationPartyRevealed).Order("created_at").Pluck("payload", &events).Error
	}))
	require.Len(t, audits, 4, "every reveal and only a reveal is audited")
	require.Contains(t, audits[0], "Synthetic Form 124 check")
	require.Len(t, events, 4)
	var first struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(events[0], &first))
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "staff_id": f.staff, "tax_year": declYear, "revision_id": approved, "role": "lender", "position": float64(1)}, first.Data)
}

func TestTaxDeclarationsStoreNoPlaintextPAN(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	approved := f.approvedDecl(f.staff, declBody(0, []any{landlord("Synthetic Landlord", 9000000, set(synLandlordPAN))}, []any{lender("Synthetic Bank", set(synLenderPAN))}))
	code, out := f.declare(f.staff, "admin", declBody(1, []any{landlord("Synthetic Landlord", 9000000, set(synOtherPAN))}, nil))
	require.Equal(t, 200, code, out)
	code, out = f.revealParty(f.staff, "approver", approved, "landlord", 1)
	require.Equal(t, 200, code, out)
	require.NoError(t, f.asManager(func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_staff_tax_declarations", "workforce_staff_tax_declaration_revisions", "workforce_staff_tax_declaration_claims", "workforce_staff_tax_declaration_parties", "workforce_audit", "outbox_events"} {
			var rows []string
			require.NoError(t, tx.Raw("SELECT to_jsonb(t)::text FROM "+table+" t").Scan(&rows).Error)
			require.NotEmpty(t, rows, table)
			for _, row := range rows {
				for _, v := range []string{synLandlordPAN, synOtherPAN, synLenderPAN} {
					require.NotContains(t, row, v, "%s stores %s in the clear", table, v)
				}
			}
		}
		return nil
	}))
}

func TestTaxDeclarationsFailClosedWithoutTheVault(t *testing.T) {
	for name, vault := range map[string]*transit.Client{"unconfigured": nil, "down": downVault(t)} {
		t.Run(name, func(t *testing.T) {
			f := newIdentifierFixture(t, vault)
			code, out := f.declare(f.staff, "admin", declBody(0, nil, []any{lender("Synthetic Bank", set(synLenderPAN))}))
			require.Equal(t, 503, code, out)
			require.Equal(t, "identifier_vault_unavailable", out["error"], out)
			// Seeded directly, as no proposal can store a ciphertext while the vault is out.
			revision := uuid.NewString()
			require.NoError(t, f.payrollWrite(
				`INSERT INTO workforce_staff_tax_declarations(tenant_id,hospital_id,staff_id,tax_year,version) VALUES('`+testutil.TenantA+`','`+f.hospital+`','`+f.staff+`','`+declYear+`',0)`,
				`INSERT INTO workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,id,staff_id,tax_year,sequence,base_version,status,reason,home_loan_interest_paise,proposed_by_subject,decision_note) VALUES('`+testutil.TenantA+`','`+f.hospital+`','`+revision+`','`+f.staff+`','`+declYear+`',1,0,'pending','Synthetic',1,'user-admin','')`,
				`INSERT INTO workforce_staff_tax_declaration_parties(tenant_id,hospital_id,revision_id,role,position,name,address,pan_ciphertext,pan_masked) VALUES('`+testutil.TenantA+`','`+f.hospital+`','`+revision+`','lender',1,'Synthetic Bank','Synthetic','vault:v1:AAAA','XXXXXX012C')`,
			))
			code, out = f.call("GET", f.declPath(f.staff, ""), "reader", nil)
			require.Equal(t, 200, code, "masked reads still work: %v", out)
			require.Equal(t, "XXXXXX012C", out["pending"].(map[string]any)["parties"].([]any)[0].(map[string]any)["pan_masked"])
			code, out = f.revealParty(f.staff, "approver", revision, "lender", 1)
			require.Equal(t, 503, code, out)
			require.Equal(t, "identifier_vault_unavailable", out["error"], out)
		})
	}
}

func TestTaxDeclarationsStayInTheirHospitalAndTenant(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	approved := f.approvedDecl(f.staff, declBody(0, nil, nil))
	other := strings.Replace(f.base, f.hospital, strings.TrimPrefix(f.other, "/v1/workforce/hospitals/"), 1)
	for _, user := range []string{"admin", "foreign"} {
		code, raw := f.do("GET", other+"/hr/payroll"+f.declPath(f.staff, ""), user, "")
		require.Equal(t, 404, code, "%s in another hospital: %s", user, raw)
	}
	code, out := f.call("GET", f.declPath(f.staff, ""), "foreign", nil)
	require.Equal(t, 404, code, "another tenant: %v", out)
	code, out = f.revealParty(f.staff, "foreign", approved, "landlord", 1)
	require.Equal(t, 404, code, "another tenant: %v", out)
	code, raw := f.do("GET", f.base+"/hr/payroll/staff/"+f.staff+"/tax-declarations/2026-28", "reader", "")
	require.Equal(t, 404, code, "a malformed year: %s", raw)

	inactive := f.enrol("DECL-LEFT")
	status, raw := f.do("PUT", f.base+"/staff/"+inactive, "admin", `{"employee_code":"DECL-LEFT","display_name":"Synthetic DECL-LEFT","job_category":"nurse","employment_status":"inactive"}`)
	require.Equal(t, 200, status, raw)
	code, out = f.declare(inactive, "admin", declBody(0, nil, nil))
	requireSalaryConflict(t, code, out, "staff_inactive", "", "", "")
}

func TestTaxDeclarationsValidateEveryField(t *testing.T) {
	f := newIdentifierFixture(t, nil)
	five := []any{}
	for range 5 {
		five = append(five, lender("Synthetic Bank", panNone()))
	}
	claims := func(c ...any) map[string]any { return with(declBody(0, nil, nil), map[string]any{"claims": c}) }
	for name, body := range map[string]map[string]any{
		"a blank reason":            with(declBody(0, nil, nil), map[string]any{"reason": " "}),
		"a duplicate claim code":    claims(map[string]any{"code": "A", "amount_paise": 1}, map[string]any{"code": " a ", "amount_paise": 2}),
		"a malformed claim code":    claims(map[string]any{"code": "A B", "amount_paise": 1}),
		"a zero claim":              claims(map[string]any{"code": "A", "amount_paise": 0}),
		"an HRA with no landlord":   with(declBody(0, nil, nil), map[string]any{"hra": map[string]any{"metro": true, "landlords": []any{}}}),
		"five lenders":              declBody(0, nil, five),
		"a blank landlord name":     declBody(0, []any{landlord(" ", 1, set(synLandlordPAN))}, nil),
		"a zero rent":               declBody(0, []any{landlord("Synthetic", 0, set(synLandlordPAN))}, nil),
		"a malformed PAN":           declBody(0, nil, []any{lender("Synthetic Bank", set("SYN123"))}),
		"a value on keep":           declBody(0, nil, []any{lender("Synthetic Bank", map[string]any{"action": "keep", "value": synLenderPAN})}),
		"a set without a value":     declBody(0, nil, []any{lender("Synthetic Bank", map[string]any{"action": "set"})}),
		"an unknown PAN action":     declBody(0, nil, []any{lender("Synthetic Bank", map[string]any{"action": "clear"})}),
		"a loan without interest":   with(declBody(0, nil, nil), map[string]any{"home_loan": map[string]any{"lenders": []any{lender("Synthetic Bank", panNone())}}}),
		"an unknown field":          with(declBody(0, nil, nil), map[string]any{"regime": "new"}),
		"a landlord without rent":   with(declBody(0, nil, nil), map[string]any{"hra": map[string]any{"metro": true, "landlords": []any{lender("Synthetic", panNone())}}}),
		"an HRA without city class": with(declBody(0, nil, nil), map[string]any{"hra": map[string]any{"landlords": []any{landlord("Synthetic", 1, set(synLandlordPAN))}}}),
	} {
		code, out := f.declare(f.staff, "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out := f.call("GET", f.declPath(f.staff, ""), "reader", nil)
	require.Equal(t, 404, code, "nothing invalid was stored: %v", out)
}

func TestTaxDeclarationDatabaseGuards(t *testing.T) {
	f := newIdentifierFixture(t, vaultClient(t))
	approved := f.approvedDecl(f.staff, declBody(0, []any{landlord("Synthetic Landlord", 9000000, set(synLandlordPAN))}, nil))
	code, out := f.declare(f.staff, "admin", declBody(1, nil, []any{lender("Synthetic Bank", set(synLenderPAN))}))
	require.Equal(t, 200, code, out)
	pending := out["pending"].(map[string]any)["id"].(string)
	staff := "'" + f.staff + "'"
	revision := func(id string, metro, interest string) string {
		return `INSERT INTO workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,id,staff_id,tax_year,sequence,base_version,status,reason,hra_metro,home_loan_interest_paise,proposed_by_subject,decision_note)
 SELECT tenant_id,hospital_id,'` + id + `',staff_id,tax_year,99,version,'pending','Synthetic',` + metro + `,` + interest + `,'user-admin','' FROM workforce_staff_tax_declarations WHERE staff_id=` + staff
	}
	party := func(rev, role string, position int, rent, ciphertext, mask string) string {
		return `INSERT INTO workforce_staff_tax_declaration_parties(tenant_id,hospital_id,revision_id,role,position,name,address,rent_paise,pan_ciphertext,pan_masked)
 SELECT tenant_id,hospital_id,id,'` + role + `',` + string(rune('0'+position)) + `,'Synthetic','Synthetic',` + rent + `,` + ciphertext + `,` + mask + ` FROM workforce_staff_tax_declaration_revisions WHERE id='` + rev + `'`
	}
	withdraw := `UPDATE workforce_staff_tax_declaration_revisions SET status='withdrawn',decided_by_subject='user-admin',decided_at=now() WHERE id='` + pending + `'`
	fresh := uuid.NewString()
	for name, tc := range map[string]struct {
		statements []string
		guard      string
	}{
		"a second pending proposal":                {[]string{revision(uuid.NewString(), "NULL", "NULL")}, "workforce_tax_declaration_one_pending"},
		"a line added to an approved revision":     {[]string{`INSERT INTO workforce_staff_tax_declaration_claims(tenant_id,hospital_id,revision_id,position,code,amount_paise) SELECT tenant_id,hospital_id,id,9,'LATE',1 FROM workforce_staff_tax_declaration_revisions WHERE id='` + approved + `'`}, "written only with their pending proposal"},
		"a line added to a pending revision later": {[]string{party(pending, "lender", 2, "NULL", "NULL", "NULL")}, "written only with their pending proposal"},
		"claims are immutable":                     {[]string{`UPDATE workforce_staff_tax_declaration_claims SET amount_paise=amount_paise+1 WHERE revision_id='` + approved + `'`}, "claims are immutable"},
		"a party is never rewritten":               {[]string{`UPDATE workforce_staff_tax_declaration_parties SET name='Changed' WHERE revision_id='` + approved + `'`}, "only loses its PAN ciphertext"},
		"an in-force PAN is never erased":          {[]string{`UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + approved + `'`}, "only loses its PAN ciphertext"},
		"a withdrawal that keeps ciphertext":       {[]string{withdraw}, "workforce_tax_declaration_erased"},
		"revision content is immutable":            {[]string{`UPDATE workforce_staff_tax_declaration_revisions SET reason='Changed' WHERE id='` + approved + `'`}, "decided once"},
		"a self approval":                          {[]string{`UPDATE workforce_staff_tax_declaration_revisions SET status='approved',approved_version=2,decided_by_subject='user-admin',decided_at=now() WHERE id='` + pending + `'`}, "workforce_tax_declaration_separation"},
		"an approval not in force":                 {[]string{`UPDATE workforce_staff_tax_declaration_revisions SET status='approved',approved_version=2,decided_by_subject='user-approver',decided_at=now() WHERE id='` + pending + `'`}, "must become the one in force"},
		"a version skip":                           {[]string{`UPDATE workforce_staff_tax_declarations SET version=3 WHERE staff_id=` + staff}, "advances one approved version"},
		"declarations are never deleted":           {[]string{`DELETE FROM workforce_staff_tax_declaration_parties WHERE revision_id='` + approved + `'`}, "destructive workforce operation"},
		"a landlord without rent":                  {[]string{withdraw + `;UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + pending + `'`, revision(fresh, "true", "NULL"), party(fresh, "landlord", 1, "NULL", "NULL", "NULL")}, "workforce_tax_declaration_party_rent"},
		"a mask without ciphertext":                {[]string{withdraw + `;UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + pending + `'`, revision(fresh, "NULL", "1"), party(fresh, "lender", 1, "NULL", "NULL", "'XXXXXX234A'")}, "workforce_tax_declaration_party_sealed"},
		"an HRA claim without landlords":           {[]string{withdraw + `;UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + pending + `'`, revision(fresh, "true", "NULL")}, "workforce_tax_declaration_shape"},
		"lenders without a home loan claim":        {[]string{withdraw + `;UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + pending + `'`, revision(fresh, "NULL", "NULL"), party(fresh, "lender", 1, "NULL", "NULL", "NULL")}, "workforce_tax_declaration_shape"},
		"a landlord without PAN and no rule":       {[]string{withdraw + `;UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='` + pending + `'`, revision(fresh, "true", "NULL"), party(fresh, "landlord", 1, "100", "NULL", "NULL")}, "workforce_tax_declaration_landlord_pan_required"},
	} {
		requireGuard(t, f.payrollWrite(tc.statements...), tc.guard, name)
	}
	require.NoError(t, f.payrollWrite(withdraw, `UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE revision_id='`+pending+`'`), "the legal withdrawal succeeds under the same session")
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_staff_tax_declarations", "workforce_staff_tax_declaration_revisions", "workforce_staff_tax_declaration_claims", "workforce_staff_tax_declaration_parties"} {
			var n int64
			require.NoError(t, tx.Table(table).Count(&n).Error)
			require.Zero(t, n, "forced RLS without hospital scope: %s", table)
		}
		return nil
	}))
	ungated := f.enrol("DECL-GATE")
	insert := `INSERT INTO workforce_staff_tax_declarations(tenant_id,hospital_id,staff_id,tax_year,version) VALUES('` + testutil.TenantA + `','` + f.hospital + `','` + ungated + `','` + declYear + `',0)`
	require.ErrorContains(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(insert).Error
	}), "row-level security", "writes need the payroll write gate")
	require.NoError(t, f.payrollWrite(insert), "the same write passes the gate")
}
