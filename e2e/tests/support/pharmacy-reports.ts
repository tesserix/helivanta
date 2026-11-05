import { randomUUID } from "node:crypto";
import { expect, type Page } from "@playwright/test";
import { owner, temporaryRole } from "./pharmacy-counter";
import { uuidv7 } from "./pharmacy";

type Actor = { tenant_id: string; subject: string };

/**
 * A patient of a hospital that has verified the outlet's pharmacy, findable
 * by the cashier at the counter (#1416, #1416b). Owner commands only.
 *
 * A requester cannot accept their own request, and only tenant
 * administrators hold the review permission. So the cashier, as pharmacy
 * owner just long enough, requests the verification, and the admin accepts
 * it for the hospital.
 */
export async function affiliatedPatient(admin: Page, cashier: Page, outlet: string) {
  const administrator = await owner<Actor>(admin, "GET", "/iam/me/permissions");
  const me = await owner<Actor>(cashier, "GET", "/iam/me/permissions");
  const pharmacy = outlet.split("/").at(-3)!;
  const hospitalId = uuidv7();
  await owner(admin, "PUT", `/workforce/hospitals/${hospitalId}`, {
    name: `TEST Reports Hospital ${randomUUID().slice(0, 8)}`,
  });
  const registry = `/patients/hospitals/${hospitalId}/access`;
  await owner(admin, "PUT", `${registry}/${encodeURIComponent(administrator.subject)}`, {
    level: "registrar",
  });
  const name = `TEST Report Patient ${randomUUID().slice(0, 8)}`;
  const { patient } = await owner<{ patient: { id: string; mrn: string } }>(
    admin,
    "POST",
    `/patients?hospital_id=${hospitalId}`,
    {
      request_id: randomUUID(),
      given_name: name,
      family_name: `SYNTHETIC-${randomUUID()}`,
      dob: "1990-01-01",
      sex: "other",
      notice_version: "TEST-E2E-1060",
      consented_by: "patient",
    },
  );

  const affiliation = uuidv7();
  const links = `/pharmacy/pharmacies/${pharmacy}/hospital-affiliations`;
  await owner(admin, "PUT", `${links}/${affiliation}`, {
    hospital_id: hospitalId,
    relationship: "serving",
  });
  const round = uuidv7();
  const endOwner = await temporaryRole(
    admin,
    administrator,
    cashier,
    "pharmacy_owner",
    "pharmacy.access.manage",
  );
  try {
    await owner(cashier, "PUT", `${links}/${affiliation}/verification-requests/${round}`, {});
  } finally {
    await endOwner("TEST end temporary affiliation requester role");
  }
  await expect
    .poll(
      async () => {
        const queue = await owner<{ data: { request_id: string }[] }>(
          admin,
          "GET",
          `/patients/pharmacy-affiliations?hospital_id=${hospitalId}`,
        );
        return queue.data.some((r) => r.request_id === round);
      },
      { timeout: 30_000, message: "the verification request reaches the hospital" },
    )
    .toBe(true);
  await owner(
    admin,
    "PUT",
    `/patients/pharmacy-affiliations/${round}/decision/${uuidv7()}?hospital_id=${hospitalId}`,
    { outcome: "accepted", reason: "TEST synthetic hospital accepts the pharmacy" },
  );
  await expect
    .poll(
      async () => {
        const ours = await owner<{ data: { id: string; verification_status: string }[] }>(
          admin,
          "GET",
          links,
        );
        return ours.data.find((l) => l.id === affiliation)?.verification_status;
      },
      { timeout: 30_000, message: "the hospital's acceptance reaches the pharmacy" },
    )
    .toBe("verified");
  const grant = `/patients/hospitals/${hospitalId}/pharmacy-lookup-access/${pharmacy}/${encodeURIComponent(me.subject)}`;
  // The link is active only once pharmacy confirms it applied the acceptance.
  await expect
    .poll(async () => (await admin.request.put(`/api/v1${grant}`, { data: {} })).status(), {
      timeout: 30_000,
      message: "the link is active on both sides",
    })
    .toBe(200);
  return { patientId: patient.id, mrn: patient.mrn, hospitalId, name };
}
