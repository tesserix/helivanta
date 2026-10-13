import assert from "node:assert/strict";
import { test } from "node:test";

import { createHMSFixtures } from "./hms-fixtures.mjs";

test("synthetic fixtures preserve tenant and hospital relationships without creating access", () => {
  const data = createHMSFixtures();
  assert.equal(data.classification, "synthetic");
  assert.equal(data.tenants.length, 2);
  assert.equal(data.tenants[0].name, "YES HOSPITAL");
  assert.equal(data.hospitals.filter((h) => h.tenant_id === data.tenants[0].id).length, 2);
  assert.equal(new Set(data.staff.map((s) => s.id)).size, data.staff.length);
  assert.deepEqual(data.access_grants, []);
  assert.deepEqual(data.sharing_grants, []);
  for (const staff of data.staff) {
    assert.equal(staff.identity_subject, null);
    assert.match(staff.email, /@example\.invalid$/);
    assert.equal(staff.phone, null);
  }
  for (const assignment of data.staff_assignments) {
    const hospital = data.hospitals.find((h) => h.id === assignment.hospital_id);
    const staff = data.staff.find((s) => s.id === assignment.staff_id);
    assert.equal(hospital.tenant_id, assignment.tenant_id);
    assert.equal(staff.tenant_id, assignment.tenant_id);
  }
  for (const registration of data.patient_registrations) {
    const hospital = data.hospitals.find((h) => h.id === registration.hospital_id);
    const patient = data.patients.find((p) => p.id === registration.patient_id);
    assert.equal(hospital.tenant_id, registration.tenant_id);
    assert.equal(patient.tenant_id, registration.tenant_id);
    assert.match(registration.mrn, /^SYN-/);
  }
});
