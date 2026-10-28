package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/supply"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type deliveryJourneyMembership struct{}

func (deliveryJourneyMembership) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	if subject == "user-owner" {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleSupplyAdmin}}, nil
	}
	if subject == "user-driver" {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleSupplyDriver}}, nil
	}
	return nil, nil
}

func TestManualSupplyDeliveryHasNoPharmacyStockFinanceOrDispenseEffects(t *testing.T) {
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"owner": testutil.TenantA, "driver": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"owner": {
				supply.PermPartyRead, supply.PermPartyWrite, supply.PermOrganizationManage,
				supply.PermAccessManage, supply.PermPremiseWrite, supply.PermDeliveryRead, supply.PermDeliveryWrite,
				pharmacy.PermAccessManage, pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite,
				pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermAccountsRead,
				pharmacy.PermAccountsWrite, pharmacy.PermAccountsPayment, pharmacy.PermDispenseRead,
			},
			"driver": {supply.PermDriverRead, supply.PermDriverAction},
		},
		Roles: deliveryJourneyMembership{}, Modules: []platform.Module{supply.New(), pharmacy.New()},
	})
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	pharmacyID, outletID, product, location, batch, receipt, payable := newID(), newID(), newID(), newID(), newID(), newID(), newID()
	pharmacyRoot := "/v1/pharmacy/pharmacies/" + pharmacyID
	outletRoot := pharmacyRoot + "/outlets/" + outletID
	request := func(method, path, token, body string) string {
		t.Helper()
		w := testutil.Do(r, method, path, token, body)
		require.Equal(t, http.StatusOK, w.Code, "%s %s: %s", method, path, w.Body.String())
		return w.Body.String()
	}
	request("PUT", pharmacyRoot, "owner", `{"code":"TEST-SIDE-EFFECTS","name":"TEST No Side Effects Pharmacy","ownership_type":"independent"}`)
	request("PUT", outletRoot, "owner", `{"code":"TEST-OUTLET","name":"TEST Pharmacy Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	request("PUT", outletRoot+"/access/user-owner", "owner", `{"level":"editor"}`)
	request("PUT", outletRoot+"/catalogue/products/"+product, "owner", `{"expected_version":0,"sku":"TEST-LOGISTICS-DRUG","brand_name":"TEST Synthetic Medicine","generic_name":"TEST ingredient","dosage_form":"tablet","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"","ingredients":[{"name":"TEST ingredient","strength":"10","unit":"mg"}],"pack_size":10,"unit_of_issue":"tablet","currency":"INR","mrp_minor":12500,"status":"active"}`)
	request("PUT", outletRoot+"/stock/locations/"+location, "owner", `{"code":"TEST-QUARANTINE","name":"TEST Quarantine"}`)
	request("PUT", outletRoot+"/stock/batches/"+batch, "owner", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2035-12-31","mrp_minor":12500}`, product))
	receiptBody := fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":100,"reason":"TEST opening stock","source_reference":"TEST-SHARED-REFERENCE"}`, location, batch)
	request("PUT", outletRoot+"/stock/movements/"+receipt, "owner", receiptBody)
	payablePath := outletRoot + "/accounts/payables/" + payable
	request("PUT", payablePath, "owner", fmt.Sprintf(`{"payee_reference":"TEST-SUPPLIER","payee_name":"TEST Synthetic Supplier","document_reference":"TEST-SHARED-REFERENCE","issued_on":"2026-10-01","due_on":"2026-10-31","currency":"INR","lines":[{"receipt_id":%q,"category":"inventory","description":"TEST acquisition cost","net_minor":10000,"tax_minor":1800,"recoverable_tax_minor":1800,"landed_minor":0}]}`, receipt))
	request("PUT", payablePath+"/payments/"+newID(), "owner", `{"kind":"payment","amount_minor":3000,"currency":"INR","paid_by":"TEST Finance operator","method":"bank","source_reference":"TEST-EXTERNAL-PAYMENT","occurred_on":"2026-10-15","reason":"TEST external settlement evidence","reversal_of":null}`)
	visit := newID()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: testutil.TenantA,
			Data: json.RawMessage(fmt.Sprintf(`{"visit_id":%q,"patient_name":"TEST Synthetic Patient","department":"TEST OPD"}`, visit)),
		})
	}))
	require.Eventually(t, func() bool {
		w := testutil.Do(r, "GET", "/v1/pharmacy/dispenses", "owner", "")
		return w.Code == http.StatusOK && json.Valid(w.Body.Bytes()) && deliveryJourneyContainsVisit(w.Body.Bytes(), visit)
	}, 20*time.Second, 100*time.Millisecond, "real NATS visit intake must populate the pharmacy dispense table before the baseline")

	snapshot := func() map[string]string {
		t.Helper()
		state := deliveryJourneyPharmacySnapshot(t, db, pharmacyID, outletID)
		for _, path := range []string{
			outletRoot + "/stock/balance?location_id=" + location + "&batch_id=" + batch,
			payablePath, payablePath + "/settlements", outletRoot + "/accounts/inventory-audit",
		} {
			state[path] = request("GET", path, "owner", "")
		}
		return state
	}
	before := snapshot()
	for table, rows := range before {
		require.NotEqual(t, "[]", rows, "%s must contain a real fixture; empty RLS visibility would give a false positive", table)
	}
	var balance struct {
		OnHand int64 `json:"on_hand_units"`
	}
	require.NoError(t, json.Unmarshal([]byte(before[outletRoot+"/stock/balance?location_id="+location+"&batch_id="+batch]), &balance))
	require.EqualValues(t, 100, balance.OnHand)
	var debt struct {
		Total int64 `json:"total_minor"`
		Paid  int64 `json:"paid_minor"`
		Left  int64 `json:"outstanding_minor"`
	}
	require.NoError(t, json.Unmarshal([]byte(before[payablePath]), &debt))
	require.Equal(t, int64(11800), debt.Total)
	require.Equal(t, int64(3000), debt.Paid)
	require.Equal(t, int64(8800), debt.Left)

	delivered := make(chan events.Event, 8)
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name: "manual-delivery-side-effect-observer", Subject: supplycontract.SubjectDeliveryTaskChanged,
		Handle: func(_ context.Context, _ *gorm.DB, event events.Event) error {
			delivered <- event
			return nil
		},
	}}))
	party, premise, task := newID(), newID(), newID()
	supplyRoot := "/v1/supply/parties/" + party
	request("PUT", supplyRoot, "owner", `{"expected_version":0,"code":"TEST-LOGISTICS-OWNER","record_kind":"owned_organisation","legal_name":"TEST Logistics Organisation","trading_name":"TEST Logistics","business_kind":"distributor","country_code":"IN","jurisdiction":"TEST-MH","registration_number":"TEST-LOGISTICS-REG","corporate_group":"","website":"","default_credit_days":0,"contacts":[],"addresses":[],"tax_registrations":[]}`)
	request("PUT", supplyRoot+"/premises/"+premise, "owner", `{"expected_version":0,"code":"TEST-DISPATCH","name":"TEST Warehouse","kind":"warehouse","line1":"TEST Industrial Building","line2":"","city":"TEST City","region":"TEST Region","postal_code":"TEST-001","country_code":"IN","time_zone":"Asia/Kolkata","status":"active"}`)
	now := time.Now().UTC().Truncate(time.Second)
	request("PUT", supplyRoot+"/grants/"+newID(), "owner", fmt.Sprintf(`{"expected_version":0,"subject":"user-driver","role":"driver","premise_id":%q,"valid_from":%q,"expires_at":%q,"reason":"TEST authorised driver assignment","status":"active"}`, premise, now.Add(-time.Hour).Format(time.RFC3339), now.Add(3*time.Hour).Format(time.RFC3339)))
	taskPath := supplyRoot + "/premises/" + premise + "/delivery-tasks/" + task
	taskBody := fmt.Sprintf(`{"expected_version":0,"code":"TEST-DELIVERY","source_reference":"TEST-SHARED-REFERENCE","destination_business_name":"TEST No Side Effects Pharmacy","destination_line1":"TEST Business Road","destination_line2":"","destination_city":"TEST City","destination_region":"TEST Region","destination_postal_code":"TEST-001","country_code":"IN","business_contact_name":"TEST Business Receiver","business_contact_phone":"+919999999999","window_start":%q,"window_end":%q,"package_count":2,"handling_notes":"TEST sealed administrative packages","driver_subject":"user-driver","assignment_expires_at":%q,"status":"assigned","change_reason":"TEST authorised manual logistics scheduling"}`, now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339))
	first := request("PUT", taskPath, "owner", taskBody)
	require.JSONEq(t, first, request("PUT", taskPath, "owner", taskBody))
	require.Equal(t, before, snapshot(), "manual scheduling must not receive stock, settle an invoice or dispense medicine")
	myTask := supplyRoot + "/premises/" + premise + "/my-deliveries/" + task
	for i, action := range []string{"start", "deliver"} {
		command := myTask + "/actions/" + newID()
		body := fmt.Sprintf(`{"expected_version":%d,"expected_assignment_generation":1,"action":%q,"reason":"TEST synthetic package handoff"}`, i+1, action)
		response := request("PUT", command, "driver", body)
		require.JSONEq(t, response, request("PUT", command, "driver", body))
		require.Equal(t, before, snapshot(), "%s must not alter pharmacy-owned stock, finance or clinical state", action)
	}
	for version := int64(1); version <= 3; version++ {
		select {
		case event := <-delivered:
			require.Equal(t, testutil.TenantA, event.TenantID)
			require.Equal(t, supplycontract.SubjectDeliveryTaskChanged, event.Type)
			var data map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(event.Data, &data))
			require.Len(t, data, 5, "delivery events must contain only the versioned opaque reference contract")
			operation := []string{"control", "start", "deliver"}[version-1]
			require.JSONEq(t, fmt.Sprintf(`{"party_id":%q,"premise_id":%q,"delivery_id":%q,"version":%d,"operation":%q}`, party, premise, task, version, operation), string(event.Data))
		case <-ctx.Done():
			t.Fatal("delivery outbox did not reach the real durable NATS observer")
		}
	}
	require.Equal(t, before, snapshot(), "durable delivery event publication must leave pharmacy-owned tables unchanged")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var payloads []string
		if err := tx.Table("outbox_events").Where("subject=?", supplycontract.SubjectDeliveryTaskChanged).Pluck("payload::text", &payloads).Error; err != nil {
			return err
		}
		require.Len(t, payloads, 3, "retries must not duplicate delivery events")
		for _, payload := range payloads {
			for _, excluded := range []string{"destination", "business_contact", "driver_subject", "source_reference", "handling_notes", "TEST Synthetic Patient", "TEST No Side Effects Pharmacy", "TEST-SHARED-REFERENCE"} {
				require.NotContains(t, payload, excluded)
			}
		}
		return nil
	}))
}

func deliveryJourneyContainsVisit(body []byte, visit string) bool {
	var page struct {
		Data []struct {
			VisitID string `json:"visit_id"`
		}
	}
	if json.Unmarshal(body, &page) != nil {
		return false
	}
	for _, row := range page.Data {
		if row.VisitID == visit {
			return true
		}
	}
	return false
}

func deliveryJourneyPharmacySnapshot(t *testing.T, db *tenantdb.DB, pharmacy, outlet string) map[string]string {
	t.Helper()
	rows := make(map[string]string)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-owner',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, outlet, pharmacy).Error; err != nil {
			return err
		}
		for _, table := range []string{"pharmacy_catalogue_products", "pharmacy_catalogue_revisions", "pharmacy_stock_locations", "pharmacy_stock_batches", "pharmacy_stock_movements", "pharmacy_payables", "pharmacy_payable_lines", "pharmacy_settlements", "pharmacy_dispenses"} {
			query := "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.id),'[]'::jsonb)::text FROM " + table + " r WHERE r.tenant_id=?"
			args := []any{testutil.TenantA}
			if table != "pharmacy_dispenses" {
				query += " AND r.outlet_id=?"
				args = append(args, outlet)
			}
			var data string
			if err := tx.Raw(query, args...).Scan(&data).Error; err != nil {
				return err
			}
			rows[table] = data
		}
		return nil
	}))
	return rows
}
